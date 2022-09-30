package controller

import (
	attachnet "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned/fake"
	kubeovn "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	_ "github.com/onsi/gomega"
	k8s "k8s.io/client-go/kubernetes/fake"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func fakeConfig() *Configuration {
	kubeClient := k8s.NewSimpleClientset()
	kubeOvnClient := kubeovn.NewSimpleClientset()
	attachnetClient := attachnet.NewSimpleClientset()
	config := Configuration{
		KubeClient:      kubeClient,
		KubeOvnClient:   kubeOvnClient,
		AttachNetClient: attachnetClient,

		KubeFactoryClient:    kubeClient,
		KubeOvnFactoryClient: kubeOvnClient,

		DefaultLogicalSwitch: "ovn-default",
		NodeSwitch:           "join",

		EnableExternalVpc: false,

		OvnLegacyClient: ovs.FakeOvnLegacyClient{},
	}
	return &config
}

func TestController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Kube-OVN controller unit test Suite")
}

var _ = Describe("", func() {
	Context("", func() {
		ctl := NewController(fakeConfig())
		Expect(ctl).ShouldNot(BeNil())
	})
})
