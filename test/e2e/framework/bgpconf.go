package framework

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/test/e2e/framework"

	"github.com/onsi/gomega"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	clientset "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned"
	v1 "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/typed/kubeovn/v1"
)

// BgpConfClient is a struct for BgpConf client.
type BgpConfClient struct {
	f *Framework
	v1.BgpConfInterface
}

func NewBgpConfClient(cs clientset.Interface) *BgpConfClient {
	return &BgpConfClient{
		BgpConfInterface: cs.KubeovnV1().BgpConves(),
	}
}

func (f *Framework) BgpConfClient() *BgpConfClient {
	return &BgpConfClient{
		f:                f,
		BgpConfInterface: f.KubeOVNClientSet.KubeovnV1().BgpConves(),
	}
}

func (c *BgpConfClient) Get(name string) *apiv1.BgpConf {
	ginkgo.GinkgoHelper()
	bgpConf, err := c.BgpConfInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return bgpConf
}

func (c *BgpConfClient) Create(bgpConf *apiv1.BgpConf) *apiv1.BgpConf {
	ginkgo.GinkgoHelper()
	bc, err := c.BgpConfInterface.Create(context.TODO(), bgpConf, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating BgpConf")
	return bc.DeepCopy()
}

func (c *BgpConfClient) Delete(name string) {
	ginkgo.GinkgoHelper()
	err := c.BgpConfInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete BgpConf %q: %v", name, err)
	}
}

func (c *BgpConfClient) DeleteSync(name string) {
	ginkgo.GinkgoHelper()
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, poll, timeout)).To(gomega.Succeed(), "wait for BgpConf %q to disappear", name)
}

func (c *BgpConfClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*apiv1.BgpConf, error) {
		bc, err := c.BgpConfInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return bc, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected BgpConf %q to not be found: %w", name, err)
	}
	return nil
}

func MakeBgpConf(name string, localASN, peerASN uint32, neighbours []string, holdTime, keepaliveTime, connectTime time.Duration, ebgpMultiHop bool) *apiv1.BgpConf {
	return &apiv1.BgpConf{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.BgpConfSpec{
			LocalASN:      localASN,
			PeerASN:       peerASN,
			Neighbours:    neighbours,
			HoldTime:      metav1.Duration{Duration: holdTime},
			KeepaliveTime: metav1.Duration{Duration: keepaliveTime},
			ConnectTime:   metav1.Duration{Duration: connectTime},
			EbgpMultiHop:  ebgpMultiHop,
		},
	}
}
