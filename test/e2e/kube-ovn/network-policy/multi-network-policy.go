package network_policy

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

func isMultusInstalled(f *framework.Framework) bool {
	_, err := f.ExtClientSet.ApiextensionsV1().CustomResourceDefinitions().Get(context.Background(), "network-attachment-definitions.k8s.cni.cncf.io", metav1.GetOptions{})
	return err == nil
}

var _ = framework.SerialDescribe("[group:network-policy]", func() {
	f := framework.NewDefaultFramework("multi-network-policy")

	var podClient *framework.PodClient
	var subnetClient *framework.SubnetClient
	var netpolClient *framework.NetworkPolicyClient
	var serviceClient *framework.ServiceClient
	var nadClient *framework.NetworkAttachmentDefinitionClient
	var vpcClient *framework.VpcClient
	var namespaceName, netpolName, subnetName, nadName, vpcName, customSubnetName string
	var serverPodName, clientPodName, serviceName string
	var cidr, ipFamily string

	ginkgo.BeforeEach(func() {
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		netpolClient = f.NetworkPolicyClient()
		serviceClient = f.ServiceClient()
		nadClient = f.NetworkAttachmentDefinitionClient()
		vpcClient = f.VpcClient()

		if !isMultusInstalled(f) {
			ginkgo.Skip("Multus CRD not installed")
		}

		namespaceName = f.Namespace.Name
		netpolName = "netpol-" + framework.RandomSuffix()
		subnetName = "subnet-" + framework.RandomSuffix()
		nadName = "nad-" + framework.RandomSuffix()
		vpcName = "vpc-" + framework.RandomSuffix()
		customSubnetName = "subnet-" + framework.RandomSuffix()
		serverPodName = "server-" + framework.RandomSuffix()
		clientPodName = "client-" + framework.RandomSuffix()
		serviceName = "svc-" + framework.RandomSuffix()
		ipFamily = f.ClusterIPFamily
		if ipFamily == "" {
			ipFamily = framework.IPv4
		}
		cidr = framework.RandomCIDR(ipFamily)
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting pods")
		podClient.DeleteSync(clientPodName)
		podClient.DeleteSync(serverPodName)

		ginkgo.By("Deleting service")
		serviceClient.DeleteSync(serviceName)

		ginkgo.By("Deleting network policy")
		netpolClient.DeleteSync(netpolName)

		ginkgo.By("Deleting subnet")
		subnetClient.DeleteSync(subnetName)

		ginkgo.By("Deleting network attachment definition")
		nadClient.Delete(nadName)

		ginkgo.By("Deleting custom subnet")
		if customSubnetName != "" {
			subnetClient.DeleteSync(customSubnetName)
		}

		ginkgo.By("Deleting VPC")
		if vpcName != "" {
			vpcClient.DeleteSync(vpcName)
		}
	})

	framework.ConformanceIt("should scope network policy to selected provider", func() {
		provider := fmt.Sprintf("%s.%s.%s", nadName, namespaceName, util.OvnProvider)

		ginkgo.By("Creating VPC " + vpcName)
		vpc := framework.MakeVpc(vpcName, "", false, false, nil)
		_ = vpcClient.CreateSync(vpc)

		ginkgo.By("Creating network attachment definition " + nadName)
		nad := framework.MakeOVNNetworkAttachmentDefinition(nadName, namespaceName, provider, nil)
		_ = nadClient.Create(nad)

		ginkgo.By("Creating subnet " + subnetName)
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", vpcName, provider, nil, nil, nil)
		_ = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating server pod " + serverPodName)
		serverLabels := map[string]string{"app": "server"}
		annotations := map[string]string{nadv1.NetworkAttachmentAnnot: fmt.Sprintf("%s/%s", namespaceName, nadName)}
		port := strconv.Itoa(8000 + rand.Intn(1000))
		serverArgs := []string{"netexec", "--http-port", port}
		serverPod := framework.MakePod(namespaceName, serverPodName, serverLabels, annotations, framework.AgnhostImage, nil, serverArgs)
		serverPod = podClient.CreateSync(serverPod)

		ginkgo.By("Creating client pod " + clientPodName)
		clientLabels := map[string]string{"app": "client"}
		clientCmd := []string{"sleep", "infinity"}
		clientPod := framework.MakePod(namespaceName, clientPodName, clientLabels, annotations, f.KubeOVNImage, clientCmd, nil)
		clientPod = podClient.CreateSync(clientPod)

		ginkgo.By("Creating network policy " + netpolName)
		netpol := &netv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name: netpolName,
				Annotations: map[string]string{
					util.NetworkPolicyForAnnotation: fmt.Sprintf("%s/%s", namespaceName, nadName),
				},
			},
			Spec: netv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{MatchLabels: serverLabels},
				PolicyTypes: []netv1.PolicyType{netv1.PolicyTypeIngress},
				Ingress:     []netv1.NetworkPolicyIngressRule{},
			},
		}
		_ = netpolClient.Create(netpol)

		secondaryIPs := splitIPsByProtocol(serverPod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, provider)])
		primaryIPs := podIPsByProtocol(serverPod)
		clientPrimaryIPs := podIPsByProtocol(clientPod)
		clientSecondaryIPs := splitIPsByProtocol(clientPod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, provider)])

		primaryIP, primaryProtocol := pickIPForClient(primaryIPs, clientPrimaryIPs, ipFamily)
		if primaryIP == "" {
			ginkgo.Skip("no primary IP matching client protocol")
		}
		secondaryIP, secondaryProtocol := pickIPForClient(secondaryIPs, clientSecondaryIPs, ipFamily)
		if secondaryIP == "" {
			ginkgo.Skip("no secondary IP matching client provider protocol")
		}

		cmd := "curl -q -s --connect-timeout 2 --max-time 2 " + net.JoinHostPort(primaryIP, port)
		ginkgo.By(fmt.Sprintf("Checking primary IP connectivity from client pod via %s", primaryProtocol))
		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			_, _, err := framework.ExecShellInPod(context.Background(), f, namespaceName, clientPod.Name, cmd)
			return err == nil, nil
		}, "")

		cmd = "curl -q -s --connect-timeout 2 --max-time 2 " + net.JoinHostPort(secondaryIP, port)
		ginkgo.By(fmt.Sprintf("Checking secondary IP connectivity from client pod via %s", secondaryProtocol))
		framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
			_, _, err := framework.ExecShellInPod(context.Background(), f, namespaceName, clientPod.Name, cmd)
			return err != nil, nil
		}, "")
	})

	framework.ConformanceIt("should not include Service ClusterIP for custom VPC default provider", func() {
		ginkgo.By("Creating VPC " + vpcName)
		vpc := framework.MakeVpc(vpcName, "", false, false, nil)
		_ = vpcClient.CreateSync(vpc)

		ginkgo.By("Creating subnet " + customSubnetName)
		subnet := framework.MakeSubnet(customSubnetName, "", cidr, "", vpcName, util.OvnProvider, nil, nil, nil)
		_ = subnetClient.CreateSync(subnet)

		annotations := map[string]string{util.LogicalSwitchAnnotation: customSubnetName}

		ginkgo.By("Creating server pod " + serverPodName)
		serverLabels := map[string]string{"app": "server"}
		port := 8080
		serverArgs := []string{"netexec", "--http-port", strconv.Itoa(port)}
		serverPod := framework.MakePod(namespaceName, serverPodName, serverLabels, annotations, framework.AgnhostImage, nil, serverArgs)
		serverPod = podClient.CreateSync(serverPod)

		ginkgo.By("Creating client pod " + clientPodName)
		clientLabels := map[string]string{"app": "client"}
		clientCmd := []string{"sleep", "infinity"}
		clientPod := framework.MakePod(namespaceName, clientPodName, clientLabels, annotations, f.KubeOVNImage, clientCmd, nil)
		_ = podClient.CreateSync(clientPod)

		ginkgo.By("Creating service " + serviceName)
		ports := []corev1.ServicePort{{Name: "http", Port: int32(port), TargetPort: intstr.FromInt(port)}}
		svc := framework.MakeService(serviceName, corev1.ServiceTypeClusterIP, nil, serverLabels, ports, corev1.ServiceAffinityNone)
		svc = serviceClient.Create(svc)

		ginkgo.By("Creating network policy " + netpolName)
		netpol := &netv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name: netpolName,
				Annotations: map[string]string{
					util.NetworkPolicyForAnnotation: "ovn",
				},
			},
			Spec: netv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{MatchLabels: clientLabels},
				PolicyTypes: []netv1.PolicyType{netv1.PolicyTypeEgress},
				Egress: []netv1.NetworkPolicyEgressRule{
					{
						To: []netv1.NetworkPolicyPeer{
							{PodSelector: &metav1.LabelSelector{MatchLabels: serverLabels}},
						},
					},
				},
			},
		}
		_ = netpolClient.Create(netpol)

		serverIPs := podIPsByProtocol(serverPod)
		if len(serverIPs) == 0 {
			ginkgo.Skip("no server IPs found")
		}

		for protocol, serverIP := range serverIPs {
			if serverIP == "" {
				continue
			}
			clusterIP := serviceClusterIPByProtocol(svc, protocol)
			asName := policyAddressSetName(netpolName, namespaceName, "egress", protocol, 0)

			ginkgo.By(fmt.Sprintf("Checking address set %s for protocol %s", asName, protocol))
			framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
				addresses, err := getAddressSetAddresses(asName)
				if err != nil {
					return false, err
				}
				return slices.Contains(addresses, serverIP), nil
			}, "")

			addresses, err := getAddressSetAddresses(asName)
			framework.ExpectNoError(err)
			framework.ExpectContainElement(addresses, serverIP)
			if clusterIP != "" {
				framework.ExpectNotContainElement(addresses, clusterIP)
			}
		}
	})

	framework.ConformanceIt("should include Service ClusterIP for default VPC provider", func() {
		ginkgo.By("Creating server pod " + serverPodName)
		serverLabels := map[string]string{"app": "server"}
		port := 8080
		serverArgs := []string{"netexec", "--http-port", strconv.Itoa(port)}
		serverPod := framework.MakePod(namespaceName, serverPodName, serverLabels, nil, framework.AgnhostImage, nil, serverArgs)
		serverPod = podClient.CreateSync(serverPod)

		ginkgo.By("Creating client pod " + clientPodName)
		clientLabels := map[string]string{"app": "client"}
		clientCmd := []string{"sleep", "infinity"}
		clientPod := framework.MakePod(namespaceName, clientPodName, clientLabels, nil, f.KubeOVNImage, clientCmd, nil)
		_ = podClient.CreateSync(clientPod)

		ginkgo.By("Creating service " + serviceName)
		ports := []corev1.ServicePort{{Name: "http", Port: int32(port), TargetPort: intstr.FromInt(port)}}
		svc := framework.MakeService(serviceName, corev1.ServiceTypeClusterIP, nil, serverLabels, ports, corev1.ServiceAffinityNone)
		svc = serviceClient.Create(svc)

		ginkgo.By("Creating network policy " + netpolName)
		netpol := &netv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name: netpolName,
				Annotations: map[string]string{
					util.NetworkPolicyForAnnotation: "ovn",
				},
			},
			Spec: netv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{MatchLabels: clientLabels},
				PolicyTypes: []netv1.PolicyType{netv1.PolicyTypeEgress},
				Egress: []netv1.NetworkPolicyEgressRule{
					{
						To: []netv1.NetworkPolicyPeer{
							{PodSelector: &metav1.LabelSelector{MatchLabels: serverLabels}},
						},
					},
				},
			},
		}
		_ = netpolClient.Create(netpol)

		serverIPs := podIPsByProtocol(serverPod)
		if len(serverIPs) == 0 {
			ginkgo.Skip("no server IPs found")
		}

		for protocol, serverIP := range serverIPs {
			clusterIP := serviceClusterIPByProtocol(svc, protocol)
			asName := policyAddressSetName(netpolName, namespaceName, "egress", protocol, 0)

			ginkgo.By(fmt.Sprintf("Checking address set %s for protocol %s", asName, protocol))
			framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
				addresses, err := getAddressSetAddresses(asName)
				if err != nil {
					return false, err
				}
				return slices.Contains(addresses, serverIP), nil
			}, "")

			addresses, err := getAddressSetAddresses(asName)
			framework.ExpectNoError(err)
			framework.ExpectContainElement(addresses, serverIP)
			if clusterIP != "" {
				framework.ExpectContainElement(addresses, clusterIP)
			}
		}
	})
	framework.ConformanceIt("should include Service ClusterIP for default VPC provider with multus default network", func() {
		provider := fmt.Sprintf("%s.%s.%s", nadName, namespaceName, util.OvnProvider)

		ginkgo.By("Creating network attachment definition " + nadName)
		nad := framework.MakeOVNNetworkAttachmentDefinition(nadName, namespaceName, provider, nil)
		_ = nadClient.Create(nad)

		ginkgo.By("Creating subnet " + subnetName)
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", util.DefaultVpc, provider, nil, nil, nil)
		_ = subnetClient.CreateSync(subnet)

		annotations := map[string]string{
			util.DefaultNetworkAnnotation:                               fmt.Sprintf("%s/%s", namespaceName, nadName),
			fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider): subnetName,
		}

		ginkgo.By("Creating server pod " + serverPodName)
		serverLabels := map[string]string{"app": "server"}
		port := 8080
		serverArgs := []string{"netexec", "--http-port", strconv.Itoa(port)}
		serverPod := framework.MakePod(namespaceName, serverPodName, serverLabels, annotations, framework.AgnhostImage, nil, serverArgs)
		serverPod = podClient.CreateSync(serverPod)

		ginkgo.By("Creating client pod " + clientPodName)
		clientLabels := map[string]string{"app": "client"}
		clientCmd := []string{"sleep", "infinity"}
		clientPod := framework.MakePod(namespaceName, clientPodName, clientLabels, annotations, f.KubeOVNImage, clientCmd, nil)
		_ = podClient.CreateSync(clientPod)

		ginkgo.By("Creating service " + serviceName)
		ports := []corev1.ServicePort{{Name: "http", Port: int32(port), TargetPort: intstr.FromInt(port)}}
		svc := framework.MakeService(serviceName, corev1.ServiceTypeClusterIP, nil, serverLabels, ports, corev1.ServiceAffinityNone)
		svc = serviceClient.Create(svc)

		ginkgo.By("Creating network policy " + netpolName)
		netpol := &netv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name: netpolName,
				Annotations: map[string]string{
					util.NetworkPolicyForAnnotation: fmt.Sprintf("%s/%s", namespaceName, nadName),
				},
			},
			Spec: netv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{MatchLabels: clientLabels},
				PolicyTypes: []netv1.PolicyType{netv1.PolicyTypeEgress},
				Egress: []netv1.NetworkPolicyEgressRule{
					{
						To: []netv1.NetworkPolicyPeer{
							{PodSelector: &metav1.LabelSelector{MatchLabels: serverLabels}},
						},
					},
				},
			},
		}
		_ = netpolClient.Create(netpol)

		providerIPs := splitIPsByProtocol(serverPod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, provider)])
		if len(providerIPs) == 0 {
			ginkgo.Skip("no provider IPs found")
		}

		for protocol, providerIP := range providerIPs {
			clusterIP := serviceClusterIPByProtocol(svc, protocol)
			asName := policyAddressSetName(netpolName, namespaceName, "egress", protocol, 0)

			ginkgo.By(fmt.Sprintf("Checking address set %s for protocol %s", asName, protocol))
			framework.WaitUntil(2*time.Second, time.Minute, func(_ context.Context) (bool, error) {
				addresses, err := getAddressSetAddresses(asName)
				if err != nil {
					return false, err
				}
				return slices.Contains(addresses, providerIP), nil
			}, "")

			addresses, err := getAddressSetAddresses(asName)
			framework.ExpectNoError(err)
			framework.ExpectContainElement(addresses, providerIP)
			if clusterIP != "" {
				framework.ExpectContainElement(addresses, clusterIP)
			}
		}
	})
})

