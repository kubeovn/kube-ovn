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

func configExternalBridge(provider, bridge, nic string, exchangeLinkName bool) error {
	brExists, err := ovs.BridgeExists(bridge)
	if err != nil {
		return fmt.Errorf("failed to check OVS bridge existence: %v", err)
	}
	output, err := ovs.Exec(ovs.MayExist, "add-br", bridge,
		"--", "set", "bridge", bridge, "external_ids:vendor="+util.CniTypeName,
		"--", "set", "bridge", bridge, fmt.Sprintf("external_ids:exchange-link-name=%v", exchangeLinkName),
	)
	if err != nil {
		return fmt.Errorf("failed to create OVS bridge %s, %v: %q", bridge, err, output)
	}
	if !brExists {
		// assign a new generated mac address only when the bridge is newly created
		output, err = ovs.Exec("set", "bridge", bridge, fmt.Sprintf(`other-config:hwaddr="%s"`, util.GenerateMac()))
		if err != nil {
			return fmt.Errorf("failed to set hwaddr of OVS bridge %s, %v: %q", bridge, err, output)
		}
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

	if output, err = ovs.Exec(ovs.IfExists, "get", "open", ".", "external-ids:ovn-bridge-mappings"); err != nil {
		return fmt.Errorf("failed to get ovn-bridge-mappings, %v", err)
	}

	bridgeMappings := fmt.Sprintf("%s:%s", provider, bridge)
	if util.IsStringIn(bridgeMappings, strings.Split(output, ",")) {
		return nil
	}
	if output != "" {
		bridgeMappings = fmt.Sprintf("%s,%s", output, bridgeMappings)
	}
	if output, err = ovs.Exec("set", "open", ".", "external-ids:ovn-bridge-mappings="+bridgeMappings); err != nil {
		return fmt.Errorf("failed to set ovn-bridge-mappings, %v: %q", err, output)
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
