package pinger

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"k8s.io/klog/v2"
	"k8s.io/utils/set"
)

// Chassis represents a row in the Chassis table.
type PortBinging struct {
	LogicalPort string `json:"logical_port"`
}

// PortBindingResponse represents the structure of the OVSDB query response.
type PortBindingResponse struct {
	Rows []PortBinging `json:"rows"`
}

// Chassis represents a row in the Chassis table.
type Chassis struct {
	UUID [2]string `json:"_uuid"`
}

// ChassisResponse represents the structure of the OVSDB query response.
type ChassisResponse struct {
	Rows []Chassis `json:"rows"`
}

func checkOvs(config *Configuration, setMetrics bool) error {
	output, err := exec.Command("/usr/share/openvswitch/scripts/ovs-ctl", "status").CombinedOutput()
	if err != nil {
		klog.Errorf("check ovs status failed %v, %s", err, string(output))
		SetOvsDownMetrics(config.NodeName)
		return err
	}
	klog.Infof("ovs-vswitchd and ovsdb are up")
	if setMetrics {
		SetOvsUpMetrics(config.NodeName)
	}
	return nil
}

func checkOvnController(config *Configuration, setMetrics bool) error {
	output, err := exec.Command("/usr/share/ovn/scripts/ovn-ctl", "status_controller").CombinedOutput()
	if err != nil {
		klog.Errorf("check ovn_controller status failed %v, %q", err, output)
		if setMetrics {
			SetOvnControllerDownMetrics(config.NodeName)
		}
		return err
	}
	klog.Infof("ovn_controller is up")
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
	sbHost := os.Getenv("OVN_SB_SERVICE_HOST")
	sbPort := os.Getenv("OVN_SB_SERVICE_PORT")

	// Create the OVSDB query with the hostname filter
	query := fmt.Sprintf(`["OVN_Southbound",{"op":"select","table":"Chassis","where":[["hostname","==","%s"]],"columns":["_uuid"]}]`, hostname)

	command := []string{
		"--timeout=10", "query", fmt.Sprintf("tcp:[%s]:%s", sbHost, sbPort), query,
	}
	if os.Getenv("ENABLE_SSL") == "true" {
		command = []string{
			"-p", "/var/run/tls/key",
			"-c", "/var/run/tls/cert",
			"-C", "/var/run/tls/cacert",
			"--timeout=10", "query", fmt.Sprintf("ssl:[%s]:%s", sbHost, sbPort), query,
		}
	}

	// Execute the ovsdb-client command and get the JSON output.
	output, err := exec.Command("ovsdb-client", command...).CombinedOutput() // #nosec G204
	if err != nil {
		klog.Errorf("failed to find chassis %v", err)
		return "", err
	}

	// Parse the JSON output.
	var responses []ChassisResponse
	err = json.Unmarshal(output, &responses)
	if err != nil {
		return "", err
	}

	if len(responses) == 0 || len(responses[0].Rows) == 0 || len(responses[0].Rows[0].UUID) < 2 {
		return "", fmt.Errorf("no chassis found for hostname %q", hostname)
	}
	return responses[0].Rows[0].UUID[1], nil
}

func getLogicalPort(chassis string) (set.Set[string], error) {
	sbHost := os.Getenv("OVN_SB_SERVICE_HOST")
	sbPort := os.Getenv("OVN_SB_SERVICE_PORT")

	query := fmt.Sprintf(`["OVN_Southbound",{"op":"select","table":"Port_Binding","where":[["chassis","==",["uuid","%s"]]],"columns":["logical_port"]}]`, chassis)

	command := []string{
		"--timeout=10", "query", fmt.Sprintf("tcp:[%s]:%s", sbHost, sbPort), query,
	}
	if os.Getenv("ENABLE_SSL") == "true" {
		command = []string{
			"-p", "/var/run/tls/key",
			"-c", "/var/run/tls/cert",
			"-C", "/var/run/tls/cacert",
			"--timeout=10", "query", fmt.Sprintf("ssl:[%s]:%s", sbHost, sbPort), query,
		}
	}
	output, err := exec.Command("ovsdb-client", command...).CombinedOutput() // #nosec G204
	if err != nil {
		return nil, fmt.Errorf("failed to query ovn sb Port_Binding: %w, %s", err, output)
	}

	// Parse the JSON output.
	var responses []PortBindingResponse
	err = json.Unmarshal(output, &responses)
	if err != nil {
		return nil, err
	}

	if len(responses) == 0 || len(responses[0].Rows) == 0 {
		return nil, fmt.Errorf("no logical port found for chassis %q", chassis)
	}

	ports := set.New[string]()
	for _, row := range responses[0].Rows {
		ports.Insert(row.LogicalPort)
	}
	return ports, nil
}

func checkSBBindings(config *Configuration) (set.Set[string], error) {
	chassis, err := getChassis(config.NodeName)
	if err != nil {
		return nil, err
	}
	return getLogicalPort(chassis)
}
