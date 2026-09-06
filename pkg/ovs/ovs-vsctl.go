package ovs

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"k8s.io/klog/v2"
	"k8s.io/utils/set"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var limiter = new(Limiter)

var readOnlyCommands = set.New(
	"",                   // no command specified
	"show",               // print overview of database contents
	"list-br",            // print the names of all the bridges
	"br-exists",          // exit 2 if BRIDGE does not exist
	"br-to-vlan",         // print the VLAN which BRIDGE is on
	"br-to-parent",       // print the parent of BRIDGE
	"br-get-external-id", // print value of KEY on BRIDGE or list key-value pairs on BRIDGE
	"list-ports",         // print the names of all the ports on BRIDGE
	"port-to-br",         // print name of bridge that contains PORT
	"list-ifaces",        // print the names of all interfaces on BRIDGE
	"iface-to-br",        // print name of bridge that contains IFACE
	"get-controller",     // print the controllers for BRIDGE
	"get-fail-mode",      // print the fail-mode for BRIDGE
	"get-manager",        // print the managers
	"get-ssl",            // print the SSL configuration
	"get-aa-mapping",     // get Auto Attach mappings from BRIDGE
	"list-zone-limits",   // list all limits configured on DATAPATH
	"list",               // list RECord (or all records) in TBL
	"find",               // list records satisfying CONDITION in TBL
	"get",                // print values of COLumns in RECord in TBL
	"wait-until",         // wait until condition is true
)

func UpdateOVSVsctlLimiter(c int32) {
	if c >= 0 {
		limiter.Update(c)
		klog.V(4).Infof("update ovs-vsctl concurrency limit to %d", limiter.Limit())
	}
}

// Glory belongs to openvswitch/ovn-kubernetes
// https://github.com/openvswitch/ovn-kubernetes/blob/master/go-controller/pkg/util/ovs.go

var podNetNsRegexp = regexp.MustCompile(`pod_netns="([^"]+)"`)

func Exec(args ...string) (string, error) {
	var command string
	for arg := range slices.Values(args) {
		if !strings.HasPrefix(arg, "-") {
			command = arg
			break
		}
	}

	if !readOnlyCommands.Has(command) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		if err := limiter.Wait(ctx); err != nil {
			klog.V(4).Infof("command %s %s waiting for execution timeout by concurrency limit of %d", OvsVsCtl, strings.Join(args, " "), limiter.Limit())
			return "", err
		}
		defer limiter.Done()
		klog.V(4).Infof("command %s %s waiting for execution concurrency %d/%d", OvsVsCtl, strings.Join(args, " "), limiter.Current(), limiter.Limit())
	}

	start := time.Now()
	args = slices.Insert(args, 0, "--timeout=30")
	output, err := exec.Command(OvsVsCtl, args...).CombinedOutput()
	elapsed := float64(time.Since(start) / time.Millisecond)
	klog.V(4).Infof("command %s %s in %vms", OvsVsCtl, strings.Join(args, " "), elapsed)

	code := "0"
	defer func() {
		ovsClientRequestLatency.WithLabelValues("ovsdb", command, code).Observe(elapsed)
	}()

	if err != nil {
		code = "1"
		klog.Warningf("ovs-vsctl command error: %s %s in %vms", OvsVsCtl, strings.Join(args, " "), elapsed)
		return "", fmt.Errorf("failed to run '%s %s': %w\n  %q", OvsVsCtl, strings.Join(args, " "), err, output)
	} else if elapsed > 500 {
		klog.Warningf("ovs-vsctl command took too long: %s %s in %vms", OvsVsCtl, strings.Join(args, " "), elapsed)
	}

	return trimCommandOutput(output), nil
}

func ovsCreate(table string, values ...string) (string, error) {
	args := append([]string{"create", table}, values...)
	return Exec(args...)
}

func ovsDestroy(table, record string) error {
	_, err := Exec("--if-exists", "destroy", table, record)
	return err
}

func Set(table, record string, values ...string) error {
	args := append([]string{"set", table, record}, values...)
	_, err := Exec(args...)
	return err
}

