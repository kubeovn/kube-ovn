package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func Test_reconcileVipAttachSubnets(t *testing.T) {
	makeSubnet := func(name, vpc string) *kubeovnv1.Subnet {
		return &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       kubeovnv1.SubnetSpec{Vpc: vpc},
		}
	}
	makeVpc := func(name, tcpLB string) *kubeovnv1.Vpc {
		return &kubeovnv1.Vpc{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status:     kubeovnv1.VpcStatus{TCPLoadBalancer: tcpLB},
		}
	}
	makeVip := func(name, subnet string, attach []string, annotations map[string]string) *kubeovnv1.Vip {
		return &kubeovnv1.Vip{
			ObjectMeta: metav1.ObjectMeta{Name: name, Annotations: annotations},
			Spec: kubeovnv1.VipSpec{
				Subnet:        subnet,
				AttachSubnets: attach,
				Type:          util.SwitchLBRuleVip,
			},
		}
	}

	t.Run("home subnet not found returns error", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, nil)
		require.NoError(t, err)
		vip := makeVip("vip1", "missing-subnet", []string{"attach1"}, nil)
		assert.Error(t, fc.fakeController.reconcileVipAttachSubnets(vip))
	})

	t.Run("vpc not found returns error", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{makeSubnet("home", "missing-vpc")},
		})
		require.NoError(t, err)
		vip := makeVip("vip1", "home", []string{"attach1"}, nil)
		assert.Error(t, fc.fakeController.reconcileVipAttachSubnets(vip))
	})

	t.Run("vpc with no load balancers is a no-op", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{makeSubnet("home", "vpc1")},
			Vpcs:    []*kubeovnv1.Vpc{makeVpc("vpc1", "")},
		})
		require.NoError(t, err)
		vip := makeVip("vip1", "home", []string{"attach1"}, nil)
		// No mock expectations — any OVN call would fail the test.
		assert.NoError(t, fc.fakeController.reconcileVipAttachSubnets(vip))
	})

	t.Run("happy path attaches to desired subnets and persists annotation", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{makeSubnet("home", "vpc1")},
			Vpcs:    []*kubeovnv1.Vpc{makeVpc("vpc1", "vpc1-tcp-lb")},
		})
		require.NoError(t, err)
		vip := makeVip("vip1", "home", []string{"attach1", "attach2"}, nil)
		_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Create(
			context.Background(), vip, metav1.CreateOptions{})
		require.NoError(t, err)

		fc.mockOvnClient.EXPECT().
			LogicalSwitchUpdateLoadBalancers("attach1", ovsdb.MutateOperationInsert, "vpc1-tcp-lb").
			Return(nil)
		fc.mockOvnClient.EXPECT().
			LogicalSwitchUpdateLoadBalancers("attach2", ovsdb.MutateOperationInsert, "vpc1-tcp-lb").
			Return(nil)

		require.NoError(t, fc.fakeController.reconcileVipAttachSubnets(vip))

		updated, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Get(
			context.Background(), "vip1", metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, "attach1,attach2", updated.Annotations[util.VipAttachSubnetsAnnotation])
	})

	t.Run("detaches subnet dropped from desired list and keeps still-desired one", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{makeSubnet("home", "vpc1")},
			Vpcs:    []*kubeovnv1.Vpc{makeVpc("vpc1", "vpc1-tcp-lb")},
		})
		require.NoError(t, err)
		// Previously attached to "old" and "keep"; now only "keep" is desired.
		vip := makeVip("vip1", "home", []string{"keep"}, map[string]string{
			util.VipAttachSubnetsAnnotation: "old,keep",
		})
		_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Create(
			context.Background(), vip, metav1.CreateOptions{})
		require.NoError(t, err)

		fc.mockOvnClient.EXPECT().
			LogicalSwitchUpdateLoadBalancers("old", ovsdb.MutateOperationDelete, "vpc1-tcp-lb").
			Return(nil)
		fc.mockOvnClient.EXPECT().
			LogicalSwitchUpdateLoadBalancers("keep", ovsdb.MutateOperationInsert, "vpc1-tcp-lb").
			Return(nil)

		require.NoError(t, fc.fakeController.reconcileVipAttachSubnets(vip))

		updated, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Get(
			context.Background(), "vip1", metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, "keep", updated.Annotations[util.VipAttachSubnetsAnnotation])
	})

	t.Run("desired list unchanged skips patch", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{makeSubnet("home", "vpc1")},
			Vpcs:    []*kubeovnv1.Vpc{makeVpc("vpc1", "vpc1-tcp-lb")},
		})
		require.NoError(t, err)
		vip := makeVip("vip1", "home", []string{"attach1"}, map[string]string{
			util.VipAttachSubnetsAnnotation: "attach1",
			"unrelated":                     "keep-me",
		})
		_, err = fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Create(
			context.Background(), vip, metav1.CreateOptions{})
		require.NoError(t, err)

		fc.mockOvnClient.EXPECT().
			LogicalSwitchUpdateLoadBalancers("attach1", ovsdb.MutateOperationInsert, "vpc1-tcp-lb").
			Return(nil)

		require.NoError(t, fc.fakeController.reconcileVipAttachSubnets(vip))

		updated, err := fc.fakeController.config.KubeOvnClient.KubeovnV1().Vips().Get(
			context.Background(), "vip1", metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, "keep-me", updated.Annotations["unrelated"], "unrelated annotations must survive a no-op reconcile")
	})

	t.Run("detach error is propagated before attach loop runs", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{makeSubnet("home", "vpc1")},
			Vpcs:    []*kubeovnv1.Vpc{makeVpc("vpc1", "vpc1-tcp-lb")},
		})
		require.NoError(t, err)
		vip := makeVip("vip1", "home", []string{"keep"}, map[string]string{
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
			Subnets: []*kubeovnv1.Subnet{makeSubnet("home", "vpc1")},
			Vpcs:    []*kubeovnv1.Vpc{makeVpc("vpc1", "vpc1-tcp-lb")},
		})
		require.NoError(t, err)
		vip := makeVip("vip1", "home", []string{"attach1"}, nil)

		fc.mockOvnClient.EXPECT().
			LogicalSwitchUpdateLoadBalancers("attach1", ovsdb.MutateOperationInsert, "vpc1-tcp-lb").
			Return(assert.AnError)

		assert.Error(t, fc.fakeController.reconcileVipAttachSubnets(vip))
	})
}

