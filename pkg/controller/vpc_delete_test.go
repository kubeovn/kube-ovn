package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

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
	ctrl.vpcKeyMutex = keymutex.NewHashed(10)

	hasSubnets, err := ctrl.hasVpcSubnets(vpc.Name)
	require.NoError(t, err)
	require.True(t, hasSubnets)
}
