package webhook

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestValidateVtepBindingSpecFields(t *testing.T) {
	t.Parallel()

	v := &ValidatingHook{}
	binding := &ovnv1.VtepBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec:       ovnv1.VtepBindingSpec{},
	}

	err := v.ValidateVtepBinding(t.Context(), binding, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "subnet")

	binding.Spec.Subnet = "subnet-a"
	err = v.ValidateVtepBinding(t.Context(), binding, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "physicalSwitch")

	binding.Spec.PhysicalSwitch = "nexus01"
	err = v.ValidateVtepBinding(t.Context(), binding, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "physicalPort")

	binding.Spec.PhysicalPort = "Ethernet1/20"
	binding.Spec.VlanID = 5000
	err = v.ValidateVtepBinding(t.Context(), binding, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "vlanID")
}

func TestValidateVtepBindingImmutableFields(t *testing.T) {
	t.Parallel()

	v := &ValidatingHook{}
	oldBinding := &ovnv1.VtepBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: ovnv1.VtepBindingSpec{
			Subnet:            "subnet-a",
			PhysicalSwitch:    "nexus01",
			VtepLogicalSwitch: "ls-a",
			PhysicalPort:      "Ethernet1/20",
			VlanID:            120,
		},
	}
	newBinding := oldBinding.DeepCopy()
	newBinding.Spec.Subnet = "subnet-b"

	err := v.ValidateVtepBinding(t.Context(), newBinding, oldBinding)
	require.Error(t, err)
	require.Contains(t, err.Error(), "subnet is immutable")

	newBinding = oldBinding.DeepCopy()
	newBinding.Spec.PhysicalSwitch = "nexus02"
	err = v.ValidateVtepBinding(t.Context(), newBinding, oldBinding)
	require.Error(t, err)
	require.Contains(t, err.Error(), "physicalSwitch is immutable")

	newBinding = oldBinding.DeepCopy()
	newBinding.Spec.VtepLogicalSwitch = "ls-b"
	err = v.ValidateVtepBinding(t.Context(), newBinding, oldBinding)
	require.Error(t, err)
	require.Contains(t, err.Error(), "vtepLogicalSwitch is immutable")
}
