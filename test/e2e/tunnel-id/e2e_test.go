package tunnel_id

import (
	"errors"
	"flag"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func init() {
	klog.SetOutput(ginkgo.GinkgoWriter)

	// Register flags.
	config.CopyFlags(config.Flags, flag.CommandLine)
	k8sframework.RegisterCommonFlags(flag.CommandLine)
	k8sframework.RegisterClusterFlags(flag.CommandLine)
}

func TestE2E(t *testing.T) {
	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)
	e2e.RunE2ETests(t)
}

var _ = framework.Describe("[group:tunnel-id]", func() {
	f := framework.NewDefaultFramework("tunnel-id")

	var podClient *framework.PodClient
	var subnetClient *framework.SubnetClient
	var namespaceName, subnetName, podName string
	var cidr string

	ginkgo.BeforeEach(func() {
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		namespaceName = f.Namespace.Name
		subnetName = "subnet-" + framework.RandomSuffix()
		podName = "pod-" + framework.RandomSuffix()
		cidr = framework.RandomCIDR(f.ClusterIPFamily)
	})
	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(podName)

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
	})

	framework.ConformanceIt("should have consistent tunnel key between subnet and pod", func() {
		f.SkipVersionPriorTo(1, 16, "Tunnel key in status was introduced in v1.16")

		ginkgo.By("Creating subnet " + subnetName)
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, []string{namespaceName})
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Validating subnet has tunnel key in status")
		var subnetTunnelKey int
		gomega.Eventually(func() error {
			subnet = subnetClient.Get(subnetName)
			subnetTunnelKey = subnet.Status.TunnelKey
			if subnetTunnelKey == 0 {
				return errors.New("subnet tunnel key is not set")
			}
			return nil
		}).WithTimeout(30 * time.Second).WithPolling(2 * time.Second).Should(gomega.Succeed())
		framework.ExpectNotEqual(subnetTunnelKey, 0, "subnet should have tunnel key in status")
		framework.Logf("Subnet %s has tunnel key: %d", subnetName, subnetTunnelKey)

		ginkgo.By("Creating pod " + podName + " in subnet " + subnetName)
		annotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}
		cmd := []string{"sleep", "infinity"}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, f.KubeOVNImage, cmd, nil)
		pod = podClient.CreateSync(pod)

		ginkgo.By("Validating pod has tunnel key annotation matching subnet")
		podTunnelKeyAnnotation := fmt.Sprintf(util.TunnelKeyAnnotationTemplate, util.OvnProvider)
		podTunnelKey := pod.Annotations[podTunnelKeyAnnotation]
		framework.ExpectNotEmpty(podTunnelKey, "pod should have tunnel key annotation")
		framework.ExpectEqual(podTunnelKey, strconv.Itoa(subnetTunnelKey), "pod tunnel key should match subnet tunnel key")
		framework.Logf("Pod %s has tunnel key: %s (matches subnet)", podName, podTunnelKey)
	})
})
