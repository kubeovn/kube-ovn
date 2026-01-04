package ovs

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"github.com/digitalocean/go-openvswitch/ovs"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func DumpFlows(client *ovs.Client, bridgeName string) ([]string, error) {
	flows, err := client.OpenFlow.DumpFlows(bridgeName)
	if err != nil {
		return nil, fmt.Errorf("ovs-ofctl dump-flows failed: %w", err)
	}

	flowStrings := make([]string, 0, len(flows))
	for _, flow := range flows {
		text, err := flow.MarshalText()
		if err != nil {
			return nil, fmt.Errorf("marshal flow failed: %w", err)
		}
		line := strings.TrimSpace(string(text))
		if line == "" {
			continue
		}
		flowStrings = append(flowStrings, line)
	}

	return flowStrings, nil
}

// ReplaceFlows uses ovs-ofctl replace-flows because go-openvswitch does not provide a native API.
func ReplaceFlows(bridgeName string, flows []string) error {
	flowData := strings.Join(flows, "\n")

	cmd := exec.Command("ovs-ofctl", "--bundle", "replace-flows", bridgeName, "-")
	cmd.Stdin = strings.NewReader(flowData)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ovs-ofctl replace-flows failed: %w, output: %s", err, strings.TrimSpace(string(output)))
	}

	return nil
}

// ClearU2OFlows clears obsolete U2O flows
func ClearU2OFlows(client *ovs.Client) error {
	bridges, err := Bridges()
	if err != nil {
		klog.Errorf("failed to get ovs bridges: %v", err)
		return err
	}

	for bridge := range slices.Values(bridges) {
		flows, err := client.OpenFlow.DumpFlows(bridge)
		if err != nil {
			klog.Errorf("failed to dump flows on bridge %s: %v", bridge, err)
			return err
		}

		for flow := range slices.Values(flows) {
			if flow.Priority != util.U2OKeepSrcMacPriority {
				continue
			}

			klog.Infof("deleting obsolete U2O keep src mac flow from bridge %s: %+v", bridge, flow)
			if err = client.OpenFlow.DelFlows(bridge, &ovs.MatchFlow{
				Protocol: flow.Protocol,
				InPort:   flow.InPort,
				Matches:  flow.Matches,
				Table:    flow.Table,
				Cookie:   flow.Cookie,
			}); err != nil {
				klog.Errorf("failed to delete obsolete U2O keep src mac flow from bridge %s: %v", bridge, err)
				return err
			}
		}
	}

	return nil
}
