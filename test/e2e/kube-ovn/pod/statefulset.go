package pod

import (
	clientset "k8s.io/client-go/kubernetes"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.Describe("[group:pod]", func() {
	f := framework.NewDefaultFramework("pod")

	var cs clientset.Interface
	var podClient *framework.PodClient
	var stsClient *framework.StatefulSetClient
	var stsName string
	var image string

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		podClient = f.PodClient()
		stsClient = f.StatefulSetClient()
		stsName = "sts-" + framework.RandomSuffix()
		if image == "" {
			image = framework.GetKubeOvnImage(cs)
		}
	})
	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting sts " + stsName)
		stsClient.DeleteSync(stsName)
	})

	framework.ConformanceIt("Should support statefulset scale Replica", func() {
		// add this case for pr https://github.com/kubeovn/kube-ovn/pull/3777
		replicas := 3
		labels := map[string]string{"app": stsName}

		ginkgo.By("Creating statefulset " + stsName)
		sts := framework.MakeStatefulSet(stsName, stsName, int32(replicas), labels, framework.PauseImage)
		sts = stsClient.CreateSync(sts)
		ginkgo.By("Delete pod for statefulset " + stsName)
		pod2Name := stsName + "-2"
		pod2 := podClient.GetPod(pod2Name)
		pod2IP := pod2.Annotations[util.IPAddressAnnotation]
		err := podClient.Delete(pod2Name)
		framework.ExpectNoError(err, "failed to delete pod "+pod2Name)
		stsClient.WaitForRunningAndReady(sts)
		pod2 = podClient.GetPod(pod2Name)
		framework.ExpectEqual(pod2.Annotations[util.IPAddressAnnotation], pod2IP)

		ginkgo.By("Scale sts replica to 1 and Scale replica to 3 " + stsName)
		patchSts := framework.MakeStatefulSet(stsName, stsName, int32(1), labels, framework.PauseImage)
		stsClient.PatchSync(sts, patchSts)
		patchSts = framework.MakeStatefulSet(stsName, stsName, int32(3), labels, framework.PauseImage)
		sts = stsClient.Get(stsName)
		stsClient.PatchSync(sts, patchSts)
		stsClient.WaitForRunningAndReady(patchSts)
	})
})
