package v1

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/stretchr/testify/require"
)

type DeepCopy[T any] interface {
	DeepCopy() T
}

type DeepCopyObject[T any] interface {
	DeepCopy[T]
	runtime.Object
	DeepCopyObject() runtime.Object
}

func deepCopyTestHelper[T any](t *testing.T, in DeepCopy[T]) {
	t.Helper()
	require.Equal(t, in, in.DeepCopy())
}

func deepCopyObjectTestHelper[T any](t *testing.T, in DeepCopyObject[T]) {
	t.Helper()

	err := gofakeit.Struct(in)
	require.NoError(t, err)

	deepCopyTestHelper(t, in)
	require.Equal(t, in.(runtime.Object), in.DeepCopyObject())
}

func TestDeepCopyObject(t *testing.T) {
	deepCopyObjectTestHelper(t, &IP{})
	deepCopyObjectTestHelper(t, &IPList{})
	deepCopyObjectTestHelper(t, &IPPool{})
	deepCopyObjectTestHelper(t, &IPPoolList{})
	deepCopyObjectTestHelper(t, &IptablesDnatRule{})
	deepCopyObjectTestHelper(t, &IptablesDnatRuleList{})
	deepCopyObjectTestHelper(t, &IptablesEIP{})
	deepCopyObjectTestHelper(t, &IptablesEIPList{})
	deepCopyObjectTestHelper(t, &IptablesFIPRule{})
	deepCopyObjectTestHelper(t, &IptablesFIPRuleList{})
	deepCopyObjectTestHelper(t, &IptablesSnatRule{})
	deepCopyObjectTestHelper(t, &IptablesSnatRuleList{})
	deepCopyObjectTestHelper(t, &OvnDnatRule{})
	deepCopyObjectTestHelper(t, &OvnDnatRuleList{})
	deepCopyObjectTestHelper(t, &OvnEip{})
	deepCopyObjectTestHelper(t, &OvnEipList{})
	deepCopyObjectTestHelper(t, &OvnFip{})
	deepCopyObjectTestHelper(t, &OvnFipList{})
	deepCopyObjectTestHelper(t, &OvnSnatRule{})
	deepCopyObjectTestHelper(t, &OvnSnatRuleList{})
	deepCopyObjectTestHelper(t, &ProviderNetwork{})
	deepCopyObjectTestHelper(t, &ProviderNetworkList{})
	deepCopyObjectTestHelper(t, &QoSPolicy{})
	deepCopyObjectTestHelper(t, &QoSPolicyList{})
	deepCopyObjectTestHelper(t, &SecurityGroup{})
	deepCopyObjectTestHelper(t, &SecurityGroupList{})
	deepCopyObjectTestHelper(t, &Subnet{})
	deepCopyObjectTestHelper(t, &SubnetList{})
	deepCopyObjectTestHelper(t, &SwitchLBRule{})
	deepCopyObjectTestHelper(t, &SwitchLBRuleList{})
	deepCopyObjectTestHelper(t, &Vip{})
	deepCopyObjectTestHelper(t, &VipList{})
	deepCopyObjectTestHelper(t, &Vlan{})
	deepCopyObjectTestHelper(t, &VlanList{})
	deepCopyObjectTestHelper(t, &Vpc{})
	deepCopyObjectTestHelper(t, &VpcList{})
	deepCopyObjectTestHelper(t, &VpcDns{})
	deepCopyObjectTestHelper(t, &VpcDnsList{})
	deepCopyObjectTestHelper(t, &VpcEgressGateway{})
	deepCopyObjectTestHelper(t, &VpcEgressGatewayList{})
	deepCopyObjectTestHelper(t, &VpcNatGateway{})
	deepCopyObjectTestHelper(t, &VpcNatGatewayList{})
}
