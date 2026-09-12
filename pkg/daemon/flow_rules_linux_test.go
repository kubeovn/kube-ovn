package daemon

import (
	"testing"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestUnderlayServiceLocalFlows(t *testing.T) {
	got := underlayServiceLocalFlows("0x1000", util.UnderlaySvcLocalOpenFlowPriority, 3, 7, "tcp", "nw_dst", "172.19.0.100", "00:00:00:01:00:01", 80)
	if len(got) != 1 {
		t.Fatalf("got %d flows, want 1", len(got))
	}
	wantPhy := "cookie=0x1000,priority=10000,in_port=3,tcp,nw_dst=172.19.0.100,tp_dst=80 actions=mod_dl_dst:00:00:00:01:00:01,output:7"
	if got[0] != wantPhy {
		t.Fatalf("physical steal flow = %q, want %q", got[0], wantPhy)
	}
}
