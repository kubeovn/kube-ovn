package pod

import (
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"k8s.io/utils/ptr"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.Describe("[group:pod]", func() {
	f := framework.NewDefaultFramework("pod")

	var podClient *framework.PodClient
	var stsClient *framework.StatefulSetClient
	var stsName string

	ginkgo.BeforeEach(ginkgo.NodeTimeout(time.Second), func(_ ginkgo.SpecContext) {
		podClient = f.PodClient()
		stsClient = f.StatefulSetClient()
		stsName = "sts-" + framework.RandomSuffix()
	})
	ginkgo.AfterEach(ginkgo.NodeTimeout(20*time.Second), func(ctx ginkgo.SpecContext) {
		ginkgo.By("Deleting sts " + stsName)
		stsClient.DeleteSync(ctx, stsName)
	})

	framework.ConformanceIt("Should support statefulset scale Replica", ginkgo.SpecTimeout(2*time.Minute), func(ctx ginkgo.SpecContext) {
		// add this case for pr https://github.com/kubeovn/kube-ovn/pull/3777
		replicas := 3
		labels := map[string]string{"app": stsName}

		ginkgo.By("Creating statefulset " + stsName)
		sts := framework.MakeStatefulSet(stsName, stsName, int32(replicas), labels, framework.PauseImage)
		sts = stsClient.CreateSync(ctx, sts)
		ginkgo.By("Delete pod for statefulset " + stsName)
		pod2Name := stsName + "-2"
		pod2 := podClient.Get(ctx, pod2Name)
		pod2IP := pod2.Annotations[util.IPAddressAnnotation]
		err := podClient.Delete(ctx, pod2Name)
		framework.ExpectNoError(err, "failed to delete pod "+pod2Name)
		stsClient.WaitForRunningAndReady(ctx, sts)
		pod2 = podClient.Get(ctx, pod2Name)
		framework.ExpectEqual(pod2.Annotations[util.IPAddressAnnotation], pod2IP)

		ginkgo.By("Scale sts replicas to 1")
		sts = stsClient.Get(ctx, stsName)
		patchSts := sts.DeepCopy()
		patchSts.Spec.Replicas = ptr.To(int32(1))
		stsClient.PatchSync(ctx, sts, patchSts)

		for index := 1; index <= 2; index++ {
			podName := fmt.Sprintf("%s-%d", stsName, index)
			ginkgo.By(fmt.Sprintf("Waiting pod %s to be deleted", podName))
			podClient.WaitForNotFound(ctx, podName)
		}
		ginkgo.By("Scale sts replicas to 3")
		sts = stsClient.Get(ctx, stsName)
		patchSts = sts.DeepCopy()
		patchSts.Spec.Replicas = ptr.To(int32(3))
		stsClient.PatchSync(ctx, sts, patchSts)
		ginkgo.By("Waiting for statefulset " + stsName + " to be ready")
		stsClient.WaitForRunningAndReady(ctx, patchSts)
	})
})