func splitIPsByProtocol(ipStr string) map[string]string {
	ips := map[string]string{}
	v4, v6 := util.SplitStringIP(ipStr)
	if v4 != "" {
		ips[util.CheckProtocol(v4)] = v4
	}
	if v6 != "" {
		ips[util.CheckProtocol(v6)] = v6
	}
	return ips
}

func pickIPForClient(ips, clientIPs map[string]string, preferred string) (string, string) {
	if preferred != "" {
		if ip := ips[preferred]; ip != "" && clientIPs[preferred] != "" {
			return ip, preferred
		}
	}
	for protocol, ip := range ips {
		if ip != "" && clientIPs[protocol] != "" {
			return ip, protocol
		}
	}
	return "", ""
}

func podIPsByProtocol(pod *corev1.Pod) map[string]string {
	ips := map[string]string{}
	for _, podIP := range pod.Status.PodIPs {
		if podIP.IP == "" {
			continue
		}
		ips[util.CheckProtocol(podIP.IP)] = podIP.IP
	}
	return ips
}

func serviceClusterIPByProtocol(svc *corev1.Service, protocol string) string {
	if len(svc.Spec.ClusterIPs) != 0 {
		for _, ip := range svc.Spec.ClusterIPs {
			if util.CheckProtocol(ip) == protocol {
				return ip
			}
		}
	}
	if svc.Spec.ClusterIP != "" && svc.Spec.ClusterIP != corev1.ClusterIPNone && util.CheckProtocol(svc.Spec.ClusterIP) == protocol {
		return svc.Spec.ClusterIP
	}
	return ""
}

func policyAddressSetName(npName, namespace, direction, protocol string, idx int) string {
	prefix := strings.ReplaceAll(fmt.Sprintf("%s.%s.%s.allow", npName, namespace, direction), "-", ".")
	return fmt.Sprintf("%s.%s.%d", prefix, protocol, idx)
}

func getAddressSetAddresses(asName string) ([]string, error) {
	cmd := fmt.Sprintf("ovn-nbctl --format=list --data=bare --no-heading --columns=addresses find Address_Set name=%s", asName)
	output, _, err := framework.NBExec(cmd)
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(string(output))
	if raw == "" {
		return nil, nil
	}
	raw = strings.Trim(raw, "[]")
	raw = strings.ReplaceAll(raw, "\"", "")
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t'
	})
	addresses := make([]string, 0, len(fields))
	for _, field := range fields {
		trimmed := strings.TrimSpace(field)
		if trimmed != "" {
			addresses = append(addresses, trimmed)
		}
	}
	return addresses, nil
}
