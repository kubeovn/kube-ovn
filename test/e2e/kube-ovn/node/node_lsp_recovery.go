package node

import (
	"context"
	"fmt"
	"strings"
	"time"

	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.SerialDescribe("[group:node]", func() {
	f := framework.NewDefaultFramework("node-lsp-recovery")

	framework.DisruptiveIt("should recreate a missing node logical switch port", func() {
		f.SkipVersionPriorTo(1, 15, "Node logical switch ports are managed by Kube-OVN")

		ginkgo.By("Getting a ready schedulable node")
		nodeList, err := e2enode.GetReadySchedulableNodes(context.Background(), f.ClientSet)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodeList.Items)
		node := nodeList.Items[0]
		portName := util.NodeLspName(node.Name)
		nodeSwitch := node.Annotations[util.LogicalSwitchAnnotation]
		framework.ExpectNotEmpty(nodeSwitch, "node logical switch annotation should be set")

		ginkgo.By("Temporarily shortening the controller GC interval")
		deploymentClient := framework.NewDeploymentClient(f.ClientSet, framework.KubeOvnNamespace)
		originalDeployment := deploymentClient.Get("kube-ovn-controller")
		var originalControllerArgs []string
		modifiedDeployment := originalDeployment.DeepCopy()
		foundController := false
		for i := range modifiedDeployment.Spec.Template.Spec.Containers {
			container := &modifiedDeployment.Spec.Template.Spec.Containers[i]
			if container.Name != "kube-ovn-controller" {
				continue
			}
			foundController = true
			originalControllerArgs = append([]string(nil), container.Args...)
			foundGCInterval := false
			for j, arg := range container.Args {
				if strings.HasPrefix(arg, "--gc-interval=") {
					container.Args[j] = "--gc-interval=5"
					foundGCInterval = true
					break
				}
			}
			if !foundGCInterval {
				container.Args = append(container.Args, "--gc-interval=5")
			}
			break
		}
		framework.ExpectTrue(foundController, "kube-ovn-controller container was not found")
		ginkgo.DeferCleanup(func() {
			currentDeployment := deploymentClient.Get("kube-ovn-controller")
			restoredDeployment := currentDeployment.DeepCopy()
			for i := range restoredDeployment.Spec.Template.Spec.Containers {
				container := &restoredDeployment.Spec.Template.Spec.Containers[i]
				if container.Name == "kube-ovn-controller" {
					container.Args = originalControllerArgs
					break
				}
			}
			deploymentClient.PatchSync(currentDeployment, restoredDeployment)
		})
		deploymentClient.PatchSync(originalDeployment, modifiedDeployment)

		nodeLSPUUID := func() (string, error) {
			output, _, err := framework.NBExec("ovn-nbctl --bare --no-heading lsp-list " + nodeSwitch)
			if err != nil {
				return "", err
			}
			for line := range strings.SplitSeq(string(output), "\n") {
				uuid, name, ok := strings.Cut(strings.TrimSpace(line), " (")
				if ok && name == portName+")" {
					return uuid, nil
				}
			}
			return "", nil
		}

		ginkgo.By("Verifying the node logical switch port exists before deletion")
		var originalUUID string
		framework.WaitUntil(time.Second, time.Minute, func(_ context.Context) (bool, error) {
			var err error
			originalUUID, err = nodeLSPUUID()
			if err != nil {
				return false, nil
			}
			return originalUUID != "", nil
		}, "node logical switch port should exist before deletion")

		ginkgo.By("Deleting node logical switch port " + portName)
		_, _, err = framework.NBExec(fmt.Sprintf("ovn-nbctl --if-exists lsp-del %s", portName))
		framework.ExpectNoError(err)

		ginkgo.By("Waiting for the controller to recreate the node logical switch port")
		framework.WaitUntil(time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			recreatedUUID, err := nodeLSPUUID()
			if err != nil {
				return false, nil
			}
			return recreatedUUID != "" && recreatedUUID != originalUUID, nil
		}, "controller should recreate the missing node logical switch port")
	})
})
