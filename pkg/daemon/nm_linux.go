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

const (
	nmIP4ConfigInterfacePropertiesChanged = gonetworkmanager.IP4ConfigInterface + ".PropertiesChanged"
	nmIP6ConfigInterfacePropertiesChanged = gonetworkmanager.IP6ConfigInterface + ".PropertiesChanged"

	nmDBusObjectPathIPConfig4 = gonetworkmanager.NetworkManagerObjectPath + "/IP4Config/"
	nmDBusObjectPathIPConfig6 = gonetworkmanager.NetworkManagerObjectPath + "/IP6Config/"
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

		for {
			event := <-ch
			if event.Name != nmIP4ConfigInterfacePropertiesChanged &&
				event.Name != nmIP6ConfigInterfacePropertiesChanged {
				continue
			}
			path := string(event.Path)
			if !strings.HasPrefix(path, nmDBusObjectPathIPConfig4) &&
				!strings.HasPrefix(path, nmDBusObjectPathIPConfig6) {
				continue
			}

			devices, err := n.manager.GetDevices()
			if err != nil {
				klog.Errorf("failed to get NetworkManager devices: %v", err)
				continue
			}

			var device gonetworkmanager.Device
			for _, dev := range devices {
				if event.Name == nmIP4ConfigInterfacePropertiesChanged {
					config, err := dev.GetPropertyIP4Config()
					if err != nil {
						klog.Errorf("failed to get ipv4 config of device %s: %v", dev.GetPath(), err)
						break
					}
					if config != nil && config.Path() == event.Path {
						device = dev
						break
					}
				} else {
					config, err := dev.GetPropertyIP6Config()
					if err != nil {
						klog.Errorf("failed to get ipv6 config of device %s: %v", dev.GetPath(), err)
						break
					}
					if config != nil && config.Path() == event.Path {
						device = dev
						break
					}
				}
			}
			if device == nil {
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

			klog.Infof("ip address change detected on device %q, adding to workqueue", name)
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

func (n *networkManagerSyncer) RemoveDevice(nicName string) {
	if n.manager == nil {
		return
	}

	n.lock.Lock()
	n.devices.Remove(nicName)
	delete(n.bridgeMap, nicName)
	n.lock.Unlock()
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
				// After setting device managed=no, the vlan interface will be set down by NetworkManager.
				klog.Infof(`device %q has a vlan interface %q mannaged by NetworkManager, will not set the NetworkManager property "managed" to %v`, name, l.Attrs().Name, managed)
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

		// Retrieving DNS configuration requires NetworkManager >= v1.6.0.
		// Do not set device managed=no if the version is < v1.6.0.
		if semver.Compare(version, "v1.6.0") < 0 {
			klog.Infof("NetworkManager version %s is less than v1.6.0", version)
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
					// After setting device managed=no on CentOS 7 with NetworkManager v1.18.x,
					// the DNS servers in /etc/resolv.conf configured on the device will be removed.
					// We don't want to change the host DNS configuration, so skip this operation.
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
