package controller

import (
	"context"
	"testing"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	discoverylisters "k8s.io/client-go/listers/discovery/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlisters "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func vipAttachTestSubnet(name, vpc string) *kubeovnv1.Subnet {
	return &kubeovnv1.Subnet{Name: name, Spec: kubeovnv1.SubnetSpec{Vpc: vpc}}
}

func vipAttachTestVpc(name, tcpLB string) *kubeovnv1.Vpc {
	return &kubeovnv1.Vpc{Name: name, Status: kubeovnv1.VpcStatus{TCPLoadBalancer: tcpLB}}
}

func vipAttachTestVip(name, subnet string, attach []string, annotations map[string]string) *kubeovnv1.Vip {
	return &kubeovnv1.Vip{
		Name:        name,
		Annotations: annotations,
		Spec: kubeovnv1.VipSpec{
			Subnet:        subnet,
			AttachSubnets: attach,
			Type:          util.SwitchLBRuleVip,
		},
	}
}

func Test_reconcileVipAttachSubnets(t *testing.T) {
	t.Run("home subnet not found returns error", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, nil)
		require.NoError(t, err)
		vip := vipAttachTestVip("vip1", "missing-subnet", []string{"attach1"}, nil)
		assert.Error(t, fc.fakeController.reconcileVipAttachSubnets(vip))
	})

	t.Run("vpc not found returns error", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{vipAttachTestSubnet("home", "missing-vpc")},
		})
		require.NoError(t, err)
		vip := vipAttachTestVip("vip1", "home", []string{"attach1"}, nil)
		assert.Error(t, fc.fakeController.reconcileVipAttachSubnets(vip))
	})

	t.Run("vpc with no load balancers is a no-op", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{vipAttachTestSubnet("home", "vpc1")},
			Vpcs:    []*kubeovnv1.Vpc{vipAttachTestVpc("vpc1", "")},
		})
		require.NoError(t, err)
		vip := vipAttachTestVip("vip1", "home", []string{"attach1"}, nil)
		// No mock expectations — any OVN call would fail the test.
		assert.NoError(t, fc.fakeController.reconcileVipAttachSubnets(vip))
	})

	t.Run("happy path attaches to desired subnets and persists annotation", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{vipAttachTestSubnet("home", "vpc1")},
			Vpcs:    []*kubeovnv1.Vpc{vipAttachTestVpc("vpc1", "vpc1-tcp-lb")},
		})
		require.NoError(t, err)
		vip := vipAttachTestVip("vip1", "home", []string{"attach1", "attach2"}, nil)
		_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Create(
			context.Background(), vip, metav1.CreateOptions{},
		)
		require.NoError(t, err)

		fc.mockOvnClient.EXPECT().
			LogicalSwitchUpdateLoadBalancers("attach1", ovsdb.MutateOperationInsert, "vpc1-tcp-lb").
			Return(nil)
		fc.mockOvnClient.EXPECT().
			LogicalSwitchUpdateLoadBalancers("attach2", ovsdb.MutateOperationInsert, "vpc1-tcp-lb").
			Return(nil)

		require.NoError(t, fc.fakeController.reconcileVipAttachSubnets(vip))

		updated, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Get(
			context.Background(), "vip1", metav1.GetOptions{},
		)
		require.NoError(t, err)
		assert.Equal(t, "attach1,attach2", updated.Annotations[util.VipAttachSubnetsAnnotation])
	})

	t.Run("detaches subnet dropped from desired list and keeps still-desired one", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{vipAttachTestSubnet("home", "vpc1")},
			Vpcs:    []*kubeovnv1.Vpc{vipAttachTestVpc("vpc1", "vpc1-tcp-lb")},
		})
		require.NoError(t, err)
		// Previously attached to "old" and "keep"; now only "keep" is desired.
		vip := vipAttachTestVip("vip1", "home", []string{"keep"}, map[string]string{
			util.VipAttachSubnetsAnnotation: "old,keep",
		})
		_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Create(
			context.Background(), vip, metav1.CreateOptions{},
		)
		require.NoError(t, err)

		fc.mockOvnClient.EXPECT().
			LogicalSwitchUpdateLoadBalancers("old", ovsdb.MutateOperationDelete, "vpc1-tcp-lb").
			Return(nil)
		fc.mockOvnClient.EXPECT().
			LogicalSwitchUpdateLoadBalancers("keep", ovsdb.MutateOperationInsert, "vpc1-tcp-lb").
			Return(nil)

		require.NoError(t, fc.fakeController.reconcileVipAttachSubnets(vip))

		updated, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Get(
			context.Background(), "vip1", metav1.GetOptions{},
		)
		require.NoError(t, err)
		assert.Equal(t, "keep", updated.Annotations[util.VipAttachSubnetsAnnotation])
	})
}

