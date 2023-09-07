package daemon

import (
	"strings"
	"sync"

	"github.com/kubeovn/gonetworkmanager/v2"
	"github.com/scylladb/go-set/strset"
	"github.com/vishvananda/netlink"
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
		devices, err := n.manager.GetAllDevices()
		if err != nil {
			klog.Errorf("failed to get all devices from NetworkManager: %v", err)
			return err
		}

		var hasVlan bool
		for _, dev := range devices {
			managed, err := device.GetPropertyManaged()
			if err != nil {
				klog.Errorf("failed to get property managed of device %s: %v", dev.GetPath(), err)
				continue
			}
			if !managed {
				continue
			}

			devType, err := dev.GetPropertyDeviceType()
			if err != nil {
				klog.Errorf("failed to get type of device %s: %v", dev.GetPath(), err)
				continue
			}
			if devType != gonetworkmanager.NmDeviceTypeVlan {
				continue
			}

			vlanName, err := dev.GetPropertyIpInterface()
			if err != nil {
				klog.Errorf("failed to get IP interface of device %s: %v", dev.GetPath(), err)
				continue
			}

			vlanLink, err := netlink.LinkByName(vlanName)
			if err != nil {
				klog.Errorf("failed to get link %s: %v", vlanName, err)
				continue
			}
			if vlanLink.Type() != "vlan" {
				klog.Errorf("unexpected link type: %s", vlanLink.Type())
				continue
			}

			if vlanLink.Attrs().ParentIndex == link.Attrs().Index {
				klog.Infof("device %s has a vlan interface %s managed by NetworkManager", name, vlanName)
				hasVlan = true
				break
			}
		}

		if hasVlan {
			klog.Infof(`will not set device %s NetworkManager property "managed" to %v`, name, managed)
			return nil
		}
	}

	klog.Infof(`setting device %s NetworkManager property "managed" to %v`, name, managed)
	if err = device.SetPropertyManaged(managed); err != nil {
		klog.Errorf("failed to set device property managed to %v: %v", managed, err)
		return err
	}

	return nil
}
