package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	networkinformers "k8s.io/client-go/informers/networking/v1"
	"k8s.io/client-go/kubernetes/fake"

	mockovs "github.com/kubeovn/kube-ovn/mocks/pkg/ovs"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovninformerfactory "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions"
	kubeovninformer "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions/kubeovn/v1"
)

type fakeControllerInformers struct {
	vpcInformer     kubeovninformer.VpcInformer
	subnetInformer  kubeovninformer.SubnetInformer
	sgInformer      kubeovninformer.SecurityGroupInformer
	serviceInformer coreinformers.ServiceInformer
	npInformer      networkinformers.NetworkPolicyInformer
	nodeInformer    coreinformers.NodeInformer
}

type fakeController struct {
	fakeController *Controller
	fakeInformers  *fakeControllerInformers
	mockOvnClient  *mockovs.MockNbClient
}

func alwaysReady() bool { return true }

func newFakeController(t *testing.T) *fakeController {
	/* fake kube client */
	kubeClient := fake.NewSimpleClientset()
	kubeInformerFactory := informers.NewSharedInformerFactory(kubeClient, 0)
	serviceInformer := kubeInformerFactory.Core().V1().Services()
	npInformer := kubeInformerFactory.Networking().V1().NetworkPolicies()
	nodeInformer := kubeInformerFactory.Core().V1().Nodes()

	/* fake kube ovn client */
	kubeovnClient := kubeovnfake.NewSimpleClientset()
	kubeovnInformerFactory := kubeovninformerfactory.NewSharedInformerFactory(kubeovnClient, 0)
	vpcInformer := kubeovnInformerFactory.Kubeovn().V1().Vpcs()
	subnetInformer := kubeovnInformerFactory.Kubeovn().V1().Subnets()
	sgInformer := kubeovnInformerFactory.Kubeovn().V1().SecurityGroups()

	fakeInformers := &fakeControllerInformers{
		vpcInformer:     vpcInformer,
		subnetInformer:  subnetInformer,
		sgInformer:      sgInformer,
		serviceInformer: serviceInformer,
		npInformer:      npInformer,
		nodeInformer:    nodeInformer,
	}

	/* ovn fake client */
	mockOvnClient := mockovs.NewMockNbClient(gomock.NewController(t))

	ctrl := &Controller{
		servicesLister:        serviceInformer.Lister(),
		npsLister:             npInformer.Lister(),
		nodesLister:           nodeInformer.Lister(),
		vpcsLister:            vpcInformer.Lister(),
		sgsLister:             sgInformer.Lister(),
		vpcSynced:             alwaysReady,
		subnetsLister:         subnetInformer.Lister(),
		subnetSynced:          alwaysReady,
		OVNNbClient:           mockOvnClient,
		syncVirtualPortsQueue: newTypedRateLimitingQueue[string]("SyncVirtualPort", nil),
	}

	ctrl.config = &Configuration{
		ClusterRouter:        "ovn-cluster",
		DefaultLogicalSwitch: "ovn-default",
		NodeSwitch:           "join",
		KubeOvnClient:        kubeovnClient,
	}

	return &fakeController{
		fakeController: ctrl,
		fakeInformers:  fakeInformers,
		mockOvnClient:  mockOvnClient,
	}
}

func Test_allSubnetReady(t *testing.T) {
	t.Parallel()

	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient

	subnets := []string{"ovn-default", "join"}

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
