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

func TestUpdateEipAllocationPreservesLiveReplayStatus(t *testing.T) {
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

	require.NoError(t, c.updateEipAllocation(mustGetIptablesEip(t, c), "2.2.2.2", "", "", "ext-net"))

	got, err := client.Get(t.Context(), "eip", metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, got.Status.Ready)
	require.Equal(t, "new-nat", got.Status.Nat)
	require.Equal(t, "new-replay", got.Status.Redo)
}

func TestUpdateEipAllocationPreservesPendingReplay(t *testing.T) {
	eip := &kubeovnv1.IptablesEIP{
		Name: "eip", UID: "eip-uid",
		Spec: kubeovnv1.IptablesEIPSpec{V4ip: "2.2.2.2", NatGwDp: "gw", ExternalSubnet: "ext-net"},
		Status: kubeovnv1.IptablesEIPStatus{
			IP: "2.2.2.2", Nat: "new-nat", Redo: "new-replay",
		},
	}
	c := newEipReplayTestController(t, eip)

	client := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs()
	gotBefore, err := client.Get(t.Context(), eip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.False(t, gotBefore.Status.Ready)

	require.NoError(t, c.updateEipAllocation(
		eip, "2.2.2.2", "", "", "ext-net",
	))

	got, err := client.Get(t.Context(), eip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.False(t, got.Status.Ready)
	require.Equal(t, "new-replay", got.Status.Redo)
	require.Equal(t, "new-nat", got.Status.Nat)
}

func mustGetIptablesEip(t *testing.T, c *Controller) *kubeovnv1.IptablesEIP {
	t.Helper()
	eip, err := c.iptablesEipsLister.Get("eip")
	require.NoError(t, err)
	return eip
}

func TestEnqueueIptablesEipReplayWhenAllocated(t *testing.T) {
	t.Parallel()
	eip := &kubeovnv1.IptablesEIP{
		Name: "eip", Status: kubeovnv1.IptablesEIPStatus{Redo: "pending-replay"},
	}
	c := newEipReplayTestController(t, eip)
	client := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs()
	c.addIptablesEipQueue = workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())
	c.updateIptablesEipQueue = workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())
	t.Cleanup(c.addIptablesEipQueue.ShutDown)
	t.Cleanup(c.updateIptablesEipQueue.ShutDown)

	require.NoError(t, c.recordEipAllocation(eip, "192.0.2.10", ""))
	allocated, err := client.Get(t.Context(), eip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "192.0.2.10", allocated.Status.IP)
	require.Equal(t, eip.Status.Redo, allocated.Status.Redo)
	require.False(t, allocated.Status.Ready)

	for _, event := range []struct {
		name    string
		enqueue func()
	}{
		{name: "first status IP", enqueue: func() { c.enqueueUpdateIptablesEip(eip, allocated) }},
		{name: "informer relist", enqueue: func() { c.enqueueAddIptablesEip(allocated) }},
	} {
		t.Run(event.name, func(t *testing.T) {
			event.enqueue()
			require.Zero(t, c.addIptablesEipQueue.Len())
			require.Equal(t, 1, c.updateIptablesEipQueue.Len())
			key, shutdown := c.updateIptablesEipQueue.Get()
			require.False(t, shutdown)
			require.Equal(t, eip.Name, key)
			c.updateIptablesEipQueue.Done(key)
		})
	}

	c.enqueueUpdateIptablesEip(allocated, allocated.DeepCopy())
	require.Zero(t, c.updateIptablesEipQueue.Len(), "already published IP leaves the queue alone")
}

func TestEipAllocationRejectsChangedObject(t *testing.T) {
	for _, change := range []struct {
		name  string
		apply func(*kubeovnv1.IptablesEIP)
	}{
		{"changed spec", func(eip *kubeovnv1.IptablesEIP) { eip.Spec.V4ip = "192.0.2.20" }},
		{"replacement object", func(eip *kubeovnv1.IptablesEIP) { eip.UID = "replacement" }},
	} {
		t.Run(change.name, func(t *testing.T) {
			expected := &kubeovnv1.IptablesEIP{
				Name: "eip", UID: "original",
				Spec: kubeovnv1.IptablesEIPSpec{V4ip: "192.0.2.10", NatGwDp: "gw"},
			}
			live := expected.DeepCopy()
			change.apply(live)
			clientset := kubeovnfake.NewSimpleClientset()
			client := clientset.KubeovnV1().IptablesEIPs()
			_, err := client.Create(t.Context(), live, metav1.CreateOptions{})
			require.NoError(t, err)
			c := &Controller{config: &Configuration{KubeOvnClient: clientset}}

			require.ErrorContains(t, c.updateEipAllocation(expected, "192.0.2.10", "", "", "ext-net"), "changed during allocation")
			require.ErrorContains(t, c.recordEipAllocation(expected, "192.0.2.10", ""), "changed before allocation status")
			got, err := client.Get(t.Context(), live.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, live.UID, got.UID)
			require.Equal(t, live.Spec, got.Spec)
			require.Equal(t, live.Status, got.Status)
		})
	}
}
