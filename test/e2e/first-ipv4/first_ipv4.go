package firstipv4

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"

	v1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

const firstIPv4HTTPPort = "8080"

var _ = framework.Describe("[group:first-ipv4]", func() {
	f := framework.NewDefaultFramework("first-ipv4")

	var podClient *framework.PodClient
	var subnetClient *framework.SubnetClient
	var namespaceName, subnetName, serverPodName, clientPodName, cidr, firstIPv4 string
	var subnet *v1.Subnet

	ginkgo.BeforeEach(func() {
		f.SkipVersionPriorTo(1, 16, "Support for allow-first-ipv4-address was introduced in v1.16")
		if !f.HasIPv4() {
			ginkgo.Skip("This suite requires IPv4 support")
		}

		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		namespaceName = f.Namespace.Name
		subnetName = "subnet-" + framework.RandomSuffix()
		serverPodName = "server-" + framework.RandomSuffix()
		clientPodName = "client-" + framework.RandomSuffix()
		cidr = strings.Replace(framework.RandomCIDR(framework.IPv4), "/24", "/29", 1)
		firstIPv4 = util.FirstIPv4Address(cidr)
		framework.ExpectNotEmpty(firstIPv4, "failed to derive the first IPv4 address from %s", cidr)

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, []string{namespaceName})
		subnet = subnetClient.CreateSync(subnet)
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting pods " + serverPodName + " and " + clientPodName)
		podClient.DeleteGracefully(serverPodName)
		podClient.DeleteGracefully(clientPodName)
		podClient.WaitForNotFound(serverPodName)
		podClient.WaitForNotFound(clientPodName)

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.Delete(subnetName)
		framework.ExpectNoError(subnetClient.WaitToDisappear(subnetName, 0, 2*time.Minute))
	})

	framework.ConformanceIt("should allow explicitly assigning the first IPv4 address and preserve pod connectivity", func() {
		ginkgo.By("Creating server pod " + serverPodName + " with first IPv4 address " + firstIPv4)
		serverAnnotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
			util.IPAddressAnnotation:     firstIPv4,
		}
		serverPod := framework.MakePod(namespaceName, serverPodName, nil, serverAnnotations, framework.AgnhostImage, nil, []string{"netexec", "--http-port", firstIPv4HTTPPort})
		serverPod = podClient.CreateSync(serverPod)

		framework.ExpectHaveKeyWithValue(serverPod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(serverPod.Annotations, util.CidrAnnotation, subnet.Spec.CIDRBlock)
		framework.ExpectHaveKeyWithValue(serverPod.Annotations, util.GatewayAnnotation, subnet.Spec.Gateway)
		framework.ExpectHaveKeyWithValue(serverPod.Annotations, util.IPAddressAnnotation, firstIPv4)
		framework.ExpectHaveKeyWithValue(serverPod.Annotations, util.LogicalSwitchAnnotation, subnetName)
		framework.ExpectHaveKeyWithValue(serverPod.Annotations, util.RoutedAnnotation, "true")
		framework.ExpectEqual(serverPod.Status.PodIP, firstIPv4)
		framework.ExpectContainElement(util.PodIPs(*serverPod), firstIPv4)

		ginkgo.By("Creating client pod " + clientPodName)
		clientAnnotations := map[string]string{util.LogicalSwitchAnnotation: subnetName}
		clientPod := framework.MakePod(namespaceName, clientPodName, nil, clientAnnotations, f.KubeOVNImage, []string{"sleep", "infinity"}, nil)
		clientPod = podClient.CreateSync(clientPod)

		clientIPv4, _ := util.SplitStringIP(clientPod.Annotations[util.IPAddressAnnotation])
		framework.ExpectNotEmpty(clientIPv4)
		framework.ExpectEqual(clientIPv4 == firstIPv4, false)
		framework.ExpectHaveKeyWithValue(clientPod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(clientPod.Annotations, util.LogicalSwitchAnnotation, subnetName)

		ginkgo.By("Verifying connectivity from client pod to the first-IPv4 server pod")
		curlCmd := fmt.Sprintf("curl -q -s --connect-timeout 5 --max-time 5 http://%s/clientip", net.JoinHostPort(firstIPv4, firstIPv4HTTPPort))
		var output string
		framework.WaitUntil(time.Second, 30*time.Second, func(ctx context.Context) (bool, error) {
			stdout, stderr, err := framework.ExecShellInPod(ctx, f, namespaceName, clientPodName, curlCmd)
			if err != nil {
				framework.Logf("curl from pod %s failed, stdout=%q stderr=%q err=%v", clientPodName, stdout, stderr, err)
				return false, nil
			}
			output = strings.TrimSpace(stdout)
			return output != "", nil
		}, fmt.Sprintf("pod %s can reach %s on %s", clientPodName, serverPodName, firstIPv4))

		observedClientIP, _, err := net.SplitHostPort(output)
		framework.ExpectNoError(err)
		framework.ExpectEqual(observedClientIP, clientIPv4)
	})
})
