package framework

import (
	"fmt"
	"os"
	"strings"
	"time"

	attachnetclientset "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubernetes/test/e2e/framework"
	admissionapi "k8s.io/pod-security-admission/api"

	"github.com/onsi/ginkgo/v2"

	kubeovncs "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	IPv4 = "ipv4"
	IPv6 = "ipv6"
	Dual = "dual"
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
	ClusterIPFamily string
	// overlay/underlay/underlay-hairpin
	ClusterNetworkMode string
}

func NewDefaultFramework(baseName string) *Framework {
	f := &Framework{
		Framework: framework.NewDefaultFramework(baseName),
	}
	f.NamespacePodSecurityEnforceLevel = admissionapi.LevelPrivileged
	f.ClusterIPFamily = os.Getenv("E2E_IP_FAMILY")
	f.ClusterVersion = os.Getenv("E2E_BRANCH")
	f.ClusterNetworkMode = os.Getenv("E2E_NETWORK_MODE")

	if strings.HasPrefix(f.ClusterVersion, "release-") {
		n, err := fmt.Sscanf(f.ClusterVersion, "release-%d.%d", &f.ClusterVersionMajor, &f.ClusterVersionMinor)
		if err != nil || n != 2 {
			defer ginkgo.GinkgoRecover()
			ginkgo.Fail(fmt.Sprintf("Failed to parse Kube-OVN version string %q", f.ClusterVersion))
		}
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
	f.ClusterIPFamily = os.Getenv("E2E_IP_FAMILY")
	f.ClusterVersion = os.Getenv("E2E_BRANCH")
	f.ClusterNetworkMode = os.Getenv("E2E_NETWORK_MODE")

	if strings.HasPrefix(f.ClusterVersion, "release-") {
		n, err := fmt.Sscanf(f.ClusterVersion, "release-%d.%d", &f.ClusterVersionMajor, &f.ClusterVersionMinor)
		if err != nil || n != 2 {
			defer ginkgo.GinkgoRecover()
			ginkgo.Fail(fmt.Sprintf("Failed to parse Kube-OVN version string %q", f.ClusterVersion))
		}
	} else {
		f.ClusterVersionMajor, f.ClusterVersionMinor = 999, 999
	}

	ginkgo.BeforeEach(func() {
		framework.TestContext.Host = ""
	})

	return f
}

func (f *Framework) IsIPv4() bool {
	return f.ClusterIPFamily == IPv4
}

func (f *Framework) IsIPv6() bool {
	return f.ClusterIPFamily == IPv6
}

func (f *Framework) IsDual() bool {
	return f.ClusterIPFamily == Dual
}

func (f *Framework) HasIPv4() bool {
	return !f.IsIPv6()
}

func (f *Framework) HasIPv6() bool {
	return !f.IsIPv4()
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

func (f *Framework) ValidateFinalizers(obj metav1.Object) {
	finalizers := obj.GetFinalizers()
	if !f.VersionPriorTo(1, 13) {
		ExpectContainElement(finalizers, util.KubeOVNControllerFinalizer)
		ExpectNotContainElement(finalizers, util.DepreciatedFinalizerName)
	} else {
		ExpectContainElement(finalizers, util.DepreciatedFinalizerName)
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
	return framework.ConformanceIt(text, body)
}

func DisruptiveIt(text string, body interface{}) bool {
	return framework.It(text, ginkgo.Offset(1), body, framework.WithDisruptive())
}