func Test_serviceScopedVipLoadBalancerNames(t *testing.T) {
	svc := vipAttachTestService("rule", "home")
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Services: []*corev1.Service{svc}})
	require.NoError(t, err)
	vip := &kubeovnv1.Vip{
		Name:   "vip1",
		Spec:   kubeovnv1.VipSpec{Type: util.SwitchLBRuleVip, Subnet: "home", V4ip: "10.0.0.10"},
		Status: kubeovnv1.VipStatus{V4ip: "10.0.0.10"},
	}
	names, err := fc.fakeController.serviceScopedVipLoadBalancerNames(vip)
	require.NoError(t, err)
	require.Len(t, names, 1)
	assert.Equal(t, serviceScopedExternalLBName(svc, corev1.ProtocolTCP, "10.0.0.10"), names[0])
}

func Test_reconcileVipAttachSubnetsUsesServiceScopedLoadBalancer(t *testing.T) {
	svc := vipAttachTestService("rule", "home")
	vip := &kubeovnv1.Vip{
		Name:   "vip1",
		Spec:   kubeovnv1.VipSpec{Type: util.SwitchLBRuleVip, Subnet: "home", V4ip: "10.0.0.10", AttachSubnets: []string{"attached"}},
		Status: kubeovnv1.VipStatus{V4ip: "10.0.0.10"},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Services: []*corev1.Service{svc},
		Subnets:  []*kubeovnv1.Subnet{vipAttachTestSubnet("home", "vpc1")},
		Vpcs:     []*kubeovnv1.Vpc{vipAttachTestVpc("vpc1", "")},
	})
	require.NoError(t, err)
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, indexer.Add(vip))
	fc.fakeController.virtualIpsLister = kubeovnlisters.NewVipLister(indexer)
	_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Create(context.Background(), vip, metav1.CreateOptions{})
	require.NoError(t, err)
	lbName := serviceScopedExternalLBName(svc, corev1.ProtocolTCP, "10.0.0.10")
	fc.mockOvnClient.EXPECT().LogicalSwitchUpdateLoadBalancers("attached", ovsdb.MutateOperationInsert, lbName).Return(nil)

	require.NoError(t, fc.fakeController.reconcileVipAttachSubnets(vip))
	updated, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Get(context.Background(), "vip1", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "attached", updated.Annotations[util.VipAttachSubnetsAnnotation])
}

func Test_reconcileVipAttachSubnetsPreservesAnnotations(t *testing.T) {
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{vipAttachTestSubnet("home", "vpc1")},
		Vpcs:    []*kubeovnv1.Vpc{vipAttachTestVpc("vpc1", "vpc1-tcp-lb")},
	})
	require.NoError(t, err)
	vip := vipAttachTestVip("vip1", "home", []string{"attach1"}, map[string]string{
		util.VipAttachSubnetsAnnotation: "attach1",
		"unrelated":                     "keep-me",
	})
	_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Create(
		context.Background(), vip, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	fc.mockOvnClient.EXPECT().
		LogicalSwitchUpdateLoadBalancers("attach1", ovsdb.MutateOperationInsert, "vpc1-tcp-lb").
		Return(nil)

	require.NoError(t, fc.fakeController.reconcileVipAttachSubnets(vip))

	updated, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Get(
		context.Background(), "vip1", metav1.GetOptions{},
	)
	require.NoError(t, err)
	assert.Equal(t, "keep-me", updated.Annotations["unrelated"], "unrelated annotations must survive a no-op reconcile")
}

