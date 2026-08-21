package framework

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/kubernetes/test/e2e/framework"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	v1 "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/typed/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// VpcWireGuardClient is a client for VpcWireGuard.
type VpcWireGuardClient struct {
	f *Framework
	v1.VpcWireGuardInterface
}

func (f *Framework) VpcWireGuardClient() *VpcWireGuardClient {
	return &VpcWireGuardClient{
		f:                     f,
		VpcWireGuardInterface: f.KubeOVNClientSet.KubeovnV1().VpcWireGuards(),
	}
}

func (c *VpcWireGuardClient) Get(name string) *apiv1.VpcWireGuard {
	ginkgo.GinkgoHelper()
	gw, err := c.VpcWireGuardInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return gw
}

func (c *VpcWireGuardClient) Create(gw *apiv1.VpcWireGuard) *apiv1.VpcWireGuard {
	ginkgo.GinkgoHelper()
	gw, err := c.VpcWireGuardInterface.Create(context.TODO(), gw, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating vpc wireguard")
	return gw.DeepCopy()
}

func (c *VpcWireGuardClient) CreateSync(gw *apiv1.VpcWireGuard) *apiv1.VpcWireGuard {
	ginkgo.GinkgoHelper()
	gw = c.Create(gw)
	ExpectTrue(c.WaitToBeReady(gw.Name, 4*time.Minute))
	return c.Get(gw.Name).DeepCopy()
}

func (c *VpcWireGuardClient) Patch(original, modified *apiv1.VpcWireGuard) *apiv1.VpcWireGuard {
	ginkgo.GinkgoHelper()
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patched *apiv1.VpcWireGuard
	err = wait.PollUntilContextTimeout(context.Background(), poll, timeout, true, func(ctx context.Context) (bool, error) {
		gw, err := c.VpcWireGuardInterface.Patch(ctx, original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch vpc wireguard %q", original.Name)
		}
		patched = gw
		return true, nil
	})
	if err == nil {
		return patched.DeepCopy()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch VpcWireGuard %s", original.Name)
	}
	Failf("error occurred while retrying to patch VpcWireGuard %s: %v", original.Name, err)
	return nil
}

func (c *VpcWireGuardClient) Delete(name string) {
	ginkgo.GinkgoHelper()
	err := c.VpcWireGuardInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete vpc wireguard %q: %v", name, err)
	}
}

func (c *VpcWireGuardClient) DeleteSync(name string) {
	ginkgo.GinkgoHelper()
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, poll, timeout)).To(gomega.Succeed(), "wait for vpc wireguard %q to disappear", name)
}

func (c *VpcWireGuardClient) WaitToBeReady(name string, timeout time.Duration) bool {
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		gw := c.Get(name)
		if gw.Status.Ready && gw.Status.Endpoint != "" && gw.Status.PublicKey != "" {
			return true
		}
		Logf("vpc wireguard %s is not ready: ready=%v endpoint=%q publicKey empty=%v",
			name, gw.Status.Ready, gw.Status.Endpoint, gw.Status.PublicKey == "")
	}
	return false
}

func (c *VpcWireGuardClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*apiv1.VpcWireGuard, error) {
		gw, err := c.VpcWireGuardInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return gw, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected VpcWireGuard %s to not be found: %w", name, err)
	}
	return nil
}

func (c *VpcWireGuardClient) WaitToBeUpdated(gw *apiv1.VpcWireGuard, timeout time.Duration) bool {
	rv, _ := big.NewInt(0).SetString(gw.ResourceVersion, 10)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		s := c.Get(gw.Name)
		if current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10); current.Cmp(rv) > 0 {
			return true
		}
	}
	return false
}

// VpcWireGuardPeerClient is a client for VpcWireGuardPeer.
type VpcWireGuardPeerClient struct {
	f *Framework
	v1.VpcWireGuardPeerInterface
}

func (f *Framework) VpcWireGuardPeerClient() *VpcWireGuardPeerClient {
	return &VpcWireGuardPeerClient{
		f:                         f,
		VpcWireGuardPeerInterface: f.KubeOVNClientSet.KubeovnV1().VpcWireGuardPeers(),
	}
}

func (c *VpcWireGuardPeerClient) Get(name string) *apiv1.VpcWireGuardPeer {
	ginkgo.GinkgoHelper()
	peer, err := c.VpcWireGuardPeerInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return peer
}

func (c *VpcWireGuardPeerClient) Create(peer *apiv1.VpcWireGuardPeer) *apiv1.VpcWireGuardPeer {
	ginkgo.GinkgoHelper()
	peer, err := c.VpcWireGuardPeerInterface.Create(context.TODO(), peer, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating vpc wireguard peer")
	return peer.DeepCopy()
}

func (c *VpcWireGuardPeerClient) CreateSync(peer *apiv1.VpcWireGuardPeer) *apiv1.VpcWireGuardPeer {
	ginkgo.GinkgoHelper()
	peer = c.Create(peer)
	ExpectTrue(c.WaitToBeReady(peer.Name, timeout))
	return c.Get(peer.Name).DeepCopy()
}

func (c *VpcWireGuardPeerClient) Delete(name string) {
	ginkgo.GinkgoHelper()
	err := c.VpcWireGuardPeerInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete vpc wireguard peer %q: %v", name, err)
	}
}

func (c *VpcWireGuardPeerClient) DeleteSync(name string) {
	ginkgo.GinkgoHelper()
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, poll, timeout)).To(gomega.Succeed(), "wait for vpc wireguard peer %q to disappear", name)
}

func (c *VpcWireGuardPeerClient) WaitToBeReady(name string, timeout time.Duration) bool {
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		peer := c.Get(name)
		if peer.Status.Ready && peer.Status.ClientIP != "" && peer.Status.PublicKey != "" {
			return true
		}
		Logf("vpc wireguard peer %s is not ready: ready=%v clientIP=%q", name, peer.Status.Ready, peer.Status.ClientIP)
	}
	return false
}

func (c *VpcWireGuardPeerClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*apiv1.VpcWireGuardPeer, error) {
		peer, err := c.VpcWireGuardPeerInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return peer, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected VpcWireGuardPeer %s to not be found: %w", name, err)
	}
	return nil
}

func MakeVpcWireGuard(name, vpc, subnet, lanIP, clientSubnet, exposureType string) *apiv1.VpcWireGuard {
	return &apiv1.VpcWireGuard{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: apiv1.VpcWireGuardSpec{
			Vpc:               vpc,
			Subnet:            subnet,
			LanIP:             lanIP,
			ClientSubnet:      clientSubnet,
			GenerateServerKey: true,
			Exposure: apiv1.VpcWireGuardExposure{
				Type: exposureType,
			},
		},
	}
}

func MakeVpcWireGuardPeer(name, wireGuard string, generateKey bool, publicKey string) *apiv1.VpcWireGuardPeer {
	return &apiv1.VpcWireGuardPeer{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: apiv1.VpcWireGuardPeerSpec{
			WireGuard:   wireGuard,
			GenerateKey: generateKey,
			PublicKey:   publicKey,
		},
	}
}
