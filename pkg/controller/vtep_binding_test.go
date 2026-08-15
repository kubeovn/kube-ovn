package controller

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

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

func TestValidateVtepBindingConflict(t *testing.T) {
	t.Parallel()

	existing := &kubeovnv1.VtepBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "existing"},
		Spec: kubeovnv1.VtepBindingSpec{
			Subnet:         "subnet-a",
			PhysicalSwitch: "nexus01",
			PhysicalPort:   "Ethernet1/20",
			VlanID:         120,
		},
	}
	candidate := &kubeovnv1.VtepBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "candidate"},
		Spec: kubeovnv1.VtepBindingSpec{
			Subnet:         "subnet-b",
			PhysicalSwitch: "nexus01",
			PhysicalPort:   "Ethernet1/20",
			VlanID:         120,
		},
	}

	c := &Controller{}
	// Inject a minimal lister via a local stub.
	c.vtepBindingsLister = &fakeVtepBindingLister{items: []*kubeovnv1.VtepBinding{existing}}

	err := c.validateVtepBindingConflict(candidate, candidate.VtepLogicalSwitchName())
	require.Error(t, err)
	require.Contains(t, err.Error(), "physicalPort")
	require.Contains(t, err.Error(), "vlanID")

	candidate.Spec.VlanID = 121
	err = c.validateVtepBindingConflict(candidate, candidate.VtepLogicalSwitchName())
	require.NoError(t, err)

	candidate.Spec.Subnet = "subnet-a"
	candidate.Spec.PhysicalPort = "Ethernet1/21"
	err = c.validateVtepBindingConflict(candidate, candidate.VtepLogicalSwitchName())
	require.Error(t, err)
	require.Contains(t, err.Error(), "vtepLogicalSwitch")
}

type fakeVtepBindingLister struct {
	items []*kubeovnv1.VtepBinding
}

func (f *fakeVtepBindingLister) List(labels.Selector) ([]*kubeovnv1.VtepBinding, error) {
	return f.items, nil
}

func (f *fakeVtepBindingLister) Get(name string) (*kubeovnv1.VtepBinding, error) {
	for _, item := range f.items {
		if item.Name == name {
			return item, nil
		}
	}
	return nil, errors.New("not found")
}
