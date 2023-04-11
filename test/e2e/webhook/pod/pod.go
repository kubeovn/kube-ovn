package pod

import (
	"context"
	"fmt"
	"math/big"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"

	"github.com/onsi/ginkgo/v2"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.Describe("[group:webhook-pod]", func() {
	f := framework.NewDefaultFramework("webhook-pod")

	var cs clientset.Interface
	var podClient *framework.PodClient
	var subnetClient *framework.SubnetClient
	var namespaceName, subnetName, podName string
	var subnet *apiv1.Subnet
	var cidr, image, conflictName, firstIPv4, lastIPv4 string

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		namespaceName = f.Namespace.Name
		subnetName = "subnet-" + framework.RandomSuffix()
		podName = "pod-" + framework.RandomSuffix()
		conflictName = podName + "-conflict"
		cidr = framework.RandomCIDR(f.ClusterIpFamily)
		if image == "" {
			image = framework.GetKubeOvnImage(cs)
		}
		cidrV4, _ := util.SplitStringIP(cidr)
		if cidrV4 == "" {
			firstIPv4 = ""
			lastIPv4 = ""
		} else {
			firstIPv4, _ = util.FirstIP(cidrV4)
			lastIPv4, _ = util.LastIP(cidrV4)
		}

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, []string{namespaceName})
		subnet = subnetClient.CreateSync(subnet)
	})
	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(podName)

		ginkgo.By("Deleting pod " + conflictName)
		podClient.DeleteSync(conflictName)

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
	})

	framework.ConformanceIt("validate static ip by pod annotation", func() {
		ginkgo.By("Creating pod " + podName)
		cmd := []string{"sh", "-c", "sleep infinity"}

		ginkgo.By("validate ip validation")
		annotations := map[string]string{
			util.IpAddressAnnotation: "10.10.10.10.10",
		}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, image, cmd, nil)
		_, err := podClient.PodInterface.Create(context.TODO(), pod, metav1.CreateOptions{})
		framework.ExpectError(err, fmt.Errorf("ip %s is not a valid %s", annotations[util.IpAddressAnnotation], util.IpAddressAnnotation))

		ginkgo.By("validate pod ip not in subnet cidr")
		staticIP := util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(lastIPv4), big.NewInt(10)))
		annotations = map[string]string{
			util.CidrAnnotation:      cidr,
			util.IpAddressAnnotation: staticIP,
		}
		framework.Logf("validate ip not in subnet range, cidr %s, staticip %s", cidr, staticIP)
		pod.Annotations = annotations

		_, err = podClient.PodInterface.Create(context.TODO(), pod, metav1.CreateOptions{})
		framework.ExpectError(err, fmt.Errorf("%s not in cidr %s", staticIP, cidr))

		ginkgo.By("validate pod ippool not in subnet cidr")
		startIP := util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(lastIPv4), big.NewInt(10)))
		endIP := util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(lastIPv4), big.NewInt(20)))
		ipPool := startIP + "," + endIP
		annotations = map[string]string{
			util.CidrAnnotation:   cidr,
			util.IpPoolAnnotation: ipPool,
		}
		framework.Logf("validate ippool not in subnet range, cidr %s, ippool %s", cidr, ipPool)
		pod.Annotations = annotations
		_, err = podClient.PodInterface.Create(context.TODO(), pod, metav1.CreateOptions{})
		framework.ExpectError(err, fmt.Errorf("%s not in cidr %s", ipPool, cidr))

		ginkgo.By("validate pod static ip success")
		staticIP = util.BigInt2Ip(big.NewInt(0).Add(util.Ip2BigInt(firstIPv4), big.NewInt(10)))
		annotations = map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
			util.CidrAnnotation:          cidr,
			util.IpAddressAnnotation:     staticIP,
		}
		pod.Annotations = annotations
		_ = podClient.CreateSync(pod)
		ipCr := podName + "." + namespaceName

		framework.WaitUntil(func() (bool, error) {
			checkPod, err := podClient.PodInterface.Get(context.TODO(), podName, metav1.GetOptions{})
			framework.ExpectNoError(err)
			return checkPod.Annotations[util.RoutedAnnotation] == "true", nil
		}, fmt.Sprintf("pod's annotation %s is true", util.RoutedAnnotation))

		ginkgo.By("validate pod ip conflict")
		framework.Logf("validate ip conflict, pod %s, ip cr %s, conflict pod %s", podName, ipCr, conflictName)
		conflictPod := framework.MakePod(namespaceName, conflictName, nil, annotations, image, cmd, nil)
		_, err = podClient.PodInterface.Create(context.TODO(), conflictPod, metav1.CreateOptions{})
		framework.ExpectError(err, fmt.Errorf("annotation static-ip %s is conflict with ip crd %s, ip %s", staticIP, ipCr, staticIP))
	})
})
