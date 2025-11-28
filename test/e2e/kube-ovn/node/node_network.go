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
	var podName, podName2, subnetName, subnetName2, namespaceName string
	var cidr, cidr2 string
	var node *corev1.Node
	var nodeNetworks map[string]string
	var originalAnnotation string
	var createdPods, createdSubnets []string

	ginkgo.BeforeEach(func() {
		cs = f.ClientSet
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		namespaceName = f.Namespace.Name
		podName = "pod-" + framework.RandomSuffix()
		podName2 = "pod-" + framework.RandomSuffix()
		subnetName = "subnet-" + framework.RandomSuffix()
		subnetName2 = "subnet-" + framework.RandomSuffix()
		cidr = framework.RandomCIDR(f.ClusterIPFamily)
		cidr2 = framework.RandomCIDR(f.ClusterIPFamily)
		createdPods = nil
		createdSubnets = nil

		ginkgo.By("Getting nodes")
		nodeList, err := e2enode.GetReadySchedulableNodes(context.Background(), cs)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(nodeList.Items)
		node = &nodeList.Items[0]

		originalAnnotation = node.Annotations[util.NodeNetworksAnnotation]
	})

	ginkgo.AfterEach(func() {
		for _, name := range createdPods {
			ginkgo.By("Deleting pod " + name)
			podClient.DeleteSync(name)
		}

		for _, name := range createdSubnets {
			ginkgo.By("Deleting subnet " + name)
			subnetClient.DeleteSync(name)
		}

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
		f.SkipVersionPriorTo(1, 15, "Per-subnet encapsulation NIC selection was introduced in v1.15")

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
			return strings.Contains(output, storageNetworkIP), nil
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
		f.SkipVersionPriorTo(1, 15, "Per-subnet encapsulation NIC selection was introduced in v1.15")

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
		createdSubnets = append(createdSubnets, subnetName)

		ginkgo.By("Creating pod " + podName)
		annotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, framework.AgnhostImage, nil, nil)
		pod.Spec.NodeName = node.Name
		pod = podClient.CreateSync(pod)
		createdPods = append(createdPods, podName)

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
		f.SkipVersionPriorTo(1, 15, "Per-subnet encapsulation NIC selection was introduced in v1.15")

		ginkgo.By("Creating subnet " + subnetName + " without nodeNetwork")
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, []string{namespaceName})
		subnetClient.CreateSync(subnet)
		createdSubnets = append(createdSubnets, subnetName)

		ginkgo.By("Creating pod " + podName)
		annotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, framework.AgnhostImage, nil, nil)
		pod.Spec.NodeName = node.Name
		pod = podClient.CreateSync(pod)
		createdPods = append(createdPods, podName)

		ginkgo.By("Verifying pod is running")
		framework.ExpectEqual(pod.Status.Phase, corev1.PodRunning)
	})

	framework.ConformanceIt("should fail to create pod when subnet nodeNetwork is not found on node", func() {
		f.SkipVersionPriorTo(1, 15, "Per-subnet encapsulation NIC selection was introduced in v1.15")

		ginkgo.By("Creating subnet " + subnetName + " with nodeNetwork=nonexistent")
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, []string{namespaceName})
		subnet.Spec.NodeNetwork = "nonexistent"
		subnetClient.CreateSync(subnet)
		createdSubnets = append(createdSubnets, subnetName)

		ginkgo.By("Creating pod " + podName + " expecting failure")
		annotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, framework.AgnhostImage, nil, nil)
		pod.Spec.NodeName = node.Name
		podClient.Create(pod)
		createdPods = append(createdPods, podName)

		ginkgo.By("Waiting for pod to fail with network not found error")
		framework.WaitUntil(2*time.Second, 60*time.Second, func(_ context.Context) (bool, error) {
			p, err := cs.CoreV1().Pods(namespaceName).Get(context.Background(), podName, metav1.GetOptions{})
			if err != nil {
				return false, nil
			}
			for _, cond := range p.Status.Conditions {
				if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionTrue {
					for _, cs := range p.Status.ContainerStatuses {
						if cs.State.Waiting != nil && strings.Contains(cs.State.Waiting.Message, "network") {
							framework.Logf("Pod failed with expected error: %s", cs.State.Waiting.Message)
							return true, nil
						}
					}
				}
			}
			events, err := f.ClientSet.CoreV1().Events(namespaceName).List(context.Background(), metav1.ListOptions{
				FieldSelector: fmt.Sprintf("involvedObject.name=%s", podName),
			})
			if err != nil {
				return false, nil
			}
			for _, event := range events.Items {
				if event.Type == "Warning" && strings.Contains(event.Message, "network") && strings.Contains(event.Message, "not found") {
					framework.Logf("Found expected event: %s", event.Message)
					return true, nil
				}
			}
			return false, nil
		}, "Pod should fail with network not found error")
	})

	framework.ConformanceIt("should dynamically update node networks annotation", func() {
		f.SkipVersionPriorTo(1, 15, "Per-subnet encapsulation NIC selection was introduced in v1.15")

		ginkgo.By("Getting ovs-ovn pod on node " + node.Name)
		ovsPod := getOvsPodOnNode(f, node.Name)

		ginkgo.By("Getting current OVS encap IPs")
		cmd := "ovs-vsctl --no-heading get open . external-ids:ovn-encap-ip"
		initialOutput, err := e2epodoutput.RunHostCmd(ovsPod.Namespace, ovsPod.Name, cmd)
		framework.ExpectNoError(err)
		initialOutput = strings.Trim(strings.TrimSpace(initialOutput), "\"")
		framework.Logf("Initial ovn-encap-ip: %s", initialOutput)

		ginkgo.By("Adding first network to node annotation")
		storageNetworkIP := "10.10.10.10"
		nodeNetworks = map[string]string{
			"storage": storageNetworkIP,
		}
		nodeNetworksJSON, err := json.Marshal(nodeNetworks)
		framework.ExpectNoError(err)

		patchPayload := fmt.Sprintf(`{"metadata":{"annotations":{%q:%q}}}`,
			util.NodeNetworksAnnotation, string(nodeNetworksJSON))
		_, err = cs.CoreV1().Nodes().Patch(context.Background(), node.Name, "application/strategic-merge-patch+json",
			[]byte(patchPayload), metav1.PatchOptions{})
		framework.ExpectNoError(err)

		ginkgo.By("Waiting for OVS encap IPs to include storage network")
		framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			output, err := e2epodoutput.RunHostCmd(ovsPod.Namespace, ovsPod.Name, cmd)
			if err != nil {
				return false, nil
			}
			output = strings.Trim(strings.TrimSpace(output), "\"")
			framework.Logf("ovn-encap-ip after adding storage: %s", output)
			return strings.Contains(output, storageNetworkIP), nil
		}, "OVS encap IPs should contain storage network IP")

		ginkgo.By("Adding second network to node annotation")
		appNetworkIP := "172.16.0.10"
		nodeNetworks["app"] = appNetworkIP
		nodeNetworksJSON, err = json.Marshal(nodeNetworks)
		framework.ExpectNoError(err)

		patchPayload = fmt.Sprintf(`{"metadata":{"annotations":{%q:%q}}}`,
			util.NodeNetworksAnnotation, string(nodeNetworksJSON))
		_, err = cs.CoreV1().Nodes().Patch(context.Background(), node.Name, "application/strategic-merge-patch+json",
			[]byte(patchPayload), metav1.PatchOptions{})
		framework.ExpectNoError(err)

		ginkgo.By("Waiting for OVS encap IPs to include both networks")
		framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			output, err := e2epodoutput.RunHostCmd(ovsPod.Namespace, ovsPod.Name, cmd)
			if err != nil {
				return false, nil
			}
			output = strings.Trim(strings.TrimSpace(output), "\"")
			framework.Logf("ovn-encap-ip after adding app: %s", output)
			return strings.Contains(output, storageNetworkIP) && strings.Contains(output, appNetworkIP), nil
		}, "OVS encap IPs should contain both network IPs")

		ginkgo.By("Removing storage network from node annotation")
		delete(nodeNetworks, "storage")
		nodeNetworksJSON, err = json.Marshal(nodeNetworks)
		framework.ExpectNoError(err)

		patchPayload = fmt.Sprintf(`{"metadata":{"annotations":{%q:%q}}}`,
			util.NodeNetworksAnnotation, string(nodeNetworksJSON))
		_, err = cs.CoreV1().Nodes().Patch(context.Background(), node.Name, "application/strategic-merge-patch+json",
			[]byte(patchPayload), metav1.PatchOptions{})
		framework.ExpectNoError(err)

		ginkgo.By("Waiting for OVS encap IPs to only contain app network")
		framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			output, err := e2epodoutput.RunHostCmd(ovsPod.Namespace, ovsPod.Name, cmd)
			if err != nil {
				return false, nil
			}
			output = strings.Trim(strings.TrimSpace(output), "\"")
			framework.Logf("ovn-encap-ip after removing storage: %s", output)
			return !strings.Contains(output, storageNetworkIP) && strings.Contains(output, appNetworkIP), nil
		}, "OVS encap IPs should only contain app network IP")
	})

	framework.ConformanceIt("should support multiple network planes on a single node", func() {
		f.SkipVersionPriorTo(1, 15, "Per-subnet encapsulation NIC selection was introduced in v1.15")

		ginkgo.By("Setting up multiple network planes on node")
		storageNetworkIP := "10.10.10.10"
		appNetworkIP := "172.16.0.10"
		nodeNetworks = map[string]string{
			"storage": storageNetworkIP,
			"app":     appNetworkIP,
		}
		nodeNetworksJSON, err := json.Marshal(nodeNetworks)
		framework.ExpectNoError(err)

		patchPayload := fmt.Sprintf(`{"metadata":{"annotations":{%q:%q}}}`,
			util.NodeNetworksAnnotation, string(nodeNetworksJSON))
		_, err = cs.CoreV1().Nodes().Patch(context.Background(), node.Name, "application/strategic-merge-patch+json",
			[]byte(patchPayload), metav1.PatchOptions{})
		framework.ExpectNoError(err)

		ginkgo.By("Getting ovs-ovn pod on node " + node.Name)
		ovsPod := getOvsPodOnNode(f, node.Name)

		ginkgo.By("Waiting for OVS to have both encap IPs")
		cmd := "ovs-vsctl --no-heading get open . external-ids:ovn-encap-ip"
		framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			output, err := e2epodoutput.RunHostCmd(ovsPod.Namespace, ovsPod.Name, cmd)
			if err != nil {
				return false, nil
			}
			output = strings.Trim(strings.TrimSpace(output), "\"")
			framework.Logf("ovn-encap-ip: %s", output)
			return strings.Contains(output, storageNetworkIP) && strings.Contains(output, appNetworkIP), nil
		}, "OVS should have both encap IPs")

		ginkgo.By("Creating storage subnet with nodeNetwork=storage")
		storageSubnet := framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, []string{namespaceName})
		storageSubnet.Spec.NodeNetwork = "storage"
		subnetClient.CreateSync(storageSubnet)
		createdSubnets = append(createdSubnets, subnetName)

		ginkgo.By("Creating app subnet with nodeNetwork=app")
		appSubnet := framework.MakeSubnet(subnetName2, "", cidr2, "", "", "", nil, nil, []string{namespaceName})
		appSubnet.Spec.NodeNetwork = "app"
		subnetClient.CreateSync(appSubnet)
		createdSubnets = append(createdSubnets, subnetName2)

		ginkgo.By("Creating storage pod")
		storageAnnotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}
		storagePod := framework.MakePod(namespaceName, podName, nil, storageAnnotations, framework.AgnhostImage, nil, nil)
		storagePod.Spec.NodeName = node.Name
		podClient.CreateSync(storagePod)
		createdPods = append(createdPods, podName)

		ginkgo.By("Creating app pod")
		appAnnotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName2,
		}
		appPod := framework.MakePod(namespaceName, podName2, nil, appAnnotations, framework.AgnhostImage, nil, nil)
		appPod.Spec.NodeName = node.Name
		podClient.CreateSync(appPod)
		createdPods = append(createdPods, podName2)

		ginkgo.By("Verifying storage pod interface has correct encap-ip")
		storageIfaceID := fmt.Sprintf("%s.%s", podName, namespaceName)
		framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			cmd := fmt.Sprintf(`ovs-vsctl --no-heading --columns=external_ids find interface external-ids:iface-id="%s"`, storageIfaceID)
			output, err := e2epodoutput.RunHostCmd(ovsPod.Namespace, ovsPod.Name, cmd)
			if err != nil {
				return false, nil
			}
			framework.Logf("Storage interface external-ids: %s", output)
			return strings.Contains(output, "encap-ip="+storageNetworkIP) || strings.Contains(output, `encap-ip="`+storageNetworkIP+`"`), nil
		}, "Storage interface should have encap-ip set to storage network IP")

		ginkgo.By("Verifying app pod interface has correct encap-ip")
		appIfaceID := fmt.Sprintf("%s.%s", podName2, namespaceName)
		framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			cmd := fmt.Sprintf(`ovs-vsctl --no-heading --columns=external_ids find interface external-ids:iface-id="%s"`, appIfaceID)
			output, err := e2epodoutput.RunHostCmd(ovsPod.Namespace, ovsPod.Name, cmd)
			if err != nil {
				return false, nil
			}
			framework.Logf("App interface external-ids: %s", output)
			return strings.Contains(output, "encap-ip="+appNetworkIP) || strings.Contains(output, `encap-ip="`+appNetworkIP+`"`), nil
		}, "App interface should have encap-ip set to app network IP")
	})

	framework.ConformanceIt("should use default encap IP when subnet nodeNetwork is empty", func() {
		f.SkipVersionPriorTo(1, 15, "Per-subnet encapsulation NIC selection was introduced in v1.15")

		ginkgo.By("Setting up network planes on node")
		storageNetworkIP := "10.10.10.10"
		nodeNetworks = map[string]string{
			"storage": storageNetworkIP,
		}
		nodeNetworksJSON, err := json.Marshal(nodeNetworks)
		framework.ExpectNoError(err)

		patchPayload := fmt.Sprintf(`{"metadata":{"annotations":{%q:%q}}}`,
			util.NodeNetworksAnnotation, string(nodeNetworksJSON))
		_, err = cs.CoreV1().Nodes().Patch(context.Background(), node.Name, "application/strategic-merge-patch+json",
			[]byte(patchPayload), metav1.PatchOptions{})
		framework.ExpectNoError(err)

		ginkgo.By("Getting ovs-ovn pod on node " + node.Name)
		ovsPod := getOvsPodOnNode(f, node.Name)

		ginkgo.By("Getting default encap IP")
		cmd := "ovs-vsctl --no-heading get open . external-ids:ovn-encap-ip-default"
		output, err := e2epodoutput.RunHostCmd(ovsPod.Namespace, ovsPod.Name, cmd)
		framework.ExpectNoError(err)
		defaultEncapIP := strings.Trim(strings.TrimSpace(output), "\"")
		framework.Logf("Default encap IP: %s", defaultEncapIP)
		framework.ExpectNotEmpty(defaultEncapIP)

		ginkgo.By("Creating subnet without nodeNetwork specified")
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, []string{namespaceName})
		subnetClient.CreateSync(subnet)
		createdSubnets = append(createdSubnets, subnetName)

		ginkgo.By("Creating pod")
		annotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, framework.AgnhostImage, nil, nil)
		pod.Spec.NodeName = node.Name
		podClient.CreateSync(pod)
		createdPods = append(createdPods, podName)

		ginkgo.By("Verifying pod interface does not have encap-ip set (uses default)")
		ifaceID := fmt.Sprintf("%s.%s", podName, namespaceName)
		cmd = fmt.Sprintf(`ovs-vsctl --no-heading --columns=external_ids find interface external-ids:iface-id="%s"`, ifaceID)
		output, err = e2epodoutput.RunHostCmd(ovsPod.Namespace, ovsPod.Name, cmd)
		framework.ExpectNoError(err)
		framework.Logf("Interface external-ids: %s", output)
		framework.ExpectFalse(strings.Contains(output, "encap-ip="), "Interface should not have encap-ip when nodeNetwork is not specified")
	})

	framework.ConformanceIt("should update pod encap-ip when subnet nodeNetwork is changed", func() {
		f.SkipVersionPriorTo(1, 15, "Per-subnet encapsulation NIC selection was introduced in v1.15")

		ginkgo.By("Setting up multiple network planes on node")
		storageNetworkIP := "10.10.10.10"
		appNetworkIP := "172.16.0.10"
		nodeNetworks = map[string]string{
			"storage": storageNetworkIP,
			"app":     appNetworkIP,
		}
		nodeNetworksJSON, err := json.Marshal(nodeNetworks)
		framework.ExpectNoError(err)

		patchPayload := fmt.Sprintf(`{"metadata":{"annotations":{%q:%q}}}`,
			util.NodeNetworksAnnotation, string(nodeNetworksJSON))
		_, err = cs.CoreV1().Nodes().Patch(context.Background(), node.Name, "application/strategic-merge-patch+json",
			[]byte(patchPayload), metav1.PatchOptions{})
		framework.ExpectNoError(err)

		ginkgo.By("Getting ovs-ovn pod on node " + node.Name)
		ovsPod := getOvsPodOnNode(f, node.Name)

		ginkgo.By("Waiting for OVS to have both encap IPs")
		cmd := "ovs-vsctl --no-heading get open . external-ids:ovn-encap-ip"
		framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			output, err := e2epodoutput.RunHostCmd(ovsPod.Namespace, ovsPod.Name, cmd)
			if err != nil {
				return false, nil
			}
			output = strings.Trim(strings.TrimSpace(output), "\"")
			return strings.Contains(output, storageNetworkIP) && strings.Contains(output, appNetworkIP), nil
		}, "OVS should have both encap IPs")

		ginkgo.By("Creating subnet with nodeNetwork=storage")
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, []string{namespaceName})
		subnet.Spec.NodeNetwork = "storage"
		subnet = subnetClient.CreateSync(subnet)
		createdSubnets = append(createdSubnets, subnetName)

		ginkgo.By("Creating pod")
		annotations := map[string]string{
			util.LogicalSwitchAnnotation: subnetName,
		}
		pod := framework.MakePod(namespaceName, podName, nil, annotations, framework.AgnhostImage, nil, nil)
		pod.Spec.NodeName = node.Name
		podClient.CreateSync(pod)
		createdPods = append(createdPods, podName)

		ginkgo.By("Verifying pod interface has storage encap-ip")
		ifaceID := fmt.Sprintf("%s.%s", podName, namespaceName)
		framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			cmd := fmt.Sprintf(`ovs-vsctl --no-heading --columns=external_ids find interface external-ids:iface-id="%s"`, ifaceID)
			output, err := e2epodoutput.RunHostCmd(ovsPod.Namespace, ovsPod.Name, cmd)
			if err != nil {
				return false, nil
			}
			return strings.Contains(output, "encap-ip="+storageNetworkIP) || strings.Contains(output, `encap-ip="`+storageNetworkIP+`"`), nil
		}, "Interface should have encap-ip set to storage network IP")

		ginkgo.By("Updating subnet nodeNetwork from storage to app")
		modifiedSubnet := subnet.DeepCopy()
		modifiedSubnet.Spec.NodeNetwork = "app"
		subnetClient.Patch(subnet, modifiedSubnet, 2*time.Minute)

		ginkgo.By("Deleting and recreating pod to pick up new nodeNetwork")
		podClient.DeleteSync(podName)
		pod = framework.MakePod(namespaceName, podName, nil, annotations, framework.AgnhostImage, nil, nil)
		pod.Spec.NodeName = node.Name
		podClient.CreateSync(pod)

		ginkgo.By("Verifying new pod interface has app encap-ip")
		framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			cmd := fmt.Sprintf(`ovs-vsctl --no-heading --columns=external_ids find interface external-ids:iface-id="%s"`, ifaceID)
			output, err := e2epodoutput.RunHostCmd(ovsPod.Namespace, ovsPod.Name, cmd)
			if err != nil {
				return false, nil
			}
			framework.Logf("Interface external-ids after nodeNetwork change: %s", output)
			return strings.Contains(output, "encap-ip="+appNetworkIP) || strings.Contains(output, `encap-ip="`+appNetworkIP+`"`), nil
		}, "Interface should have encap-ip set to app network IP after nodeNetwork change")
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
