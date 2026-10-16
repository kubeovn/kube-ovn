package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestEnqueueAddIPPoolRoutesTerminatingToDeleteQueue(t *testing.T) {
	c := &Controller{
		addOrUpdateIPPoolQueue: newTypedRateLimitingQueue[string]("AddIPPool", nil),
		deleteIPPoolQueue:      newTypedRateLimitingQueue[*kubeovnv1.IPPool]("DeleteIPPool", nil),
	}
	t.Cleanup(func() {
		c.addOrUpdateIPPoolQueue.ShutDown()
		c.deleteIPPoolQueue.ShutDown()
	})

	// After a controller restart the informer (re)list surfaces a terminating
	// ippool only via the add handler; it must go to the delete queue or the
	// finalizer is never removed.
	now := metav1.Now()
	terminating := &kubeovnv1.IPPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "stuck",
			DeletionTimestamp: &now,
			Finalizers:        []string{util.KubeOVNControllerFinalizer},
		},
	}
	c.enqueueAddIPPool(terminating)
	require.Equal(t, 0, c.addOrUpdateIPPoolQueue.Len())
	require.Equal(t, 1, c.deleteIPPoolQueue.Len())
	item, _ := c.deleteIPPoolQueue.Get()
	require.Same(t, terminating, item)
	c.deleteIPPoolQueue.Done(item)

	// A live ippool keeps going to the add/update queue.
	live := &kubeovnv1.IPPool{ObjectMeta: metav1.ObjectMeta{Name: "live"}}
	c.enqueueAddIPPool(live)
	require.Equal(t, 1, c.addOrUpdateIPPoolQueue.Len())
	require.Equal(t, 0, c.deleteIPPoolQueue.Len())
}
