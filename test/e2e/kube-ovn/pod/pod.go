package pod

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientset "k8s.io/client-go/kubernetes"

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
	var vpcClient *framework.VpcClient
	var namespaceName, subnetName, podName, vpcName string
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
		vpcClient = f.VpcClient()
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

		if vpcName != "" {
			ginkgo.By("Deleting custom vpc " + vpcName)
			vpcClient.DeleteSync(vpcName)
		}
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

	framework.ConformanceIt("should support http and tcp liveness probe and readiness probe in custom vpc pod ", func() {
		f.SkipVersionPriorTo(1, 12, "This feature was introduced in v1.12")

		ginkgo.By("Create Custom Vpc subnet Pod")
		vpcName = "vpc-" + framework.RandomSuffix()
		customVPC := framework.MakeVpc(vpcName, "", false, false, []string{namespaceName})
		vpcClient.CreateSync(customVPC)

		ginkgo.By("Creating subnet " + subnetName)
		cidr = framework.RandomCIDR(f.ClusterIpFamily)
		subnet := framework.MakeSubnet(vpcName, "", cidr, "", vpcName, "", nil, nil, []string{namespaceName})
		_ = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating pod with HTTP liveness and readiness probe that port is accessible " + subnetName)

		pod := framework.MakePod(namespaceName, podName, nil, nil, framework.NginxImage, nil, nil)
		pod.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Port: intstr.FromInt(80),
				},
			},
			InitialDelaySeconds: 15,
		}
		pod.Spec.Containers[0].LivenessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Port: intstr.FromInt(80),
				},
			},
			InitialDelaySeconds: 10,
		}

		pod = podClient.CreateSync(pod)

		framework.ExpectEqual(pod.Status.ContainerStatuses[0].Ready, true)

		podClient.DeleteSync(podName)

		ginkgo.By("Creating pod with HTTP liveness and readiness probe that port is not accessible  " + subnetName)
		pod = framework.MakePod(namespaceName, podName, nil, nil, framework.NginxImage, nil, nil)
		pod.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(81),
				},
			},
			InitialDelaySeconds: 15,
		}
		pod.Spec.Containers[0].LivenessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(81),
				},
			},
			InitialDelaySeconds: 10,
		}

		pod = podClient.Create(pod)
		time.Sleep(11 * time.Second)

		pod = podClient.GetPod(podName)
		framework.ExpectEqual(pod.Status.ContainerStatuses[0].Ready, false)

		podClient.DeleteSync(podName)

		ginkgo.By("Creating pod with TCP probe liveness and readiness probe that port is accessible " + subnetName)
		pod = framework.MakePod(namespaceName, podName, nil, nil, framework.NginxImage, nil, nil)
		pod.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(80),
				},
			},
			InitialDelaySeconds: 15,
		}
		pod.Spec.Containers[0].LivenessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(80),
				},
			},
			InitialDelaySeconds: 10,
		}

		pod = podClient.CreateSync(pod)
		framework.ExpectEqual(pod.Status.ContainerStatuses[0].Ready, true)

		podClient.DeleteSync(podName)

		ginkgo.By("Creating pod with TCP probe liveness and readiness probe that port is not accessible  " + subnetName)
		pod = framework.MakePod(namespaceName, podName, nil, nil, framework.NginxImage, nil, nil)
		pod.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(81),
				},
			},
			InitialDelaySeconds: 15,
		}
		pod.Spec.Containers[0].LivenessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(81),
				},
			},
			InitialDelaySeconds: 10,
		}

		pod = podClient.Create(pod)
		time.Sleep(11 * time.Second)

		pod = podClient.GetPod(podName)
		framework.ExpectEqual(pod.Status.ContainerStatuses[0].Ready, false)
	})
})
