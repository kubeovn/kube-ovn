package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
)

func newEipReplayTestController(t *testing.T, eip *kubeovnv1.IptablesEIP) *Controller {
	t.Helper()
	client := kubeovnfake.NewSimpleClientset()
	_, err := client.KubeovnV1().IptablesEIPs().Create(t.Context(), eip, metav1.CreateOptions{})
	require.NoError(t, err)
	index := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, index.Add(eip.DeepCopy()))
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())
	t.Cleanup(queue.ShutDown)
	return &Controller{
		config:                  &Configuration{KubeOvnClient: client},
		iptablesEipsLister:      kubeovnlister.NewIptablesEIPLister(index),
		updateSubnetStatusQueue: queue,
	}
}

func TestCreateOrUpdateEipCRPreservesLiveReplayStatus(t *testing.T) {
	oldStatus := kubeovnv1.IptablesEIPStatus{
		IP: "2.2.2.2", Ready: true, Nat: "old-nat", Redo: "old-replay",
	}
	eip := &kubeovnv1.IptablesEIP{
		Name: "eip", UID: "eip-uid",
		Spec:   kubeovnv1.IptablesEIPSpec{V4ip: "2.2.2.2", NatGwDp: "gw", ExternalSubnet: "ext-net"},
		Status: oldStatus,
	}
	c := newEipReplayTestController(t, eip)

	client := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs()
	live := eip.DeepCopy()
	live.Status = kubeovnv1.IptablesEIPStatus{
		IP: "2.2.2.2", Ready: true, Nat: "new-nat", Redo: "new-replay",
	}
	_, err := client.UpdateStatus(t.Context(), live, metav1.UpdateOptions{})
	require.NoError(t, err)

	require.Equal(t, "old-replay", mustGetIptablesEip(t, c).Status.Redo)

	require.NoError(t, c.createOrUpdateEipCR("eip", "2.2.2.2", "", "", "gw", "", "ext-net"))

	got, err := client.Get(t.Context(), "eip", metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, got.Status.Ready)
	require.Equal(t, "new-nat", got.Status.Nat)
	require.Equal(t, "new-replay", got.Status.Redo)
}

func mustGetIptablesEip(t *testing.T, c *Controller) *kubeovnv1.IptablesEIP {
	t.Helper()
	eip, err := c.iptablesEipsLister.Get("eip")
	require.NoError(t, err)
	return eip
}
