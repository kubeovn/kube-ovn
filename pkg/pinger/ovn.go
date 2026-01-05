package pinger

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
	"k8s.io/utils/set"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var sbServiceAddress string

func init() {
	sbHost, sbPort := util.InjectedServiceVariables("ovn-sb")
	sbServiceAddress = ovs.OvsdbServerAddress(sbHost, intstr.FromString(sbPort))
}

func checkOvs(config *Configuration, setMetrics bool) error {
	for component, err := range getOvsStatus() {
		if err != nil {
			klog.Errorf("%s is down", component)
			if setMetrics {
				SetOvsDownMetrics(config.NodeName)
			}
			return err
		}
	}
	klog.Infof("%s and %s are up", ovs.OvsdbServer, ovs.OvsVswitchd)
	if setMetrics {
		SetOvsUpMetrics(config.NodeName)
	}
	return nil
}

func checkOvnController(config *Configuration, setMetrics bool) error {
	_, err := ovs.Appctl(ovs.OvnController, "-T", "1", "version")
	if err != nil {
		klog.Errorf("failed to get status of %s: %v", ovs.OvnController, err)
		if setMetrics {
			SetOvnControllerDownMetrics(config.NodeName)
		}
		return err
	}
	klog.Infof("%s is up", ovs.OvnController)
	if setMetrics {
		SetOvnControllerUpMetrics(config.NodeName)
	}
	return nil
}

func checkPortBindings(config *Configuration, setMetrics bool) error {
	klog.Infof("start to check port binding")
	ovsBindings, err := checkOvsBindings()
	if err != nil {
		klog.Error(err)
		return err
	}
	klog.Infof("ports in ovs: %v", strings.Join(ovsBindings.SortedList(), ", "))

	sbBindings, err := checkSBBindings(config)
	if err != nil {
		klog.Error(err)
		return err
	}
	klog.Infof("ports in sb: %v", strings.Join(sbBindings.SortedList(), ", "))

	if misMatch := ovsBindings.Difference(sbBindings); misMatch.Len() > 0 {
		err = fmt.Errorf("%d ports not exist in ovn-sb-bindings: %s", misMatch.Len(), strings.Join(misMatch.SortedList(), ", "))
		klog.Error(err)
		if setMetrics {
			inconsistentPortBindingGauge.WithLabelValues(config.NodeName).Set(float64(misMatch.Len()))
		}
		return err
	}

	klog.Info("ovs and ovn-sb port binding check passed")
	if setMetrics {
		inconsistentPortBindingGauge.WithLabelValues(config.NodeName).Set(0)
	}
	return nil
}

func checkOvsBindings() (set.Set[string], error) {
	output, err := exec.Command(
		"ovs-vsctl",
		"--no-heading",
		"--data=bare",
		"--format=csv",
		"--columns=external_ids",
		"--timeout=10",
		"find",
		"interface",
		"external_ids:iface-id!=\"\"").CombinedOutput()
	if err != nil {
		klog.Errorf("failed to get ovs interface %v", err)
		return nil, err
	}
	result := set.New[string]()
	for line := range strings.SplitSeq(string(output), "\n") {
		// In dual-stack clusters, the output may look like:
		// "iface-id=kube-ovn-pinger-ljqss.kube-system ip=10.180.160.6,2341::10:180:160:6 ... vendor=kube-ovn"
		// so we need to trim the quotes and split by space.
		for id := range strings.FieldsSeq(strings.Trim(line, `"`)) {
			if after, found := strings.CutPrefix(id, "iface-id="); found {
				result.Insert(strings.TrimSpace(after))
				continue
			}
		}
	}
	return result, nil
}

func getChassis(hostname string) (string, error) {
	result, err := ovs.Query(sbServiceAddress, ovnsb.DatabaseName, 10, ovsdb.Operation{
		Op:    ovsdb.OperationSelect,
		Table: ovnsb.ChassisTable,
		Where: []ovsdb.Condition{{
			Column:   "hostname",
			Function: ovsdb.ConditionEqual,
			Value:    hostname,
		}},
		Columns: []string{"_uuid"},
	})
	if err != nil {
		klog.Errorf("failed to get chassis UUID by hostname %q: %v", hostname, err)
		return "", err
	}
	if len(result) != 1 {
		return "", fmt.Errorf("unexpected number of results when getting chassis UUID for hostname %q: %d", hostname, len(result))
	}
	if len(result[0].Rows) == 0 {
		return "", fmt.Errorf("no chassis found for hostname %q", hostname)
	}
	if len(result[0].Rows) != 1 {
		return "", fmt.Errorf("unexpected number of rows when getting chassis UUID for hostname %q: %d", hostname, len(result[0].Rows))
	}

	if uuid, ok := result[0].Rows[0]["_uuid"].(ovsdb.UUID); ok {
		return uuid.GoUUID, nil
	}

	return "", fmt.Errorf("unexpected data format for chassis UUID for hostname %q: %v", hostname, result[0].Rows[0]["_uuid"])
}

func getLogicalPort(chassisUUID string) (set.Set[string], error) {
	result, err := ovs.Query(sbServiceAddress, ovnsb.DatabaseName, 10, ovsdb.Operation{
		Op:    ovsdb.OperationSelect,
		Table: ovnsb.PortBindingTable,
		Where: []ovsdb.Condition{{
			Column:   "chassis",
			Function: ovsdb.ConditionEqual,
			Value:    ovsdb.UUID{GoUUID: chassisUUID},
		}},
		Columns: []string{"logical_port"},
	})
	if err != nil {
		klog.Errorf("failed to get logical ports by chassis UUID %q: %v", chassisUUID, err)
		return nil, err
	}
	if len(result) != 1 {
		return nil, fmt.Errorf("unexpected number of results when getting logical ports for chassis UUID %q: %d", chassisUUID, len(result))
	}

	ports := set.New[string]()
	for row := range slices.Values(result[0].Rows) {
		if lp, ok := row["logical_port"].(string); ok {
			ports.Insert(lp)
		} else {
			klog.Errorf("unexpected data format for logical_port in row %v", row)
		}
	}
	return ports, nil
}

func checkSBBindings(config *Configuration) (set.Set[string], error) {
	chassisUUID, err := getChassis(config.NodeName)
	if err != nil {
		return nil, err
	}
	return getLogicalPort(chassisUUID)
}
