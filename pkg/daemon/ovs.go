package daemon

import (
	"errors"
	"fmt"
	"strings"
	"time"

	goping "github.com/prometheus-community/pro-bing"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const gatewayCheckMaxRetry = 200

func pingGateway(gw, src string, verbose bool, maxRetry int, done chan struct{}) (count int, err error) {
	pinger, err := goping.NewPinger(gw)
	if err != nil {
		return 0, fmt.Errorf("failed to init pinger: %w", err)
	}
	pinger.SetPrivileged(true)
	pinger.Count = maxRetry
	pinger.Timeout = time.Duration(maxRetry) * time.Second
	pinger.Interval = time.Second

	pinger.OnRecv = func(_ *goping.Packet) {
		pinger.Stop()
	}

	pinger.OnSend = func(_ *goping.Packet) {
		if pinger.PacketsRecv == 0 && pinger.PacketsSent != 0 && pinger.PacketsSent%3 == 0 {
			klog.Warningf("%s network not ready after %d ping to gateway %s", src, pinger.PacketsSent, gw)
		}
	}

	if done != nil {
		defer func() {
			select {
			case done <- struct{}{}:
			default:
			}
		}()

		finish := make(chan struct{}, 1)
		pinger.OnFinish = func(_ *goping.Statistics) {
			finish <- struct{}{}
		}
		go func() {
			select {
			case <-done:
				pinger.Stop()
			case <-finish:
			}
		}()
	}

	if err = pinger.Run(); err != nil {
		klog.Errorf("failed to run pinger for destination %s: %v", gw, err)
		return 0, err
	}

	if pinger.PacketsRecv == 0 {
		if pinger.PacketsSent < maxRetry {
			return pinger.PacketsSent, fmt.Errorf("gateway check of %s canceled after %d retries", gw, pinger.PacketsSent)
		}
		klog.Warningf("%s network not ready after %d ping to gateway %s", src, pinger.PacketsSent, gw)
		return pinger.PacketsSent, fmt.Errorf("no packets received from gateway %s", gw)
	}

	cniConnectivityResult.WithLabelValues(nodeName).Add(float64(pinger.PacketsSent))
	if verbose {
		klog.Infof("%s network ready after %d ping to gateway %s", src, pinger.PacketsSent, gw)
	}
	return pinger.PacketsSent, nil
}

func configureGlobalMirror(portName string, mtu int) error {
	nicExist, err := linkExists(portName)
	if err != nil {
		klog.Error(err)
		return err
	}

	if !nicExist {
		klog.Infof("nic %s not exist, create it", portName)
		raw, err := ovs.Exec(ovs.MayExist, "add-port", "br-int", portName, "--",
			"set", "interface", portName, "type=internal", "--",
			"clear", "bridge", "br-int", "mirrors", "--",
			"--id=@mirror0", "get", "port", portName, "--",
			"--id=@m", "create", "mirror", "name="+util.MirrorDefaultName, "select_all=true", "output_port=@mirror0", "--",
			"add", "bridge", "br-int", "mirrors", "@m")
		if err != nil {
			klog.Errorf("failed to configure mirror nic %s, %q, %v", portName, raw, err)
			return errors.New(raw)
		}
	} else {
		klog.Infof("nic %s exist, configure it", portName)
		raw, err := ovs.Exec(ovs.MayExist, "add-port", "br-int", portName, "--",
			"clear", "bridge", "br-int", "mirrors", "--",
			"--id=@mirror0", "get", "port", portName, "--",
			"--id=@m", "create", "mirror", "name="+util.MirrorDefaultName, "select_all=true", "output_port=@mirror0", "--",
			"add", "bridge", "br-int", "mirrors", "@m")
		if err != nil {
			klog.Errorf("failed to configure mirror nic %s, %q, %v", portName, raw, err)
			return errors.New(raw)
		}
	}

	return configureMirrorLink(portName, mtu)
}

func configureEmptyMirror(portName string, mtu int) error {
	nicExist, err := linkExists(portName)
	if err != nil {
		klog.Error(err)
		return err
	}

	if !nicExist {
		klog.Infof("nic %s not exist, create it", portName)
		raw, err := ovs.Exec(ovs.MayExist, "add-port", "br-int", portName, "--",
			"set", "interface", portName, "type=internal", "--",
			"clear", "bridge", "br-int", "mirrors", "--",
			"--id=@mirror0", "get", "port", portName, "--",
			"--id=@m", "create", "mirror", "name="+util.MirrorDefaultName, "output_port=@mirror0", "--",
			"add", "bridge", "br-int", "mirrors", "@m")
		if err != nil {
			klog.Errorf("failed to configure mirror nic %s %q, %v", portName, raw, err)
			return errors.New(raw)
		}
	} else {
		klog.Infof("nic %s exist, configure it", portName)
		raw, err := ovs.Exec(ovs.MayExist, "add-port", "br-int", portName, "--",
			"clear", "bridge", "br-int", "mirrors", "--",
			"--id=@mirror0", "get", "port", portName, "--",
			"--id=@m", "create", "mirror", "name="+util.MirrorDefaultName, "output_port=@mirror0", "--",
			"add", "bridge", "br-int", "mirrors", "@m")
		if err != nil {
			klog.Errorf("failed to configure mirror nic %s %q", portName, raw)
			return errors.New(raw)
		}
	}
	return configureMirrorLink(portName, mtu)
}

func decodeOvnMappings(s string) map[string]string {
	if len(s) == 0 {
		return map[string]string{}
	}

	fields := strings.Split(s, ",")
	mappings := make(map[string]string, len(fields)+1)
	for _, f := range fields {
		idx := strings.IndexRune(f, ':')
		if idx <= 0 || idx == len(f)-1 {
			klog.Warningf("invalid mapping entry: %s", f)
			continue
		}
		mappings[f[:idx]] = f[idx+1:]
	}
	return mappings
}

func encodeOvnMappings(mappings map[string]string) string {
	if len(mappings) == 0 {
		return ""
	}

	fields := make([]string, 0, len(mappings))
	for k, v := range mappings {
		fields = append(fields, fmt.Sprintf("%s:%v", k, v))
	}
	return strings.Join(fields, ",")
}

func getOvnMappings(name string) (map[string]string, error) {
	output, err := ovs.Exec(ovs.IfExists, "get", "open", ".", "external-ids:"+name)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s, %w: %q", name, err, output)
	}

	return decodeOvnMappings(output), nil
}

