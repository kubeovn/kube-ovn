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

func pingGateway(gw, src string, verbose bool, maxRetry int) error {
	pinger, err := goping.NewPinger(gw)
	if err != nil {
		return fmt.Errorf("failed to init pinger: %v", err)
	}
	pinger.SetPrivileged(true)
	// CNITimeoutSec = 220, cannot exceed
	pinger.Count = maxRetry
	pinger.Timeout = time.Duration(maxRetry) * time.Second
	pinger.Interval = time.Second

	pinger.OnRecv = func(p *goping.Packet) {
		pinger.Stop()
	}

	// long time keep ping may result in readiness probe failed when start
	// show ping fails when gw not work
	go func() {
		for {
			time.Sleep(3 * time.Second)
			stats := pinger.Statistics()
			if stats.PacketsSent >= pinger.Count {
				break
			}
			if stats.PacketsRecv == 0 {
				klog.Errorf("%s network not ready after %d ping %s, %v%% packet loss", src, stats.PacketsSent, gw, stats.PacketLoss)
			} else {
				break
			}
		}
	}()

	pinger.Run()
	cniConnectivityResult.WithLabelValues(nodeName).Add(float64(pinger.PacketsSent))
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
		return nil, fmt.Errorf("failed to get %s, %v: %q", name, err, output)
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
		return fmt.Errorf("failed to set %s, %v: %q", name, err, output)
	}

	return nil
}

func addOvnMapping(name, key, value string, overwrite bool) error {
	mappings, err := getOvnMappings(name)
	if err != nil {
		return err
	}

	if mappings[key] == value || (mappings[key] != "" && !overwrite) {
		return nil
	}

	mappings[key] = value
	if err = setOvnMappings(name, mappings); err != nil {
		return err
	}

	return nil
}

func removeOvnMapping(name, key string) error {
	mappings, err := getOvnMappings(name)
	if err != nil {
		return err
	}

	length := len(mappings)
	delete(mappings, key)
	if len(mappings) == length {
		return nil
	}
	if err = setOvnMappings(name, mappings); err != nil {
		return err
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