func ovsAdd(table, record, column string, values ...string) error {
	args := append([]string{"add", table, record, column}, values...)
	_, err := Exec(args...)
	return err
}

// Returns the given column of records that match the condition
func ovsFind(table, column string, conditions ...string) ([]string, error) {
	args := make([]string, len(conditions)+4)
	args[0], args[1], args[2], args[3] = "--no-heading", "--columns="+column, "find", table
	copy(args[4:], conditions)
	output, err := Exec(args...)
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	ret := parseOvsFindOutput(output)
	return ret, nil
}

func parseOvsFindOutput(output string) []string {
	values := strings.Split(output, "\n\n")
	// We want "bare" values for strings, but we can't pass --bare to ovs-vsctl because
	// it breaks more complicated types. So try passing each value through Unquote();
	// if it fails, that means the value wasn't a quoted string, so use it as-is.
	for i, val := range values {
		if unquoted, err := strconv.Unquote(val); err == nil {
			values[i] = unquoted
		}
	}
	ret := make([]string, 0, len(values))
	for _, val := range values {
		if strings.TrimSpace(val) != "" {
			ret = append(ret, strings.Trim(strings.TrimSpace(val), "\""))
		}
	}
	return ret
}

func Remove(table, record, column string, keys ...string) error {
	args := append([]string{"remove", table, record, column}, keys...)
	_, err := Exec(args...)
	return err
}

func ovsClear(table, record string, columns ...string) error {
	args := append([]string{"--if-exists", "clear", table, record}, columns...)
	_, err := Exec(args...)
	return err
}

func Get(table, record, column, key string, ifExists bool) (string, error) {
	var columnVal string
	if key == "" {
		columnVal = column
	} else {
		columnVal = column + ":" + key
	}
	args := []string{"get", table, record, columnVal}
	if ifExists {
		args = append([]string{"--if-exists"}, args...)
	}
	return Exec(args...)
}

// Bridges returns bridges created by Kube-OVN.
func Bridges(providers ...compat.TableProvider) ([]string, error) {
	if len(providers) != 0 {
		if providers[0] == nil {
			return nil, errors.New("vswitch table provider is nil")
		}
		var rows []vswitch.Bridge
		if err := providers[0].Table(&vswitch.Bridge{}).Filter(context.Background(), func(row *vswitch.Bridge) bool {
			return row.ExternalIDs[ExternalIDVendor] == util.CniTypeName
		}, &rows); err != nil {
			return nil, fmt.Errorf("list Kube-OVN OVS bridges: %w", err)
		}
		bridges := make([]string, 0, len(rows))
		for _, row := range rows {
			bridges = append(bridges, row.Name)
		}
		return bridges, nil
	}
	return ovsFind("bridge", "name", "external-ids:vendor="+util.CniTypeName)
}

// BridgeExists checks whether the bridge already exists
func BridgeExists(name string, providers ...compat.TableProvider) (bool, error) {
	bridges, err := Bridges(providers...)
	if err != nil {
		klog.Error(err)
		return false, err
	}
	return slices.Contains(bridges, name), nil
}

// PortExists checks whether the port already exists

func PortExists(name string, providers ...compat.TableProvider) (bool, error) {
	if len(providers) != 0 {
		if providers[0] == nil {
			return false, errors.New("vswitch table provider is nil")
		}
		var rows []vswitch.Port
		if err := providers[0].Table(&vswitch.Port{}).Filter(context.Background(), func(row *vswitch.Port) bool {
			return row.Name == name
		}, &rows); err != nil {
			return false, fmt.Errorf("find OVS port %q: %w", name, err)
		}
		return len(rows) != 0, nil
	}
	result, err := ovsFind("port", "_uuid", "name="+name)
	if err != nil {
		klog.Errorf("failed to find port with name %s: %v", name, err)
		return false, err
	}
	return len(result) != 0, nil
}