func Test_reconcileVipAttachSubnetsErrors(t *testing.T) {
	t.Run("detach error is propagated before attach loop runs", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{vipAttachTestSubnet("home", "vpc1")},
			Vpcs:    []*kubeovnv1.Vpc{vipAttachTestVpc("vpc1", "vpc1-tcp-lb")},
		})
		require.NoError(t, err)
		vip := vipAttachTestVip("vip1", "home", []string{"keep"}, map[string]string{
			util.VipAttachSubnetsAnnotation: "old,keep",
		})

		fc.mockOvnClient.EXPECT().
			LogicalSwitchUpdateLoadBalancers("old", ovsdb.MutateOperationDelete, "vpc1-tcp-lb").
			Return(assert.AnError)
		// No INSERT expectation — attach loop must not run once detach fails.

		assert.Error(t, fc.fakeController.reconcileVipAttachSubnets(vip))
	})

	t.Run("attach error is propagated", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{vipAttachTestSubnet("home", "vpc1")},
			Vpcs:    []*kubeovnv1.Vpc{vipAttachTestVpc("vpc1", "vpc1-tcp-lb")},
		})
		require.NoError(t, err)
		vip := vipAttachTestVip("vip1", "home", []string{"attach1"}, nil)

		fc.mockOvnClient.EXPECT().
			LogicalSwitchUpdateLoadBalancers("attach1", ovsdb.MutateOperationInsert, "vpc1-tcp-lb").
			Return(assert.AnError)

		assert.Error(t, fc.fakeController.reconcileVipAttachSubnets(vip))
	})
}

func Test_detachAllVipAttachSubnets(t *testing.T) {
	t.Run("nothing to detach is a no-op", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, nil)
		require.NoError(t, err)
		vip := vipAttachTestVip("vip1", "home", nil, nil)
		// No lister data and no mock expectations — the function must return
		// before ever touching the subnet/vpc listers.
		assert.NoError(t, fc.fakeController.detachAllVipAttachSubnets(vip))
	})

	t.Run("home subnet not found returns error", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, nil)
		require.NoError(t, err)
		vip := vipAttachTestVip("vip1", "missing-subnet", []string{"attach1"}, nil)
		assert.Error(t, fc.fakeController.detachAllVipAttachSubnets(vip))
	})

	t.Run("vpc not found returns error", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{vipAttachTestSubnet("home", "missing-vpc")},
		})
		require.NoError(t, err)
		vip := vipAttachTestVip("vip1", "home", []string{"attach1"}, nil)
		assert.Error(t, fc.fakeController.detachAllVipAttachSubnets(vip))
	})

	t.Run("vpc with no load balancers is a no-op", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{vipAttachTestSubnet("home", "vpc1")},
			Vpcs:    []*kubeovnv1.Vpc{vipAttachTestVpc("vpc1", "")},
		})
		require.NoError(t, err)
		vip := vipAttachTestVip("vip1", "home", []string{"attach1"}, nil)
		assert.NoError(t, fc.fakeController.detachAllVipAttachSubnets(vip))
	})

	t.Run("union of spec and annotation subnets is deduped and detached", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{vipAttachTestSubnet("home", "vpc1")},
			Vpcs:    []*kubeovnv1.Vpc{vipAttachTestVpc("vpc1", "vpc1-tcp-lb")},
		})
		require.NoError(t, err)
		vip := vipAttachTestVip("vip1", "home", []string{"shared", "spec-only"}, map[string]string{
			util.VipAttachSubnetsAnnotation: "shared,anno-only",
		})

		fc.mockOvnClient.EXPECT().
			LogicalSwitchUpdateLoadBalancers("shared", ovsdb.MutateOperationDelete, "vpc1-tcp-lb").
			Return(nil).Times(1)
		fc.mockOvnClient.EXPECT().
			LogicalSwitchUpdateLoadBalancers("spec-only", ovsdb.MutateOperationDelete, "vpc1-tcp-lb").
			Return(nil)
		fc.mockOvnClient.EXPECT().
			LogicalSwitchUpdateLoadBalancers("anno-only", ovsdb.MutateOperationDelete, "vpc1-tcp-lb").
			Return(nil)

		assert.NoError(t, fc.fakeController.detachAllVipAttachSubnets(vip))
	})

	t.Run("OVN error is propagated", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{vipAttachTestSubnet("home", "vpc1")},
			Vpcs:    []*kubeovnv1.Vpc{vipAttachTestVpc("vpc1", "vpc1-tcp-lb")},
		})
		require.NoError(t, err)
		vip := vipAttachTestVip("vip1", "home", []string{"attach1"}, nil)

		fc.mockOvnClient.EXPECT().
			LogicalSwitchUpdateLoadBalancers("attach1", ovsdb.MutateOperationDelete, "vpc1-tcp-lb").
			Return(assert.AnError)

		assert.Error(t, fc.fakeController.detachAllVipAttachSubnets(vip))
	})
}

