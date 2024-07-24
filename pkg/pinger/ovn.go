package pinger

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"k8s.io/klog/v2"
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
		SetOvnControllerDownMetrics(config.NodeName)
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

	sbBindings, err := checkSBBindings(config)
	if err != nil {
		klog.Error(err)
		return err
	}
	klog.Infof("port in sb is %v", sbBindings)
	misMatch := []string{}
	for _, port := range ovsBindings {
		if !slices.Contains(sbBindings, port) {
			misMatch = append(misMatch, port)
		}
	}
	if len(misMatch) > 0 {
		klog.Errorf("%d port %v not exist in sb-bindings", len(misMatch), misMatch)
		inconsistentPortBindingGauge.WithLabelValues(config.NodeName).Set(float64(len(misMatch)))
		return fmt.Errorf("%d port %v not exist in sb-bindings", len(misMatch), misMatch)
	}

	klog.Infof("ovs and ovn-sb binding check passed")
	if setMetrics {
		inconsistentPortBindingGauge.WithLabelValues(config.NodeName).Set(0)
	}
	return nil
}

func checkOvsBindings() ([]string, error) {
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
	result := make([]string, 0, len(strings.Split(string(output), "\n")))
	for _, line := range strings.Split(string(output), "\n") {
		for _, id := range strings.Split(line, " ") {
			if strings.Contains(id, "iface-id") {
				result = append(result, strings.TrimPrefix(id, "iface-id="))
				break
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
		return "", fmt.Errorf("No chassis found for hostname: %s", hostname)
	}
	return responses[0].Rows[0].UUID[1], nil
}

func getLogicalPort(chassis string) ([]string, error) {
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
		return nil, fmt.Errorf("Failed to query OVSDB: %w, %s", err, output)
	}

	// Parse the JSON output.
	var responses []PortBindingResponse
	err = json.Unmarshal(output, &responses)
	if err != nil {
		return nil, err
	}

	if len(responses) == 0 || len(responses[0].Rows) == 0 {
		return nil, fmt.Errorf("No logical port found for chassis: %s", chassis)
	}

	var ports []string
	for _, row := range responses[0].Rows {
		ports = append(ports, row.LogicalPort)
	}
	return ports, nil
}

func checkSBBindings(config *Configuration) ([]string, error) {
	chassis, err := getChassis(config.NodeName)
	if err != nil {
		return nil, err
	}
	return getLogicalPort(chassis)
}
