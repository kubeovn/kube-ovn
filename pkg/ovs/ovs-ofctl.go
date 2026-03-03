package ovs

import (
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"

	"github.com/digitalocean/go-openvswitch/ovs"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

// openFlowStdinReader incrementally renders a flow slice as a newline-delimited
// stream for ovs-ofctl stdin without constructing one large joined string.
type openFlowStdinReader struct {
	flows      []string
	flowIndex  int
	flowOffset int
	needEOL    bool
}

// Read implements io.Reader over r.flows, producing output equivalent to
// strings.Join(flows, "\n"), but in small chunks to reduce peak allocations.
func (r *openFlowStdinReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.flowIndex >= len(r.flows) && !r.needEOL {
		return 0, io.EOF
	}

	total := 0
	for total < len(p) {
		if r.needEOL {
			p[total] = '\n'
			total++
			r.needEOL = false
			if total == len(p) {
				return total, nil
			}
			continue
		}

		if r.flowIndex >= len(r.flows) {
			break
		}

		flow := r.flows[r.flowIndex]
		if r.flowOffset >= len(flow) {
			r.flowIndex++
			r.flowOffset = 0
			r.needEOL = r.flowIndex < len(r.flows)
			continue
		}

		copied := copy(p[total:], flow[r.flowOffset:])
		total += copied
		r.flowOffset += copied
	}

	if total == 0 {
		return 0, io.EOF
	}
	return total, nil
}

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
// It streams flows to ovs-ofctl stdin to avoid allocating a large joined string.
func ReplaceFlows(bridgeName string, flows []string) error {
	cmd := exec.Command("ovs-ofctl", "--bundle", "replace-flows", bridgeName, "-")
	cmd.Stdin = &openFlowStdinReader{flows: flows}

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
