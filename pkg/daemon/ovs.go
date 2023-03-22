package daemon

import (
	"fmt"
	"strings"
	"time"

	goping "github.com/oilbeater/go-ping"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const gatewayCheckMaxRetry = 200

func pingGateway(gw, src string, verbose bool) error {
	pinger, err := goping.NewPinger(gw)
	if err != nil {
		return fmt.Errorf("failed to init pinger: %v", err)
	}
	pinger.SetPrivileged(true)
	// CNITimeoutSec = 220, cannot exceed
	pinger.Count = gatewayCheckMaxRetry
	pinger.Timeout = gatewayCheckMaxRetry * time.Second
	pinger.Interval = time.Second

	var success bool
	pinger.OnRecv = func(p *goping.Packet) {
		success = true
		pinger.Stop()
	}
	pinger.Run()

	cniConnectivityResult.WithLabelValues(nodeName).Add(float64(pinger.PacketsSent))
	if !success {
		return fmt.Errorf("%s network not ready after %d ping %s", src, gatewayCheckMaxRetry, gw)
	}
	if verbose {
		klog.Infof("%s network ready after %d ping, gw %s", src, pinger.PacketsSent, gw)
	}

	return nil
}

func configureGlobalMirror(portName string, mtu int) error {
	raw, err := ovs.Exec(ovs.MayExist, "add-port", "br-int", portName, "--",
		"set", "interface", portName, "type=internal", "--",
		"clear", "bridge", "br-int", "mirrors", "--",
		"--id=@mirror0", "get", "port", portName, "--",
		"--id=@m", "create", "mirror", fmt.Sprintf("name=%s", util.MirrorDefaultName), "select_all=true", "output_port=@mirror0", "--",
		"add", "bridge", "br-int", "mirrors", "@m")
	if err != nil {
		klog.Errorf("failed to configure mirror nic %s %q", portName, raw)
		return fmt.Errorf(raw)
	}
	return configureMirrorLink(portName, mtu)
}

func configureEmptyMirror(portName string, mtu int) error {
	raw, err := ovs.Exec(ovs.MayExist, "add-port", "br-int", portName, "--",
		"set", "interface", portName, "type=internal", "--",
		"clear", "bridge", "br-int", "mirrors", "--",
		"--id=@mirror0", "get", "port", portName, "--",
		"--id=@m", "create", "mirror", fmt.Sprintf("name=%s", util.MirrorDefaultName), "output_port=@mirror0", "--",
		"add", "bridge", "br-int", "mirrors", "@m")
	if err != nil {
		klog.Errorf("failed to configure mirror nic %s %q", portName, raw)
		return fmt.Errorf(raw)
	}
	return configureMirrorLink(portName, mtu)
}

func updateOvnMapping(name, key, value string) error {
	output, err := ovs.Exec(ovs.IfExists, "get", "open", ".", "external-ids:"+name)
	if err != nil {
		return fmt.Errorf("failed to get %s, %v: %q", name, err, output)
	}

	fields := strings.Split(output, ",")
	mappings := make(map[string]string, len(fields)+1)
	for _, f := range fields {
		idx := strings.IndexRune(f, ':')
		if idx <= 0 || idx == len(f)-1 {
			klog.Warningf("invalid mapping entry: %s", f)
			continue
		}
		mappings[f[:idx]] = f[idx+1:]
	}
	mappings[key] = value

	fields = make([]string, 0, len(mappings))
	for k, v := range mappings {
		fields = append(fields, fmt.Sprintf("%s:%s", k, v))
	}

	if len(fields) == 0 {
		output, err = ovs.Exec(ovs.IfExists, "remove", "open", ".", "external-ids", name)
	} else {
		output, err = ovs.Exec("set", "open", ".", fmt.Sprintf("external-ids:%s=%s", name, strings.Join(fields, ",")))
	}
	if err != nil {
		return fmt.Errorf("failed to set %s, %v: %q", name, err, output)
	}

	return nil
}

func configExternalBridge(provider, bridge, nic string, exchangeLinkName, macLearningFallback bool) error {
	brExists, err := ovs.BridgeExists(bridge)
	if err != nil {
		return fmt.Errorf("failed to check OVS bridge existence: %v", err)
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
		return fmt.Errorf("failed to create OVS bridge %s, %v: %q", bridge, err, output)
	}
	if output, err = ovs.Exec("list-ports", bridge); err != nil {
		return fmt.Errorf("failed to list ports of OVS bridge %s, %v: %q", bridge, err, output)
	}
	if output != "" {
		for _, port := range strings.Split(output, "\n") {
			if port != nic {
				ok, err := ovs.ValidatePortVendor(port)
				if err != nil {
					return fmt.Errorf("failed to check vendor of port %s: %v", port, err)
				}
				if ok {
					if err = removeProviderNic(port, bridge); err != nil {
						return fmt.Errorf("failed to remove port %s from OVS bridge %s: %v", port, bridge, err)
					}
				}
			}
		}
	}

	if err = updateOvnMapping("ovn-bridge-mappings", provider, bridge); err != nil {
		klog.Error(err)
		return err
	}

	return nil
}

func initProviderChassisMac(provider string) error {
	output, err := ovs.Exec(ovs.IfExists, "get", "open", ".", "external-ids:ovn-chassis-mac-mappings")
	if err != nil {
		return fmt.Errorf("failed to get ovn-bridge-mappings, %v", err)
	}

	for _, macMap := range strings.Split(output, ",") {
		if len(macMap) == len(provider)+18 && strings.Contains(output, provider) {
			return nil
		}
	}

	macMappings := fmt.Sprintf("%s:%s", provider, util.GenerateMac())
	if output != "" {
		macMappings = fmt.Sprintf("%s,%s", output, macMappings)
	}
	if output, err = ovs.Exec("set", "open", ".", "external-ids:ovn-chassis-mac-mappings="+macMappings); err != nil {
		return fmt.Errorf("failed to set ovn-chassis-mac-mappings, %v: %q", err, output)
	}
	return nil
}