func Test_enqueueUpdateVirtualIP_attachSubnetsChange(t *testing.T) {
	fc, err := newFakeControllerWithOptions(t, nil)
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.updateVirtualIPQueue = newTypedRateLimitingQueue[string]("UpdateVirtualIPTest", nil)
	ctrl.updateVirtualParentsQueue = newTypedRateLimitingQueue[string]("UpdateVirtualParentsTest", nil)

	base := &kubeovnv1.Vip{
		Name: "vip1",
		Spec: kubeovnv1.VipSpec{
			Subnet:        "subnet1",
			AttachSubnets: []string{"attach1"},
		},
	}

	t.Run("no change does not enqueue", func(t *testing.T) {
		newVip := base.DeepCopy()
		ctrl.enqueueUpdateVirtualIP(base, newVip)
		assert.Equal(t, 0, ctrl.updateVirtualIPQueue.Len())
		assert.Equal(t, 0, ctrl.updateVirtualParentsQueue.Len())
	})

	t.Run("AttachSubnets change enqueues update", func(t *testing.T) {
		newVip := base.DeepCopy()
		newVip.Spec.AttachSubnets = []string{"attach1", "attach2"}
		ctrl.enqueueUpdateVirtualIP(base, newVip)

		require.Equal(t, 1, ctrl.updateVirtualIPQueue.Len())
		item, _ := ctrl.updateVirtualIPQueue.Get()
		ctrl.updateVirtualIPQueue.Done(item)
		assert.Equal(t, "vip1", item)
		assert.Equal(t, 0, ctrl.updateVirtualParentsQueue.Len())
	})
}

func Test_reconcileVipAttachSubnetsDoesNotDetachSharedSubnet(t *testing.T) {
	current := &kubeovnv1.Vip{
		Name:        "current",
		Annotations: map[string]string{util.VipAttachSubnetsAnnotation: "shared"},
		Spec:        kubeovnv1.VipSpec{Type: util.SwitchLBRuleVip, Subnet: "home", AttachSubnets: nil},
	}
	other := &kubeovnv1.Vip{
		Name: "other",
		Spec: kubeovnv1.VipSpec{Type: util.SwitchLBRuleVip, Subnet: "home", AttachSubnets: []string{"shared"}},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{vipAttachTestSubnet("home", "vpc1"), vipAttachTestSubnet("shared", "vpc1")},
		Vpcs:    []*kubeovnv1.Vpc{{Name: "vpc1", Status: kubeovnv1.VpcStatus{TCPLoadBalancer: "tcp-lb"}}},
	})
	require.NoError(t, err)
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, indexer.Add(other))
	fc.fakeController.virtualIpsLister = kubeovnlisters.NewVipLister(indexer)

	// The other VIP still owns the shared subnet, so no delete transaction is allowed.
	require.NoError(t, fc.fakeController.reconcileVipAttachSubnets(current))
}

