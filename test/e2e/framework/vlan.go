package framework

import (
	"context"
	"errors"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	v1 "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/typed/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// VlanClient is a struct for vlan client.
type VlanClient struct {
	f *Framework
	v1.VlanInterface
}

func (f *Framework) VlanClient() *VlanClient {
	return &VlanClient{
		f:             f,
		VlanInterface: f.KubeOVNClientSet.KubeovnV1().Vlans(),
	}
}

func (c *VlanClient) Get(name string) *apiv1.Vlan {
	vlan, err := c.VlanInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return vlan
}

// Create creates a new vlan according to the framework specifications
func (c *VlanClient) Create(pn *apiv1.Vlan) *apiv1.Vlan {
	vlan, err := c.VlanInterface.Create(context.TODO(), pn, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating vlan")
	return vlan.DeepCopy()
}

// Patch patches the vlan
func (c *VlanClient) Patch(original, modified *apiv1.Vlan, timeout time.Duration) *apiv1.Vlan {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedVlan *apiv1.Vlan
	err = wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		pn, err := c.VlanInterface.Patch(ctx, original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch vlan %q", original.Name)
		}
		patchedVlan = pn
		return true, nil
	})
	if err == nil {
		return patchedVlan.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch VLAN %s", original.Name)
	}
	Failf("error occurred while retrying to patch VLAN %s: %v", original.Name, err)

	return nil
}

// Delete deletes a vlan if the vlan exists
func (c *VlanClient) Delete(name string, options metav1.DeleteOptions) {
	err := c.VlanInterface.Delete(context.TODO(), name, options)
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete vlan %q: %v", name, err)
	}
}

func MakeVlan(name, provider string, id int) *apiv1.Vlan {
	vlan := &apiv1.Vlan{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.VlanSpec{
			Provider: provider,
			ID:       id,
		},
	}
	return vlan
}
