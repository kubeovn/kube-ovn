package daemon

import (
	"strings"
	"sync"

	"github.com/kubeovn/gonetworkmanager/v2"
	"github.com/scylladb/go-set/strset"
	"github.com/vishvananda/netlink"
	"golang.org/x/mod/semver"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

type networkManagerSyncer struct {
	manager   gonetworkmanager.NetworkManager
	workqueue workqueue.Interface
	devices   *strset.Set
	bridgeMap map[string]string
	lock      sync.Mutex
}

func newNetworkManagerSyncer() *networkManagerSyncer {
	syncer := &networkManagerSyncer{}

	manager, err := gonetworkmanager.NewNetworkManager()
	if err != nil {
		klog.V(3).Infof("failed to connect to NetworkManager: %v", err)
		return syncer
	}

	running, err := manager.Running()
	if err != nil {
		klog.V(3).Infof("failed to check NetworkManager running state: %v", err)
		return syncer
	}
	if !running {
		klog.V(3).Info("NetworkManager is not running, ignore")
		return syncer
	}

	syncer.manager = manager
	syncer.workqueue = workqueue.NewNamed("NetworkManagerSyncer")
	syncer.devices = strset.New()
	syncer.bridgeMap = make(map[string]string)
	return syncer
}

func (n *networkManagerSyncer) Run(handler func(nic, bridge string, delNonExistent bool) (int, error)) {
	if n.manager == nil {
		return
	}

	go func() {
		for n.ProcessNextItem(handler) {
		}
	}()

	go func() {
		ch := n.manager.Subscribe()
		defer n.manager.Unsubscribe()

		suffix := "." + gonetworkmanager.ActiveConnectionSignalStateChanged
		for {
			event := <-ch
			if len(event.Body) == 0 || !strings.HasSuffix(event.Name, suffix) {
				continue
			}
			state, ok := event.Body[0].(uint32)
			if !ok {
				klog.Warningf("failed to convert %#v to uint32", event.Body[0])
				continue
			}
			if gonetworkmanager.NmDeviceState(state) != gonetworkmanager.NmDeviceStateActivated {
				continue
			}

			devices, err := n.manager.GetDevices()
			if err != nil {
				klog.Errorf("failed to get NetworkManager devices: %v", err)
				continue
			}

			var device gonetworkmanager.Device
			for _, dev := range devices {
				if dev.GetPath() == event.Path {
					device = dev
					break
				}
			}
			if device == nil {
				klog.Warningf("NetworkManager device %s not found", event.Path)
				continue
			}

			name, err := device.GetPropertyIpInterface()
			if err != nil {
				klog.Errorf("failed to get IP interface of device %s: %v", device.GetPath(), err)
				continue
			}

			n.lock.Lock()
			if !n.devices.Has(name) {
				n.lock.Unlock()
				continue
			}
			n.lock.Unlock()

			klog.Infof("adding device %s to workqueue", name)
			n.workqueue.Add(name)
		}
	}()
}

func (n *networkManagerSyncer) ProcessNextItem(handler func(nic, bridge string, delNonExistent bool) (int, error)) bool {
	item, shutdown := n.workqueue.Get()
	if shutdown {
		return false
	}
	defer n.workqueue.Done(item)

	klog.Infof("process device %v", item)

	nic := item.(string)
	var bridge string
	n.lock.Lock()
	if !n.devices.Has(nic) {
		n.lock.Unlock()
		return true
	}
	bridge = n.bridgeMap[nic]
	n.lock.Unlock()

	if _, err := handler(nic, bridge, true); err != nil {
		klog.Errorf("failed to handle NetworkManager event for device %s with bridge %s: %v", nic, bridge, err)
	}

	return true
}

func (n *networkManagerSyncer) AddDevice(nicName, bridge string) error {
	if n.manager == nil {
		return nil
	}

	n.lock.Lock()
	klog.V(3).Infof("adding device %s with bridge %s", nicName, bridge)
	n.devices.Add(nicName)
	n.bridgeMap[nicName] = bridge
	n.lock.Unlock()

	return nil
}

func (n *networkManagerSyncer) RemoveDevice(nicName string) error {
	if n.manager == nil {
		return nil
	}

	n.lock.Lock()
	n.devices.Remove(nicName)
	delete(n.bridgeMap, nicName)
	n.lock.Unlock()

	return nil
}

func (n *networkManagerSyncer) SetManaged(name string, managed bool) error {
	if n.manager == nil {
		return nil
	}

	link, err := netlink.LinkByName(name)
	if err != nil {
		klog.Errorf("failed to get link %q: %v", name, err)
		return err
	}

	device, err := n.manager.GetDeviceByIpIface(name)
	if err != nil {
		klog.Errorf("failed to get device by IP iface %q: %v", name, err)
		return err
	}
	current, err := device.GetPropertyManaged()
	if err != nil {
		klog.Errorf("failed to get device property managed: %v", err)
		return err
	}
	if current == managed {
		return nil
	}

	if !managed {
		links, err := netlink.LinkList()
		if err != nil {
			klog.Errorf("failed to list network links: %v", err)
			return err
		}

		for _, l := range links {
			if l.Attrs().ParentIndex != link.Attrs().Index || l.Type() != "vlan" {
				continue
			}

			d, err := n.manager.GetDeviceByIpIface(l.Attrs().Name)
			if err != nil {
				klog.Errorf("failed to get device by IP iface %q: %v", l.Attrs().Name, err)
				return err
			}
			vlanManaged, err := d.GetPropertyManaged()
			if err != nil {
				klog.Errorf("failed to get property managed of device %s: %v", l.Attrs().Name, err)
				continue
			}
			if vlanManaged {
				klog.Infof(`will not set device %s NetworkManager property "managed" to %v`, name, managed)
				return nil
			}
		}

		version, err := n.manager.GetPropertyVersion()
		if err != nil {
			klog.Errorf("failed to get NetworkManager version: %v", err)
			return err
		}

		if !strings.HasPrefix(version, "v") {
			version = "v" + version
		}

		if semver.Compare(version, "v1.6.0") < 0 {
			klog.Infof("NetworkManager version %s is less than v1.6.0")
			return nil
		}

		// requires NetworkManager >= v1.6
		dnsManager, err := gonetworkmanager.NewDnsManager()
		if err != nil {
			klog.Errorf("failed to initialize NetworkManager DNS manager: %v", err)
			return err
		}

		configurations, err := dnsManager.GetPropertyConfiguration()
		if err != nil {
			klog.Errorf("failed to get NetworkManager DNS configuration: %v", err)
			return err
		}

		for _, c := range configurations {
			if c.Interface == name {
				if len(c.Nameservers) != 0 {
					klog.Infof("DNS servers %s are configured on interface %s", strings.Join(c.Nameservers, ","), name)
					if semver.MajorMinor(version) == "v1.18" {
						klog.Infof("NetworkManager's version is v1.18.x")
						return nil
					}
				}
				break
			}
		}
	}

	klog.Infof(`setting device %s NetworkManager property "managed" to %v`, name, managed)
	if err = device.SetPropertyManaged(managed); err != nil {
		klog.Errorf("failed to set device property managed to %v: %v", managed, err)
		return err
	}

	return nil
}
