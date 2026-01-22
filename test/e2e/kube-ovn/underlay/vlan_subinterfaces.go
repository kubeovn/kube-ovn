package underlay

import (
	"context"
	"fmt"
	"strings"
	"time"

	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	v1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/kind"
)

var _ = framework.SerialDescribe("[group:underlay]", func() {
	f := framework.NewDefaultFramework("underlay")
	var providerNetworkClient *framework.ProviderNetworkClient
	var providerNetworkNames []string
	var allocProviderNetworkName func() string
	var kindNodeMap map[string]kind.Node
	var readyKindNodes []kind.Node
	var kindNodes []kind.Node
	var dockerNetwork *dockernetwork.Inspect
	var dockerNetworkName string
	var pnDefaultParentInterface string  // interface name (on the first node) for the test docker network; used as the default
	var nodeInterfaces map[string]string // actual interface name per node for the test docker network, keyed by node name

	ginkgo.BeforeEach(func() {
		providerNetworkClient = f.ProviderNetworkClient()
		providerNetworkNames = providerNetworkNames[:0]
		allocProviderNetworkName = func() string {
			name := "tpn-" + framework.RandomSuffix()[:6]
			providerNetworkNames = append(providerNetworkNames, name)
			return name
		}

		readyNodes, err := e2enode.GetReadySchedulableNodes(context.Background(), f.ClientSet)
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(readyNodes.Items)

		clusterName, ok := kind.IsKindProvided(readyNodes.Items[0].Spec.ProviderID)
		framework.ExpectTrue(ok, "underlay spec only runs on kind clusters")

		kindNodes, err = kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err)
		framework.ExpectNotEmpty(kindNodes)

		dockerNetworkName = "vlan-subif-" + framework.RandomSuffix()[:6]
		ginkgo.By(fmt.Sprintf("Creating docker network %s", dockerNetworkName))
		dockerNetwork, err = docker.NetworkCreate(dockerNetworkName, true, true)
		framework.ExpectNoError(err, "creating docker network "+dockerNetworkName)

		ginkgo.By(fmt.Sprintf("Connecting nodes to docker network %s", dockerNetworkName))
		err = kind.NetworkConnect(dockerNetwork.ID, kindNodes)
		framework.ExpectNoError(err, "connecting nodes to network "+dockerNetworkName)

		kindNodes, err = kind.ListNodes(clusterName, "")
		framework.ExpectNoError(err)

		kindNodeMap = make(map[string]kind.Node, len(kindNodes))
		readyKindNodes = readyKindNodes[:0]
		readySet := make(map[string]struct{}, len(readyNodes.Items))
		for i := range readyNodes.Items {
			readySet[readyNodes.Items[i].Name] = struct{}{}
		}

		nodeInterfaces = make(map[string]string, len(kindNodes))
		for i := range kindNodes {
			node := kindNodes[i]
			kindNodeMap[node.Name()] = node
			if _, ok := readySet[node.Name()]; ok {
				readyKindNodes = append(readyKindNodes, node)
			}

			links, err := node.ListLinks()
			framework.ExpectNoError(err, "list links on node %s", node.Name())
			mac := node.NetworkSettings.Networks[dockerNetworkName].MacAddress.String()
			for _, link := range links {
				if link.Address == mac {
					nodeInterfaces[node.Name()] = link.IfName
					break
				}
			}
			framework.ExpectHaveKey(nodeInterfaces, node.Name(), "expected interface for node %s in network %s", node.Name(), dockerNetworkName)
		}

		pnDefaultParentInterface = nodeInterfaces[kindNodes[0].Name()]
	})

	ginkgo.AfterEach(func() {
		for i := len(providerNetworkNames) - 1; i >= 0; i-- {
			providerNetworkClient.DeleteSync(providerNetworkNames[i])
		}
		if dockerNetwork != nil {
			ginkgo.By(fmt.Sprintf("Disconnecting nodes from docker network %s", dockerNetworkName))
			framework.ExpectNoError(kind.NetworkDisconnect(dockerNetwork.ID, kindNodes))

			ginkgo.By(fmt.Sprintf("Removing docker network %s", dockerNetworkName))
			framework.ExpectNoError(docker.NetworkRemove(dockerNetwork.ID))

			ginkgo.By(fmt.Sprintf("Waiting for docker network %s to disappear", dockerNetworkName))
			framework.WaitUntil(2*time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
				_, err := docker.NetworkInspect(dockerNetworkName)
				if err != nil {
					if strings.Contains(err.Error(), "does not exist") {
						return true, nil
					}
					return false, err
				}
				return false, nil
			}, fmt.Sprintf("docker network %s removal", dockerNetworkName))
		}
	})

	framework.ConformanceIt(`should create vlan subinterface when autoCreateVlanSubinterfaces is true`, func() {
		f.SkipVersionPriorTo(1, 14, "vlan subinterfaces are not supported before 1.14.0")
		providerNetworkName := allocProviderNetworkName()
		pnDefaultInterface := pnDefaultParentInterface + ".100" // VLAN interface we expect to manage (physical interface + VLAN ID)
		vlanID := extractVlanID(pnDefaultInterface)

		customInterfaces := makeCustomInterfaceMap(vlanID, nodeInterfaces)
		pn := createVlanSubinterfaceTestProviderNetwork(providerNetworkName, pnDefaultInterface, true, customInterfaces)
		providerNetworkClient.CreateSync(pn)

		framework.ExpectTrue(providerNetworkClient.WaitToBeReady(providerNetworkName, time.Minute))

		for _, node := range readyKindNodes {
			nodeName := node.Name()
			nodeIface := nodeInterfaceNameFor(nodeName, pnDefaultInterface, nodeInterfaces)
			framework.ExpectTrue(vlanSubinterfaceExists(kindNodeMap, nodeName, nodeIface), fmt.Sprintf("VLAN subinterface %s should exist on node %s", nodeIface, nodeName))
			framework.ExpectTrue(isKubeOVNAutoCreatedInterface(kindNodeMap, nodeName, nodeIface), fmt.Sprintf("VLAN subinterface %s should be created by Kube-OVN on node %s", nodeIface, nodeName))
		}

		providerNetworkClient.DeleteSync(providerNetworkName)
	})

	framework.ConformanceIt(`should isolate subinterfaces across multiple provider networks`, func() {
		f.SkipVersionPriorTo(1, 14, "vlan subinterfaces are not supported before 1.14.0")
		pn1Name := allocProviderNetworkName()
		pn1Interface := pnDefaultParentInterface + ".100"
		pn1VlanID := extractVlanID(pn1Interface)
		customPn1Interfaces := makeCustomInterfaceMap(pn1VlanID, nodeInterfaces)
		pn1 := createVlanSubinterfaceTestProviderNetwork(pn1Name, pn1Interface, true, customPn1Interfaces)
		providerNetworkClient.CreateSync(pn1)

		pn2Name := allocProviderNetworkName()
		pn2Interface := pnDefaultParentInterface
		pn2VlanID := extractVlanID(pn2Interface)
		customPn2Interfaces := makeCustomInterfaceMap(pn2VlanID, nodeInterfaces)
		pn2 := createVlanSubinterfaceTestProviderNetwork(pn2Name, pn2Interface, false, customPn2Interfaces)
		providerNetworkClient.CreateSync(pn2)

		pn3Name := allocProviderNetworkName()
		pn3Interface := pnDefaultParentInterface + ".300"
		pn3VlanID := extractVlanID(pn3Interface)
		customPn3Interfaces := makeCustomInterfaceMap(pn3VlanID, nodeInterfaces)
		pn3 := createVlanSubinterfaceTestProviderNetwork(pn3Name, pn3Interface, true, customPn3Interfaces)
		providerNetworkClient.CreateSync(pn3)

		framework.ExpectTrue(providerNetworkClient.WaitToBeReady(pn1Name, time.Minute))
		framework.ExpectTrue(providerNetworkClient.WaitToBeReady(pn2Name, time.Minute))
		framework.ExpectTrue(providerNetworkClient.WaitToBeReady(pn3Name, time.Minute))

		for _, node := range readyKindNodes {
			nodeName := node.Name()
			nodePn1Interface := nodeInterfaceNameFor(nodeName, pn1Interface, nodeInterfaces)
			framework.ExpectTrue(vlanSubinterfaceExists(kindNodeMap, nodeName, nodePn1Interface), fmt.Sprintf("VLAN subinterface %s should exist on node %s", nodePn1Interface, nodeName))
			framework.ExpectTrue(isKubeOVNAutoCreatedInterface(kindNodeMap, nodeName, nodePn1Interface), fmt.Sprintf("VLAN subinterface %s should be created by Kube-OVN on node %s", nodePn1Interface, nodeName))

			nodePn2Interface := nodeInterfaceNameFor(nodeName, pn2Interface, nodeInterfaces)
			framework.ExpectFalse(isKubeOVNAutoCreatedInterface(kindNodeMap, nodeName, nodePn2Interface), fmt.Sprintf("Base interface %s on node %s should not be modified by Kube-OVN when autoCreateVlanSubinterfaces is false", nodePn2Interface, nodeName))

			nodePn3Interface := nodeInterfaceNameFor(nodeName, pn3Interface, nodeInterfaces)
			framework.ExpectTrue(vlanSubinterfaceExists(kindNodeMap, nodeName, nodePn3Interface), fmt.Sprintf("VLAN subinterface %s should exist on node %s", nodePn3Interface, nodeName))
			framework.ExpectTrue(isKubeOVNAutoCreatedInterface(kindNodeMap, nodeName, nodePn3Interface), fmt.Sprintf("VLAN subinterface %s should be created by Kube-OVN on node %s", nodePn3Interface, nodeName))
		}

		providerNetworkClient.DeleteSync(pn1Name)

		for _, node := range readyKindNodes {
			nodeName := node.Name()
			nodePn1Interface := nodeInterfaceNameFor(nodeName, pn1Interface, nodeInterfaces)
			waitForInterfaceState(kindNodeMap, nodeName, nodePn1Interface, false, 2*time.Minute)
			nodePn3Interface := nodeInterfaceNameFor(nodeName, pn3Interface, nodeInterfaces)
			framework.ExpectTrue(vlanSubinterfaceExists(kindNodeMap, nodeName, nodePn3Interface), fmt.Sprintf("VLAN subinterface %s should still exist on node %s", nodePn3Interface, nodeName))
		}

		providerNetworkClient.DeleteSync(pn2Name)
		providerNetworkClient.DeleteSync(pn3Name)
	})

	framework.ConformanceIt(`should cleanup auto-created subinterfaces when provider network is deleted`, func() {
		f.SkipVersionPriorTo(1, 14, "vlan subinterfaces are not supported before 1.14.0")
		providerNetworkName := allocProviderNetworkName()
		pnDefaultInterface := pnDefaultParentInterface + ".100"
		vlanID := extractVlanID(pnDefaultInterface)

		customInterfaces := makeCustomInterfaceMap(vlanID, nodeInterfaces)
		pn := createVlanSubinterfaceTestProviderNetwork(providerNetworkName, pnDefaultInterface, true, customInterfaces)
		providerNetworkClient.CreateSync(pn)

		framework.ExpectTrue(providerNetworkClient.WaitToBeReady(providerNetworkName, time.Minute))

		for _, node := range readyKindNodes {
			nodeName := node.Name()
			nodeIface := nodeInterfaceNameFor(nodeName, pnDefaultInterface, nodeInterfaces)
			framework.ExpectTrue(vlanSubinterfaceExists(kindNodeMap, nodeName, nodeIface), fmt.Sprintf("VLAN subinterface %s should exist on node %s", nodeIface, nodeName))
			framework.ExpectTrue(isKubeOVNAutoCreatedInterface(kindNodeMap, nodeName, nodeIface), fmt.Sprintf("VLAN subinterface %s should be created by Kube-OVN on node %s", nodeIface, nodeName))
		}

		providerNetworkClient.DeleteSync(providerNetworkName)

		for _, node := range readyKindNodes {
			nodeIface := nodeInterfaceNameFor(node.Name(), pnDefaultInterface, nodeInterfaces)
			waitForInterfaceState(kindNodeMap, node.Name(), nodeIface, false, 2*time.Minute)
		}
	})

	framework.ConformanceIt(`should cleanup auto-created subinterfaces when switching to base interface`, func() {
		f.SkipVersionPriorTo(1, 14, "vlan subinterfaces are not supported before 1.14.0")
		providerNetworkName := allocProviderNetworkName()
		pnDefaultInterface := pnDefaultParentInterface
		vlanID := "100"
		vlanInterface := pnDefaultInterface + "." + vlanID

		customInterfaces := makeCustomInterfaceMap(vlanID, nodeInterfaces)
		pn := createVlanSubinterfaceTestProviderNetwork(providerNetworkName, pnDefaultInterface, true, customInterfaces)
		pn = providerNetworkClient.CreateSync(pn)

		framework.ExpectTrue(providerNetworkClient.WaitToBeReady(providerNetworkName, time.Minute))

		for _, node := range readyKindNodes {
			nodeName := node.Name()
			nodeIface := nodeInterfaceNameFor(nodeName, vlanInterface, nodeInterfaces)
			framework.ExpectTrue(vlanSubinterfaceExists(kindNodeMap, nodeName, nodeIface), fmt.Sprintf("VLAN subinterface %s should exist on node %s", nodeIface, nodeName))
			framework.ExpectTrue(isKubeOVNAutoCreatedInterface(kindNodeMap, nodeName, nodeIface), fmt.Sprintf("VLAN subinterface %s should be created by Kube-OVN on node %s", nodeIface, nodeName))
		}

		original := pn.DeepCopy()
		pn.Spec.CustomInterfaces = buildCustomInterfaces(makeCustomInterfaceMap("", nodeInterfaces))
		providerNetworkClient.Patch(original, pn)
		framework.ExpectTrue(providerNetworkClient.WaitToBeReady(providerNetworkName, time.Minute))

		for _, node := range readyKindNodes {
			nodeName := node.Name()
			nodeIface := nodeInterfaceNameFor(nodeName, vlanInterface, nodeInterfaces)
			waitForInterfaceState(kindNodeMap, nodeName, nodeIface, false, 2*time.Minute)
		}

		providerNetworkClient.DeleteSync(providerNetworkName)
	})

	framework.ConformanceIt(`should preserve auto-created subinterfaces when preserveVlanInterfaces is true`, func() {
		f.SkipVersionPriorTo(1, 15, "preserveVlanInterfaces is not supported before 1.15.0")
		providerNetworkName := allocProviderNetworkName()
		pnDefaultInterface := pnDefaultParentInterface
		vlanID := "200"
		vlanInterface := pnDefaultInterface + "." + vlanID

		customInterfaces := makeCustomInterfaceMap(vlanID, nodeInterfaces)
		pn := createVlanSubinterfaceTestProviderNetwork(providerNetworkName, pnDefaultInterface, true, customInterfaces)
		pn.Spec.PreserveVlanInterfaces = true
		pn = providerNetworkClient.CreateSync(pn)

		framework.ExpectTrue(providerNetworkClient.WaitToBeReady(providerNetworkName, time.Minute))

		for _, node := range readyKindNodes {
			nodeName := node.Name()
			nodeIface := nodeInterfaceNameFor(nodeName, vlanInterface, nodeInterfaces)
			framework.ExpectTrue(vlanSubinterfaceExists(kindNodeMap, nodeName, nodeIface), fmt.Sprintf("VLAN subinterface %s should exist on node %s", nodeIface, nodeName))
			framework.ExpectTrue(isKubeOVNAutoCreatedInterface(kindNodeMap, nodeName, nodeIface), fmt.Sprintf("VLAN subinterface %s should be created by Kube-OVN on node %s", nodeIface, nodeName))
		}

		original := pn.DeepCopy()
		pn.Spec.CustomInterfaces = buildCustomInterfaces(makeCustomInterfaceMap("", nodeInterfaces))
		providerNetworkClient.Patch(original, pn)
		framework.ExpectTrue(providerNetworkClient.WaitToBeReady(providerNetworkName, time.Minute))

		for _, node := range readyKindNodes {
			nodeName := node.Name()
			nodeIface := nodeInterfaceNameFor(nodeName, vlanInterface, nodeInterfaces)
			framework.ExpectTrue(vlanSubinterfaceExists(kindNodeMap, nodeName, nodeIface), fmt.Sprintf("VLAN subinterface %s should be preserved on node %s", nodeIface, nodeName))
		}

		providerNetworkClient.DeleteSync(providerNetworkName)
	})

	framework.ConformanceIt(`should not cleanup existing subinterfaces when autoCreateVlanSubinterfaces set to false`, func() {
		f.SkipVersionPriorTo(1, 14, "vlan subinterfaces are not supported before 1.14.0")
		providerNetworkName := allocProviderNetworkName()
		pnDefaultInterface := pnDefaultParentInterface + ".100"
		vlanID := extractVlanID(pnDefaultInterface)

		customInterfaces := makeCustomInterfaceMap(vlanID, nodeInterfaces)
		pn := createVlanSubinterfaceTestProviderNetwork(providerNetworkName, pnDefaultInterface, true, customInterfaces)
		pn = providerNetworkClient.CreateSync(pn)

		framework.ExpectTrue(providerNetworkClient.WaitToBeReady(providerNetworkName, time.Minute))

		for _, node := range readyKindNodes {
			nodeName := node.Name()
			nodeIface := nodeInterfaceNameFor(nodeName, pnDefaultInterface, nodeInterfaces)
			framework.ExpectTrue(vlanSubinterfaceExists(kindNodeMap, nodeName, nodeIface), fmt.Sprintf("VLAN subinterface %s should exist on node %s", nodeIface, nodeName))
			framework.ExpectTrue(isKubeOVNAutoCreatedInterface(kindNodeMap, nodeName, nodeIface), fmt.Sprintf("VLAN subinterface %s should be created by Kube-OVN on node %s", nodeIface, nodeName))
		}

		original := pn.DeepCopy()
		pn.Spec.AutoCreateVlanSubinterfaces = false
		providerNetworkClient.Patch(original, pn)
		framework.ExpectTrue(providerNetworkClient.WaitToBeReady(providerNetworkName, time.Minute))

		for _, node := range readyKindNodes {
			nodeIface := nodeInterfaceNameFor(node.Name(), pnDefaultInterface, nodeInterfaces)
			time.Sleep(5 * time.Second)
			framework.ExpectTrue(vlanSubinterfaceExists(kindNodeMap, node.Name(), nodeIface), fmt.Sprintf("VLAN subinterface %s should still exist on node %s when autoCreateVlanSubinterfaces is false", nodeIface, node.Name()))
		}

		providerNetworkClient.DeleteSync(providerNetworkName)
	})

	framework.ConformanceIt(`should handle subinterfaces edge cases properly`, func() {
		f.SkipVersionPriorTo(1, 14, "vlan subinterfaces are not supported before 1.14.0")
		ginkgo.By("should not create subinterface for non-VLAN interface name")
		{
			providerNetworkName := allocProviderNetworkName()
			pnDefaultInterface := pnDefaultParentInterface
			vlanID := extractVlanID(pnDefaultInterface)

			customInterfaces := makeCustomInterfaceMap(vlanID, nodeInterfaces)
			pn := createVlanSubinterfaceTestProviderNetwork(providerNetworkName, pnDefaultInterface, true, customInterfaces)
			providerNetworkClient.CreateSync(pn)

			framework.ExpectTrue(providerNetworkClient.WaitToBeReady(providerNetworkName, time.Minute))

			for _, node := range readyKindNodes {
				nodeName := node.Name()
				nodeIface := nodeInterfaceNameFor(nodeName, pnDefaultInterface, nodeInterfaces)
				exists := vlanSubinterfaceExists(kindNodeMap, nodeName, nodeIface)
				if exists {
					framework.ExpectFalse(isKubeOVNAutoCreatedInterface(kindNodeMap, nodeName, nodeIface), fmt.Sprintf("Interface %s on node %s should not be a Kube-OVN created subinterface", nodeIface, nodeName))
				}
			}

			providerNetworkClient.DeleteSync(providerNetworkName)
		}
	})
})

