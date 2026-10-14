package controller

import (
	"testing"

	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestReconcileForServiceCIDRChangeEnqueuesDataPlaneSubnets(t *testing.T) {
	t.Parallel()

	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{
			{Name: "distributed-default", Spec: kubeovnv1.SubnetSpec{Vpc: util.DefaultVpc, GatewayType: kubeovnv1.GWDistributedType}},
			{Name: "distributed-other-vpc", Spec: kubeovnv1.SubnetSpec{Vpc: "other-vpc", GatewayType: kubeovnv1.GWDistributedType}},
			{Name: "u2o-other-vpc", Spec: kubeovnv1.SubnetSpec{Vpc: "other-vpc", U2OInterconnection: true}},
			{Name: "centralized-default", Spec: kubeovnv1.SubnetSpec{Vpc: util.DefaultVpc, GatewayType: kubeovnv1.GWCentralizedType}},
		},
	})
	require.NoError(t, err)

	queue := newTypedRateLimitingQueue[string]("service-cidr-test", nil)
	t.Cleanup(queue.ShutDown)
	fc.fakeController.addOrUpdateSubnetQueue = queue

	fc.fakeController.reconcileForServiceCIDRChange()

	got := make([]string, 0, queue.Len())
	for queue.Len() > 0 {
		key, shutdown := queue.Get()
		require.False(t, shutdown)
		got = append(got, key)
		queue.Done(key)
	}
	require.ElementsMatch(t, []string{"distributed-default", "u2o-other-vpc"}, got)
}
