package pod

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/iproute"
)

var _ = framework.SerialDescribe("[group:pod]", func() {
	f := framework.NewDefaultFramework("pod")

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

	framework.ConformanceIt("should support north gateway via pod annotation", func() {
		f.SkipVersionPriorTo(1, 12, "This feature was introduced in v1.12")
		if f.ClusterNetworkMode == "underlay" {
			ginkgo.Skip("This test is only for overlay network")
		}

		ginkgo.By("Creating pod " + podName + " with north gateway annotation")
		northGateway := "100.64.0.100"
		ipSuffix := "ip4"
		if f.ClusterIPFamily == "ipv6" {
			northGateway = "fd00:100:64::100"
			ipSuffix = "ip6"
		}

		annotations := map[string]string{
			util.NorthGatewayAnnotation: northGateway,
		}
		cmd := []string{"sleep", "infinity"}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, f.KubeOVNImage, cmd, nil)
		pod = podClient.CreateSync(pod)

		podIP := pod.Status.PodIP
		nbCmd := fmt.Sprintf("ovn-nbctl --format=csv --data=bare --no-heading --columns=match,action,nexthops find logical_router_policy priority=%d", util.NorthGatewayRoutePolicyPriority)
		out, _, err := framework.NBExec(nbCmd)
		framework.ExpectNoError(err)
		framework.ExpectEqual(strings.TrimSpace(string(out)), fmt.Sprintf("%s.src == %s,reroute,%s", ipSuffix, podIP, northGateway))

		ginkgo.By("Deleting pod " + podName + " with north gateway annotation")
		f.PodClientNS(namespaceName).DeleteSync(podName)
		framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			out, _, err = framework.NBExec(nbCmd)
			if err == nil && strings.TrimSpace(string(out)) == "" {
				return true, nil
			}
			return false, err
		}, "policy has been gc")

		ginkgo.By("gc policy route")
		podClient.CreateSync(framework.MakePod(namespaceName, podName, nil, annotations, f.KubeOVNImage, cmd, nil))

		ginkgo.By("restart kube-ovn-controller")
		deployClient := f.DeploymentClientNS(framework.KubeOvnNamespace)
		deploy := deployClient.Get("kube-ovn-controller")
		framework.ExpectNotNil(deploy.Spec.Replicas)
		deployClient.SetScale(deploy.Name, 0)
		deployClient.RolloutStatus(deploy.Name)

		f.PodClientNS(namespaceName).DeleteSync(podName)

		deployClient.SetScale(deploy.Name, 1)
		deployClient.RolloutStatus(deploy.Name)

		framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			out, _, err = framework.NBExec(nbCmd)
			if err == nil && strings.TrimSpace(string(out)) == "" {
				return true, nil
			}
			return false, err
		}, "policy has been gc")

		ginkgo.By("remove legacy lsp")
		deleteLspCmd := fmt.Sprintf("ovn-nbctl --if-exists lsp-del %s.%s", pod.Name, pod.Namespace)
		_, _, err = framework.NBExec(deleteLspCmd)
		framework.ExpectNoError(err)
		err = f.KubeOVNClientSet.KubeovnV1().IPs().Delete(context.Background(), fmt.Sprintf("%s.%s", pod.Name, pod.Namespace), metav1.DeleteOptions{})
	})

	framework.ConformanceIt("should support configuring routes via pod annotation", func() {
		f.SkipVersionPriorTo(1, 12, "This feature was introduced in v1.12")

		ginkgo.By("Generating routes")
		routes := make([]request.Route, 0, 4)
		for s := range strings.SplitSeq(cidr, ",") {
			gw, err := util.LastIP(s)
			framework.ExpectNoError(err)
			var dst string
			switch util.CheckProtocol(gw) {
			case apiv1.ProtocolIPv4:
				dst = "114.114.114.0/26"
			case apiv1.ProtocolIPv6:
				dst = "2400:3200::/126"
			}
			routes = append(routes, request.Route{Gateway: gw}, request.Route{Gateway: framework.PrevIP(gw), Destination: dst})
		}

		buff, err := json.Marshal(routes)
		framework.ExpectNoError(err)

		ginkgo.By("Creating subnet " + subnetName)
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, []string{namespaceName})
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating pod " + podName)
		annotations := map[string]string{
			util.RoutesAnnotation: string(buff),
		}
		cmd := []string{"sleep", "infinity"}
		pod := framework.MakePrivilegedPod(namespaceName, podName, nil, annotations, f.KubeOVNImage, cmd, nil)
		pod = podClient.CreateSync(pod)

		ginkgo.By("Validating pod annotations")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, subnet.Spec.CIDRBlock)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, subnet.Spec.Gateway)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnet.Name)
		framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")

		ginkgo.By("Getting pod routes")
		podRoutes, err := iproute.RouteShow("", "eth0", func(cmd ...string) ([]byte, []byte, error) {
			return framework.KubectlExec(pod.Namespace, pod.Name, cmd...)
		})
		framework.ExpectNoError(err)

		ginkgo.By("Validating pod routes")
		actualRoutes := make([]request.Route, 0, len(podRoutes))
		for _, r := range podRoutes {
			if r.Gateway != "" || r.Dst != "" {
				actualRoutes = append(actualRoutes, request.Route{Destination: r.Dst, Gateway: r.Gateway})
			}
		}
		for _, r := range routes {
			if r.Destination == "" {
				r.Destination = "default"
			}
			framework.ExpectContainElement(actualRoutes, r)
		}
	})
})
