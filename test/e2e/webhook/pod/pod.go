package pod

import (
	"context"
	"fmt"
	"math/big"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.Describe("[group:webhook-pod]", func() {
	f := framework.NewDefaultFramework("webhook-pod")

	var cs clientset.Interface
	var podClient *framework.PodClient
	var subnetClient *framework.SubnetClient
	var namespaceName, subnetName, podName string
	var cidr, image, conflictName, firstIPv4, lastIPv4 string

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		namespaceName = f.Namespace.Name
		subnetName = "subnet-" + framework.RandomSuffix()
		podName = "pod-" + framework.RandomSuffix()
		conflictName = podName + "-conflict"
		cidr = framework.RandomCIDR(f.ClusterIPFamily)
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
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, []string{namespaceName})
		_ = subnetClient.CreateSync(subnet)
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
			util.IPAddressAnnotation: "10.10.10.10.10",
		}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, image, cmd, nil)
		_, err := podClient.PodInterface.Create(context.TODO(), pod, metav1.CreateOptions{})
		framework.ExpectError(err, "ip %s is not a valid %s", annotations[util.IPAddressAnnotation], util.IPAddressAnnotation)

		ginkgo.By("validate pod ip not in subnet cidr")
		staticIP := util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(lastIPv4), big.NewInt(10)))
		annotations = map[string]string{
			util.CidrAnnotation:      cidr,
			util.IPAddressAnnotation: staticIP,
		}
		framework.Logf("validate ip not in subnet range, cidr %s, staticip %s", cidr, staticIP)
		pod.Annotations = annotations

		_, err = podClient.PodInterface.Create(context.TODO(), pod, metav1.CreateOptions{})
		framework.ExpectError(err, "%s not in cidr %s", staticIP, cidr)

		ginkgo.By("validate pod ippool not in subnet cidr")
		startIP := util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(lastIPv4), big.NewInt(10)))
		endIP := util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(lastIPv4), big.NewInt(20)))
		ipPool := startIP + "," + endIP
		annotations = map[string]string{
			util.CidrAnnotation:   cidr,
			util.IPPoolAnnotation: ipPool,
		}
		framework.Logf("validate ippool not in subnet range, cidr %s, ippool %s", cidr, ipPool)
		pod.Annotations = annotations
		_, err = podClient.PodInterface.Create(context.TODO(), pod, metav1.CreateOptions{})
		framework.ExpectError(err, "%s not in cidr %s", ipPool, cidr)

		ginkgo.By("validate pod static ip success")
		staticIP = util.BigInt2Ip(big.NewInt(0).Add(util.IP2BigInt(firstIPv4), big.NewInt(10)))
		annotations = map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
			util.CidrAnnotation:          cidr,
			util.IPAddressAnnotation:     staticIP,
		}
		pod.Annotations = annotations
		_ = podClient.CreateSync(pod)
		ipCR := podName + "." + namespaceName

		framework.WaitUntil(2*time.Second, time.Minute, func(ctx context.Context) (bool, error) {
			checkPod, err := podClient.PodInterface.Get(ctx, podName, metav1.GetOptions{})
			framework.ExpectNoError(err)
			return checkPod.Annotations[util.RoutedAnnotation] == "true", nil
		}, fmt.Sprintf("pod's annotation %s is true", util.RoutedAnnotation))

		ginkgo.By("validate pod ip conflict")
		framework.Logf("validate ip conflict, pod %s, ip cr %s, conflict pod %s", podName, ipCR, conflictName)
		conflictPod := framework.MakePod(namespaceName, conflictName, nil, annotations, image, cmd, nil)
		_, err = podClient.PodInterface.Create(context.TODO(), conflictPod, metav1.CreateOptions{})
		framework.ExpectError(err, "annotation static-ip %s is conflict with ip crd %s, ip %s", staticIP, ipCR, staticIP)
	})
})
