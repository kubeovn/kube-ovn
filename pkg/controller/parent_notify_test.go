package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// The delete queues are independent, so a child that disappears must re-enqueue its
// terminating parent. These cover the two paths that carry the parent reference in the
// event object rather than in the lister: legacy children without a finalizer, and
// tombstone (DeletedFinalStateUnknown) events after a watch gap.

func terminatingVpc(name string) *kubeovnv1.Vpc {
	now := metav1.NewTime(time.Now())
	return &kubeovnv1.Vpc{Name: name, DeletionTimestamp: &now}
}

// A terminating object belongs to its delete queue only. Many producers enqueue by name
// without checking DeletionTimestamp (node, vlan, service-cidr, and the "no subnets"
// retry in handleUpdateVpcStatus), so the add/update handlers must refuse to reconcile
// one back into existence while its delete handler is tearing it down.

func TestHandleAddOrUpdateVpcSkipsTerminating(t *testing.T) {
	vpc := terminatingVpc("vpc-a")
	vpc.Status.Router = "vpc-a"
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Vpcs: []*kubeovnv1.Vpc{vpc}})
	require.NoError(t, err)

	// A nil OVN client would panic if the handler reached router/LB creation.
	fc.fakeController.OVNNbClient = nil
	require.NoError(t, fc.fakeController.handleAddOrUpdateVpc(vpc.Name))
}

func TestHandleAddOrUpdateSubnetSkipsTerminating(t *testing.T) {
	now := metav1.NewTime(time.Now())
	subnet := &kubeovnv1.Subnet{
		Name: "subnet-a", DeletionTimestamp: &now, Finalizers: []string{util.KubeOVNControllerFinalizer},
		Spec: kubeovnv1.SubnetSpec{Vpc: "vpc-a", CIDRBlock: "10.0.0.0/24", Protocol: kubeovnv1.ProtocolIPv4},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Subnets: []*kubeovnv1.Subnet{subnet}})
	require.NoError(t, err)

	// A nil OVN client would panic if the handler reached logical switch creation.
	fc.fakeController.OVNNbClient = nil
	require.NoError(t, fc.fakeController.handleAddOrUpdateSubnet(subnet.Name))
}

func TestEnqueueDelVpcSkipsExternalVpc(t *testing.T) {
	// External VPCs mirror foreign OVN logical routers (syncExternalVpc). Their
	// Status.Router names a router kube-ovn does not own, so handleDelVpc must never
	// see them or it would tear that router down.
	tests := []struct {
		name        string
		vpc         *kubeovnv1.Vpc
		wantEnqueue bool
	}{
		{
			name: "external vpc is skipped",
			vpc: &kubeovnv1.Vpc{
				Name: "ext", Labels: map[string]string{util.VpcExternalLabel: "true"},
				Status: kubeovnv1.VpcStatus{Router: "foreign-lr", Default: false},
			},
			wantEnqueue: false,
		},
		{
			name:        "user vpc is torn down",
			vpc:         &kubeovnv1.Vpc{Name: "vpc-a"},
			wantEnqueue: true,
		},
		{
			name: "default vpc is torn down",
			vpc: &kubeovnv1.Vpc{
				Name:   "ovn-cluster",
				Status: kubeovnv1.VpcStatus{Default: true},
			},
			wantEnqueue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := &Controller{delVpcQueue: newTypedRateLimitingQueue[*kubeovnv1.Vpc]("DeleteVpc", nil)}
			t.Cleanup(ctrl.delVpcQueue.ShutDown)

			ctrl.enqueueDelVpc(tt.vpc)
			ctrl.enqueueDelVpc(cache.DeletedFinalStateUnknown{Key: tt.vpc.Name, Obj: tt.vpc})

			if tt.wantEnqueue {
				require.NotZero(t, ctrl.delVpcQueue.Len())
			} else {
				require.Zero(t, ctrl.delVpcQueue.Len())
			}
		})
	}
}

func TestEnqueueDeleteSubnetNotifiesTerminatingVpc(t *testing.T) {
	vpc := terminatingVpc("vpc-a")
	subnetOf := func(finalizers []string) *kubeovnv1.Subnet {
		return &kubeovnv1.Subnet{
			Name: "subnet-a", Finalizers: finalizers,
			Spec: kubeovnv1.SubnetSpec{Vpc: vpc.Name},
		}
	}

	tests := []struct {
		name string
		obj  any
		// legacy subnets without a finalizer still need the idempotent cleanup pass
	}{
		{
			name: "with finalizer",
			obj:  subnetOf([]string{util.KubeOVNControllerFinalizer}),
		},
		{
			name: "without finalizer",
			obj:  subnetOf(nil),
		},
		{
			name: "tombstone still carries spec.vpc",
			obj:  cache.DeletedFinalStateUnknown{Key: "subnet-a", Obj: subnetOf([]string{util.KubeOVNControllerFinalizer})},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Vpcs: []*kubeovnv1.Vpc{vpc}})
			require.NoError(t, err)
			ctrl := fc.fakeController
			ctrl.delVpcQueue = newTypedRateLimitingQueue[*kubeovnv1.Vpc]("DeleteVpc", nil)
			ctrl.deleteSubnetQueue = newTypedRateLimitingQueue[*kubeovnv1.Subnet]("DeleteSubnet", nil)
			t.Cleanup(ctrl.delVpcQueue.ShutDown)
			t.Cleanup(ctrl.deleteSubnetQueue.ShutDown)

			ctrl.enqueueDeleteSubnet(tt.obj)

			require.Zero(t, ctrl.delVpcQueue.Len(), "cleanup must finish before parent notification")
			require.Equal(t, 1, ctrl.deleteSubnetQueue.Len())
		})
	}
}

