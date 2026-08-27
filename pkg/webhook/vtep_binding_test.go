package webhook

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

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

func TestValidateVtepBindingMissingSubnet(t *testing.T) {
	t.Parallel()

	v := &ValidatingHook{cache: &mockCache{objects: map[string]runtime.Object{}}}
	binding := &ovnv1.VtepBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: ovnv1.VtepBindingSpec{
			Subnet:         "missing-subnet",
			PhysicalSwitch: "nexus01",
			PhysicalPort:   "Ethernet1/20",
			VlanID:         120,
		},
	}
	err := v.ValidateVtepBinding(t.Context(), binding, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to get subnet")
}

func TestValidateVtepBindingConflicts(t *testing.T) {
	t.Parallel()

	existing := &ovnv1.VtepBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "existing"},
		Spec: ovnv1.VtepBindingSpec{
			Subnet:         "subnet-a",
			PhysicalSwitch: "nexus01",
			PhysicalPort:   "Ethernet1/20",
			VlanID:         120,
		},
	}
	v := &ValidatingHook{
		cache: &mockCache{objects: map[string]runtime.Object{
			"/subnet-a": &ovnv1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "subnet-a"}},
			"/subnet-b": &ovnv1.Subnet{ObjectMeta: metav1.ObjectMeta{Name: "subnet-b"}},
			"/existing": existing,
		}},
	}

	portVLANConflict := &ovnv1.VtepBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "candidate"},
		Spec: ovnv1.VtepBindingSpec{
			Subnet:         "subnet-b",
			PhysicalSwitch: "nexus01",
			PhysicalPort:   "Ethernet1/20",
			VlanID:         120,
		},
	}
	err := v.ValidateVtepBinding(t.Context(), portVLANConflict, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "physicalPort")
	require.Contains(t, err.Error(), "vlanID")

	logicalSwitchConflict := &ovnv1.VtepBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "candidate"},
		Spec: ovnv1.VtepBindingSpec{
			Subnet:         "subnet-a",
			PhysicalSwitch: "nexus01",
			PhysicalPort:   "Ethernet1/21",
			VlanID:         121,
		},
	}
	err = v.ValidateVtepBinding(t.Context(), logicalSwitchConflict, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "vtepLogicalSwitch")
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

	newBinding = oldBinding.DeepCopy()
	newBinding.Spec.PhysicalPort = "Ethernet1/21"
	err = v.ValidateVtepBinding(t.Context(), newBinding, oldBinding)
	require.Error(t, err)
	require.Contains(t, err.Error(), "physicalPort is immutable")

	newBinding = oldBinding.DeepCopy()
	newBinding.Spec.VlanID = 121
	err = v.ValidateVtepBinding(t.Context(), newBinding, oldBinding)
	require.Error(t, err)
	require.Contains(t, err.Error(), "vlanID is immutable")
}
