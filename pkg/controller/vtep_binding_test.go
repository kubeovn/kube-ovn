package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
)

func TestVtepBindingLogicalSwitchName(t *testing.T) {
	t.Parallel()

	binding := &kubeovnv1.VtepBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "rack1"},
		Spec: kubeovnv1.VtepBindingSpec{
			Subnet: "tenant-a",
		},
	}
	require.Equal(t, "tenant-a", binding.VtepLogicalSwitchName())

	binding.Spec.VtepLogicalSwitch = "custom-ls"
	require.Equal(t, "custom-ls", binding.VtepLogicalSwitchName())
}

func TestGetVtepLogicalSwitchPortName(t *testing.T) {
	t.Parallel()
	require.Equal(t, "vtep.rack1-tenant-a", ovs.GetVtepLogicalSwitchPortName("rack1-tenant-a"))
}
