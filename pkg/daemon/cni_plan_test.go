package daemon

import (
	"testing"

	"github.com/kubeovn/kube-ovn/pkg/request"
)

func TestCNIResponseForPlanPreservesAddressFamilies(t *testing.T) {
	plan := &request.CNIPlan{
		IfName:         "eth0",
		NetNs:          "/var/run/netns/pod",
		MacAddress:     "0a:58:fd:00:00:02",
		IP:             "10.16.0.2,fd00::2",
		CIDR:           "10.16.0.0/16,fd00::/64",
		Gateway:        "10.16.0.1,fd00::1",
		IsDefaultRoute: true,
	}
	response := cniResponseForPlan(plan)
	if len(response.IPs) != 2 {
		t.Fatalf("expected two IP configs, got %d", len(response.IPs))
	}
	if response.IPs[0].Gateway != "10.16.0.1" || response.IPs[1].Gateway != "fd00::1" {
		t.Fatalf("unexpected gateways: %#v", response.IPs)
	}
}