func GetQosList(podName, podNamespace, ifaceID string, providers ...compat.TableProvider) ([]string, error) {
	if len(providers) != 0 {
		if providers[0] == nil {
			return nil, errors.New("vswitch table provider is nil")
		}
		var rows []vswitch.QoS
		if err := providers[0].Table(&vswitch.QoS{}).Filter(context.Background(), func(row *vswitch.QoS) bool {
			if ifaceID != "" {
				return row.ExternalIDs["iface-id"] == ifaceID
			}
			return row.ExternalIDs["pod"] == podNamespace+"/"+podName
		}, &rows); err != nil {
			return nil, fmt.Errorf("list QoS rows: %w", err)
		}
		qosIDs := make([]string, 0, len(rows))
		for _, row := range rows {
			qosIDs = append(qosIDs, row.UUID)
		}
		return qosIDs, nil
	}
	var qosList []string
	var err error

	if ifaceID != "" {
		qosList, err = ovsFind("qos", "_uuid", fmt.Sprintf(`external-ids:iface-id="%s"`, ifaceID))
		if err != nil {
			klog.Error(err)
			return qosList, err
		}
	} else {
		qosList, err = ovsFind("qos", "_uuid", fmt.Sprintf(`external-ids:pod="%s/%s"`, podNamespace, podName))
		if err != nil {
			klog.Error(err)
			return qosList, err
		}
	}

	return qosList, nil
}

// ClearPodBandwidth remove qos related to this pod.
func ClearPodBandwidth(podName, podNamespace, ifaceID string, providers ...compat.TableProvider) error {
	if len(providers) == 0 || providers[0] == nil {
		return errors.New("vswitch table provider is nil")
	}
	return clearPodBandwidthTable(providers[0], podName, podNamespace, ifaceID)
}

var lastInterfacePodMap map[string]string

func ListInterfacePodMap(providers ...compat.TableProvider) (map[string]string, error) {
	if len(providers) != 0 {
		if providers[0] == nil {
			return nil, errors.New("vswitch table provider is nil")
		}
		var rows []vswitch.Interface
		if err := providers[0].Table(&vswitch.Interface{}).Filter(context.Background(), func(row *vswitch.Interface) bool {
			return row.ExternalIDs["pod_name"] != "" && row.ExternalIDs["pod_namespace"] != "" &&
				(row.LinkState == nil || *row.LinkState != vswitch.InterfaceLinkStateUp)
		}, &rows); err != nil {
			return nil, fmt.Errorf("list OVS interfaces: %w", err)
		}
		result := make(map[string]string, len(rows))
		for _, row := range rows {
			errText := ""
			if row.Error != nil {
				errText = *row.Error
			}
			result[row.Name] = fmt.Sprintf("%s/%s/%s", row.ExternalIDs["pod_namespace"], row.ExternalIDs["pod_name"], errText)
		}
		if !maps.Equal(result, lastInterfacePodMap) {
			klog.Infof("interface pod map: %v", result)
			lastInterfacePodMap = maps.Clone(result)
		}
		return result, nil
	}
	output, err := Exec("--data=bare", "--format=csv", "--no-heading", "--columns=name,error,external_ids", "find",
		"interface", "external_ids:pod_name!=[]", "external_ids:pod_namespace!=[]", "link_state!=up")
	if err != nil {
		klog.Errorf("failed to list interface, %v", err)
		return nil, err
	}
	lines := strings.Split(output, "\n")
	result := make(map[string]string, len(lines))
	for _, l := range lines {
		if len(strings.TrimSpace(l)) == 0 {
			continue
		}
		parts := strings.SplitN(strings.TrimSpace(l), ",", 3)
		if len(parts) != 3 {
			continue
		}
		ifaceName := strings.TrimSpace(parts[0])
		errText := strings.TrimSpace(parts[1])
		var podNamespace, podName string
		for externalID := range strings.FieldsSeq(parts[2]) {
			if strings.Contains(externalID, "pod_name=") {
				podName = strings.TrimPrefix(strings.TrimSpace(externalID), "pod_name=")
			}

			if strings.Contains(externalID, "pod_namespace=") {
				podNamespace = strings.TrimPrefix(strings.TrimSpace(externalID), "pod_namespace=")
			}
		}
		result[ifaceName] = fmt.Sprintf("%s/%s/%s", podNamespace, podName, errText)
	}
	if !maps.Equal(result, lastInterfacePodMap) {
		klog.Infof("interface pod map: %v", result)
		lastInterfacePodMap = maps.Clone(result)
	}
	return result, nil
}

