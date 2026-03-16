package speaker

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/util/workqueue"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// initTestQueue initializes only the eipQueue for testing purposes,
// without requiring informer factory or other dependencies.
func (c *Controller) initTestQueue() {
	c.eipQueue = workqueue.NewTypedRateLimitingQueueWithConfig(
		workqueue.DefaultTypedControllerRateLimiter[string](),
		workqueue.TypedRateLimitingQueueConfig[string]{Name: "NodeRouteEIPTest"},
	)
}

func TestEnqueueAddEIP(t *testing.T) {
	tests := []struct {
		name           string
		eip            *kubeovnv1.IptablesEIP
		expectEnqueued bool
	}{
		{
			name: "ready EIP enqueued",
			eip: &kubeovnv1.IptablesEIP{
				ObjectMeta: metav1.ObjectMeta{Name: "test-eip"},
				Status:     kubeovnv1.IptablesEIPStatus{Ready: true},
			},
			expectEnqueued: true,
		},
		{
			name: "non-ready EIP skipped",
			eip: &kubeovnv1.IptablesEIP{
				ObjectMeta: metav1.ObjectMeta{Name: "test-eip"},
				Status:     kubeovnv1.IptablesEIPStatus{Ready: false},
			},
			expectEnqueued: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Controller{}
			c.initTestQueue()

			c.enqueueAddNodeRouteEIP(tt.eip)

			if tt.expectEnqueued {
				assert.Equal(t, 1, c.eipQueue.Len())
			} else {
				assert.Equal(t, 0, c.eipQueue.Len())
			}

			c.eipQueue.ShutDown()
		})
	}
}

func TestEnqueueUpdateEIP(t *testing.T) {
	tests := []struct {
		name           string
		oldEIP         *kubeovnv1.IptablesEIP
		newEIP         *kubeovnv1.IptablesEIP
		expectEnqueued bool
	}{
		{
			name: "normal update enqueued",
			oldEIP: &kubeovnv1.IptablesEIP{
				ObjectMeta: metav1.ObjectMeta{Name: "test-eip"},
			},
			newEIP: &kubeovnv1.IptablesEIP{
				ObjectMeta: metav1.ObjectMeta{Name: "test-eip"},
			},
			expectEnqueued: true,
		},
		{
			name: "deleting EIP skipped",
			oldEIP: &kubeovnv1.IptablesEIP{
				ObjectMeta: metav1.ObjectMeta{Name: "test-eip"},
			},
			newEIP: &kubeovnv1.IptablesEIP{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-eip",
					DeletionTimestamp: &metav1.Time{},
				},
			},
			expectEnqueued: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Controller{}
			c.initTestQueue()

			c.enqueueUpdateNodeRouteEIP(tt.oldEIP, tt.newEIP)

			if tt.expectEnqueued {
				assert.Equal(t, 1, c.eipQueue.Len())
			} else {
				assert.Equal(t, 0, c.eipQueue.Len())
			}

			c.eipQueue.ShutDown()
		})
	}
}

// NOTE: enqueueDeleteNodeRouteEIP is not tested because it calls withdrawEIPRoutes
// which requires a running BGP server (isRouteAnnounced uses bgpServer.ListPath).

