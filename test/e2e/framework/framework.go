package framework

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	nad "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned"
	"github.com/onsi/ginkgo/v2"
	"go.podman.io/image/v5/docker/reference"
	extClientSet "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	versionutil "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/utils/format"
	admissionapi "k8s.io/pod-security-admission/api"
	"kubevirt.io/client-go/kubecli"
	anpclient "sigs.k8s.io/network-policy-api/pkg/client/clientset/versioned"

	kubeovncs "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	IPv4 = "ipv4"
	IPv6 = "ipv6"
	Dual = "dual"
)

const (
	Overlay  = "overlay"
	Underlay = "underlay"
)

const (
	// poll is how often to Poll resources.
	poll = 2 * time.Second

	timeout = 2 * time.Minute
)

func LoadKubeOVNClientSet() (*kubeovncs.Clientset, error) {
	config, err := framework.LoadConfig()
	if err != nil {
		return nil, err
	}

	config.QPS = 20
	config.Burst = 50
	return kubeovncs.NewForConfig(config)
}

type Framework struct {
	KubeContext string
	*framework.Framework
	KubeOVNClientSet  kubeovncs.Interface
	KubeVirtClientSet kubecli.KubevirtClient
	MetallbClientSet  *MetallbClientSet
	AttachNetClient   nad.Interface
	ExtClientSet      *extClientSet.Clientset
	AnpClientSet      anpclient.Interface
	KubeOVNVersion    *versionutil.Version
	// master/release-1.10/...
	ClusterVersion string
	// 999.999 for master
	ClusterVersionMajor uint
	ClusterVersionMinor uint
	// ipv4/ipv6/dual
	ClusterIPFamily string
	// overlay/underlay/underlay-hairpin
	ClusterNetworkMode string
	// image info
	KubeOVNImage       string
	KubeOVNImageDomain string
	KubeOVNImageRepo   string
	KubeOVNImageTag    string
}

func (f *Framework) parseEnv() {
	f.ClusterIPFamily = os.Getenv("E2E_IP_FAMILY")
	f.ClusterNetworkMode = os.Getenv("E2E_NETWORK_MODE")

	envBranch := os.Getenv("E2E_BRANCH")
	if !strings.HasPrefix(envBranch, "release-") {
		f.KubeOVNVersion = versionutil.MustParseMajorMinor("999.999")
	} else {
		var err error
		if f.KubeOVNVersion, err = versionutil.ParseMajorMinor(strings.TrimPrefix(envBranch, "release-")); err != nil {
			defer ginkgo.GinkgoRecover()
			ginkgo.Fail(fmt.Sprintf("Failed to parse Kube-OVN version %q", envBranch))
		}
	}
}

func dumpEvents(ctx context.Context, f *framework.Framework, namespace string) {
	ginkgo.By("Dumping events in namespace " + namespace)
	events, err := f.ClientSet.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		Logf("Failed to get events: %v", err)
		return
	}
	for _, event := range events.Items {
		event.ManagedFields = nil
		fmt.Fprintln(ginkgo.GinkgoWriter, format.Object(event, 2))
	}
}