func CleanInterface(name string, providers ...compat.TableProvider) error {
	if len(providers) != 0 {
		if providers[0] == nil {
			return errors.New("vswitch table provider is nil")
		}
		return DeleteVswitchPort(context.Background(), providers[0], name)
	}
	qosList, err := ovsFind("port", "qos", "name="+name)
	if err != nil {
		klog.Errorf("failed to find related port %v", err)
		return err
	}
	klog.Infof("delete lost port %s", name)
	output, err := Exec("--if-exists", "--with-iface", "del-port", name)
	if err != nil {
		klog.Errorf("failed to delete ovs port %v, %s", err, output)
		return err
	}
	for _, qos := range qosList {
		qos = strings.TrimSpace(qos)
		if qos != "" && qos != "[]" {
			klog.Infof("delete lost qos %s", qos)
			err = ovsDestroy("qos", qos)
			if err != nil {
				klog.Errorf("failed to delete qos %s, %v", qos, err)
				return err
			}
		}
	}
	return nil
}

// Find and remove any existing OVS port with this iface-id. Pods can
// have multiple sandboxes if some are waiting for garbage collection,
// but only the latest one should have the iface-id set.
// See: https://github.com/ovn-org/ovn-kubernetes/pull/869
func CleanDuplicatePort(ifaceID, portName string, providers ...compat.TableProvider) {
	if len(providers) != 0 {
		if providers[0] == nil {
			klog.Error("vswitch table provider is nil")
			return
		}
		var interfaces []vswitch.Interface
		if err := providers[0].Table(&vswitch.Interface{}).Filter(context.Background(), func(row *vswitch.Interface) bool {
			return row.ExternalIDs["iface-id"] == ifaceID && row.Name != portName
		}, &interfaces); err != nil {
			klog.Errorf("failed to list duplicate OVS interfaces for %s: %v", ifaceID, err)
			return
		}
		for i := range interfaces {
			iface := &interfaces[i]
			if err := providers[0].Table(&vswitch.Interface{}).Mutate(context.Background(), "interface-duplicate-cleanup", iface,
				model.Mutation{Field: &iface.ExternalIDs, Mutator: ovsdb.MutateOperationDelete, Value: map[string]string{"iface-id": ifaceID}}); err != nil {
				klog.Errorf("failed to clear stale OVS interface %q iface-id %q: %v", iface.UUID, ifaceID, err)
			}
		}
		return
	}
	uuids, _ := ovsFind("Interface", "_uuid", "external-ids:iface-id="+ifaceID, "name!="+portName)
	for _, uuid := range uuids {
		if out, err := Exec("remove", "Interface", uuid, "external-ids", "iface-id"); err != nil {
			klog.Errorf("failed to clear stale OVS port %q iface-id %q: %v\n  %q", uuid, ifaceID, err, out)
		}
	}
}

// ValidatePortVendor returns true if the port's external_ids:vendor=kube-ovn
func ValidatePortVendor(port string, providers ...compat.TableProvider) (bool, error) {
	if len(providers) != 0 {
		if providers[0] == nil {
			return false, errors.New("vswitch table provider is nil")
		}
		var rows []vswitch.Port
		if err := providers[0].Table(&vswitch.Port{}).Filter(context.Background(), func(row *vswitch.Port) bool {
			return row.Name == port && row.ExternalIDs[ExternalIDVendor] == util.CniTypeName
		}, &rows); err != nil {
			return false, fmt.Errorf("validate OVS port %q: %w", port, err)
		}
		return len(rows) != 0, nil
	}
	output, err := ovsFind("Port", "name", "external_ids:vendor="+util.CniTypeName)
	return slices.Contains(output, port), err
}