func createVlanSubinterfaceTestProviderNetwork(name, defaultInterface string, autoCreate bool, customInterfaces map[string][]string) *v1.ProviderNetwork {
	customIfs := buildCustomInterfaces(customInterfaces)
	return &v1.ProviderNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.ProviderNetworkSpec{
			DefaultInterface:            defaultInterface,
			AutoCreateVlanSubinterfaces: autoCreate,
			ExchangeLinkName:            false,
			CustomInterfaces:            customIfs,
		},
	}
}

func buildCustomInterfaces(customInterfaceNodes map[string][]string) []v1.CustomInterface {
	customIfs := make([]v1.CustomInterface, 0, len(customInterfaceNodes))
	for ifName, nodes := range customInterfaceNodes {
		customIfs = append(customIfs, v1.CustomInterface{
			Interface: ifName,
			Nodes:     nodes,
		})
	}
	return customIfs
}

// makeCustomInterfaceMap builds customInterfaces based on the VLAN ID and each node's actual interface name.
func makeCustomInterfaceMap(vlanID string, nodeInterfaces map[string]string) map[string][]string {
	customInterfaceNodes := make(map[string][]string, len(nodeInterfaces))
	for nodeName, nodeIface := range nodeInterfaces {
		perNodeInterface := buildNodeInterfaceName(nodeIface, vlanID)
		customInterfaceNodes[perNodeInterface] = append(customInterfaceNodes[perNodeInterface], nodeName)
	}

	return customInterfaceNodes
}