func Test_handleAddVirtualIPRetriesAttachAfterStatusAllocation(t *testing.T) {
	vip := &kubeovnv1.Vip{
		Name:   "vip1",
		Spec:   kubeovnv1.VipSpec{Type: util.SwitchLBRuleVip, Subnet: "home", AttachSubnets: []string{"target"}},
		Status: kubeovnv1.VipStatus{Mac: "00:00:00:00:00:01"},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{{Name: "home", Spec: kubeovnv1.SubnetSpec{Vpc: "vpc1"}}},
		Vpcs:    []*kubeovnv1.Vpc{{Name: "vpc1", Status: kubeovnv1.VpcStatus{TCPLoadBalancer: "tcp-lb"}}},
	})
	require.NoError(t, err)
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, indexer.Add(vip))
	fc.fakeController.virtualIpsLister = kubeovnlisters.NewVipLister(indexer)
	fc.mockOvnClient.EXPECT().LogicalSwitchPortExists("home-vpc1").Return(false, nil)
	fc.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(gomock.Any()).Return(nil)
	fc.mockOvnClient.EXPECT().LogicalSwitchUpdateLoadBalancers("target", ovsdb.MutateOperationInsert, "tcp-lb").Return(nil)
	require.NoError(t, fc.fakeController.handleAddVirtualIP(vip.Name))
}

func Test_handleUpdateVirtualIPTypeChangeDetachesAttachSubnets(t *testing.T) {
	vip := &kubeovnv1.Vip{
		Name:        "vip1",
		Finalizers:  []string{util.KubeOVNControllerFinalizer},
		Annotations: map[string]string{util.VipAttachSubnetsAnnotation: "target"},
		Spec:        kubeovnv1.VipSpec{Type: "router_lb_vip", Subnet: "home", MacAddress: "00:00:00:00:00:01", AttachSubnets: []string{"target"}},
		Status:      kubeovnv1.VipStatus{Mac: "00:00:00:00:00:01", Type: util.SwitchLBRuleVip},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{vipAttachTestSubnet("home", "vpc1")},
		Vpcs:    []*kubeovnv1.Vpc{vipAttachTestVpc("vpc1", "tcp-lb")},
	})
	require.NoError(t, err)
	_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Create(
		context.Background(), vip, metav1.CreateOptions{},
	)
	require.NoError(t, err)
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, indexer.Add(vip))
	fc.fakeController.virtualIpsLister = kubeovnlisters.NewVipLister(indexer)
	fc.mockOvnClient.EXPECT().
		LogicalSwitchUpdateLoadBalancers("target", ovsdb.MutateOperationDelete, "tcp-lb").
		Return(nil)
	fc.mockOvnClient.EXPECT().LogicalSwitchPortExists("home-vpc1").Return(false, nil)

	require.NoError(t, fc.fakeController.handleUpdateVirtualIP(vip.Name))
}

