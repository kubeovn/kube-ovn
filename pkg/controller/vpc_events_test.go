package controller

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestRecordVpcResourceError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		object runtime.Object
	}{
		{name: "Vpc", object: &kubeovnv1.Vpc{Name: "vpc-1"}},
		{name: "VpcNatGateway", object: &kubeovnv1.VpcNatGateway{Name: "nat-gw-1"}},
		{name: "VpcDns", object: &kubeovnv1.VpcDns{Name: "dns-1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			recorder := record.NewFakeRecorder(1)
			controller := &Controller{recorder: recorder}
			sourceErr := errors.New("boom")

			_ = controller.recordResourceError(tt.object, "ReconcileFailed", sourceErr)

			require.Equal(t, "Warning ReconcileFailed boom", requireRecorderEvent(t, recorder))
		})
	}
}

func TestHandleAddOrUpdateVpcRecordsFailureEvent(t *testing.T) {
	t.Parallel()

	vpc := &kubeovnv1.Vpc{
		Name: "vpc-1", Finalizers: []string{util.KubeOVNControllerFinalizer},
		Spec: kubeovnv1.VpcSpec{StaticRoutes: []*kubeovnv1.StaticRoute{{
			CIDR:       "invalid",
			NextHopIP:  "10.0.0.1",
			RouteTable: util.MainRouteTable,
		}}},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Vpcs: []*kubeovnv1.Vpc{vpc}})
	require.NoError(t, err)
	fc.fakeController.vpcKeyMutex = keymutex.NewHashed(0)
	recorder := record.NewFakeRecorder(1)
	fc.fakeController.recorder = recorder

	err = fc.fakeController.handleAddOrUpdateVpc(vpc.Name)

	require.ErrorContains(t, err, "invalid ip")
	require.Equal(t, "Warning ReconcileFailed invalid ip \"invalid\"", requireRecorderEvent(t, recorder))
}

func TestHandleDeleteVpcRecordsBlockingSubnetEvent(t *testing.T) {
	t.Parallel()

	vpc := &kubeovnv1.Vpc{
		Name:   "vpc-1",
		Status: kubeovnv1.VpcStatus{Subnets: []string{"subnet-1"}},
	}
	subnet := &kubeovnv1.Subnet{Name: "subnet-1"}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Vpcs:    []*kubeovnv1.Vpc{vpc},
		Subnets: []*kubeovnv1.Subnet{subnet},
	})
	require.NoError(t, err)
	fc.fakeController.vpcKeyMutex = keymutex.NewHashed(0)
	recorder := record.NewFakeRecorder(1)
	fc.fakeController.recorder = recorder

	err = fc.fakeController.handleDelVpc(vpc)

	require.ErrorContains(t, err, "please delete subnet subnet-1 first")
	require.Equal(t,
		"Warning DeleteFailed failed to delete vpc vpc-1, please delete subnet subnet-1 first",
		requireRecorderEvent(t, recorder),
	)
}

func TestHandleAddOrUpdateVpcNatGatewayRecordsFailureEvent(t *testing.T) {
	gw := &kubeovnv1.VpcNatGateway{
		Name: "nat-gw-1", Finalizers: []string{util.KubeOVNControllerFinalizer},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{VpcNatGateways: []*kubeovnv1.VpcNatGateway{gw}})
	require.NoError(t, err)
	fc.fakeController.vpcNatGwKeyMutex = keymutex.NewHashed(0)
	recorder := record.NewFakeRecorder(1)
	fc.fakeController.recorder = recorder

	oldEnabled := vpcNatEnabled
	vpcNatEnabled = "false"
	t.Cleanup(func() { vpcNatEnabled = oldEnabled })

	err = fc.fakeController.handleAddOrUpdateVpcNatGw(gw.Name)

	require.ErrorContains(t, err, "iptables nat gw not enable")
	require.Equal(t, "Warning ReconcileFailed iptables nat gw not enable", requireRecorderEvent(t, recorder))
}

func TestHandleInitVpcNatGatewayRecordsFailureEvent(t *testing.T) {
	gw := &kubeovnv1.VpcNatGateway{Name: "nat-gw-1"}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{VpcNatGateways: []*kubeovnv1.VpcNatGateway{gw}})
	require.NoError(t, err)
	recorder := record.NewFakeRecorder(1)
	fc.fakeController.recorder = recorder

	oldEnabled := vpcNatEnabled
	vpcNatEnabled = "false"
	t.Cleanup(func() { vpcNatEnabled = oldEnabled })

	err = fc.fakeController.handleInitVpcNatGw(gw.Name)

	require.ErrorContains(t, err, "iptables nat gw not enable")
	require.Equal(t, "Warning InitializeFailed iptables nat gw not enable", requireRecorderEvent(t, recorder))
}

func TestHandleAddOrUpdateVpcDNSRecordsFailureAndInactiveStatus(t *testing.T) {
	dns := &kubeovnv1.VpcDns{
		Name:   "dns-1",
		Status: kubeovnv1.VpcDNSStatus{Active: true},
	}
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, indexer.Add(dns))
	client := kubeovnfake.NewSimpleClientset(dns)
	var updatedDNS *kubeovnv1.VpcDns
	client.PrependReactor("update", "vpc-dnses", func(action ktesting.Action) (bool, runtime.Object, error) {
		updatedDNS = action.(ktesting.UpdateAction).GetObject().(*kubeovnv1.VpcDns).DeepCopy()
		return true, updatedDNS, nil
	})
	recorder := record.NewFakeRecorder(1)
	controller := &Controller{
		config:       &Configuration{KubeOvnClient: client},
		recorder:     recorder,
		vpcDNSLister: kubeovnlister.NewVpcDnsLister(indexer),
	}

	oldEnabled, oldImage := enableCoreDNS, corednsImage
	enableCoreDNS, corednsImage = true, ""
	t.Cleanup(func() {
		enableCoreDNS, corednsImage = oldEnabled, oldImage
	})

	err := controller.handleAddOrUpdateVPCDNS(dns.Name)

	require.ErrorContains(t, err, "coredns image should be set")
	require.Equal(t, "Warning ReconcileFailed vpc-dns coredns image should be set", requireRecorderEvent(t, recorder))
	require.NotNil(t, updatedDNS)
	require.False(t, updatedDNS.Status.Active)
}

func TestRecordVpcResourceEventAllowsNilRecorder(t *testing.T) {
	t.Parallel()

	controller := &Controller{}
	require.NotPanics(t, func() {
		controller.recordResourceEvent(&kubeovnv1.Vpc{}, corev1.EventTypeNormal, "ReconcileSuccess", "done")
	})
}
