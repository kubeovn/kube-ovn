package daemon

import (
	"sync"

	"github.com/kubeovn/gonetworkmanager/v2"
	"github.com/scylladb/go-set/strset"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

type networkManagerSyncer struct {
	manager     gonetworkmanager.NetworkManager
	workqueue   workqueue.Interface
	devicePaths *strset.Set
	pathMap     map[string]string
	bridgeMap   map[string]string
	lock        sync.Mutex
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
	syncer.devicePaths = strset.New()
	syncer.pathMap = make(map[string]string)
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

		stateChange := gonetworkmanager.DeviceInterface + "." + gonetworkmanager.ActiveConnectionSignalStateChanged
		for {
			event := <-ch

			n.lock.Lock()
			if len(event.Body) == 0 || event.Name != stateChange || !n.devicePaths.Has(string(event.Path)) {
				n.lock.Unlock()
				continue
			}
			n.lock.Unlock()

			state, ok := event.Body[0].(uint32)
			if !ok {
				klog.Warningf("failed to convert %#v to uint32", event.Body[0])
				continue
			}
			if gonetworkmanager.NmDeviceState(state) != gonetworkmanager.NmDeviceStateActivated {
				continue
			}

			klog.Infof("adding dbus object path %s to workqueue", event.Path)
			n.workqueue.Add(string(event.Path))
		}
	}()
}

func (n *networkManagerSyncer) ProcessNextItem(handler func(nic, bridge string, delNonExistent bool) (int, error)) bool {
	item, shutdown := n.workqueue.Get()
	if shutdown {
		return false
	}
	defer n.workqueue.Done(item)

	klog.Infof("process dbus object path %v", item)
	path := item.(string)
	n.lock.Lock()
	if !n.devicePaths.Has(path) {
		n.lock.Unlock()
		return true
	}
	var nic string
	for k, v := range n.pathMap {
		if v == path {
			nic = k
			break
		}
	}
	n.lock.Unlock()

	bridge := n.bridgeMap[nic]
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
	defer n.lock.Unlock()

	if _, ok := n.pathMap[nicName]; ok {
		return nil
	}

	device, err := n.manager.GetDeviceByIpIface(nicName)
	if err != nil {
		klog.Errorf("failed to get device by IP iface %q: %v", nicName, err)
		return err
	}

	path := string(device.GetPath())
	klog.V(3).Infof("adding device %s with dbus object path %s and bridge %s", nicName, path, bridge)
	n.devicePaths.Add(path)
	n.pathMap[nicName] = path
	n.bridgeMap[nicName] = bridge

	return nil
}

func (n *networkManagerSyncer) RemoveDevice(nicName string) error {
	if n.manager == nil {
		return nil
	}

	n.lock.Lock()
	n.devicePaths.Remove(n.pathMap[nicName])
	delete(n.pathMap, nicName)
	delete(n.bridgeMap, nicName)
	n.lock.Unlock()

	return nil
}

func (n *networkManagerSyncer) SetManaged(name string, managed bool) error {
	if n.manager == nil {
		return nil
	}

	device, err := n.manager.GetDeviceByIpIface(name)
	if err != nil {
		klog.Errorf("failed to get device by IP iface %q: %v", name, err)
		return err
	}
	// ignore if device type is bond
	nmDeviceType, err := device.GetPropertyDeviceType()
	if err != nil {
		klog.Errorf("failed to get device type %q: %v", name, err)
		return err
	}

	if nmDeviceType == gonetworkmanager.NmDeviceTypeBond {
		return nil
	}

	current, err := device.GetPropertyManaged()
	if err != nil {
		klog.Errorf("failed to get device property managed: %v", err)
		return err
	}
	if current == managed {
		return nil
	}

	klog.Infof(`setting device %s NetworkManager property "managed" to %v`, name, managed)
	if err = device.SetPropertyManaged(managed); err != nil {
		klog.Errorf("failed to set device property managed to %v: %v", managed, err)
		return err
	}

	return nil
}
