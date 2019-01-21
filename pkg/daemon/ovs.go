package daemon

import (
	"fmt"
	"os/exec"
)

func (csc CniServerHandler) configureNic(podName, podNamespace, netns, containerID, mac, ip string) error {
	nicName := generateNicName(containerID)

	// 1. add ovs port
	output, err := exec.Command("ovs-vsctl", "add-port", "br-int", nicName+"_h", "--", "set", "interface", nicName+"_h", fmt.Sprintf("external_ids:iface-id=%s.%s", podName, podNamespace)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("add nic to ovs failed %v: %s", err, output)
	}
	//output, err = exec.Command("ovs-vsctl", "set", "Interface", nicName+"_h", fmt.Sprintf("external_ids:iface-id=%s", fmt.Sprintf("%s.%s", podName, podNamespace))).CombinedOutput()
	//if err != nil {
	//	return fmt.Errorf("set interface id failed %v: %s", err, output)
	//}

	//// 2. link netns to /var/run/netns/{container_id}
	//output, err = exec.Command("ln", "-s", netns, fmt.Sprintf("/var/run/netns/%s", nicName)).CombinedOutput()
	//if err != nil {
	//	return fmt.Errorf("failed to link netns %v %s", err, output)
	//}
	//
	//// 3. config nic
	//output, err = exec.Command("ip", "link", "set", nicName, "netns", nicName).CombinedOutput()
	//if err != nil {
	//	return fmt.Errorf("failed to move nic %v %s", err, output)
	//}
	//output, err = exec.Command("ip", "netns", "exec", nicName, "ip", "link", "set", nicName, "name", "eth0").CombinedOutput()
	//if err != nil {
	//	return fmt.Errorf("failed to rename nic %v %s", err, output)
	//}
	//
	//output, err = exec.Command("ip", "netns", "exec", nicName, "ip", "link", "set", "eth0", "address", mac).CombinedOutput()
	//if err != nil {
	//	return fmt.Errorf("failed to set mac %v %s", err, output)
	//}
	//output, err = exec.Command("ip", "netns", "exec", nicName, "ip", "addr", "add", ip, "dev", "eth0").CombinedOutput()
	//if err != nil {
	//	return fmt.Errorf("failed to set ip %v %s", err, output)
	//}
	//output, err = exec.Command("ip", "netns", "exec", nicName, "ip", "link", "set", "dev", "eth0", "up").CombinedOutput()
	//if err != nil {
	//	return fmt.Errorf("failed to set ip %v %s", err, output)
	//}

	return nil
}

func (csc CniServerHandler) deleteNic(netns, containerID string) error {
	nicName := generateNicName(containerID)

	output, err := exec.Command("ovs-vsctl", "--if-exists", "--with-iface", "del-port", "br-int", nicName+"_h").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete ovs port %v, %s", err, output)
	}
	//// TODO: check not exists
	//if _, err := os.Stat("/var/run/netns/"+nicName); err != nil {
	//	klog.Infof("stat netns %s failed %v", "/var/run/netns/"+nicName, err)
	//	return nil
	//}
	//os.Remove("/var/run/netns/"+nicName)
	return nil
}

func generateNicName(containerID string) string {
	return containerID[0:12]
}
