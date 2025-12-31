package controller

// Unified Fake Controller for Testing
//
// This file provides a unified approach to creating fake controllers for testing.
// The main function is newFakeControllerWithOptions() which accepts optional parameters
// for subnets, NADs (Network Attachment Definitions), pods, and namespaces.
//
// The fake controller properly initializes:
// - Kubernetes fake client with pods and namespaces
// - NAD fake client with network attachment definitions (populated via API)
// - KubeOVN fake client with subnets (populated via API)
// - All necessary informers with proper synchronization
// - Mock OVN client for OVN operations

import (
	"context"
	"testing"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	nadfake "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned/fake"
	nadinformers "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/informers/externalversions"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes/fake"

	mockovs "github.com/kubeovn/kube-ovn/mocks/pkg/ovs"
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovninformerfactory "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions"
	kubeovninformer "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type fakeControllerInformers struct {
	vpcInformer       kubeovninformer.VpcInformer
	vpcNatGwInformer  kubeovninformer.VpcNatGatewayInformer
	subnetInformer    kubeovninformer.SubnetInformer
	serviceInformer   coreinformers.ServiceInformer
	namespaceInformer coreinformers.NamespaceInformer
	podInformer       coreinformers.PodInformer
}

type fakeController struct {
	fakeController *Controller
	fakeInformers  *fakeControllerInformers
	mockOvnClient  *mockovs.MockNbClient
}

func alwaysReady() bool { return true }

// FakeControllerOptions holds optional parameters for creating a fake controller
type FakeControllerOptions struct {
	Subnets            []*kubeovnv1.Subnet
	NetworkAttachments []*nadv1.NetworkAttachmentDefinition
	Pods               []*corev1.Pod
	Namespaces         []*corev1.Namespace
}

// newFakeControllerWithOptions creates a fake controller with optional pre-populated objects
func newFakeControllerWithOptions(t *testing.T, opts *FakeControllerOptions) (*fakeController, error) {
	if opts == nil {
		opts = &FakeControllerOptions{}
	}

	namespaces := opts.Namespaces
	if len(namespaces) == 0 {
		// Create default namespace if none provided
		namespaces = []*corev1.Namespace{{
			ObjectMeta: metav1.ObjectMeta{
				Name: metav1.NamespaceDefault,
				Annotations: map[string]string{
					util.LogicalSwitchAnnotation: util.DefaultSubnet,
				},
			},
		}}
	}

	// Create fake Kubernetes client with namespaces and pods
	kubeObjects := make([]runtime.Object, 0, len(namespaces)+len(opts.Pods))
	for _, ns := range namespaces {
		kubeObjects = append(kubeObjects, ns)
	}
	for _, pod := range opts.Pods {
		kubeObjects = append(kubeObjects, pod)
	}
	kubeClient := fake.NewClientset(kubeObjects...)

	// Create fake NAD client
	nadClient := nadfake.NewSimpleClientset()
	for _, nad := range opts.NetworkAttachments {
		_, err := nadClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions(nad.Namespace).Create(
			context.Background(), nad, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
	}

	// Create fake KubeOVN client
	kubeovnClient := kubeovnfake.NewClientset()
	for _, subnet := range opts.Subnets {
		_, err := kubeovnClient.KubeovnV1().Subnets().Create(
			context.Background(), subnet, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
	}

	// Create informer factories
	kubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(kubeClient, 0,
		informers.WithTransform(util.TrimManagedFields),
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.Watch = true
			options.AllowWatchBookmarks = true
		}),
	)
	serviceInformer := kubeInformerFactory.Core().V1().Services()
	namespaceInformer := kubeInformerFactory.Core().V1().Namespaces()
	podInformer := kubeInformerFactory.Core().V1().Pods()

	nadInformerFactory := nadinformers.NewSharedInformerFactoryWithOptions(nadClient, 0,
		nadinformers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.Watch = true
			options.AllowWatchBookmarks = true
		}),
	)
	nadInformer := nadInformerFactory.K8sCniCncfIo().V1().NetworkAttachmentDefinitions()

	kubeovnInformerFactory := kubeovninformerfactory.NewSharedInformerFactoryWithOptions(kubeovnClient, 0,
		kubeovninformerfactory.WithTransform(util.TrimManagedFields),
		kubeovninformerfactory.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.Watch = true
			options.AllowWatchBookmarks = true
		}),
	)
	vpcInformer := kubeovnInformerFactory.Kubeovn().V1().Vpcs()
	subnetInformer := kubeovnInformerFactory.Kubeovn().V1().Subnets()
	vpcNatGwInformer := kubeovnInformerFactory.Kubeovn().V1().VpcNatGateways()

	fakeInformers := &fakeControllerInformers{
		vpcInformer:       vpcInformer,
		vpcNatGwInformer:  vpcNatGwInformer,
		subnetInformer:    subnetInformer,
		serviceInformer:   serviceInformer,
		namespaceInformer: namespaceInformer,
		podInformer:       podInformer,
	}

	// Create mock OVN client
	mockOvnClient := mockovs.NewMockNbClient(gomock.NewController(t))

	// Create controller with all informers
	ctrl := &Controller{
		servicesLister:        serviceInformer.Lister(),
		namespacesLister:      namespaceInformer.Lister(),
		podsLister:            podInformer.Lister(),
		vpcsLister:            vpcInformer.Lister(),
		vpcSynced:             alwaysReady,
		subnetsLister:         subnetInformer.Lister(),
		subnetSynced:          alwaysReady,
		netAttachLister:       nadInformer.Lister(),
		netAttachSynced:       alwaysReady,
		OVNNbClient:           mockOvnClient,
		syncVirtualPortsQueue: newTypedRateLimitingQueue[string]("SyncVirtualPort", nil),
	}

	ctrl.config = &Configuration{
		ClusterRouter:        util.DefaultVpc,
		DefaultLogicalSwitch: util.DefaultSubnet,
		NodeSwitch:           "join",
		KubeOvnClient:        kubeovnClient,
		KubeClient:           kubeClient,
		PodNamespace:         metav1.NamespaceSystem,
		AttachNetClient:      nadClient,
	}

	// Start informers and wait for sync
	stopCh := make(chan struct{})
	t.Cleanup(func() { close(stopCh) })

	kubeInformerFactory.Start(stopCh)
	nadInformerFactory.Start(stopCh)
	kubeovnInformerFactory.Start(stopCh)

	kubeInformerFactory.WaitForCacheSync(stopCh)
	nadInformerFactory.WaitForCacheSync(stopCh)
	kubeovnInformerFactory.WaitForCacheSync(stopCh)

	return &fakeController{
		fakeController: ctrl,
		fakeInformers:  fakeInformers,
		mockOvnClient:  mockOvnClient,
	}, nil
}

