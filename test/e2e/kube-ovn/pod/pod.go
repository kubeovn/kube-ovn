package pod

import (
	"encoding/json"
	"strings"

	clientset "k8s.io/client-go/kubernetes"

	"github.com/onsi/ginkgo/v2"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/iproute"
)

var _ = framework.Describe("[group:pod]", func() {
	f := framework.NewDefaultFramework("pod")

	var cs clientset.Interface
	var podClient *framework.PodClient
	var subnetClient *framework.SubnetClient
	var namespaceName, subnetName, podName string
	var subnet *apiv1.Subnet
	var cidr, image string

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		namespaceName = f.Namespace.Name
		subnetName = "subnet-" + framework.RandomSuffix()
		podName = "pod-" + framework.RandomSuffix()
		cidr = framework.RandomCIDR(f.ClusterIpFamily)
		if image == "" {
			image = framework.GetKubeOvnImage(cs)
		}

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, []string{namespaceName})
		subnet = subnetClient.CreateSync(subnet)
	})
	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(podName)

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
	})

	framework.ConformanceIt("should support configuring routes via pod annotation", func() {
		f.SkipVersionPriorTo(1, 12, "This feature was introduced inn v1.12")

		ginkgo.By("Generating routes")
		routes := make([]request.Route, 0, 4)
		for _, s := range strings.Split(cidr, ",") {
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

		ginkgo.By("Creating pod " + podName)
		annotations := map[string]string{
			util.RoutesAnnotation: string(buff),
		}
		cmd := []string{"sh", "-c", "sleep infinity"}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, image, cmd, nil)
		pod = podClient.CreateSync(pod)

		framework.ExpectHaveKeyWithValue(pod.Annotations, util.AllocatedAnnotation, "true")
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.CidrAnnotation, subnet.Spec.CIDRBlock)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.GatewayAnnotation, subnet.Spec.Gateway)
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.LogicalSwitchAnnotation, subnet.Name)
		framework.ExpectMAC(pod.Annotations[util.MacAddressAnnotation])
		framework.ExpectHaveKeyWithValue(pod.Annotations, util.RoutedAnnotation, "true")

		podRoutes, err := iproute.RouteShow("", "eth0", func(cmd ...string) ([]byte, []byte, error) {
			return framework.KubectlExec(pod.Namespace, pod.Name, cmd...)
		})
		framework.ExpectNoError(err)

		actualRoutes := make([]request.Route, len(podRoutes))
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
