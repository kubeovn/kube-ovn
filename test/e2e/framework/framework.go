package framework

import (
	"fmt"
	"os"
	"strings"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubernetes/test/e2e/framework"
	admissionapi "k8s.io/pod-security-admission/api"

	attachnetclientset "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned"

	"github.com/onsi/ginkgo/v2"

	kubeovncs "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned"
)

const (
	// poll is how often to Poll resources.
	poll = 2 * time.Second

	timeout = 2 * time.Minute
)

type Framework struct {
	KubeContext string
	*framework.Framework
	KubeOVNClientSet kubeovncs.Interface
	AttachNetClient  attachnetclientset.Interface
	// master/release-1.10/...
	ClusterVersion string
	// 999.999 for master
	ClusterVersionMajor uint
	ClusterVersionMinor uint
	// ipv4/ipv6/dual
	ClusterIpFamily string
	// overlay/underlay/underlay-hairpin
	ClusterNetworkMode string
}

func NewDefaultFramework(baseName string) *Framework {
	f := &Framework{
		Framework: framework.NewDefaultFramework(baseName),
	}
	f.NamespacePodSecurityEnforceLevel = admissionapi.LevelPrivileged
	f.ClusterIpFamily = os.Getenv("E2E_IP_FAMILY")
	f.ClusterVersion = os.Getenv("E2E_BRANCH")
	f.ClusterNetworkMode = os.Getenv("E2E_NETWORK_MODE")

	if strings.HasPrefix(f.ClusterVersion, "release-") {
		n, err := fmt.Sscanf(f.ClusterVersion, "release-%d.%d", &f.ClusterVersionMajor, &f.ClusterVersionMinor)
		ExpectNoError(err)
		ExpectEqual(n, 2)
	} else {
		f.ClusterVersionMajor, f.ClusterVersionMinor = 999, 999
	}

	ginkgo.BeforeEach(f.BeforeEach)

	return f
}

func (f *Framework) useContext() error {
	if f.KubeContext == "" {
		return nil
	}

	pathOptions := clientcmd.NewDefaultPathOptions()
	pathOptions.GlobalFile = framework.TestContext.KubeConfig
	pathOptions.EnvVar = ""

	config, err := pathOptions.GetStartingConfig()
	if err != nil {
		return err
	}

	if config.CurrentContext != f.KubeContext {
		Logf("Switching context to " + f.KubeContext)
		config.CurrentContext = f.KubeContext
		if err = clientcmd.ModifyConfig(pathOptions, *config, true); err != nil {
			return err
		}
	}

	return nil
}

func NewFrameworkWithContext(baseName, kubeContext string) *Framework {
	f := &Framework{KubeContext: kubeContext}
	ginkgo.BeforeEach(f.BeforeEach)

	f.Framework = framework.NewDefaultFramework(baseName)
	f.NamespacePodSecurityEnforceLevel = admissionapi.LevelPrivileged
	f.ClusterIpFamily = os.Getenv("E2E_IP_FAMILY")
	f.ClusterVersion = os.Getenv("E2E_BRANCH")
	f.ClusterNetworkMode = os.Getenv("E2E_NETWORK_MODE")

	ginkgo.BeforeEach(func() {
		framework.TestContext.Host = ""
	})

	return f
}

func (f *Framework) IPv6() bool {
	return f.ClusterIpFamily == "ipv6"
}

// BeforeEach gets a kube-ovn client
func (f *Framework) BeforeEach() {
	ginkgo.By("Setting kubernetes context")
	ExpectNoError(f.useContext())

	if f.KubeOVNClientSet == nil {
		ginkgo.By("Creating a Kube-OVN client")
		config, err := framework.LoadConfig()
		ExpectNoError(err)

		config.QPS = f.Options.ClientQPS
		config.Burst = f.Options.ClientBurst
		f.KubeOVNClientSet, err = kubeovncs.NewForConfig(config)
		ExpectNoError(err)
	}

	if f.AttachNetClient == nil {
		ginkgo.By("Creating a nad client")
		config, err := framework.LoadConfig()
		ExpectNoError(err)

		config.QPS = f.Options.ClientQPS
		config.Burst = f.Options.ClientBurst
		f.AttachNetClient, err = attachnetclientset.NewForConfig(config)
		ExpectNoError(err)
	}

	framework.TestContext.Host = ""
}

func (f *Framework) VersionPriorTo(major, minor uint) bool {
	return f.ClusterVersionMajor < major || (f.ClusterVersionMajor == major && f.ClusterVersionMinor < minor)
}

func (f *Framework) SkipVersionPriorTo(major, minor uint, reason string) {
	if f.VersionPriorTo(major, minor) {
		ginkgo.Skip(reason)
	}
}

func Describe(text string, body func()) bool {
	return ginkgo.Describe("[CNI:Kube-OVN] "+text, body)
}

func SerialDescribe(text string, body func()) bool {
	return ginkgo.Describe("[CNI:Kube-OVN] "+text, ginkgo.Serial, body)
}

func OrderedDescribe(text string, body func()) bool {
	return ginkgo.Describe("[CNI:Kube-OVN] "+text, ginkgo.Ordered, body)
}

// ConformanceIt is wrapper function for ginkgo It.
// Adds "[Conformance]" tag and makes static analysis easier.
func ConformanceIt(text string, body interface{}) bool {
	return ginkgo.It(text+" [Conformance]", ginkgo.Offset(1), body)
}