// newFakeController creates a basic fake controller
func newFakeController(t *testing.T) *fakeController {
	controller, err := newFakeControllerWithOptions(t, nil)
	require.NoError(t, err)
	return controller
}

func Test_allSubnetReady(t *testing.T) {
	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{{
			ObjectMeta: metav1.ObjectMeta{Name: util.DefaultSubnet},
		}, {
			ObjectMeta: metav1.ObjectMeta{Name: "join"},
		}},
	})
	require.NoError(t, err)
	ctrl := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient

	subnets := []string{util.DefaultSubnet, "join"}

	t.Run("all subnet ready", func(t *testing.T) {
		mockOvnClient.EXPECT().LogicalSwitchExists(gomock.Any()).Return(true, nil).Times(2)

		ready, err := ctrl.allSubnetReady(subnets...)
		require.NoError(t, err)
		require.True(t, ready)
	})

	t.Run("some subnet are not ready", func(t *testing.T) {
		mockOvnClient.EXPECT().LogicalSwitchExists(subnets[0]).Return(true, nil)
		mockOvnClient.EXPECT().LogicalSwitchExists(subnets[1]).Return(false, nil)

		ready, err := ctrl.allSubnetReady(subnets...)
		require.NoError(t, err)
		require.False(t, ready)
	})
}

// TestFakeControllerWithOptions demonstrates usage of the unified fake controller
func TestFakeControllerWithOptions(t *testing.T) {
	// Example: creating a fake controller with NADs, subnets, and pods
	opts := &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{{
			ObjectMeta: metav1.ObjectMeta{Name: "net1-subnet"},
			Spec:       kubeovnv1.SubnetSpec{CIDRBlock: "192.168.1.0/24"},
		}},
		NetworkAttachments: []*nadv1.NetworkAttachmentDefinition{{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "net1",
				Namespace: metav1.NamespaceDefault,
			},
			Spec: nadv1.NetworkAttachmentDefinitionSpec{
				Config: `{"cniVersion": "0.3.1", "name": "net1", "type": "kube-ovn"}`,
			},
		}},
		Pods: []*corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: metav1.NamespaceDefault,
				Annotations: map[string]string{
					nadv1.NetworkAttachmentAnnot: `[{"name": "net1"}]`,
				},
			},
		}},
	}

	fakeCtrl, err := newFakeControllerWithOptions(t, opts)
	require.NoError(t, err)
	ctrl := fakeCtrl.fakeController

	// Verify that the fake controller was created successfully
	require.NotNil(t, ctrl)
	require.NotNil(t, ctrl.config)
	require.NotNil(t, ctrl.config.AttachNetClient)
	require.NotNil(t, ctrl.config.KubeOvnClient)

	// Verify that NADs can be retrieved
	nadClient := ctrl.config.AttachNetClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions(metav1.NamespaceDefault)
	retrievedNAD, err := nadClient.Get(context.Background(), "net1", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "net1", retrievedNAD.Name)

	// Verify that subnets can be retrieved
	subnetClient := ctrl.config.KubeOvnClient.KubeovnV1().Subnets()
	retrievedSubnet, err := subnetClient.Get(context.Background(), "net1-subnet", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "net1-subnet", retrievedSubnet.Name)
}

func Test_trimManagedFields(t *testing.T) {
	tests := []struct {
		name  string
		arg   any
		check bool
	}{{
		name: "trim managed fields from object",
		arg: &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-subnet",
				ManagedFields: []metav1.ManagedFieldsEntry{{
					Manager:   "controller",
					Operation: metav1.ManagedFieldsOperationApply,
				}},
			},
		},
		check: true,
	}, {
		name: "object without managed fields",
		arg: &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-subnet-no-managed-fields",
			},
		},
		check: true,
	}, {
		name:  "non-object input",
		arg:   "this is a string, not an object",
		check: false,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret, err := trimManagedFields(tt.arg)
			require.NoError(t, err) // trimManagedFields should not error out
			if tt.check {
				// check whether managed fields are trimmed
				accessor, err := meta.Accessor(ret)
				require.NoError(t, err)
				require.Empty(t, accessor.GetManagedFields())
			}
		})
	}
}
