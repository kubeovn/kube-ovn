package daemon

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// default max fdb age is 300s in ovs-vswitchd
const fdbSyncPeriod = 100 * time.Second

// regexp to match fdb entry line
var macMatch = regexp.MustCompile(`(?i)\s+([0-9a-f]{2}:){5}([0-9a-f]{2})\s+`)

// fdbIndex represents the identity of a fdb entry
type fdbIndex struct {
	vlan int
	mac  string
}

// fdbEntries represents fdb entries on an ovs bridge
type fdbEntries map[fdbIndex]string

func newFdbEntries() fdbEntries {
	return make(fdbEntries)
}

func (f fdbEntries) Insert(vlan int, mac, port string) {
	f[fdbIndex{vlan, mac}] = port
}

// parse output of command `ovs-appctl fdb/show <bridge>` and return static fdb entries
func parseStaticFdbEntries(bridge, output string, ports map[int]string) fdbEntries {
	// example output:
	//  port  VLAN  MAC                Age
	//     1   341  be:42:32:58:3d:73   65
	//     1   341  da:4c:ba:95:93:60  static
	entries := newFdbEntries()
	for s := range strings.SplitAfterSeq(strings.TrimSpace(output), "\n") {
		fields := strings.Fields(s)
		if len(fields) != 4 || !macMatch.MatchString(s) {
			continue
		}
		if fields[0] == "LOCAL" || fields[3] != "static" {
			continue
		}
		klog.V(3).Infof("found static fdb entry on bridge %s: %s", bridge, s)

		ofport, err1 := strconv.Atoi(fields[0])
		vlan, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			klog.Warningf("failed to parse ofport/vlan from fdb entry on bridge %s: %s", bridge, s)
			continue
		}

		mac := fields[2]
		if port := ports[ofport]; port != "" {
			klog.V(3).Infof("parsed static fdb entry on bridge %s: vlan %d mac %s port %s", bridge, vlan, mac, port)
			entries.Insert(vlan, mac, port)
		}
	}

	return entries
}

func (c *Controller) requestFdbSync() {
	select {
	case c.fdbSyncChan <- struct{}{}:
		klog.V(5).Info("fdb sync requested")
	default:
		klog.V(5).Info("fdb sync request has already been queued")
	}
}

func (c *Controller) syncFdb() {
	c.fdbSyncMutex.Lock()
	defer c.fdbSyncMutex.Unlock()

	klog.Info("starting fdb sync")

	bridges, err := c.vswitchClient.ListBridge(true, nil)
	if err != nil {
		klog.Errorf("failed to list ovs bridges: %v", err)
		return
	}
	ports, err := c.vswitchClient.ListPort(func(port *vswitch.Port) bool {
		return len(port.ExternalIDs) != 0 && port.ExternalIDs["ovn-localnet-port"] != ""
	})
	if err != nil {
		klog.Errorf("failed to list ovs patch ports: %v", err)
		return
	}
	interfaces, err := c.vswitchClient.ListInterface(func(iface *vswitch.Interface) bool {
		return iface.Type == "patch"
	})
	if err != nil {
		klog.Errorf("failed to list ovs patch interfaces: %v", err)
		return
	}

	patchInterfaces := make(map[string]*int, len(interfaces))
	for iface := range slices.Values(interfaces) {
		if iface.Ofport == nil {
			klog.Warningf("ofport of interface %s is empty", iface.Name)
			continue
		}
		patchInterfaces[iface.Name] = iface.Ofport
	}
	klog.V(3).Infof("found patch interfaces: %v", patchInterfaces)
	patchPorts := make(map[string]map[int]string, len(ports))
	for port := range slices.Values(ports) {
		if ofport := patchInterfaces[port.Name]; ofport != nil {
			patchPorts[port.UUID] = map[int]string{*ofport: port.Name}
		}
	}
	klog.V(3).Infof("found patch ports: %v", patchPorts)

	current := make(map[string]fdbEntries)
	for bridge := range slices.Values(bridges) {
		bridgePatchPorts := make(map[int]string, len(bridge.Ports))
		for uuid := range slices.Values(bridge.Ports) {
			maps.Insert(bridgePatchPorts, maps.All(patchPorts[uuid]))
		}
		klog.V(3).Infof("found patch ports on bridge %s: %v", bridge.Name, bridgePatchPorts)
		output, err := ovs.Appctl(ovs.OvsVswitchd, "fdb/show", bridge.Name)
		if err != nil {
			klog.Errorf("failed to show fdb for bridge %s: %v", bridge.Name, err)
			continue
		}
		current[bridge.Name] = parseStaticFdbEntries(bridge.Name, output, bridgePatchPorts)
		klog.V(3).Infof("current static fdb entries on bridge %s: %v", bridge.Name, current[bridge.Name])
	}

	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets: %v", err)
		return
	}

	for subnet := range slices.Values(subnets) {
		if subnet.Spec.Vlan == "" ||
			!subnet.Spec.U2OInterconnection ||
			subnet.Status.U2OInterconnectionMAC == "" {
			continue
		}

		vlan, err := c.vlansLister.Get(subnet.Spec.Vlan)
		if err != nil {
			klog.Errorf("failed to get vlan %q for subnet %s: %v", subnet.Spec.Vlan, subnet.Name, err)
			continue
		}
		pn, err := c.providerNetworksLister.Get(vlan.Spec.Provider)
		if err != nil {
			klog.Errorf("failed to get provider network %q for vlan %s: %v", vlan.Spec.Provider, vlan.Name, err)
			continue
		}

		bridge := util.ExternalBridgeName(pn.Name)
		port := fmt.Sprintf("patch-localnet.%s-to-br-int", subnet.Name)
		if _, ok := patchInterfaces[port]; !ok {
			klog.Warningf("patch port %s not found on bridge %s", port, bridge)
			continue
		}
		index := fdbIndex{vlan.Spec.VlanID, subnet.Status.U2OInterconnectionMAC}
		entries := current[bridge]
		if entries != nil && entries[index] == port {
			// the fdb entry already exists, remove it from current entries to avoid deletion
			delete(current[bridge], index)
			continue
		}
		// install static fdb entry
		klog.Infof("adding fdb entry vlan %d mac %s port %s on bridge %s", index.vlan, index.mac, port, bridge)
		if _, err = ovs.Appctl(ovs.OvsVswitchd, "fdb/add", bridge, port, strconv.Itoa(index.vlan), index.mac); err != nil {
			klog.Errorf("failed to add fdb entry vlan %d mac %s port %s on bridge %s: %v", index.vlan, index.mac, port, bridge, err)
		}
	}

	// delete unused fdb entries
	for bridge, entries := range current {
		for index := range entries {
			klog.Infof("deleting fdb entry vlan %d mac %s on bridge %s", index.vlan, index.mac, bridge)
			if _, err = ovs.Appctl(ovs.OvsVswitchd, "fdb/del", bridge, strconv.Itoa(index.vlan), index.mac); err != nil {
				klog.Errorf("failed to delete fdb entry vlan %d mac %s on bridge %s: %v", index.vlan, index.mac, bridge, err)
			}
		}
	}
}

func (c *Controller) runFdbSync(stopCh <-chan struct{}) {
	klog.Info("Starting fdb sync loop")

	ticker := time.NewTicker(fdbSyncPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.syncFdb()
		case <-c.fdbSyncChan:
			ticker.Reset(fdbSyncPeriod)
			c.syncFdb()
		case <-stopCh:
			klog.Info("Stopping fdb sync loop")
			return
		}
	}
}
