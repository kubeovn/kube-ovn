package non_primary_cni

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
)

var _ = framework.SerialDescribe("[group:non-primary-cni]", func() {
	f := framework.NewDefaultFramework("non-primary-cni-vpc-egress-gateway")

	ginkgo.Context("VPC Egress Gateway", ginkgo.Label("Feature:VPC-Egress-Gateway"), func() {
		ginkgo.It("uses the internal NAD as default and SNATs through the external attachment", func() {
			runNonPrimaryVpcEgressGatewayCase(f, apiv1.ExternalNetworkModeAttachment)
		})

		ginkgo.It("routes and SNATs through the primary CNI", func() {
			f.SkipVersionPriorTo(1, 17, "PrimaryCNI egress gateway mode requires v1.17+")
			runNonPrimaryVpcEgressGatewayCase(f, apiv1.ExternalNetworkModePrimaryCNI)
		})
	})
})

func runNonPrimaryVpcEgressGatewayCase(f *framework.Framework, mode string) {
	ginkgo.GinkgoHelper()

	namespace := f.Namespace.Name
	suffix := framework.RandomSuffix()
	nadName := "veg-internal-" + suffix
	internalNetwork := namespace + "/" + nadName
	provider := fmt.Sprintf("%s.%s.ovn", nadName, namespace)
	nadClient := f.NetworkAttachmentDefinitionClient()
	nadClient.Create(framework.MakeOVNNetworkAttachmentDefinition(nadName, namespace, provider, nil))
	ginkgo.DeferCleanup(func() { nadClient.Delete(nadName) })

	vpcName := "veg-vpc-" + suffix
	vpcClient := f.VpcClient()
	ginkgo.DeferCleanup(func() { vpcClient.DeleteSync(vpcName) })
	vpcClient.CreateSync(&apiv1.Vpc{Name: vpcName, Spec: apiv1.VpcSpec{Namespaces: []string{namespace}}})

	subnetClient := f.SubnetClient()
	createInternalSubnet := func(prefix string) *apiv1.Subnet {
		name := prefix + suffix
		ginkgo.DeferCleanup(func() { subnetClient.DeleteSync(name) })
		return subnetClient.CreateSync(framework.MakeSubnet(
			name, "", framework.RandomCIDR(f.ClusterIPFamily), "", vpcName, provider, nil, nil, []string{namespace},
		))
	}
	internalSubnet := createInternalSubnet("veg-internal-")
	sourceSubnet := createInternalSubnet("veg-source-")

	serverAnnotations := map[string]string{}
	externalSubnetName := ""
	externalNetwork := ""
	if mode == apiv1.ExternalNetworkModeAttachment {
		externalNADName := "veg-external-" + suffix
		externalNetwork = namespace + "/" + externalNADName
		externalProvider := externalNADName + "." + namespace
		nadClient.Create(framework.MakeMacvlanNetworkAttachmentDefinition(externalNADName, namespace, "eth1", "bridge", externalProvider, nil))
		ginkgo.DeferCleanup(func() { nadClient.Delete(externalNADName) })

		network, err := docker.NetworkInspect("kind-underlay")
		framework.ExpectNoError(err)
		var cidrs, gateways []string
		for _, config := range network.IPAM.Config {
			if !config.Subnet.IsValid() || !config.Gateway.IsValid() {
				continue
			}
			if (config.Subnet.Addr().Is4() && f.HasIPv4()) || (config.Subnet.Addr().Is6() && f.HasIPv6()) {
				cidrs = append(cidrs, config.Subnet.String())
				gateways = append(gateways, config.Gateway.String())
			}
		}
		framework.ExpectNotEmpty(cidrs, "kind-underlay must have an enabled address family")
		externalSubnetName = "veg-external-" + suffix
		ginkgo.DeferCleanup(func() { subnetClient.DeleteSync(externalSubnetName) })
		subnetClient.CreateSync(framework.MakeSubnet(
			externalSubnetName, "", strings.Join(cidrs, ","), strings.Join(gateways, ","), "",
			externalProvider, gateways, nil, []string{namespace},
		))
		serverAnnotations[nadv1.NetworkAttachmentAnnot] = externalNetwork
		serverAnnotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, externalProvider)] = externalSubnetName
	}

	podClient := f.PodClient()
	serverName := "veg-server-" + suffix
	server := framework.MakePrivilegedPod(namespace, serverName, nil, serverAnnotations, framework.AgnhostImage, nil, []string{"netexec", "--http-port", "8080"})
	ginkgo.DeferCleanup(func() { podClient.DeleteSync(serverName) })
	server = podClient.CreateSync(server)
	serverIPs := util.PodIPs(*server)
	if mode == apiv1.ExternalNetworkModeAttachment {
		var err error
		serverIPs, err = util.PodAttachmentIPs(server, externalNetwork)
		framework.ExpectNoError(err)
	}
	framework.ExpectNotEmpty(serverIPs)

	gwName := "veg-" + suffix
	gw := framework.MakeVpcEgressGateway(namespace, gwName, vpcName, 1, internalSubnet.Name, externalSubnetName)
	if mode == apiv1.ExternalNetworkModePrimaryCNI {
		gw.Spec.ExternalNetworkMode = mode
	}
	gw.Spec.Policies = []apiv1.VpcEgressGatewayPolicy{{SNAT: true, Subnets: []string{sourceSubnet.Name}}}
	gwClient := f.VpcEgressGatewayClient()
	ginkgo.DeferCleanup(func() { gwClient.DeleteSync(gwName) })
	gw = gwClient.CreateSync(gw)
	gomega.Expect(gw.Status.InternalIPs).To(gomega.HaveLen(1))
	gomega.Expect(gw.Status.ExternalIPs).To(gomega.HaveLen(1))

	deploy := f.DeploymentClient().Get(gw.Status.Workload.Name)
	pods, err := f.DeploymentClient().GetPods(deploy)
	framework.ExpectNoError(err)
	gomega.Expect(pods.Items).To(gomega.HaveLen(1))
	gwPod := &pods.Items[0]
	attachmentNetwork := externalNetwork
	if mode == apiv1.ExternalNetworkModePrimaryCNI {
		attachmentNetwork = internalNetwork
	}
	gomega.Expect(gwPod.Annotations[nadv1.NetworkAttachmentAnnot]).To(gomega.Equal(attachmentNetwork))
	if mode == apiv1.ExternalNetworkModePrimaryCNI {
		gomega.Expect(gwPod.Annotations).ToNot(gomega.HaveKey(util.DefaultNetworkAnnotation))
	} else {
		gomega.Expect(gwPod.Annotations[util.DefaultNetworkAnnotation]).To(gomega.Equal(internalNetwork))
		gomega.Expect(gwPod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider)]).To(gomega.Equal(internalSubnet.Name))
		internalRoutes := gwPod.Annotations[fmt.Sprintf(util.RoutesAnnotationTemplate, provider)]
		for cidr := range strings.SplitSeq(sourceSubnet.Spec.CIDRBlock, ",") {
			gomega.Expect(internalRoutes).To(gomega.ContainSubstring(cidr))
		}
	}

	attachmentIPs, err := util.PodAttachmentIPs(gwPod, attachmentNetwork)
	framework.ExpectNoError(err)
	internalIPs, externalIPs := util.PodIPs(*gwPod), attachmentIPs
	if mode == apiv1.ExternalNetworkModePrimaryCNI {
		internalIPs, externalIPs = attachmentIPs, util.PodIPs(*gwPod)
	}
	gomega.Expect(strings.Split(gw.Status.InternalIPs[0], ",")).To(gomega.ConsistOf(internalIPs))
	gomega.Expect(strings.Split(gw.Status.ExternalIPs[0], ",")).To(gomega.ConsistOf(externalIPs))

	serverIPv4, serverIPv6 := util.SplitIpsByProtocol(serverIPs)
	internalIPv4, internalIPv6 := util.SplitIpsByProtocol(internalIPs)
	externalIPv4, externalIPv6 := util.SplitIpsByProtocol(externalIPs)
	sourceGatewayIPv4, sourceGatewayIPv6 := util.SplitStringIP(sourceSubnet.Spec.Gateway)
	routes := util.NewPodRoutes()
	for _, ip := range serverIPv4 {
		routes.Add(provider, ip, sourceGatewayIPv4)
	}
	for _, ip := range serverIPv6 {
		routes.Add(provider, ip, sourceGatewayIPv6)
	}
	annotations, err := routes.ToAnnotations()
	framework.ExpectNoError(err)
	annotations[nadv1.NetworkAttachmentAnnot] = internalNetwork
	annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider)] = sourceSubnet.Name
	sourceName := "veg-source-" + suffix
	source := framework.MakePrivilegedPod(namespace, sourceName, nil, annotations, f.KubeOVNImage, []string{"sleep", "infinity"}, nil)
	ginkgo.DeferCleanup(func() { podClient.DeleteSync(sourceName) })
	source = podClient.CreateSync(source)
	sourceIPs, err := util.PodAttachmentIPs(source, internalNetwork)
	framework.ExpectNoError(err)
	sourceIPv4, sourceIPv6 := util.SplitIpsByProtocol(sourceIPs)

	for _, family := range []struct {
		af          int
		serverIPs   []string
		sourceIPs   []string
		internalIPs []string
		externalIPs []string
	}{
		{af: 4, serverIPs: serverIPv4, sourceIPs: sourceIPv4, internalIPs: internalIPv4, externalIPs: externalIPv4},
		{af: 6, serverIPs: serverIPv6, sourceIPs: sourceIPv6, internalIPs: internalIPv6, externalIPs: externalIPv6},
	} {
		if len(family.serverIPs) == 0 {
			continue
		}
		gomega.Expect(family.internalIPs).ToNot(gomega.BeEmpty())
		gomega.Expect(family.externalIPs).ToNot(gomega.BeEmpty())
		gomega.Expect(family.sourceIPs).ToNot(gomega.BeEmpty())
		serverIP := family.serverIPs[0]
		sourceIP := family.sourceIPs[0]
		internalIP := family.internalIPs[0]
		externalIP := family.externalIPs[0]
		policyQuery := fmt.Sprintf(
			"ovn-nbctl --format=csv --data=bare --no-heading --columns=nexthops find Logical_Router_Policy priority=%d external_ids:vpc-egress-gateway=%s/%s external_ids:af=%d",
			util.EgressGatewayPolicyPriority, namespace, gwName, family.af,
		)
		framework.WaitUntil(time.Second, 2*time.Minute, func(context.Context) (bool, error) {
			output, _, err := framework.NBExec(policyQuery)
			return err == nil && strings.Contains(string(output), internalIP) && !strings.Contains(string(output), externalIP), nil
		}, fmt.Sprintf("IPv%d OVN next hop to use the internal attachment", family.af))

		routeOutput, err := e2epodoutput.RunHostCmd(namespace, source.Name, fmt.Sprintf("ip -%d route get %s", family.af, serverIP))
		framework.ExpectNoError(err)
		gomega.Expect(routeOutput).To(gomega.ContainSubstring("dev net1"))
		gatewayRoute, err := e2epodoutput.RunHostCmd(namespace, gwPod.Name, fmt.Sprintf("ip -%d route get %s", family.af, serverIP))
		framework.ExpectNoError(err)
		externalInterface := "net1"
		if mode == apiv1.ExternalNetworkModePrimaryCNI {
			externalInterface = "eth0"
		}
		gomega.Expect(gatewayRoute).To(gomega.ContainSubstring("dev " + externalInterface))
		internalInterface := "eth0"
		if mode == apiv1.ExternalNetworkModePrimaryCNI {
			internalInterface = "net1"
		}
		gatewayReturnRoute, err := e2epodoutput.RunHostCmd(namespace, gwPod.Name, fmt.Sprintf("ip -%d route get %s", family.af, sourceIP))
		framework.ExpectNoError(err)
		gomega.Expect(gatewayReturnRoute).To(gomega.ContainSubstring("dev " + internalInterface))

		url := "http://" + net.JoinHostPort(serverIP, "8080") + "/clientip"
		gomega.Eventually(func() (string, error) {
			output, err := e2epodoutput.RunHostCmd(namespace, source.Name, "curl -q -sS --connect-timeout 2 --max-time 4 "+url)
			if err != nil {
				return "", err
			}
			ip, _, err := net.SplitHostPort(strings.TrimSpace(output))
			return ip, err
		}, 2*time.Minute, 2*time.Second).Should(gomega.Equal(externalIP))
	}
}