func Test_enqueueUpdateVpcLoadBalancerChangeRequeuesVips(t *testing.T) {
	vip := &kubeovnv1.Vip{
		Name: "vip1",
		Spec: kubeovnv1.VipSpec{Type: util.SwitchLBRuleVip, Subnet: "home"},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Subnets: []*kubeovnv1.Subnet{{Name: "home", Spec: kubeovnv1.SubnetSpec{Vpc: "vpc1"}}},
	})
	require.NoError(t, err)
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, indexer.Add(vip))
	fc.fakeController.virtualIpsLister = kubeovnlisters.NewVipLister(indexer)
	fc.fakeController.updateVirtualIPQueue = newTypedRateLimitingQueue[string]("VipVpcStatus", nil)
	oldVpc := &kubeovnv1.Vpc{Name: "vpc1", Status: kubeovnv1.VpcStatus{TCPLoadBalancer: "old"}}
	newVpc := oldVpc.DeepCopy()
	newVpc.Status.TCPLoadBalancer = "new"
	fc.fakeController.enqueueUpdateVpc(oldVpc, newVpc)
	item, shutdown := fc.fakeController.updateVirtualIPQueue.Get()
	require.False(t, shutdown)
	require.Equal(t, "vip1", item)
	fc.fakeController.updateVirtualIPQueue.Done(item)
}

func vipAttachTestService(name, subnet string) *corev1.Service {
	svc := &corev1.Service{
		Name: "slr-" + name, Namespace: "default",
		Annotations: map[string]string{
			util.SwitchLBRuleVipsAnnotation: "10.0.0.10",
			util.LogicalSwitchAnnotation:    subnet,
		},
		Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Protocol: corev1.ProtocolTCP}}},
	}
	setServiceScopedLBOwner(svc, switchLBRuleLBOwnerKind, name, name+"-uid")
	return svc
}

func TestVipScopedAttachmentsIsolateNetworks(t *testing.T) {
	for _, address := range []string{"10.0.0.10", "fd00::10"} {
		t.Run(address, func(t *testing.T) {
			own := vipAttachTestService("own", "home")
			other := vipAttachTestService("other", "other-home")
			own.Annotations[util.SwitchLBRuleVipsAnnotation] = address
			other.Annotations[util.SwitchLBRuleVipsAnnotation] = address
			vip := vipAttachTestVip("vip1", "home", []string{"attached"}, nil)
			foreignVip := vipAttachTestVip("vip2", "other-home", []string{"foreign-attached"}, nil)
			for _, item := range []*kubeovnv1.Vip{vip, foreignVip} {
				item.Spec.V4ip, item.Spec.V6ip = util.SplitStringIP(address)
			}
			fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Services: []*corev1.Service{own, other},
				Subnets:  []*kubeovnv1.Subnet{vipAttachTestSubnet("home", "vpc1"), vipAttachTestSubnet("other-home", "vpc2")},
				Vpcs:     []*kubeovnv1.Vpc{vipAttachTestVpc("vpc1", ""), vipAttachTestVpc("vpc2", "")},
			})
			require.NoError(t, err)
			ctrl := fc.fakeController
			indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			require.NoError(t, indexer.Add(vip))
			require.NoError(t, indexer.Add(foreignVip))
			ctrl.virtualIpsLister = kubeovnlisters.NewVipLister(indexer)
			ctrl.addOrUpdateEndpointSliceQueue = newTypedRateLimitingQueue[string]("VipNetworks", nil)
			t.Cleanup(ctrl.addOrUpdateEndpointSliceQueue.ShutDown)

			names, err := ctrl.serviceScopedVipLoadBalancerNames(vip)
			require.NoError(t, err)
			assert.Equal(t, []string{serviceScopedExternalLBName(own, corev1.ProtocolTCP, address)}, names)
			subnets, err := ctrl.serviceScopedVipAttachSubnets(own, "home")
			require.NoError(t, err)
			assert.Equal(t, []string{"attached"}, subnets)
			_, err = ctrl.config.KubeOvnClient.KubeovnV1().Vips().Create(t.Context(), vip, metav1.CreateOptions{})
			require.NoError(t, err)
			fc.mockOvnClient.EXPECT().LogicalSwitchUpdateLoadBalancers("attached", ovsdb.MutateOperationInsert, names[0]).Return(nil)
			require.NoError(t, ctrl.reconcileVipAttachSubnets(vip))
			require.Equal(t, 1, ctrl.addOrUpdateEndpointSliceQueue.Len())
			key, shutdown := ctrl.addOrUpdateEndpointSliceQueue.Get()
			require.False(t, shutdown)
			assert.Equal(t, "default/slr-own", key)
			ctrl.addOrUpdateEndpointSliceQueue.Done(key)
		})
	}
}

