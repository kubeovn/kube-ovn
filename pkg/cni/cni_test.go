package cni

import (
	"testing"

	"github.com/kubeovn/kube-ovn/pkg/request"
)

func TestExecutorRejectsIncompletePlan(t *testing.T) {
	if _, err := NewExecutor(ExecutorConfig{}).Add(&request.CNIPlan{}); err == nil {
		t.Fatal("expected incomplete CNI plan to be rejected")
	}
}

func TestExecutorRejectsInvalidMAC(t *testing.T) {
	plan := &request.CNIPlan{
		PodName:      "pod",
		PodNamespace: "namespace",
		ContainerID:  "container",
		NetNs:        "/var/run/netns/pod",
		IfName:       "eth0",
		MacAddress:   "not-a-mac",
	}

	if _, err := NewExecutor(ExecutorConfig{}).Add(plan); err == nil {
		t.Fatal("expected invalid MAC to be rejected")
	}
}
