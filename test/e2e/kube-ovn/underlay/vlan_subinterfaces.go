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
	var baseInterface string
	var customInterfaces map[string][]string

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
		dockerNetwork, err = docker.NetworkCreate(dockerNetworkName, true, true)
		framework.ExpectNoError(err, "creating docker network "+dockerNetworkName)

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

		ifaceMap := make(map[string]string, len(kindNodes))
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
					ifaceMap[node.Name()] = link.IfName
					break
				}
			}
			framework.ExpectHaveKey(ifaceMap, node.Name(), "expected interface for node %s in network %s", node.Name(), dockerNetworkName)
		}

		baseInterface = ifaceMap[kindNodes[0].Name()]
		customInterfaces = make(map[string][]string)
		for name, ifName := range ifaceMap {
			if ifName != baseInterface {
				customInterfaces[ifName] = append(customInterfaces[ifName], name)
			}
		}
	})

	ginkgo.AfterEach(func() {
		for i := len(providerNetworkNames) - 1; i >= 0; i-- {
			providerNetworkClient.DeleteSync(providerNetworkNames[i])
		}
		if dockerNetwork != nil {
			_ = kind.NetworkDisconnect(dockerNetwork.ID, kindNodes)
			_ = docker.NetworkRemove(dockerNetwork.ID)
		}
	})

	framework.ConformanceIt(`should create vlan subinterface when autoCreateVlanSubinterfaces is true`, func() {
		f.SkipVersionPriorTo(1, 15, "vlan subinterfaces are not supported before 1.15.0")
		providerNetworkName := allocProviderNetworkName()
		interfaceName := baseInterface + ".100"

		pn := createVlanSubinterfaceTestProviderNetwork(providerNetworkName, interfaceName, true, customInterfaces)
		providerNetworkClient.CreateSync(pn)

		framework.ExpectTrue(providerNetworkClient.WaitToBeReady(providerNetworkName, time.Minute))

		for _, node := range readyKindNodes {
			nodeName := node.Name()
			framework.ExpectTrue(vlanSubinterfaceExists(kindNodeMap, nodeName, interfaceName), fmt.Sprintf("VLAN subinterface %s should exist on node %s", interfaceName, nodeName))
			framework.ExpectTrue(isKubeOVNAutoCreatedInterface(kindNodeMap, nodeName, interfaceName), fmt.Sprintf("VLAN subinterface %s should be created by Kube-OVN on node %s", interfaceName, nodeName))
		}

		providerNetworkClient.DeleteSync(providerNetworkName)
	})

	framework.ConformanceIt(`should isolate subinterfaces across multiple provider networks`, func() {
		f.SkipVersionPriorTo(1, 15, "vlan subinterfaces are not supported before 1.15.0")
		pn1Name := allocProviderNetworkName()
		pn1Interface := baseInterface + ".100"
		pn1 := createVlanSubinterfaceTestProviderNetwork(pn1Name, pn1Interface, true, customInterfaces)
		providerNetworkClient.CreateSync(pn1)

		pn2Name := allocProviderNetworkName()
		pn2Interface := baseInterface
		pn2 := createVlanSubinterfaceTestProviderNetwork(pn2Name, pn2Interface, false, customInterfaces)
		providerNetworkClient.CreateSync(pn2)

		pn3Name := allocProviderNetworkName()
		pn3Interface := baseInterface + ".300"
		pn3 := createVlanSubinterfaceTestProviderNetwork(pn3Name, pn3Interface, true, customInterfaces)
		providerNetworkClient.CreateSync(pn3)

		framework.ExpectTrue(providerNetworkClient.WaitToBeReady(pn1Name, time.Minute))
		framework.ExpectTrue(providerNetworkClient.WaitToBeReady(pn2Name, time.Minute))
		framework.ExpectTrue(providerNetworkClient.WaitToBeReady(pn3Name, time.Minute))

		for _, node := range readyKindNodes {
			nodeName := node.Name()
			framework.ExpectTrue(vlanSubinterfaceExists(kindNodeMap, nodeName, pn1Interface), fmt.Sprintf("VLAN subinterface %s should exist on node %s", pn1Interface, nodeName))
			framework.ExpectTrue(isKubeOVNAutoCreatedInterface(kindNodeMap, nodeName, pn1Interface), fmt.Sprintf("VLAN subinterface %s should be created by Kube-OVN on node %s", pn1Interface, nodeName))

			framework.ExpectFalse(isKubeOVNAutoCreatedInterface(kindNodeMap, nodeName, pn2Interface), fmt.Sprintf("Base interface %s on node %s should not be modified by Kube-OVN when autoCreateVlanSubinterfaces is false", pn2Interface, nodeName))

			framework.ExpectTrue(vlanSubinterfaceExists(kindNodeMap, nodeName, pn3Interface), fmt.Sprintf("VLAN subinterface %s should exist on node %s", pn3Interface, nodeName))
			framework.ExpectTrue(isKubeOVNAutoCreatedInterface(kindNodeMap, nodeName, pn3Interface), fmt.Sprintf("VLAN subinterface %s should be created by Kube-OVN on node %s", pn3Interface, nodeName))
		}

		providerNetworkClient.DeleteSync(pn1Name)

		for _, node := range readyKindNodes {
			nodeName := node.Name()
			waitForInterfaceState(kindNodeMap, nodeName, pn1Interface, false, 2*time.Minute)
			framework.ExpectTrue(vlanSubinterfaceExists(kindNodeMap, nodeName, pn3Interface), fmt.Sprintf("VLAN subinterface %s should still exist on node %s", pn3Interface, nodeName))
		}

		providerNetworkClient.DeleteSync(pn2Name)
		providerNetworkClient.DeleteSync(pn3Name)
	})

	framework.ConformanceIt(`should cleanup auto-created subinterfaces when provider network is deleted`, func() {
		f.SkipVersionPriorTo(1, 15, "vlan subinterfaces are not supported before 1.15.0")
		providerNetworkName := allocProviderNetworkName()
		interfaceName := baseInterface + ".100"

		pn := createVlanSubinterfaceTestProviderNetwork(providerNetworkName, interfaceName, true, customInterfaces)
		providerNetworkClient.CreateSync(pn)

		framework.ExpectTrue(providerNetworkClient.WaitToBeReady(providerNetworkName, time.Minute))

		for _, node := range readyKindNodes {
			nodeName := node.Name()
			framework.ExpectTrue(vlanSubinterfaceExists(kindNodeMap, nodeName, interfaceName), fmt.Sprintf("VLAN subinterface %s should exist on node %s", interfaceName, nodeName))
			framework.ExpectTrue(isKubeOVNAutoCreatedInterface(kindNodeMap, nodeName, interfaceName), fmt.Sprintf("VLAN subinterface %s should be created by Kube-OVN on node %s", interfaceName, nodeName))
		}

		providerNetworkClient.DeleteSync(providerNetworkName)

		for _, node := range readyKindNodes {
			waitForInterfaceState(kindNodeMap, node.Name(), interfaceName, false, 2*time.Minute)
		}
	})

	framework.ConformanceIt(`should not cleanup existing subinterfaces when autoCreateVlanSubinterfaces set to false`, func() {
		f.SkipVersionPriorTo(1, 15, "vlan subinterfaces are not supported before 1.15.0")
		providerNetworkName := allocProviderNetworkName()
		interfaceName := baseInterface + ".100"

		pn := createVlanSubinterfaceTestProviderNetwork(providerNetworkName, interfaceName, true, customInterfaces)
		pn = providerNetworkClient.CreateSync(pn)

		framework.ExpectTrue(providerNetworkClient.WaitToBeReady(providerNetworkName, time.Minute))

		for _, node := range readyKindNodes {
			nodeName := node.Name()
			framework.ExpectTrue(vlanSubinterfaceExists(kindNodeMap, nodeName, interfaceName), fmt.Sprintf("VLAN subinterface %s should exist on node %s", interfaceName, nodeName))
			framework.ExpectTrue(isKubeOVNAutoCreatedInterface(kindNodeMap, nodeName, interfaceName), fmt.Sprintf("VLAN subinterface %s should be created by Kube-OVN on node %s", interfaceName, nodeName))
		}

		original := pn.DeepCopy()
		pn.Spec.AutoCreateVlanSubinterfaces = false
		providerNetworkClient.Patch(original, pn)
		framework.ExpectTrue(providerNetworkClient.WaitToBeReady(providerNetworkName, time.Minute))

		for _, node := range readyKindNodes {
			time.Sleep(5 * time.Second)
			framework.ExpectTrue(vlanSubinterfaceExists(kindNodeMap, node.Name(), interfaceName), fmt.Sprintf("VLAN subinterface %s should still exist on node %s when autoCreateVlanSubinterfaces is false", interfaceName, node.Name()))
		}

		providerNetworkClient.DeleteSync(providerNetworkName)
	})

	framework.ConformanceIt(`should handle edge cases properly`, func() {
		f.SkipVersionPriorTo(1, 15, "vlan subinterfaces are not supported before 1.15.0")
		ginkgo.By("should not create subinterface for non-VLAN interface name")
		{
			providerNetworkName := allocProviderNetworkName()
			interfaceName := baseInterface

			pn := createVlanSubinterfaceTestProviderNetwork(providerNetworkName, interfaceName, true, customInterfaces)
			providerNetworkClient.CreateSync(pn)

			framework.ExpectTrue(providerNetworkClient.WaitToBeReady(providerNetworkName, time.Minute))

			for _, node := range readyKindNodes {
				nodeName := node.Name()
				exists := vlanSubinterfaceExists(kindNodeMap, nodeName, interfaceName)
				if exists {
					framework.ExpectFalse(isKubeOVNAutoCreatedInterface(kindNodeMap, nodeName, interfaceName), fmt.Sprintf("Interface %s on node %s should not be a Kube-OVN created subinterface", interfaceName, nodeName))
				}
			}

			providerNetworkClient.DeleteSync(providerNetworkName)
		}
	})
})

func createVlanSubinterfaceTestProviderNetwork(name, interfaceName string, autoCreate bool, customInterfaces map[string][]string) *v1.ProviderNetwork {
	customIfs := make([]v1.CustomInterface, 0, len(customInterfaces))
	for ifName, nodes := range customInterfaces {
		customIfs = append(customIfs, v1.CustomInterface{
			Interface: ifName,
			Nodes:     nodes,
		})
	}

	return &v1.ProviderNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.ProviderNetworkSpec{
			DefaultInterface:            interfaceName,
			AutoCreateVlanSubinterfaces: autoCreate,
			ExchangeLinkName:            false,
			CustomInterfaces:            customIfs,
		},
	}
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
