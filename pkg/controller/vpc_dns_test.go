package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func setVpcDNSGlobals(t *testing.T) {
	enableCoreDNSBak, corednsImageBak, corednsVipBak := enableCoreDNS, corednsImage, corednsVip
	t.Cleanup(func() {
		enableCoreDNS, corednsImage, corednsVip = enableCoreDNSBak, corednsImageBak, corednsVipBak
	})
	enableCoreDNS = true
	corednsImage = "registry.k8s.io/coredns/coredns:v1.13.1"
	corednsVip = "10.96.0.3"
}

func TestHandleAddOrUpdateVPCDNS(t *testing.T) {
	setVpcDNSGlobals(t)

	t.Run("mark vpc dns inactive when reconciliation fails", func(t *testing.T) {
		fakeController := newFakeController(t)
		ctrl := fakeController.fakeController

		vpcDNS := &kubeovnv1.VpcDns{
			ObjectMeta: metav1.ObjectMeta{Name: "test-dns"},
			Spec: kubeovnv1.VpcDNSSpec{
				Vpc:    "missing-vpc",
				Subnet: "missing-subnet",
			},
			Status: kubeovnv1.VpcDNSStatus{Active: true},
		}
		_, err := ctrl.config.KubeOvnClient.KubeovnV1().VpcDnses().Create(context.Background(), vpcDNS, metav1.CreateOptions{})
		require.NoError(t, err)
		require.NoError(t, fakeController.fakeInformers.vpcDNSInformer.Informer().GetStore().Add(vpcDNS))

		require.Error(t, ctrl.handleAddOrUpdateVPCDNS(vpcDNS.Name))

		updated, err := ctrl.config.KubeOvnClient.KubeovnV1().VpcDnses().Get(context.Background(), vpcDNS.Name, metav1.GetOptions{})
		require.NoError(t, err)
		require.False(t, updated.Status.Active, "vpc dns should be marked inactive when reconciliation fails")
	})

	t.Run("keep vpc dns inactive when reconciliation keeps failing", func(t *testing.T) {
		fakeController := newFakeController(t)
		ctrl := fakeController.fakeController

		vpcDNS := &kubeovnv1.VpcDns{
			ObjectMeta: metav1.ObjectMeta{Name: "test-dns"},
			Spec: kubeovnv1.VpcDNSSpec{
				Vpc:    "missing-vpc",
				Subnet: "missing-subnet",
			},
		}
		_, err := ctrl.config.KubeOvnClient.KubeovnV1().VpcDnses().Create(context.Background(), vpcDNS, metav1.CreateOptions{})
		require.NoError(t, err)
		require.NoError(t, fakeController.fakeInformers.vpcDNSInformer.Informer().GetStore().Add(vpcDNS))

		require.Error(t, ctrl.handleAddOrUpdateVPCDNS(vpcDNS.Name))

		updated, err := ctrl.config.KubeOvnClient.KubeovnV1().VpcDnses().Get(context.Background(), vpcDNS.Name, metav1.GetOptions{})
		require.NoError(t, err)
		require.False(t, updated.Status.Active)
	})
}

func TestCheckVpcDNSDuplicated(t *testing.T) {
	addVpcDNS := func(t *testing.T, fakeController *fakeController, name, vpc string, active bool) *kubeovnv1.VpcDns {
		vpcDNS := &kubeovnv1.VpcDns{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       kubeovnv1.VpcDNSSpec{Vpc: vpc},
			Status:     kubeovnv1.VpcDNSStatus{Active: active},
		}
		require.NoError(t, fakeController.fakeInformers.vpcDNSInformer.Informer().GetStore().Add(vpcDNS))
		return vpcDNS
	}

	t.Run("reject when another active vpc dns exists in the same vpc", func(t *testing.T) {
		fakeController := newFakeController(t)
		addVpcDNS(t, fakeController, "existing-dns", "vpc1", true)
		newDNS := addVpcDNS(t, fakeController, "new-dns", "vpc1", false)

		require.Error(t, fakeController.fakeController.checkVpcDNSDuplicated(newDNS))
	})

	t.Run("allow when the other vpc dns in the same vpc is inactive", func(t *testing.T) {
		fakeController := newFakeController(t)
		addVpcDNS(t, fakeController, "existing-dns", "vpc1", false)
		newDNS := addVpcDNS(t, fakeController, "new-dns", "vpc1", false)

		require.NoError(t, fakeController.fakeController.checkVpcDNSDuplicated(newDNS))
	})
}
