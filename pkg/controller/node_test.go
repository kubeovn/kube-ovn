package controller

import (
	"sort"
	"testing"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func newBFDPortVpc(name string, selector map[string]string) *kubeovnv1.Vpc {
	return &kubeovnv1.Vpc{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: kubeovnv1.VpcSpec{
			BFDPort: &kubeovnv1.BFDPort{
				Enabled: true,
				IP:      "169.254.0.1/32",
				NodeSelector: &metav1.LabelSelector{
					MatchLabels: selector,
				},
			},
		},
	}
}

func newBFDPortVpcWithoutNodeSelector(name string) *kubeovnv1.Vpc {
	return &kubeovnv1.Vpc{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: kubeovnv1.VpcSpec{
			BFDPort: &kubeovnv1.BFDPort{
				Enabled: true,
				IP:      "169.254.0.1/32",
			},
		},
	}
}

func prepareNodeQueueTestController(t *testing.T, vpcs ...*kubeovnv1.Vpc) *Controller {
	t.Helper()

	fakeController := newFakeController(t)
	for _, vpc := range vpcs {
		require.NoError(t, fakeController.fakeInformers.vpcInformer.Informer().GetStore().Add(vpc))
	}

	ctrl := fakeController.fakeController
	ctrl.addNodeQueue = newTypedRateLimitingQueue[string]("AddNode", nil)
	ctrl.updateNodeQueue = newTypedRateLimitingQueue[string]("UpdateNode", nil)
	ctrl.deleteNodeQueue = newTypedRateLimitingQueue[string]("DeleteNode", nil)
	ctrl.deletingNodeObjMap = xsync.NewMap[string, *corev1.Node]()
	ctrl.addOrUpdateVpcQueue = newTypedRateLimitingQueue[string]("AddOrUpdateVpc", nil)
	return ctrl
}

func drainVpcQueue(t *testing.T, ctrl *Controller) []string {
	t.Helper()

	keys := make([]string, 0, ctrl.addOrUpdateVpcQueue.Len())
	for ctrl.addOrUpdateVpcQueue.Len() > 0 {
		key, shutdown := ctrl.addOrUpdateVpcQueue.Get()
		require.False(t, shutdown)
		keys = append(keys, key)
		ctrl.addOrUpdateVpcQueue.Done(key)
	}
	sort.Strings(keys)
	return keys
}

func TestEnqueueAddNodeEnqueuesMatchingVpcBFDPort(t *testing.T) {
	ctrl := prepareNodeQueueTestController(t,
		newBFDPortVpc("selected-vpc", map[string]string{"egress": "true"}),
		newBFDPortVpc("unselected-vpc", map[string]string{"egress": "false"}),
	)

	ctrl.enqueueAddNode(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "node-a",
			Labels: map[string]string{"egress": "true"},
		},
	})

	require.Equal(t, []string{"selected-vpc"}, drainVpcQueue(t, ctrl))
}

func TestEnqueueUpdateNodeEnqueuesVpcBFDPortOnSelectorMembershipChange(t *testing.T) {
	t.Run("node starts matching selector", func(t *testing.T) {
		ctrl := prepareNodeQueueTestController(t, newBFDPortVpc("selected-vpc", map[string]string{"egress": "true"}))
		oldNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}}
		newNode := oldNode.DeepCopy()
		newNode.Labels = map[string]string{"egress": "true"}

		ctrl.enqueueUpdateNode(oldNode, newNode)

		require.Zero(t, ctrl.addNodeQueue.Len())
		require.Zero(t, ctrl.updateNodeQueue.Len())
		require.Equal(t, []string{"selected-vpc"}, drainVpcQueue(t, ctrl))
	})

	t.Run("node stops matching selector", func(t *testing.T) {
		ctrl := prepareNodeQueueTestController(t, newBFDPortVpc("selected-vpc", map[string]string{"egress": "true"}))
		oldNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "node-a",
				Labels: map[string]string{"egress": "true"},
			},
		}
		newNode := oldNode.DeepCopy()
		newNode.Labels = map[string]string{"egress": "false"}

		ctrl.enqueueUpdateNode(oldNode, newNode)

		require.Zero(t, ctrl.addNodeQueue.Len())
		require.Zero(t, ctrl.updateNodeQueue.Len())
		require.Equal(t, []string{"selected-vpc"}, drainVpcQueue(t, ctrl))
	})

	t.Run("matching node readiness change enqueues vpc", func(t *testing.T) {
		ctrl := prepareNodeQueueTestController(t, newBFDPortVpc("selected-vpc", map[string]string{"egress": "true"}))
		oldNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "node-a",
				Labels: map[string]string{"egress": "true"},
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionFalse,
				}},
			},
		}
		newNode := oldNode.DeepCopy()
		newNode.Status.Conditions[0].Status = corev1.ConditionTrue

		ctrl.enqueueUpdateNode(oldNode, newNode)

		require.Equal(t, []string{"selected-vpc"}, drainVpcQueue(t, ctrl))
	})

	t.Run("node keeps matching selector does not enqueue vpc on label change", func(t *testing.T) {
		ctrl := prepareNodeQueueTestController(t, newBFDPortVpc("selected-vpc", map[string]string{"egress": "true"}))
		oldNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "node-a",
				Labels: map[string]string{"egress": "true", "role": "worker"},
			},
		}
		newNode := oldNode.DeepCopy()
		newNode.Labels = map[string]string{"egress": "true", "role": "gateway"}

		ctrl.enqueueUpdateNode(oldNode, newNode)

		require.Empty(t, drainVpcQueue(t, ctrl))
	})

	t.Run("default selector does not enqueue vpc on label change", func(t *testing.T) {
		ctrl := prepareNodeQueueTestController(t, newBFDPortVpcWithoutNodeSelector("default-vpc"))
		oldNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "node-a",
				Labels: map[string]string{"role": "worker"},
			},
		}
		newNode := oldNode.DeepCopy()
		newNode.Labels = map[string]string{"role": "gateway"}

		ctrl.enqueueUpdateNode(oldNode, newNode)

		require.Empty(t, drainVpcQueue(t, ctrl))
	})
}

func TestEnqueueDeleteNodeEnqueuesMatchingVpcBFDPort(t *testing.T) {
	ctrl := prepareNodeQueueTestController(t, newBFDPortVpc("selected-vpc", map[string]string{"egress": "true"}))
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "node-a",
			Labels: map[string]string{"egress": "true"},
		},
	}

	ctrl.enqueueDeleteNode(cache.DeletedFinalStateUnknown{Obj: node})

	require.Equal(t, []string{"selected-vpc"}, drainVpcQueue(t, ctrl))
}
