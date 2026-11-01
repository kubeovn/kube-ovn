package controller

import (
	"fmt"
	"slices"
	"strings"

	"github.com/scylladb/go-set/strset"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/set"

	v1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"
	v1alpha2 "sigs.k8s.io/network-policy-api/apis/v1alpha2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// policyNamedPortSelectors describes the Pods selected by a policy subject or
// peer. Named ports are resolved only against these Pods.
type policyNamedPortSelectors struct {
	namespaces labels.Selector
	pods       labels.Selector
}

func (c *Controller) namedPortSelectors(namespaceSelector, podSelector *metav1.LabelSelector) (policyNamedPortSelectors, error) {
	ns := &metav1.LabelSelector{}
	if namespaceSelector != nil {
		ns = namespaceSelector
	}
	nsSelector, err := metav1.LabelSelectorAsSelector(ns)
	if err != nil {
		return policyNamedPortSelectors{}, fmt.Errorf("error creating namespace label selector: %w", err)
	}
	podSel := labels.Everything()
	if podSelector != nil {
		podSel, err = metav1.LabelSelectorAsSelector(podSelector)
		if err != nil {
			return policyNamedPortSelectors{}, fmt.Errorf("error creating pod label selector: %w", err)
		}
	}
	return policyNamedPortSelectors{namespaces: nsSelector, pods: podSel}, nil
}

func namedPortSelectorsForAnpSubject(subject *v1alpha1.AdminNetworkPolicySubject) (*metav1.LabelSelector, *metav1.LabelSelector) {
	if subject.Namespaces != nil {
		return subject.Namespaces, nil
	}
	return &subject.Pods.NamespaceSelector, &subject.Pods.PodSelector
}

func namedPortSelectorsForCnpSubject(subject *v1alpha2.ClusterNetworkPolicySubject) (*metav1.LabelSelector, *metav1.LabelSelector) {
	if subject.Namespaces != nil {
		return subject.Namespaces, nil
	}
	return &subject.Pods.NamespaceSelector, &subject.Pods.PodSelector
}

func namedPortSelectorsForAnpEgressPeer(peer *v1alpha1.AdminNetworkPolicyEgressPeer) (*metav1.LabelSelector, *metav1.LabelSelector) {
	if peer.Namespaces != nil {
		return peer.Namespaces, nil
	}
	if peer.Pods == nil {
		return nil, nil
	}
	return &peer.Pods.NamespaceSelector, &peer.Pods.PodSelector
}

func namedPortSelectorsForCnpEgressPeer(peer *v1alpha2.ClusterNetworkPolicyEgressPeer) (*metav1.LabelSelector, *metav1.LabelSelector) {
	if peer.Namespaces != nil {
		return peer.Namespaces, nil
	}
	if peer.Pods == nil {
		return nil, nil
	}
	return &peer.Pods.NamespaceSelector, &peer.Pods.PodSelector
}

func requestedNamedPortNamesAnpIngress(rules []v1alpha1.AdminNetworkPolicyIngressRule) map[string]struct{} {
	requested := make(map[string]struct{})
	for _, rule := range rules {
		if rule.Ports == nil {
			continue
		}
		for _, port := range *rule.Ports {
			if port.NamedPort != nil {
				requested[*port.NamedPort] = struct{}{}
			}
		}
	}
	return requested
}

func requestedNamedPortNamesAnpEgress(rules []v1alpha1.AdminNetworkPolicyEgressRule) map[string]struct{} {
	requested := make(map[string]struct{})
	for _, rule := range rules {
		if rule.Ports == nil {
			continue
		}
		for _, port := range *rule.Ports {
			if port.NamedPort != nil {
				requested[*port.NamedPort] = struct{}{}
			}
		}
	}
	return requested
}

func requestedNamedPortNamesBanpIngress(rules []v1alpha1.BaselineAdminNetworkPolicyIngressRule) map[string]struct{} {
	requested := make(map[string]struct{})
	for _, rule := range rules {
		if rule.Ports == nil {
			continue
		}
		for _, port := range *rule.Ports {
			if port.NamedPort != nil {
				requested[*port.NamedPort] = struct{}{}
			}
		}
	}
	return requested
}

func requestedNamedPortNamesBanpEgress(rules []v1alpha1.BaselineAdminNetworkPolicyEgressRule) map[string]struct{} {
	requested := make(map[string]struct{})
	for _, rule := range rules {
		if rule.Ports == nil {
			continue
		}
		for _, port := range *rule.Ports {
			if port.NamedPort != nil {
				requested[*port.NamedPort] = struct{}{}
			}
		}
	}
	return requested
}

func requestedNamedPortNamesCnp(rules []v1alpha2.ClusterNetworkPolicyIngressRule) map[string]struct{} {
	requested := make(map[string]struct{})
	for _, rule := range rules {
		for _, protocol := range rule.Protocols {
			if protocol.DestinationNamedPort != "" {
				requested[protocol.DestinationNamedPort] = struct{}{}
			}
		}
	}
	return requested
}

func requestedNamedPortNamesCnpEgress(rules []v1alpha2.ClusterNetworkPolicyEgressRule) map[string]struct{} {
	requested := make(map[string]struct{})
	for _, rule := range rules {
		for _, protocol := range rule.Protocols {
			if protocol.DestinationNamedPort != "" {
				requested[protocol.DestinationNamedPort] = struct{}{}
			}
		}
	}
	return requested
}

func cnpNamedPortProtocol(variant namedPortKey) v1alpha2.ClusterNetworkPolicyProtocol {
	port := &v1alpha2.Port{Number: variant.port}
	protocol := v1alpha2.ClusterNetworkPolicyProtocol{}
	switch variant.protocol {
	case corev1.ProtocolUDP:
		protocol.UDP = &v1alpha2.ClusterNetworkPolicyProtocolUDP{DestinationPort: port}
	case corev1.ProtocolSCTP:
		protocol.SCTP = &v1alpha2.ClusterNetworkPolicyProtocolSCTP{DestinationPort: port}
	default:
		protocol.TCP = &v1alpha2.ClusterNetworkPolicyProtocolTCP{DestinationPort: port}
	}
	return protocol
}

func mergeNamedPortAddresses(dst, src map[namedPortKey]*policyNamedPortAddresses) {
	for key, addresses := range src {
		current := dst[key]
		if current == nil {
			current = &policyNamedPortAddresses{variant: key}
			dst[key] = current
		}
		current.v4 = append(current.v4, addresses.v4...)
		current.v6 = append(current.v6, addresses.v6...)
	}
}

func (c *Controller) enqueueNamedPortPolicies() {
	if c.config.EnableANP {
		if c.anpsLister != nil {
			if policies, err := c.anpsLister.List(labels.Everything()); err == nil {
				for _, policy := range policies {
					if hasAnpNamedPort(policy.Spec.Ingress, policy.Spec.Egress) && c.addAnpQueue != nil {
						c.addAnpQueue.Add(policy.Name)
					}
				}
			}
		}
		if c.banpsLister != nil {
			if policies, err := c.banpsLister.List(labels.Everything()); err == nil {
				for _, policy := range policies {
					if hasBanpNamedPort(policy.Spec.Ingress, policy.Spec.Egress) && c.addBanpQueue != nil {
						c.addBanpQueue.Add(policy.Name)
					}
				}
			}
		}
		if c.cnpsLister != nil {
			if policies, err := c.cnpsLister.List(labels.Everything()); err == nil {
				for _, policy := range policies {
					if hasCnpNamedPort(policy) && c.addCnpQueue != nil {
						c.addCnpQueue.Add(policy.Name)
					}
				}
			}
		}
	}
}

func hasAnpNamedPort(ingress []v1alpha1.AdminNetworkPolicyIngressRule, egress []v1alpha1.AdminNetworkPolicyEgressRule) bool {
	for _, rule := range ingress {
		if rule.Ports != nil && slices.ContainsFunc(*rule.Ports, func(port v1alpha1.AdminNetworkPolicyPort) bool { return port.NamedPort != nil }) {
			return true
		}
	}
	for _, rule := range egress {
		if rule.Ports != nil && slices.ContainsFunc(*rule.Ports, func(port v1alpha1.AdminNetworkPolicyPort) bool { return port.NamedPort != nil }) {
			return true
		}
	}
	return false
}

func hasBanpNamedPort(ingress []v1alpha1.BaselineAdminNetworkPolicyIngressRule, egress []v1alpha1.BaselineAdminNetworkPolicyEgressRule) bool {
	for _, rule := range ingress {
		if rule.Ports != nil && slices.ContainsFunc(*rule.Ports, func(port v1alpha1.AdminNetworkPolicyPort) bool { return port.NamedPort != nil }) {
			return true
		}
	}
	for _, rule := range egress {
		if rule.Ports != nil && slices.ContainsFunc(*rule.Ports, func(port v1alpha1.AdminNetworkPolicyPort) bool { return port.NamedPort != nil }) {
			return true
		}
	}
	return false
}

func hasCnpNamedPort(policy *v1alpha2.ClusterNetworkPolicy) bool {
	for _, rule := range policy.Spec.Ingress {
		for _, protocol := range rule.Protocols {
			if protocol.DestinationNamedPort != "" {
				return true
			}
		}
	}
	for _, rule := range policy.Spec.Egress {
		for _, protocol := range rule.Protocols {
			if protocol.DestinationNamedPort != "" {
				return true
			}
		}
	}
	return false
}

func (c *Controller) namedPortAddressesForCnpEgressPeers(peers []v1alpha2.ClusterNetworkPolicyEgressPeer, name string) (map[namedPortKey]*policyNamedPortAddresses, error) {
	result := make(map[namedPortKey]*policyNamedPortAddresses)
	for _, peer := range peers {
		nsSelector, podSelector := namedPortSelectorsForCnpEgressPeer(&peer)
		if nsSelector == nil && podSelector == nil {
			continue
		}
		selectors, err := c.namedPortSelectors(nsSelector, podSelector)
		if err != nil {
			return nil, err
		}
		addresses, err := c.namedPortAddressesForSelectors(selectors, name, "")
		if err != nil {
			return nil, err
		}
		mergeNamedPortAddresses(result, addresses)
	}
	return result, nil
}

func (c *Controller) namedPortAddressesForAnpEgressPeers(peers []v1alpha1.AdminNetworkPolicyEgressPeer, name string) (map[namedPortKey]*policyNamedPortAddresses, error) {
	result := make(map[namedPortKey]*policyNamedPortAddresses)
	for _, peer := range peers {
		nsSelector, podSelector := namedPortSelectorsForAnpEgressPeer(&peer)
		if nsSelector == nil && podSelector == nil {
			continue
		}
		selectors, err := c.namedPortSelectors(nsSelector, podSelector)
		if err != nil {
			return nil, err
		}
		addresses, err := c.namedPortAddressesForSelectors(selectors, name, "")
		if err != nil {
			return nil, err
		}
		mergeNamedPortAddresses(result, addresses)
	}
	return result, nil
}

func (c *Controller) namedPortAddressesForBanpEgressPeers(peers []v1alpha1.BaselineAdminNetworkPolicyEgressPeer, name string) (map[namedPortKey]*policyNamedPortAddresses, error) {
	result := make(map[namedPortKey]*policyNamedPortAddresses)
	for _, peer := range peers {
		var nsSelector, podSelector *metav1.LabelSelector
		if peer.Namespaces != nil {
			nsSelector, podSelector = peer.Namespaces, nil
		} else if peer.Pods != nil {
			nsSelector, podSelector = &peer.Pods.NamespaceSelector, &peer.Pods.PodSelector
		}
		if nsSelector == nil && podSelector == nil {
			continue
		}
		selectors, err := c.namedPortSelectors(nsSelector, podSelector)
		if err != nil {
			return nil, err
		}
		addresses, err := c.namedPortAddressesForSelectors(selectors, name, "")
		if err != nil {
			return nil, err
		}
		mergeNamedPortAddresses(result, addresses)
	}
	return result, nil
}

func (c *Controller) namedPortVariantsForSelectors(selectors policyNamedPortSelectors, requested map[string]struct{}, baseName string) ([]networkPolicyNamedPortVariant, error) {
	namespaces, err := c.namespacesLister.List(selectors.namespaces)
	if err != nil {
		return nil, fmt.Errorf("failed to list selected namespaces: %w", err)
	}
	byName := make(map[string]*networkPolicyNamedPortVariant)
	for _, namespace := range namespaces {
		indexed := c.namedPort.getNamedPortVariantsByNs(namespace.Name)
		for name, nameVariants := range indexed {
			for key, podNames := range nameVariants {
				if _, ok := requested[name]; !ok {
					continue
				}
				ports, err := c.fetchNamedPortLSPs(namespace.Name, selectors.pods, nil, podNames)
				if err != nil {
					return nil, err
				}
				if len(ports) == 0 {
					continue
				}
				pgName := networkPolicyNamedPortGroupName(baseName, name, key.protocol, key.port)
				variant := byName[pgName]
				if variant == nil {
					variant = &networkPolicyNamedPortVariant{name: name, protocol: key.protocol, port: key.port, pgName: pgName}
					byName[pgName] = variant
				}
				variant.ports = append(variant.ports, ports...)
			}
		}
	}
	variants := make([]networkPolicyNamedPortVariant, 0, len(byName))
	for _, variant := range byName {
		variant.ports = slices.Compact(variant.ports)
		variants = append(variants, *variant)
	}
	return variants, nil
}

func (c *Controller) ensurePolicyNamedPortGroups(externalID, externalValue string, variants []networkPolicyNamedPortVariant) error {
	desired := make(map[string]struct{}, len(variants))
	for _, variant := range variants {
		desired[variant.pgName] = struct{}{}
		if err := c.OVNNbClient.CreatePortGroup(variant.pgName, map[string]string{
			externalID:                externalValue,
			networkPolicyNamedPortKey: "true",
		}); err != nil {
			return err
		}
		if err := c.OVNNbClient.PortGroupSetPorts(variant.pgName, variant.ports); err != nil {
			return err
		}
		if err := c.OVNNbClient.DeleteAcls(variant.pgName, portGroupKey, "to-lport", nil); err != nil {
			return err
		}
	}
	groups, err := c.OVNNbClient.ListPortGroups(map[string]string{externalID: externalValue, networkPolicyNamedPortKey: "true"})
	if err != nil {
		return err
	}
	for _, group := range groups {
		if _, ok := desired[group.Name]; ok {
			continue
		}
		if err := c.OVNNbClient.DeletePortGroup(group.Name); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) deletePolicyNamedPortGroups(externalID, externalValue string) error {
	groups, err := c.OVNNbClient.ListPortGroups(map[string]string{externalID: externalValue, networkPolicyNamedPortKey: "true"})
	if err != nil {
		return err
	}
	for _, group := range groups {
		if err := c.OVNNbClient.DeletePortGroup(group.Name); err != nil {
			return err
		}
	}
	return nil
}

func namedPortNumber(protocol corev1.Protocol, port int32) v1alpha1.AdminNetworkPolicyPort {
	return v1alpha1.AdminNetworkPolicyPort{PortNumber: &v1alpha1.Port{Protocol: protocol, Port: port}}
}

func (c *Controller) fetchNamedPortLSPs(namespace string, selector labels.Selector, providers set.Set[string], podNames *strset.Set) ([]string, error) {
	pods, err := c.podsLister.Pods(namespace).List(selector)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}
	ports := make([]string, 0)
	for _, pod := range pods {
		if pod.Spec.HostNetwork || !podNames.Has(pod.Name) {
			continue
		}
		podName := c.getNameByPod(pod)
		podNets, err := c.getPodKubeovnNets(pod)
		if err != nil {
			return nil, fmt.Errorf("failed to get pod networks: %w", err)
		}
		for _, podNet := range podNets {
			if !isOvnSubnet(podNet.Subnet) || !netpolAppliesToProvider(podNet.ProviderName, providers) {
				continue
			}
			if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)] == "true" {
				ports = append(ports, ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName))
			}
		}
	}
	return ports, nil
}

