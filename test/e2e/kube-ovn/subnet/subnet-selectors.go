package subnet

import (
	"context"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/onsi/ginkgo/v2"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.Describe("[group:subnet]", func() {
	f := framework.NewDefaultFramework("subnet")

	var nsClient *framework.NamespaceClient
	var subnetClient *framework.SubnetClient
	var ns1Name, ns2Name, ns3Name, subnetName string
	var subnet *apiv1.Subnet
	var ns1, ns2, ns3 *v1.Namespace
	var cidr string
	var ns1Selector metav1.LabelSelector
	var namespaceSelectors []metav1.LabelSelector
	const projectKey = "e2e.cpaas.io/project"

	ginkgo.BeforeEach(func() {
		f.SkipVersionPriorTo(1, 13, "Support for namespaceSelector in subnet is introduced in v1.13")

		nsClient = f.NamespaceClient()
		subnetClient = f.SubnetClient()

		ns1Name = "ns1-" + framework.RandomSuffix()
		ns2Name = "ns2-" + framework.RandomSuffix()
		ns3Name = "ns3-" + framework.RandomSuffix()
		subnetName = "subnet-" + framework.RandomSuffix()
		cidr = framework.RandomCIDR(f.ClusterIPFamily)

		ginkgo.By("Creating namespace " + ns1Name)
		ns1 = framework.MakeNamespace(ns1Name, map[string]string{projectKey: ns1Name}, nil)
		ns1 = nsClient.Create(ns1)

		ginkgo.By("Creating namespace " + ns2Name)
		ns2 = framework.MakeNamespace(ns2Name, map[string]string{projectKey: ns2Name}, nil)
		ns2 = nsClient.Create(ns2)

		ginkgo.By("Creating namespace " + ns3Name)
		ns3 = framework.MakeNamespace(ns3Name, nil, nil)
		ns3 = nsClient.Create(ns3)

		ns1MatchLabels := map[string]string{projectKey: ns1Name}
		ns1Selector = metav1.LabelSelector{MatchLabels: ns1MatchLabels}
		namespaceSelectors = append(namespaceSelectors, ns1Selector)

		ginkgo.By("Creating subnet " + subnetName)
		subnet = framework.MakeSubnetWithNamespaceSelectors(subnetName, "", cidr, "", "", "", nil, nil, []string{}, namespaceSelectors)
		subnet = subnetClient.CreateSync(subnet)
	})
	ginkgo.AfterEach(func() {
		// Level 1: Delete namespaces in parallel
		ginkgo.By("Deleting namespaces " + ns1Name + ", " + ns2Name + ", " + ns3Name)
		nsClient.Delete(ns1Name)
		nsClient.Delete(ns2Name)
		nsClient.Delete(ns3Name)

		framework.ExpectNoError(nsClient.WaitToDisappear(ns1Name, 0, 2*time.Minute))
		framework.ExpectNoError(nsClient.WaitToDisappear(ns2Name, 0, 2*time.Minute))
		framework.ExpectNoError(nsClient.WaitToDisappear(ns3Name, 0, 2*time.Minute))

		// Level 2: Subnet (needs namespaces gone)
		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
	})

	framework.ConformanceIt("create subnet with namespaceSelector, matched with namespace labels", func() {
		// 1. create subnet with namespaceSelector, original check
		ginkgo.By("Check namespace " + ns1Name + ", should annotated with subnet " + subnet.Name)
		checkNs1 := nsClient.Get(ns1Name)
		framework.ExpectHaveKeyWithValue(checkNs1.Annotations, util.LogicalSwitchAnnotation, subnet.Name)

		// 2. add namespaceSelector
		ginkgo.By("Add subnet namespaceSelector matched with namespace " + ns2Name + ", should update annotation with subnet " + subnet.Name)
		ns2MatchLabels := map[string]string{projectKey: ns2Name}
		ns2Selector := metav1.LabelSelector{MatchLabels: ns2MatchLabels}
		subnetSelectors := subnet.Spec.NamespaceSelectors
		subnetSelectors = append(subnetSelectors, ns2Selector)

		modifiedSubnet := subnet.DeepCopy()
		modifiedSubnet.Spec.NamespaceSelectors = subnetSelectors
		subnet = subnetClient.PatchSync(subnet, modifiedSubnet)

		checkNs2 := nsClient.Get(ns2Name)
		framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			checkNs2 = nsClient.Get(ns2Name)
			if checkNs2.Annotations[util.LogicalSwitchAnnotation] == subnet.Name {
				return true, nil
			}
			return false, nil
		}, "failed to update annotation for ns "+checkNs2.Name)

		// 3. delete namespaceSelector
		ginkgo.By("Delete subnet namespaceSelector matched with namespace " + ns2Name + ", should delete annotation with subnet " + subnet.Name)
		modifiedSubnet = subnet.DeepCopy()
		modifiedSubnet.Spec.NamespaceSelectors = []metav1.LabelSelector{ns1Selector}
		subnet = subnetClient.PatchSync(subnet, modifiedSubnet)

		checkNs2 = nsClient.Get(ns2Name)
		framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			checkNs2 = nsClient.Get(ns2Name)
			if checkNs2.Annotations[util.LogicalSwitchAnnotation] != subnet.Name {
				return true, nil
			}
			return false, nil
		}, "failed to update annotation for ns "+checkNs2.Name)
	})

	framework.ConformanceIt("create subnet with namespaceSelector, update namespaces to match with selector", func() {
		// 1. original check for ns, with no labels
		ginkgo.By("Check labels for namespace " + ns3Name + ", should not annotation with subnet " + subnet.Name)
		checkNs3 := nsClient.Get(ns3Name)
		lsAnnotation := checkNs3.Annotations[util.LogicalSwitchAnnotation]
		framework.ExpectNotEqual(lsAnnotation, subnet.Name)

		// 2. add labels matched with subnet namespaceSelector
		ginkgo.By("Add labels for namespace " + ns3Name + ", should annotate with subnet " + subnet.Name)
		originLabels := checkNs3.Labels
		modifiedNs3 := checkNs3.DeepCopy()
		if modifiedNs3.Labels == nil {
			modifiedNs3.Labels = make(map[string]string, 1)
		}
		modifiedNs3.Labels[projectKey] = ns1Name // label with ns1Name since subnet namespaceSelector matches with ns1Name
		ginkgo.By("Update namespace " + ns3Name + " and check annotations")
		checkNs3 = nsClient.Patch(checkNs3, modifiedNs3)
		framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			checkNs3 = nsClient.Get(ns3Name)
			if checkNs3.Annotations[util.LogicalSwitchAnnotation] == subnet.Name {
				return true, nil
			}
			return false, nil
		}, "failed to update annotation for ns "+checkNs3.Name)

		// 3. delete labels matched with subnet namespaceSelector
		ginkgo.By("Delete labels for namespace " + ns3Name + ", should not annotate with subnet " + subnet.Name)
		modifiedNs3 = checkNs3.DeepCopy()
		modifiedNs3.Labels = originLabels
		ginkgo.By("Update namespace " + ns3Name + " and check annotations")
		checkNs3 = nsClient.Patch(checkNs3, modifiedNs3)
		framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			checkNs3 = nsClient.Get(ns3Name)
			if checkNs3.Annotations[util.LogicalSwitchAnnotation] != subnet.Name {
				return true, nil
			}
			return false, nil
		}, "failed to update annotation for ns "+checkNs3.Name)
	})

	framework.ConformanceIt("update namespace with labelSelector, and set subnet spec namespaces with selected namespace", func() {
		// 1. subnet.spec.namespaces contains ns2Name
		ginkgo.By("Add namespace " + ns2Name + " to subnet " + subnet.Name + " spec namespaces")
		modifiedSubnet := subnet.DeepCopy()
		modifiedSubnet.Spec.Namespaces = append(modifiedSubnet.Spec.Namespaces, ns2Name)
		subnet = subnetClient.PatchSync(subnet, modifiedSubnet)
		checkNs2 := nsClient.Get(ns2Name)
		framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			checkNs2 = nsClient.Get(ns2Name)
			if checkNs2.Annotations[util.LogicalSwitchAnnotation] == subnet.Name {
				return true, nil
			}
			return false, nil
		}, "failed to update annotation for ns "+checkNs2.Name)

		// 2. add namespaceSelector for subnet, which select with ns2
		ginkgo.By("Add subnet namespaceSelector matched with " + ns2Name + ", should update annotation with subnet " + subnet.Name)
		ns2MatchLabels := map[string]string{projectKey: ns2Name}
		ns2Selector := metav1.LabelSelector{MatchLabels: ns2MatchLabels}
		subnetSelectors := subnet.Spec.NamespaceSelectors
		subnetSelectors = append(subnetSelectors, ns2Selector)

		modifiedSubnet = subnet.DeepCopy()
		modifiedSubnet.Spec.NamespaceSelectors = subnetSelectors
		subnet = subnetClient.PatchSync(subnet, modifiedSubnet)

		ginkgo.By("Check namespace " + ns2Name + " to annotate with subnet " + subnet.Name)
		checkNs2 = nsClient.Get(ns2Name)
		framework.ExpectHaveKeyWithValue(checkNs2.Annotations, util.LogicalSwitchAnnotation, subnet.Name)

		// 3. delete subnet namespaceSelector with ns2
		ginkgo.By("Delete subnet namespaceSelector matched with " + ns2Name + ", should keep annotation with subnet " + subnet.Name + " since subnet.spec.namespaces has this ns")
		modifiedSubnet = subnet.DeepCopy()
		modifiedSubnet.Spec.NamespaceSelectors = []metav1.LabelSelector{ns1Selector}
		subnet = subnetClient.PatchSync(subnet, modifiedSubnet)

		ginkgo.By("Check namespace " + ns2Name + " to annotate with subnet " + subnet.Name)
		checkNs2 = nsClient.Get(ns2Name)
		framework.ExpectHaveKeyWithValue(checkNs2.Annotations, util.LogicalSwitchAnnotation, subnet.Name)

		// 4. re-add namespaceSelector for subnet, which select with ns2
		ginkgo.By("Add subnet namespaceSelector matched with " + ns2Name + ", should update annotation with subnet " + subnet.Name)
		modifiedSubnet = subnet.DeepCopy()
		modifiedSubnet.Spec.NamespaceSelectors = subnetSelectors
		subnet = subnetClient.PatchSync(subnet, modifiedSubnet)

		ginkgo.By("Check namespace " + ns2Name + " to annotate with subnet " + subnet.Name)
		checkNs2 = nsClient.Get(ns2Name)
		framework.ExpectHaveKeyWithValue(checkNs2.Annotations, util.LogicalSwitchAnnotation, subnet.Name)

		// 5. delete subnet spec namespaces with ns2
		ginkgo.By("Delete subnet spec namespaces with " + ns2Name)
		modifiedSubnet = subnet.DeepCopy()
		modifiedSubnet.Spec.Namespaces = []string{}
		subnet = subnetClient.PatchSync(subnet, modifiedSubnet)

		ginkgo.By("Check namespace " + ns2Name + " to annotate with subnet " + subnet.Name)
		checkNs2 = nsClient.Get(ns2Name)
		framework.ExpectHaveKeyWithValue(checkNs2.Annotations, util.LogicalSwitchAnnotation, subnet.Name)

		// 6. delete subnet namespaceSelector with ns2
		ginkgo.By("Delete subnet namespaceSelector matched with namespace " + ns2Name + ", should delete annotation with subnet " + subnet.Name)
		modifiedSubnet = subnet.DeepCopy()
		modifiedSubnet.Spec.NamespaceSelectors = []metav1.LabelSelector{ns1Selector}
		subnet = subnetClient.PatchSync(subnet, modifiedSubnet)

		checkNs2 = nsClient.Get(ns2Name)
		framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			checkNs2 = nsClient.Get(ns2Name)
			if checkNs2.Annotations[util.LogicalSwitchAnnotation] != subnet.Name {
				return true, nil
			}
			return false, nil
		}, "failed to update annotation for ns "+checkNs2.Name)
	})
})