func NewDefaultFramework(baseName string) *Framework {
	f := &Framework{
		Framework: framework.NewDefaultFramework(baseName),
	}
	f.NamespacePodSecurityEnforceLevel = admissionapi.LevelPrivileged
	f.NamespacePodSecurityWarnLevel = admissionapi.LevelPrivileged
	f.DumpAllNamespaceInfo = dumpEvents
	f.parseEnv()

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
	ginkgo.BeforeEach(f.BeforeEach)

	f.NamespacePodSecurityEnforceLevel = admissionapi.LevelPrivileged
	f.NamespacePodSecurityWarnLevel = admissionapi.LevelPrivileged
	f.DumpAllNamespaceInfo = dumpEvents
	f.parseEnv()

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

func (f *Framework) IsOverlay() bool {
	return f.ClusterNetworkMode == Overlay
}

func (f *Framework) IsUnderlay() bool {
	return f.ClusterNetworkMode == Underlay
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

	if f.KubeVirtClientSet == nil {
		ginkgo.By("Creating a KubeVirt client")
		config, err := framework.LoadConfig()
		ExpectNoError(err)

		config.QPS = f.Options.ClientQPS
		config.Burst = f.Options.ClientBurst
		f.KubeVirtClientSet, err = kubecli.GetKubevirtClientFromRESTConfig(config)
		ExpectNoError(err)
	}

	if f.ExtClientSet == nil {
		ginkgo.By("Creating a Kubernetes client")
		config, err := framework.LoadConfig()
		ExpectNoError(err)

		config.QPS = f.Options.ClientQPS
		config.Burst = f.Options.ClientBurst
		f.ExtClientSet, err = extClientSet.NewForConfig(config)
		ExpectNoError(err)
	}

	if f.AttachNetClient == nil {
		ginkgo.By("Creating a network attachment definition client")
		config, err := framework.LoadConfig()
		ExpectNoError(err)

		config.QPS = f.Options.ClientQPS
		config.Burst = f.Options.ClientBurst
		f.AttachNetClient, err = nad.NewForConfig(config)
		ExpectNoError(err)
	}

	if f.MetallbClientSet == nil {
		ginkgo.By("Creating a MetalLB client")
		config, err := framework.LoadConfig()
		ExpectNoError(err)

		config.QPS = f.Options.ClientQPS
		config.Burst = f.Options.ClientBurst
		f.MetallbClientSet, err = NewMetallbClientSet(config)
		ExpectNoError(err)
	}

	if f.AnpClientSet == nil {
		ginkgo.By("Creating an AdminNetworkPolicy client")
		config, err := framework.LoadConfig()
		ExpectNoError(err)

		config.QPS = f.Options.ClientQPS
		config.Burst = f.Options.ClientBurst
		f.AnpClientSet, err = anpclient.NewForConfig(config)
		ExpectNoError(err)
	}

	if f.KubeOVNImage == "" && f.ClientSet != nil {
		framework.Logf("Getting Kube-OVN image")
		f.KubeOVNImage = GetKubeOvnImage(f.ClientSet)
		framework.Logf("Got Kube-OVN image: %s", f.KubeOVNImage)
		ref, err := reference.Parse(f.KubeOVNImage)
		ExpectNoError(err)
		taggedRef := ref.(reference.Tagged)
		ExpectNotNil(taggedRef, "Failed to get tagged reference from Kube-OVN image")
		f.KubeOVNImageTag = taggedRef.Tag()
		namedRef := ref.(reference.Named)
		ExpectNotNil(namedRef, "Failed to get named reference from Kube-OVN image")
		f.KubeOVNImageDomain = reference.Domain(namedRef)
		f.KubeOVNImageRepo = reference.Path(namedRef)
	}

	framework.TestContext.Host = ""
}

// VersionPriorTo returns true if the Kube-OVN version is prior to the specified version.
func (f *Framework) VersionPriorTo(major, minor uint) bool {
	return f.KubeOVNVersion.LessThan(versionutil.MustParseMajorMinor(fmt.Sprintf("%d.%d", major, minor)))
}

// SkipVersionPriorTo skips the test if the Kube-OVN version is prior to the specified version.
func (f *Framework) SkipVersionPriorTo(major, minor uint, reason string) {
	ginkgo.GinkgoHelper()

	if f.VersionPriorTo(major, minor) {
		ginkgo.Skip(reason)
	}
}

// Image returns the image reference with the specified name.
// .e.g. Image("vpc-nat-gateway") returns "docker.io/kubeovn/vpc-nat-gateway:v1.16.0"
func (f *Framework) Image(name string) string {
	repo := path.Clean(path.Join(path.Dir(f.KubeOVNImageRepo), name))
	return fmt.Sprintf("%s/%s:%s", f.KubeOVNImageDomain, repo, f.KubeOVNImageTag)
}

// VpcNatGatewayImage returns the VPC NAT gateway image reference.
// .e.g. "docker.io/kubeovn/vpc-nat-gateway:v1.16.0"
func (f *Framework) VpcNatGatewayImage() string {
	return f.Image("vpc-nat-gateway")
}

func (f *Framework) ValidateFinalizers(obj metav1.Object) {
	ginkgo.GinkgoHelper()

	finalizers := obj.GetFinalizers()
	if !f.VersionPriorTo(1, 13) {
		ExpectContainElement(finalizers, util.KubeOVNControllerFinalizer)
		ExpectNotContainElement(finalizers, util.DepreciatedFinalizerName)
	} else {
		ExpectContainElement(finalizers, util.DepreciatedFinalizerName)
	}
}

func Describe(text string, body func()) bool {
	return ginkgo.Describe("[CNI:Kube-OVN] "+text, ginkgo.Offset(1), body)
}

func FDescribe(text string, body func()) bool {
	return ginkgo.FDescribe("[CNI:Kube-OVN] "+text, ginkgo.Offset(1), body)
}

func SerialDescribe(text string, body func()) bool {
	return ginkgo.Describe("[CNI:Kube-OVN] "+text, ginkgo.Offset(1), ginkgo.Serial, body)
}

func OrderedDescribe(text string, body func()) bool {
	return ginkgo.Describe("[CNI:Kube-OVN] "+text, ginkgo.Offset(1), ginkgo.Ordered, body)
}

var ConformanceIt func(args ...any) bool = framework.ConformanceIt

func DisruptiveIt(text string, body any) bool {
	return framework.It(text, ginkgo.Offset(1), body, framework.WithDisruptive())
}