func GetInterfacePodNs(iface string, providers ...compat.TableProvider) (string, error) {
	if len(providers) != 0 {
		if providers[0] == nil {
			return "", errors.New("vswitch table provider is nil")
		}
		var rows []vswitch.Interface
		if err := providers[0].Table(&vswitch.Interface{}).Filter(context.Background(), func(row *vswitch.Interface) bool {
			return row.ExternalIDs["iface-id"] == iface
		}, &rows); err != nil {
			return "", fmt.Errorf("find OVS interface %q: %w", iface, err)
		}
		if len(rows) == 0 {
			return "", nil
		}
		return rows[0].ExternalIDs["pod_netns"], nil
	}
	ret, err := ovsFind("interface", "external-ids", "external-ids:iface-id="+iface)
	if err != nil {
		klog.Error(err)
		return "", err
	}

	if len(ret) == 0 {
		return "", nil
	}

	podNetNs := ""
	match := podNetNsRegexp.FindStringSubmatch(ret[0])
	if len(match) > 1 {
		podNetNs = match[1]
	}

	return podNetNs, nil
}

// config mirror for interface by pod annotations and install param
func ConfigInterfaceMirror(globalMirror bool, open, iface string, providers ...compat.TableProvider) error {
	if globalMirror {
		return nil
	}
	if len(providers) != 0 {
		if providers[0] == nil {
			return errors.New("vswitch table provider is nil")
		}
		return ConfigVswitchInterfaceMirror(context.Background(), providers[0], open == "true", iface)
	}
	// find interface name for port
	interfaceList, err := ovsFind("interface", "name", "external-ids:iface-id="+iface)
	if err != nil {
		klog.Error(err)
		return err
	}
	for _, ifName := range interfaceList {
		// ifName example: xxx_h
		// find port uuid by interface name
		portUUIDs, err := ovsFind("port", "_uuid", "name="+ifName)
		if err != nil {
			klog.Error(err)
			return err
		}
		if len(portUUIDs) != 1 {
			return fmt.Errorf("find port failed, portName=%s", ifName)
		}
		portID := portUUIDs[0]
		if open == "true" {
			// add port to mirror
			err = ovsAdd("mirror", util.MirrorDefaultName, "select_dst_port", portID)
			if err != nil {
				klog.Error(err)
				return err
			}
		} else {
			mirrorPorts, err := ovsFind("mirror", "select_dst_port", "name="+util.MirrorDefaultName)
			if err != nil {
				klog.Error(err)
				return err
			}
			if len(mirrorPorts) == 0 {
				return fmt.Errorf("find mirror failed, mirror name=%s", util.MirrorDefaultName)
			}
			if len(mirrorPorts) > 1 {
				return fmt.Errorf("repeated mirror data, mirror name=%s", util.MirrorDefaultName)
			}
			for _, mirrorPortIDs := range mirrorPorts {
				if strings.Contains(mirrorPortIDs, portID) {
					// remove port from mirror
					_, err := Exec("remove", "mirror", util.MirrorDefaultName, "select_dst_port", portID)
					if err != nil {
						klog.Error(err)
						return err
					}
				}
			}
		}
	}
	return nil
}

// remove qos related to this port.
func ClearPortQosBinding(ifaceID string, providers ...compat.TableProvider) error {
	if len(providers) == 0 || providers[0] == nil {
		return errors.New("vswitch table provider is nil")
	}
	return clearPortQosBindingTable(providers[0], ifaceID)
}