func TestVipTypeChangeDetachesScopedLoadBalancer(t *testing.T) {
	vip := vipAttachTestVip("vip1", "home", []string{"target"}, map[string]string{util.VipAttachSubnetsAnnotation: "target"})
	vip.Spec.Type = "router_lb_vip"
	vip.Spec.V4ip = "10.0.0.10"
	vip.Spec.MacAddress = "00:00:00:00:00:01"
	vip.Status = kubeovnv1.VipStatus{Type: util.SwitchLBRuleVip, V4ip: vip.Spec.V4ip, Mac: vip.Spec.MacAddress}
	vip.Finalizers = []string{util.KubeOVNControllerFinalizer}
	svc := vipAttachTestService("own", "home")
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Services: []*corev1.Service{svc},
		Subnets:  []*kubeovnv1.Subnet{vipAttachTestSubnet("home", "vpc1"), vipAttachTestSubnet("target", "vpc2")},
		Vpcs:     []*kubeovnv1.Vpc{vipAttachTestVpc("vpc1", "")},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	_, err = ctrl.config.KubeOvnClient.KubeovnV1().Vips().Create(t.Context(), vip, metav1.CreateOptions{})
	require.NoError(t, err)
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, indexer.Add(vip))
	ctrl.virtualIpsLister = kubeovnlisters.NewVipLister(indexer)
	ctrl.addOrUpdateEndpointSliceQueue = newTypedRateLimitingQueue[string]("VipTypeChange", nil)
	t.Cleanup(ctrl.addOrUpdateEndpointSliceQueue.ShutDown)
	lbName := serviceScopedExternalLBName(svc, corev1.ProtocolTCP, vip.Spec.V4ip)
	fc.mockOvnClient.EXPECT().LogicalSwitchUpdateLoadBalancers("target", ovsdb.MutateOperationDelete, lbName).Return(nil)
	fc.mockOvnClient.EXPECT().LogicalSwitchPortExists("home-vpc1").Return(false, nil)

	require.NoError(t, ctrl.handleUpdateVirtualIP(vip.Name))
	updated, err := ctrl.config.KubeOvnClient.KubeovnV1().Vips().Get(t.Context(), vip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.NotContains(t, updated.Annotations, util.VipAttachSubnetsAnnotation)
	require.Equal(t, 1, ctrl.addOrUpdateEndpointSliceQueue.Len())
	key, shutdown := ctrl.addOrUpdateEndpointSliceQueue.Get()
	require.False(t, shutdown)
	assert.Equal(t, "default/slr-own", key)
	ctrl.addOrUpdateEndpointSliceQueue.Done(key)
	// The Service worker must not restore attachments from the VIP's old type.
	subnets, err := ctrl.serviceScopedVipAttachSubnets(svc, "home")
	require.NoError(t, err)
	assert.Empty(t, subnets)
}

func TestVipScopedAttachmentsResolveDefaultNetwork(t *testing.T) {
	svc := vipAttachTestService("own", "")
	spoofed := vipAttachTestService("spoofed", util.DefaultSubnet)
	spoofed.OwnerReferences = nil
	vip := vipAttachTestVip("vip1", util.DefaultSubnet, []string{"attached"}, nil)
	vip.Spec.V4ip = "10.0.0.10"
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{Services: []*corev1.Service{svc, spoofed}})
	require.NoError(t, err)
	ctrl := fc.fakeController
	names, err := ctrl.serviceScopedVipLoadBalancerNames(vip)
	require.NoError(t, err)
	assert.Equal(t, []string{serviceScopedExternalLBName(svc, corev1.ProtocolTCP, vip.Spec.V4ip)}, names)

	vip.Spec.Subnet = "other-home"
	names, err = ctrl.serviceScopedVipLoadBalancerNames(vip)
	require.NoError(t, err)
	assert.Empty(t, names)
}