func TestEnqueueDeleteSubnetSkipsLiveVpc(t *testing.T) {
	// A subnet may be deleted on its own; that must not drag a healthy VPC into deletion.
	vpc := &kubeovnv1.Vpc{Name: "vpc-a"}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Vpcs: []*kubeovnv1.Vpc{vpc}})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.delVpcQueue = newTypedRateLimitingQueue[*kubeovnv1.Vpc]("DeleteVpc", nil)
	ctrl.deleteSubnetQueue = newTypedRateLimitingQueue[*kubeovnv1.Subnet]("DeleteSubnet", nil)
	t.Cleanup(ctrl.delVpcQueue.ShutDown)
	t.Cleanup(ctrl.deleteSubnetQueue.ShutDown)

	ctrl.enqueueDeleteSubnet(&kubeovnv1.Subnet{
		Name: "subnet-a",
		Spec: kubeovnv1.SubnetSpec{Vpc: vpc.Name},
	})

	require.Zero(t, ctrl.delVpcQueue.Len())
	require.Equal(t, 1, ctrl.deleteSubnetQueue.Len())
}

func TestEnqueueDeleteVpcNatGwNotifiesTerminatingVpc(t *testing.T) {
	vpc := terminatingVpc("vpc-a")
	gw := &kubeovnv1.VpcNatGateway{
		Name: "gw-a",
		Spec: kubeovnv1.VpcNatGatewaySpec{Vpc: vpc.Name},
	}

	for name, obj := range map[string]any{
		"delete event": gw,
		"tombstone":    cache.DeletedFinalStateUnknown{Key: "gw-a", Obj: gw},
	} {
		t.Run(name, func(t *testing.T) {
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Vpcs: []*kubeovnv1.Vpc{vpc}})
			require.NoError(t, err)
			ctrl := fc.fakeController
			ctrl.delVpcQueue = newTypedRateLimitingQueue[*kubeovnv1.Vpc]("DeleteVpc", nil)
			ctrl.delVpcNatGatewayQueue = newTypedRateLimitingQueue[*kubeovnv1.VpcNatGateway]("DeleteVpcNatGw", nil)
			t.Cleanup(ctrl.delVpcQueue.ShutDown)
			t.Cleanup(ctrl.delVpcNatGatewayQueue.ShutDown)

			ctrl.enqueueDeleteVpcNatGw(obj)

			// Cleanup completes in handleDelVpcNatGw before it wakes the parent.
			require.Zero(t, ctrl.delVpcQueue.Len())
			require.Equal(t, 1, ctrl.delVpcNatGatewayQueue.Len())
		})
	}
}

func TestEnqueueDeleteRouterLBRuleNotifiesTerminatingVpc(t *testing.T) {
	vpc := terminatingVpc("vpc-a")
	rule := &kubeovnv1.RouterLBRule{
		Name: "rlr-a",
		Spec: kubeovnv1.RouterLBRuleSpec{Vpc: vpc.Name},
	}

	for name, obj := range map[string]any{
		"delete event": rule,
		"tombstone":    cache.DeletedFinalStateUnknown{Key: "rlr-a", Obj: rule},
	} {
		t.Run(name, func(t *testing.T) {
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Vpcs: []*kubeovnv1.Vpc{vpc}})
			require.NoError(t, err)
			ctrl := fc.fakeController
			ctrl.delVpcQueue = newTypedRateLimitingQueue[*kubeovnv1.Vpc]("DeleteVpc", nil)
			ctrl.delRouterLBRuleQueue = newTypedRateLimitingQueue[*RouterLBRuleInfo]("DeleteRouterLBRule", nil)
			t.Cleanup(ctrl.delVpcQueue.ShutDown)
			t.Cleanup(ctrl.delRouterLBRuleQueue.ShutDown)

			ctrl.enqueueDeleteRouterLBRule(obj)

			// RouterLBRule has no finalizer; handleDelRouterLBRule notifies only after
			// its OVN/service cleanup succeeds.
			require.Zero(t, ctrl.delVpcQueue.Len())
			require.Equal(t, 1, ctrl.delRouterLBRuleQueue.Len())
		})
	}
}