func ListExternalIDs(table string, providers ...compat.TableProvider) (map[string]string, error) {
	if len(providers) != 0 {
		if providers[0] == nil {
			return nil, errors.New("vswitch table provider is nil")
		}
		result := make(map[string]string)
		ctx := context.Background()
		switch strings.ToLower(table) {
		case "interface":
			var rows []vswitch.Interface
			if err := providers[0].Table(&vswitch.Interface{}).Filter(ctx, func(row *vswitch.Interface) bool { return row.ExternalIDs["iface-id"] != "" }, &rows); err != nil {
				return nil, err
			}
			for _, row := range rows {
				result[row.ExternalIDs["iface-id"]] = row.UUID
			}
		case "port":
			var rows []vswitch.Port
			if err := providers[0].Table(&vswitch.Port{}).Filter(ctx, func(row *vswitch.Port) bool { return row.ExternalIDs["iface-id"] != "" }, &rows); err != nil {
				return nil, err
			}
			for _, row := range rows {
				result[row.ExternalIDs["iface-id"]] = row.UUID
			}
		case "qos":
			var rows []vswitch.QoS
			if err := providers[0].Table(&vswitch.QoS{}).Filter(ctx, func(row *vswitch.QoS) bool { return row.ExternalIDs["iface-id"] != "" }, &rows); err != nil {
				return nil, err
			}
			for _, row := range rows {
				result[row.ExternalIDs["iface-id"]] = row.UUID
			}
		case "queue":
			var rows []vswitch.Queue
			if err := providers[0].Table(&vswitch.Queue{}).Filter(ctx, func(row *vswitch.Queue) bool { return row.ExternalIDs["iface-id"] != "" }, &rows); err != nil {
				return nil, err
			}
			for _, row := range rows {
				result[row.ExternalIDs["iface-id"]] = row.UUID
			}
		default:
			return nil, fmt.Errorf("unsupported OVS table %q", table)
		}
		return result, nil
	}
	output, err := Exec("--data=bare", "--format=csv", "--no-heading", "--columns=_uuid,external_ids", "find", table, "external_ids:iface-id!=[]")
	if err != nil {
		klog.Errorf("failed to list %s, %v", table, err)
		return nil, err
	}
	lines := strings.Split(output, "\n")
	result := make(map[string]string, len(lines))
	for _, l := range lines {
		if len(strings.TrimSpace(l)) == 0 {
			continue
		}
		parts := strings.Split(strings.TrimSpace(l), ",")
		if len(parts) != 2 {
			continue
		}
		uuid := strings.TrimSpace(parts[0])
		for externalID := range strings.FieldsSeq(parts[1]) {
			if !strings.Contains(externalID, "iface-id=") {
				continue
			}
			iface := strings.TrimPrefix(strings.TrimSpace(externalID), "iface-id=")
			result[iface] = uuid
			break
		}
	}
	return result, nil
}

func ListQosQueueIDs(providers ...compat.TableProvider) (map[string]string, error) {
	if len(providers) != 0 {
		if providers[0] == nil {
			return nil, errors.New("vswitch table provider is nil")
		}
		var rows []vswitch.QoS
		if err := providers[0].Table(&vswitch.QoS{}).Filter(context.Background(), func(row *vswitch.QoS) bool {
			_, ok := row.Queues[0]
			return ok
		}, &rows); err != nil {
			return nil, fmt.Errorf("list QoS queue IDs: %w", err)
		}
		result := make(map[string]string, len(rows))
		for _, row := range rows {
			result[row.UUID] = row.Queues[0]
		}
		return result, nil
	}
	output, err := Exec("--data=bare", "--format=csv", "--no-heading", "--columns=_uuid,queues", "find", "qos", "queues:0!=[]")
	if err != nil {
		klog.Errorf("failed to list qos, %v", err)
		return nil, err
	}
	lines := strings.Split(output, "\n")
	result := make(map[string]string, len(lines))
	for _, l := range lines {
		if len(strings.TrimSpace(l)) == 0 {
			continue
		}
		parts := strings.Split(strings.TrimSpace(l), ",")
		if len(parts) != 2 {
			continue
		}
		qosID := strings.TrimSpace(parts[0])
		if !strings.Contains(strings.TrimSpace(parts[1]), "0=") {
			continue
		}
		queueID := strings.TrimPrefix(strings.TrimSpace(parts[1]), "0=")
		result[qosID] = queueID
	}
	return result, nil
}