func TestVipScopedAttachmentsSkipDeletingVip(t *testing.T) {
	svc := vipAttachTestService("own", "home")
	vip := vipAttachTestVip("vip1", "home", []string{"attached"}, nil)
	vip.Spec.V4ip = "10.0.0.10"
	vip.DeletionTimestamp = new(metav1.Now())
	fc, err := newFakeControllerWithOptions(t, nil)
	require.NoError(t, err)
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, indexer.Add(vip))
	fc.fakeController.virtualIpsLister = kubeovnlisters.NewVipLister(indexer)

	subnets, err := fc.fakeController.serviceScopedVipAttachSubnets(svc, "home")
	require.NoError(t, err)
	assert.Empty(t, subnets)
}

func TestVipScopedAttachmentsResolveSecondaryNetwork(t *testing.T) {
	svc := vipAttachTestService("own", "")
	svc.Spec.Selector = map[string]string{"app": "backend"}
	vip := vipAttachTestVip("vip1", "secondary", []string{"attached"}, nil)
	vip.Spec.V4ip = "10.0.0.10"
	pod := &corev1.Pod{
		Name: "backend", Namespace: "default", Labels: svc.Spec.Selector,
		Annotations: map[string]string{
			nadv1.NetworkAttachmentAnnot:                    `[{"name":"net1"}]`,
			"net1.default.ovn.kubernetes.io/logical_switch": "secondary",
			"net1.default.ovn.kubernetes.io/logical_router": "vpc1",
			"net1.default.ovn.kubernetes.io/ip_address":     "192.168.1.10",
		},
		Status: corev1.PodStatus{PodIP: "10.244.0.5"},
	}
	fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Services: []*corev1.Service{svc}, Pods: []*corev1.Pod{pod},
		Subnets: []*kubeovnv1.Subnet{{Name: "secondary", Spec: kubeovnv1.SubnetSpec{
			Vpc: "vpc1", Provider: "net1.default.ovn", CIDRBlock: "192.168.1.0/24",
		}}},
		NetworkAttachments: []*nadv1.NetworkAttachmentDefinition{{
			Name: "net1", Namespace: "default",
			Spec: nadv1.NetworkAttachmentDefinitionSpec{Config: `{"cniVersion":"0.3.1","name":"net1","type":"kube-ovn","provider":"net1.default.ovn"}`},
		}},
	})
	require.NoError(t, err)
	ctrl := fc.fakeController
	ctrl.config.EnableNonPrimaryCNI = true
	endpointSlice := &discoveryv1.EndpointSlice{
		Name: "backends", Namespace: svc.Namespace, AddressType: discoveryv1.AddressTypeIPv4,
		Labels: map[string]string{discoveryv1.LabelServiceName: svc.Name},
		Endpoints: []discoveryv1.Endpoint{{
			Addresses: []string{pod.Status.PodIP},
			TargetRef: &corev1.ObjectReference{Kind: util.KindPod, Namespace: pod.Namespace, Name: pod.Name},
		}},
	}
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, indexer.Add(endpointSlice))
	ctrl.endpointSlicesLister = discoverylisters.NewEndpointSliceLister(indexer)

	names, err := ctrl.serviceScopedVipLoadBalancerNames(vip)
	require.NoError(t, err)
	assert.Equal(t, []string{serviceScopedExternalLBName(svc, corev1.ProtocolTCP, vip.Spec.V4ip)}, names)
	assert.Equal(t, []string{pod.Status.PodIP}, endpointSlice.Endpoints[0].Addresses, "informer objects must remain unchanged")
	vip.Spec.Subnet = util.DefaultSubnet
	names, err = ctrl.serviceScopedVipLoadBalancerNames(vip)
	require.NoError(t, err)
	assert.Empty(t, names)
}
