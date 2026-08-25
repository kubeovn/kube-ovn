package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestEnqueueAddVpcExternalLabel(t *testing.T) {
	for _, value := range []string{"", "false", "true"} {
		t.Run(value, func(t *testing.T) {
			ctrl := &Controller{
				addOrUpdateVpcQueue: newTypedRateLimitingQueue[string]("AddOrUpdateVpc", nil),
				delVpcQueue:         newTypedRateLimitingQueue[*kubeovnv1.Vpc]("DeleteVpc", nil),
			}
			t.Cleanup(ctrl.addOrUpdateVpcQueue.ShutDown)
			t.Cleanup(ctrl.delVpcQueue.ShutDown)

			ctrl.enqueueAddVpc(&kubeovnv1.Vpc{
				Name:   "vpc-a",
				Labels: map[string]string{util.VpcExternalLabel: value},
			})
			if value == "true" {
				require.Zero(t, ctrl.addOrUpdateVpcQueue.Len())
				return
			}
			require.Equal(t, 1, ctrl.addOrUpdateVpcQueue.Len())
		})
	}
}

func TestEnqueueAddTerminatingVpc(t *testing.T) {
	now := metav1.Now()
	vpc := &kubeovnv1.Vpc{Name: "vpc-a", DeletionTimestamp: &now}
	ctrl := &Controller{
		addOrUpdateVpcQueue: newTypedRateLimitingQueue[string]("AddOrUpdateVpc", nil),
		delVpcQueue:         newTypedRateLimitingQueue[*kubeovnv1.Vpc]("DeleteVpc", nil),
	}
	t.Cleanup(ctrl.addOrUpdateVpcQueue.ShutDown)
	t.Cleanup(ctrl.delVpcQueue.ShutDown)

	ctrl.enqueueAddVpc(vpc)
	require.Zero(t, ctrl.addOrUpdateVpcQueue.Len())
	require.Equal(t, 1, ctrl.delVpcQueue.Len())
}

func TestHandleDelVpcBlockedByNatRule(t *testing.T) {
	vpcName := "vpc-a"
	vpc := &kubeovnv1.Vpc{Name: vpcName}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Vpcs: []*kubeovnv1.Vpc{vpc},
		OvnDnatRules: []*kubeovnv1.OvnDnatRule{{
			Name: "dnat-a",
			Spec: kubeovnv1.OvnDnatRuleSpec{Vpc: vpcName},
		}},
	})
	require.NoError(t, err)

	require.ErrorContains(t, fc.fakeController.handleDelVpc(vpc), "still has NAT rules")
}

func TestVpcHasNatRulesIncludesStatusVpc(t *testing.T) {
	vpcName := "vpc-a"
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		OvnDnatRules: []*kubeovnv1.OvnDnatRule{{
			Name:   "dnat-a",
			Status: kubeovnv1.OvnDnatRuleStatus{Vpc: vpcName},
		}},
	})
	require.NoError(t, err)

	referenced, err := fc.fakeController.vpcHasNatRules(vpcName)
	require.NoError(t, err)
	require.True(t, referenced)
}

func TestHandleDelVpcWaitsForRouterLBRule(t *testing.T) {
	vpc := &kubeovnv1.Vpc{Name: "vpc-a"}
	rule := &kubeovnv1.RouterLBRule{
		Name: "rlr-a",
		Spec: kubeovnv1.RouterLBRuleSpec{Vpc: vpc.Name},
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Vpcs:          []*kubeovnv1.Vpc{vpc},
		RouterLBRules: []*kubeovnv1.RouterLBRule{rule},
	})
	require.NoError(t, err)

	// RouterLBRule cleanup needs vpc.Status.*LoadBalancer, so the VPC must outlive it
	require.ErrorContains(t, fc.fakeController.handleDelVpc(vpc), "still has router LB rules")
}

func TestHandleDelVpcWaitsForNatGateway(t *testing.T) {
	vpc := &kubeovnv1.Vpc{Name: "vpc-a"}
	gw := &kubeovnv1.VpcNatGateway{
		Name:              "gw-a",
		DeletionTimestamp: &metav1.Time{Time: time.Now()},
		Spec:              kubeovnv1.VpcNatGatewaySpec{Vpc: vpc.Name},
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Vpcs:           []*kubeovnv1.Vpc{vpc},
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{gw},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController

	hasNatGws, err := ctrl.hasVpcNatGateways(vpc.Name)
	require.NoError(t, err)
	require.True(t, hasNatGws)
	// deletion must not proceed to router teardown while the gateway still exists
	require.ErrorContains(t, ctrl.handleDelVpc(vpc), "still has NAT gateways")
}

func TestHasVpcSubnetsIncludesTerminatingSubnet(t *testing.T) {
	vpc := &kubeovnv1.Vpc{Name: "vpc-a"}
	subnet := &kubeovnv1.Subnet{
		Name:              "subnet-a",
		DeletionTimestamp: &metav1.Time{Time: time.Now()},
		Spec:              kubeovnv1.SubnetSpec{Vpc: vpc.Name},
	}

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Vpcs:    []*kubeovnv1.Vpc{vpc},
		Subnets: []*kubeovnv1.Subnet{subnet},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController

	hasSubnets, err := ctrl.hasVpcSubnets(vpc.Name)
	require.NoError(t, err)
	require.True(t, hasSubnets)
}