func setOvnMappings(name string, mappings map[string]string) error {
	var err error
	var output string
	if s := encodeOvnMappings(mappings); len(s) == 0 {
		output, err = ovs.Exec(ovs.IfExists, "remove", "open", ".", "external-ids", name)
	} else {
		output, err = ovs.Exec("set", "open", ".", fmt.Sprintf("external-ids:%s=%s", name, s))
	}
	if err != nil {
		return fmt.Errorf("failed to set %s, %w: %q", name, err, output)
	}

	return nil
}

func addOvnMapping(name, key, value string, overwrite bool) error {
	mappings, err := getOvnMappings(name)
	if err != nil {
		klog.Error(err)
		return err
	}

	if mappings[key] == value || (mappings[key] != "" && !overwrite) {
		return nil
	}

	mappings[key] = value
	return setOvnMappings(name, mappings)
}

func removeOvnMapping(name, key string) error {
	mappings, err := getOvnMappings(name)
	if err != nil {
		klog.Error(err)
		return err
	}

	length := len(mappings)
	delete(mappings, key)
	if len(mappings) == length {
		return nil
	}
	return setOvnMappings(name, mappings)
}

func (c *Controller) configExternalBridge(provider, bridge, nic string, exchangeLinkName, macLearningFallback bool, vlanInterfaceMap map[string]int) error {
	// check if nic exists before configuring external bridge
	nicExists, err := linkExists(nic)
	if err != nil {
		return fmt.Errorf("failed to check if nic %s exists: %w", nic, err)
	}
	if !nicExists {
		return fmt.Errorf("nic %s does not exist", nic)
	}

	klog.Infof("Configuring external bridge %s for provider %s, nic %s, and vlan interfaces %v", bridge, provider, nic, vlanInterfaceMap)

	brExists, err := ovs.BridgeExists(bridge)
	if err != nil {
		return fmt.Errorf("failed to check OVS bridge existence: %w", err)
	}
	cmd := []string{
		ovs.MayExist, "add-br", bridge,
		"--", "set", "bridge", bridge, fmt.Sprintf("other_config:mac-learning-fallback=%v", macLearningFallback),
		"--", "set", "bridge", bridge, "external_ids:vendor=" + util.CniTypeName,
		"--", "set", "bridge", bridge, fmt.Sprintf("external_ids:exchange-link-name=%v", exchangeLinkName),
	}
	if !brExists {
		// assign a new generated mac address only when the bridge is newly created
		cmd = append(cmd, "--", "set", "bridge", bridge, fmt.Sprintf(`other-config:hwaddr="%s"`, util.GenerateMac()))
	}
	output, err := ovs.Exec(cmd...)
	if err != nil {
		return fmt.Errorf("failed to create OVS bridge %s, %w: %q", bridge, err, output)
	}
	if output, err = ovs.Exec("list-ports", bridge); err != nil {
		return fmt.Errorf("failed to list ports of OVS bridge %s, %w: %q", bridge, err, output)
	}
	if output != "" {
		for port := range strings.SplitSeq(output, "\n") {
			// Skip the main NIC or VLAN subinterfaces belonging to it
			if port == nic {
				klog.Infof("Skipping main NIC port %s on bridge %s", port, bridge)
				continue
			}
			// Check if this port is a VLAN internal port we created (e.g., br-eth0-vlan10)
			isVlanInternalPort := false
			for vlanIf, vlanID := range vlanInterfaceMap {
				expectedInternalPort := fmt.Sprintf("%s-vlan%d", bridge, vlanID)
				if port == expectedInternalPort {
					klog.Infof("Preserving VLAN internal port %s for VLAN interface %s on bridge %s", port, vlanIf, bridge)
					isVlanInternalPort = true
					break
				}
			}
			if isVlanInternalPort {
				continue
			}

			ok, err := ovs.ValidatePortVendor(port)
			if err != nil {
				return fmt.Errorf("failed to check vendor of port %s: %w", port, err)
			}

			if ok {
				klog.Infof("Removing unmanaged port %s from bridge %s", port, bridge)
				if err = c.removeProviderNic(port, bridge); err != nil {
					return fmt.Errorf("failed to remove port %s from OVS bridge %s: %w", port, bridge, err)
				}
			} else {
				klog.Infof("Port %s on bridge %s has different vendor, skipping removal", port, bridge)
			}
		}
	}

	if err = addOvnMapping("ovn-bridge-mappings", provider, bridge, true); err != nil {
		klog.Error(err)
		return err
	}

	return nil
}

func initProviderChassisMac(provider string) error {
	if err := addOvnMapping("ovn-chassis-mac-mappings", provider, util.GenerateMac(), false); err != nil {
		klog.Error(err)
		return err
	}
	return nil
}

func GetProviderChassisMac(provider string) (string, error) {
	mappings, err := getOvnMappings("ovn-chassis-mac-mappings")
	if err != nil {
		return "", fmt.Errorf("failed to get chassis mac for provider %s: %w", provider, err)
	}

	mac, ok := mappings[provider]
	if !ok {
		return "", fmt.Errorf("no chassis mac found for provider %s", provider)
	}

	return mac, nil
}