type policyNamedPortAddresses struct {
	variant namedPortKey
	v4      []string
	v6      []string
}

func (c *Controller) namedPortAddressesForSelectors(selectors policyNamedPortSelectors, name string, transport corev1.Protocol) (map[namedPortKey]*policyNamedPortAddresses, error) {
	namespaces, err := c.namespacesLister.List(selectors.namespaces)
	if err != nil {
		return nil, fmt.Errorf("failed to list selected namespaces: %w", err)
	}
	result := make(map[namedPortKey]*policyNamedPortAddresses)
	for _, namespace := range namespaces {
		variants := c.namedPort.getNamedPortVariantsByNs(namespace.Name)[name]
		if len(variants) == 0 {
			continue
		}
		pods, err := c.podsLister.Pods(namespace.Name).List(selectors.pods)
		if err != nil {
			return nil, fmt.Errorf("failed to list pods: %w", err)
		}
		for _, pod := range pods {
			if pod.Spec.HostNetwork {
				continue
			}
			podNets, err := c.getPodKubeovnNets(pod)
			if err != nil {
				return nil, fmt.Errorf("failed to get pod networks: %w", err)
			}
			for variant, podNames := range variants {
				if (transport != "" && variant.protocol != transport) || !podNames.Has(pod.Name) {
					continue
				}
				addresses := result[variant]
				if addresses == nil {
					addresses = &policyNamedPortAddresses{variant: variant}
					result[variant] = addresses
				}
				for _, podNet := range podNets {
					if !isOvnSubnet(podNet.Subnet) {
						continue
					}
					if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)] != "true" {
						continue
					}
					for podIP := range strings.SplitSeq(pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, podNet.ProviderName)], ",") {
						switch protocol := util.CheckProtocol(podIP); protocol {
						case kubeovnv1.ProtocolIPv4:
							addresses.v4 = append(addresses.v4, podIP)
						case kubeovnv1.ProtocolIPv6:
							addresses.v6 = append(addresses.v6, podIP)
						}
					}
				}
			}
		}
	}
	return result, nil
}
