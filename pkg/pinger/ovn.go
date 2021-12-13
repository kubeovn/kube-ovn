package pinger

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func checkOvs(config *Configuration) error {
	output, err := exec.Command("/usr/share/openvswitch/scripts/ovs-ctl", "status").CombinedOutput()
	if err != nil {
		klog.Errorf("check ovs status failed %v, %s", err, string(output))
		SetOvsDownMetrics(config.NodeName)
		return err
	}
	klog.Infof("ovs-vswitchd and ovsdb are up")
	SetOvsUpMetrics(config.NodeName)
	return nil
}

func checkOvnController(config *Configuration) error {
	output, err := exec.Command("/usr/share/ovn/scripts/ovn-ctl", "status_controller").CombinedOutput()
	if err != nil {
		klog.Errorf("check ovn_controller status failed %v, %q", err, output)
		SetOvnControllerDownMetrics(config.NodeName)
		return err
	}
	klog.Infof("ovn_controller is up")
	SetOvnControllerUpMetrics(config.NodeName)
	return nil
}

func checkPortBindings(config *Configuration) error {
	klog.Infof("start to check port binding")
	ovsBindings, err := checkOvsBindings()
	if err != nil {
		return err
	}

	sbBindings, err := checkSBBindings(config)
	if err != nil {
		return err
	}
	klog.Infof("port in sb is %v", sbBindings)
	misMatch := []string{}
	for _, port := range ovsBindings {
		if !util.IsStringIn(port, sbBindings) {
			misMatch = append(misMatch, port)
		}
	}
	if len(misMatch) > 0 {
		klog.Errorf("%d port %v not exist in sb-bindings", len(misMatch), misMatch)
		inconsistentPortBindingGauge.WithLabelValues(config.NodeName).Set(float64(len(misMatch)))
		return fmt.Errorf("%d port %v not exist in sb-bindings", len(misMatch), misMatch)
	} else {
		klog.Infof("ovs and ovn-sb binding check passed")
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

func checkSBBindings(config *Configuration) ([]string, error) {
	sbHost := os.Getenv("OVN_SB_SERVICE_HOST")
	sbPort := os.Getenv("OVN_SB_SERVICE_PORT")
	command := []string{
		fmt.Sprintf("--db=tcp:[%s]:%s", sbHost, sbPort),
		"--format=csv",
		"--no-heading",
		"--data=bare",
		"--columns=_uuid",
		"--timeout=10",
		"find",
		"chassis",
		fmt.Sprintf("hostname=%s", config.NodeName),
	}
	if os.Getenv("ENABLE_SSL") == "true" {
		command = []string{
			"-p", "/var/run/tls/key",
			"-c", "/var/run/tls/cert",
			"-C", "/var/run/tls/cacert",
			fmt.Sprintf("--db=ssl:[%s]:%s", sbHost, sbPort),
			"--format=csv",
			"--no-heading",
			"--data=bare",
			"--columns=_uuid",
			"--timeout=10",
			"find",
			"chassis",
			fmt.Sprintf("hostname=%s", config.NodeName),
		}
	}
	output, err := exec.Command("ovn-sbctl", command...).CombinedOutput()
	if err != nil {
		klog.Errorf("failed to find chassis %v", err)
		return nil, err
	}
	if len(output) == 0 {
		klog.Errorf("chassis for node %s not exist", config.NodeName)
		return nil, fmt.Errorf("chassis for node %s not exist", config.NodeName)
	}

	chassis := strings.TrimSpace(string(output))
	klog.Infof("chassis id is %s", chassis)
	command = []string{
		fmt.Sprintf("--db=tcp:[%s]:%s", sbHost, sbPort),
		"--format=csv",
		"--no-heading",
		"--data=bare",
		"--columns=logical_port",
		"--timeout=10",
		"find",
		"port_binding",
		fmt.Sprintf("chassis=%s", chassis)}
	if os.Getenv("ENABLE_SSL") == "true" {
		command = []string{
			"-p", "/var/run/tls/key",
			"-c", "/var/run/tls/cert",
			"-C", "/var/run/tls/cacert",
			fmt.Sprintf("--db=ssl:[%s]:%s", sbHost, sbPort),
			"--format=csv",
			"--no-heading",
			"--data=bare",
			"--columns=logical_port",
			"--timeout=10",
			"find",
			"port_binding",
			fmt.Sprintf("chassis=%s", chassis),
		}
	}
	output, err = exec.Command("ovn-sbctl", command...).CombinedOutput()
	if err != nil {
		klog.Errorf("failed to list port_binding in ovn-sb %v", err)
		return nil, err
	}

	return strings.Split(strings.TrimSpace(string(output)), "\n"), nil
}