func nodeInterfaceNameFor(nodeName, targetInterface string, nodeInterfaces map[string]string) string {
	if nodeInterfaces == nil {
		return targetInterface
	}
	iface, ok := nodeInterfaces[nodeName]
	if !ok {
		return targetInterface
	}
	vlanID := extractVlanID(targetInterface)
	return buildNodeInterfaceName(iface, vlanID)
}

func buildNodeInterfaceName(nodeInterface, vlanID string) string {
	if vlanID == "" {
		return nodeInterface
	}
	return nodeInterface + "." + vlanID
}

func extractVlanID(interfaceName string) string {
	if idx := strings.Index(interfaceName, "."); idx != -1 && idx+1 < len(interfaceName) {
		return interfaceName[idx+1:]
	}
	return ""
}

func waitForInterfaceState(nodeExecMap map[string]kind.Node, nodeName, interfaceName string, expected bool, timeout time.Duration) {
	ginkgo.GinkgoHelper()

	gomega.Eventually(func() bool {
		return vlanSubinterfaceExists(nodeExecMap, nodeName, interfaceName)
	}, timeout, 5*time.Second).Should(gomega.Equal(expected), fmt.Sprintf("interface %s on node %s state should be %t", interfaceName, nodeName, expected))
}

func vlanSubinterfaceExists(nodeExecMap map[string]kind.Node, nodeName, interfaceName string) bool {
	output, ok := interfaceOutput(nodeExecMap, nodeName, interfaceName)
	return ok && strings.Contains(output, interfaceName)
}

func isKubeOVNAutoCreatedInterface(nodeExecMap map[string]kind.Node, nodeName, interfaceName string) bool {
	output, ok := interfaceOutput(nodeExecMap, nodeName, interfaceName)
	return ok && strings.Contains(output, "kube-ovn:")
}

func interfaceOutput(nodeExecMap map[string]kind.Node, nodeName, interfaceName string) (string, bool) {
	node, ok := nodeExecMap[nodeName]
	if !ok {
		return "", false
	}

	stdout, _, err := node.Exec("ip", "link", "show", interfaceName)
	if err != nil {
		return "", false
	}

	return string(stdout), true
}
