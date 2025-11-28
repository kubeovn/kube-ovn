package node

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.SerialDescribe("[group:node]", func() {
	f := framework.NewDefaultFramework("node-network")

	var cs clientset.Interface
	var podClient *framework.PodClient
	var subnetClient *framework.SubnetClient
	var podName, subnetName, namespaceName string
	var cidr string
	var node *corev1.Node
	var nodeNetworks map[string]string
	var originalAnnotation string

	ginkgo.BeforeEach(func() {
		f.SkipVersionPriorTo(1, 15, "This feature was introduced in v1.15")

		cs = f.ClientSet
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		namespaceName = f.Namespace.Name
		podName = "pod-" + framework.RandomSuffix()
		subnetName = "subnet-" + framework.RandomSuffix()
		cidr = framework.RandomCIDR(f.ClusterIPFamily)

		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodeList.Items)
		node = &nodeList.Items[0]

		originalAnnotation = node.Annotations[util.NodeNetworksAnnotation]
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting pod " + podName)
		podClient.DeleteSync(podName)

		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)

		ginkgo.By("Restoring node annotation")
		patchPayload := fmt.Sprintf(`[{"op": "replace", "path": "/metadata/annotations/%s", "value": %q}]`,
			strings.ReplaceAll(util.NodeNetworksAnnotation, "/", "~1"), originalAnnotation)
		if originalAnnotation == "" {
			patchPayload = fmt.Sprintf(`[{"op": "remove", "path": "/metadata/annotations/%s"}]`,
				strings.ReplaceAll(util.NodeNetworksAnnotation, "/", "~1"))
		}
		_, err := cs.CoreV1().Nodes().Patch(context.Background(), node.Name, "application/json-patch+json",
			[]byte(patchPayload), metav1.PatchOptions{})
		if err != nil {
			framework.Logf("Failed to restore node annotation: %v", err)
		}
	})

	framework.ConformanceIt("should sync node networks annotation to OVS", func() {
		ginkgo.By("Getting node internal IP as test encap IP")
		storageNetowkrIP := "10.10.10.10"
		framework.ExpectNotEmpty(storageNetowkrIP, "Node should have internal IP")

		nodeNetworks = map[string]string{
			"storage": storageNetowkrIP,
		}
		nodeNetworksJSON, err := json.Marshal(nodeNetworks)
		framework.ExpectNoError(err)

		ginkgo.By("Patching node with node_networks annotation")
		patchPayload := fmt.Sprintf(`{"metadata":{"annotations":{%q:%q}}}`,
			util.NodeNetworksAnnotation, string(nodeNetworksJSON))
		_, err = cs.CoreV1().Nodes().Patch(context.Background(), node.Name, "application/strategic-merge-patch+json",
			[]byte(patchPayload), metav1.PatchOptions{})
		framework.ExpectNoError(err)

		ginkgo.By("Getting ovs-ovn pod on node " + node.Name)
		ovsPod := getOvsPodOnNode(f, node.Name)

		ginkgo.By("Waiting for OVS external-ids to be updated")
		framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			cmd := "ovs-vsctl --no-heading get open . external-ids:ovn-encap-ip"
			output, err := e2epodoutput.RunHostCmd(ovsPod.Namespace, ovsPod.Name, cmd)
			if err != nil {
				return false, nil
			}
			output = strings.Trim(strings.TrimSpace(output), "\"")
			framework.Logf("ovn-encap-ip: %s", output)
			return strings.Contains(output, storageNetowkrIP), nil
		}, "OVS ovn-encap-ip should contain the node network IP")

		ginkgo.By("Verifying ovn-encap-ip-default is set")
		cmd := "ovs-vsctl --no-heading get open . external-ids:ovn-encap-ip-default"
		output, err := e2epodoutput.RunHostCmd(ovsPod.Namespace, ovsPod.Name, cmd)
		framework.ExpectNoError(err)
		output = strings.Trim(strings.TrimSpace(output), "\"")
		framework.ExpectNotEmpty(output, "ovn-encap-ip-default should be set")
		framework.Logf("ovn-encap-ip-default: %s", output)
	})

	framework.ConformanceIt("should set encap-ip on pod interface when nodeNetwork is specified", func() {
		ginkgo.By("Getting node internal IP as test encap IP")
		storageNetworkIP := "10.10.10.10"
		framework.ExpectNotEmpty(storageNetworkIP, "Node should have internal IP")

		nodeNetworks = map[string]string{
			"storage": storageNetworkIP,
		}
		nodeNetworksJSON, err := json.Marshal(nodeNetworks)
		framework.ExpectNoError(err)

		ginkgo.By("Patching node with node_networks annotation")
		patchPayload := fmt.Sprintf(`{"metadata":{"annotations":{%q:%q}}}`,
			util.NodeNetworksAnnotation, string(nodeNetworksJSON))
		_, err = cs.CoreV1().Nodes().Patch(context.Background(), node.Name, "application/strategic-merge-patch+json",
			[]byte(patchPayload), metav1.PatchOptions{})
		framework.ExpectNoError(err)

		ginkgo.By("Creating subnet " + subnetName + " with nodeNetwork=storage")
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, []string{namespaceName})
		subnet.Spec.NodeNetwork = "storage"
		subnetClient.CreateSync(subnet)

		ginkgo.By("Creating pod " + podName)
		annotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, framework.AgnhostImage, nil, nil)
		pod.Spec.NodeName = node.Name
		pod = podClient.CreateSync(pod)

		ginkgo.By("Getting ovs-ovn pod on node " + node.Name)
		ovsPod := getOvsPodOnNode(f, node.Name)

		ginkgo.By("Verifying interface encap-ip is set correctly")
		ifaceID := fmt.Sprintf("%s.%s", podName, namespaceName)
		framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			cmd := fmt.Sprintf(`ovs-vsctl --no-heading get interface %s external-ids:encap-ip 2>/dev/null || ovs-vsctl --no-heading --columns=external_ids find interface external-ids:iface-id="%s"`, pod.Name, ifaceID)
			output, err := e2epodoutput.RunHostCmd(ovsPod.Namespace, ovsPod.Name, cmd)
			if err != nil {
				return false, nil
			}
			framework.Logf("Interface external-ids output: %s", output)
			return strings.Contains(output, "encap-ip="+storageNetworkIP) || strings.Contains(output, `encap-ip="`+storageNetworkIP+`"`), nil
		}, "Interface should have encap-ip set to "+storageNetworkIP)
	})

	framework.ConformanceIt("should create pod without nodeNetwork specified", func() {
		ginkgo.By("Creating subnet " + subnetName + " without nodeNetwork")
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, []string{namespaceName})
		subnetClient.CreateSync(subnet)

		ginkgo.By("Creating pod " + podName)
		annotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, framework.AgnhostImage, nil, nil)
		pod.Spec.NodeName = node.Name
		pod = podClient.CreateSync(pod)

		ginkgo.By("Verifying pod is running")
		framework.ExpectEqual(pod.Status.Phase, corev1.PodRunning)
	})
})

func getOvsPodOnNode(f *framework.Framework, node string) *corev1.Pod {
	ginkgo.GinkgoHelper()

	daemonSetClient := f.DaemonSetClientNS(framework.KubeOvnNamespace)
	ds := daemonSetClient.Get("ovs-ovn")
	pod, err := daemonSetClient.GetPodOnNode(ds, node)
	framework.ExpectNoError(err)
	return pod
}