func TestHasNatGwPodOnLocalNode(t *testing.T) {
	tests := []struct {
		name       string
		eip        *kubeovnv1.IptablesEIP
		nodeName   string
		namespace  string
		pods       map[string]*corev1.Pod
		wantResult bool
	}{
		{
			name: "local pod running",
			eip: &kubeovnv1.IptablesEIP{
				ObjectMeta: metav1.ObjectMeta{Name: "test-eip"},
				Spec:       kubeovnv1.IptablesEIPSpec{NatGwDp: "test-gw"},
			},
			nodeName:  "node1",
			namespace: "kube-system",
			pods: map[string]*corev1.Pod{
				"kube-system/" + util.GenNatGwPodName("test-gw"): {
					ObjectMeta: metav1.ObjectMeta{
						Name:      util.GenNatGwPodName("test-gw"),
						Namespace: "kube-system",
					},
					Spec:   corev1.PodSpec{NodeName: "node1"},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			wantResult: true,
		},
		{
			name: "remote pod",
			eip: &kubeovnv1.IptablesEIP{
				ObjectMeta: metav1.ObjectMeta{Name: "test-eip"},
				Spec:       kubeovnv1.IptablesEIPSpec{NatGwDp: "test-gw"},
			},
			nodeName:  "node1",
			namespace: "kube-system",
			pods: map[string]*corev1.Pod{
				"kube-system/" + util.GenNatGwPodName("test-gw"): {
					ObjectMeta: metav1.ObjectMeta{
						Name:      util.GenNatGwPodName("test-gw"),
						Namespace: "kube-system",
					},
					Spec:   corev1.PodSpec{NodeName: "node2"},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			wantResult: false,
		},
		{
			name: "local pod not running",
			eip: &kubeovnv1.IptablesEIP{
				ObjectMeta: metav1.ObjectMeta{Name: "test-eip"},
				Spec:       kubeovnv1.IptablesEIPSpec{NatGwDp: "test-gw"},
			},
			nodeName:  "node1",
			namespace: "kube-system",
			pods: map[string]*corev1.Pod{
				"kube-system/" + util.GenNatGwPodName("test-gw"): {
					ObjectMeta: metav1.ObjectMeta{
						Name:      util.GenNatGwPodName("test-gw"),
						Namespace: "kube-system",
					},
					Spec:   corev1.PodSpec{NodeName: "node1"},
					Status: corev1.PodStatus{Phase: corev1.PodPending},
				},
			},
			wantResult: false,
		},
		{
			name: "missing pod",
			eip: &kubeovnv1.IptablesEIP{
				ObjectMeta: metav1.ObjectMeta{Name: "test-eip"},
				Spec:       kubeovnv1.IptablesEIPSpec{NatGwDp: "test-gw"},
			},
			nodeName:   "node1",
			namespace:  "kube-system",
			pods:       map[string]*corev1.Pod{},
			wantResult: false,
		},
		{
			name: "empty NatGwDp",
			eip: &kubeovnv1.IptablesEIP{
				ObjectMeta: metav1.ObjectMeta{Name: "test-eip"},
				Spec:       kubeovnv1.IptablesEIPSpec{NatGwDp: ""},
			},
			nodeName:   "node1",
			namespace:  "kube-system",
			pods:       map[string]*corev1.Pod{},
			wantResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Controller{
				config: &Configuration{
					NodeName:          tt.nodeName,
					VpcNatGwNamespace: tt.namespace,
				},
				gwPodsLister: &fakePodLister{pods: tt.pods, namespace: tt.namespace},
			}

			result := c.hasNatGwPodOnLocalNode(tt.eip)
			assert.Equal(t, tt.wantResult, result)
		})
	}
}

// fakePodLister implements listerv1.PodLister for testing
type fakePodLister struct {
	pods      map[string]*corev1.Pod
	namespace string
}

func (f *fakePodLister) List(_ labels.Selector) (ret []*corev1.Pod, err error) {
	for _, p := range f.pods {
		ret = append(ret, p)
	}
	return ret, nil
}

func (f *fakePodLister) Pods(namespace string) listerv1.PodNamespaceLister {
	return &fakePodNamespaceLister{pods: f.pods, namespace: namespace}
}

type fakePodNamespaceLister struct {
	pods      map[string]*corev1.Pod
	namespace string
}

func (f *fakePodNamespaceLister) List(_ labels.Selector) (ret []*corev1.Pod, err error) {
	for _, p := range f.pods {
		if p.Namespace == f.namespace {
			ret = append(ret, p)
		}
	}
	return ret, nil
}

func (f *fakePodNamespaceLister) Get(name string) (*corev1.Pod, error) {
	key := f.namespace + "/" + name
	if p, ok := f.pods[key]; ok {
		return p, nil
	}
	return nil, errors.New("pod not found")
}
