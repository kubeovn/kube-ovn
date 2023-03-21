package framework

import (
	"context"
	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	v1 "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/typed/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"time"
)

// VipClient is a struct for vip client.
type VipClient struct {
	f *Framework
	v1.VipInterface
}

func (f *Framework) VipClient() *VipClient {
	return &VipClient{
		f:            f,
		VipInterface: f.KubeOVNClientSet.KubeovnV1().Vips(),
	}
}

func (c *VipClient) Get(name string) *apiv1.Vip {
	vip, err := c.VipInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return vip.DeepCopy()
}

// Create creates a new vip according to the framework specifications
func (c *VipClient) Create(pn *apiv1.Vip) *apiv1.Vip {
	vip, err := c.VipInterface.Create(context.TODO(), pn, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating vip")
	return vip.DeepCopy()
}

// Patch patches the vip
func (c *VipClient) Patch(original, modified *apiv1.Vip, timeout time.Duration) *apiv1.Vip {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedVip *apiv1.Vip
	err = wait.PollImmediate(2*time.Second, timeout, func() (bool, error) {
		p, err := c.VipInterface.Patch(context.TODO(), original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch vip %q", original.Name)
		}
		patchedVip = p
		return true, nil
	})
	if err == nil {
		return patchedVip.DeepCopy()
	}

	if IsTimeout(err) {
		Failf("timed out while retrying to patch vip %s", original.Name)
	}
	ExpectNoError(maybeTimeoutError(err, "patching vip %s", original.Name))

	return nil
}

// Delete deletes a vip if the vip exists
func (c *VipClient) Delete(name string, options metav1.DeleteOptions) {
	err := c.VipInterface.Delete(context.TODO(), name, options)
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete vip %q: %v", name, err)
	}
}

func MakeVip(name, subnet, v4ip, v6ip string) *apiv1.Vip {
	vip := &apiv1.Vip{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.VipSpec{
			Subnet: subnet,
			V4ip:   v4ip,
			V6ip:   v6ip,
		},
	}
	return vip
}