func Test_detachAllVipAttachSubnets(t *testing.T) {
	makeSubnet := func(name, vpc string) *kubeovnv1.Subnet {
		return &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       kubeovnv1.SubnetSpec{Vpc: vpc},
		}
	}
	makeVpc := func(name, tcpLB string) *kubeovnv1.Vpc {
		return &kubeovnv1.Vpc{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Status:     kubeovnv1.VpcStatus{TCPLoadBalancer: tcpLB},
		}
	}
	makeVip := func(name, subnet string, attach []string, annotations map[string]string) *kubeovnv1.Vip {
		return &kubeovnv1.Vip{
			ObjectMeta: metav1.ObjectMeta{Name: name, Annotations: annotations},
			Spec: kubeovnv1.VipSpec{
				Subnet:        subnet,
				AttachSubnets: attach,
				Type:          util.SwitchLBRuleVip,
			},
		}
	}

	t.Run("nothing to detach is a no-op", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, nil)
		require.NoError(t, err)
		vip := makeVip("vip1", "home", nil, nil)
		// No lister data and no mock expectations — the function must return
		// before ever touching the subnet/vpc listers.
		assert.NoError(t, fc.fakeController.detachAllVipAttachSubnets(vip))
	})

	t.Run("home subnet not found returns error", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, nil)
		require.NoError(t, err)
		vip := makeVip("vip1", "missing-subnet", []string{"attach1"}, nil)
		assert.Error(t, fc.fakeController.detachAllVipAttachSubnets(vip))
	})

	t.Run("vpc not found returns error", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{makeSubnet("home", "missing-vpc")},
		})
		require.NoError(t, err)
		vip := makeVip("vip1", "home", []string{"attach1"}, nil)
		assert.Error(t, fc.fakeController.detachAllVipAttachSubnets(vip))
	})

	t.Run("vpc with no load balancers is a no-op", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{makeSubnet("home", "vpc1")},
			Vpcs:    []*kubeovnv1.Vpc{makeVpc("vpc1", "")},
		})
		require.NoError(t, err)
		vip := makeVip("vip1", "home", []string{"attach1"}, nil)
		assert.NoError(t, fc.fakeController.detachAllVipAttachSubnets(vip))
	})

	t.Run("union of spec and annotation subnets is deduped and detached", func(t *testing.T) {
		fc, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
			Subnets: []*kubeovnv1.Subnet{makeSubnet("home", "vpc1")},
			Vpcs:    []*kubeovnv1.Vpc{makeVpc("vpc1", "vpc1-tcp-lb")},
		})
		require.NoError(t, err)
		vip := makeVip("vip1", "home", []string{"shared", "spec-only"}, map[string]string{
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
			Subnets: []*kubeovnv1.Subnet{makeSubnet("home", "vpc1")},
			Vpcs:    []*kubeovnv1.Vpc{makeVpc("vpc1", "vpc1-tcp-lb")},
		})
		require.NoError(t, err)
		vip := makeVip("vip1", "home", []string{"attach1"}, nil)

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
		ObjectMeta: metav1.ObjectMeta{Name: "vip1"},
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
