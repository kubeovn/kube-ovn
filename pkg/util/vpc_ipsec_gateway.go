package util

import (
	"fmt"
	"maps"
	"strings"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

const (
	VpcIPsecGwNameDefaultPrefix = "vpc-ipsec-gw"
	// StatefulSet controller appends "-<10-char-hash>" to controller-revision-hash.
	IPsecGwStatefulSetNameMaxLength = validation.LabelValueMaxLength - statefulSetRevisionHashSuffixLength
	DefaultIPsecPSKSecretKey        = "psk"
)

// VpcIPsecGwNamePrefix is appended to the name of the StatefulSet/Pods for IPsec gateways.
var VpcIPsecGwNamePrefix = VpcIPsecGwNameDefaultPrefix

// GenIPsecGwName returns the full name of an IPsec gateway StatefulSet.
func GenIPsecGwName(name string) string {
	prefix := VpcIPsecGwNamePrefix
	if prefix == "" {
		prefix = VpcIPsecGwNameDefaultPrefix
	}
	return fmt.Sprintf("%s-%s", prefix, name)
}

// GenIPsecGwPodName returns the full name of the IPsec gateway pod within a StatefulSet.
func GenIPsecGwPodName(name string) string {
	return fmt.Sprintf("%s-0", GenIPsecGwName(name))
}

// GenIPsecGwConfName returns the ConfigMap name that holds swanctl configuration.
func GenIPsecGwConfName(name string) string {
	return fmt.Sprintf("%s-conf", GenIPsecGwName(name))
}

// ValidateIPsecGwStatefulSetNameLength validates generated StatefulSet name length.
func ValidateIPsecGwStatefulSetNameLength(gwName string) error {
	statefulSetName := GenIPsecGwName(gwName)
	if len(statefulSetName) > IPsecGwStatefulSetNameMaxLength {
		return fmt.Errorf("generated IPsec gateway statefulset name %q length %d exceeds max %d; choose a shorter gateway name",
			statefulSetName, len(statefulSetName), IPsecGwStatefulSetNameMaxLength)
	}
	return nil
}

// GenIPsecGwLabels returns labels for IPsec gateway workloads.
func GenIPsecGwLabels(gwName string) map[string]string {
	return map[string]string{
		"app":                    GenIPsecGwName(gwName),
		VpcIPsecGatewayLabel:     "true",
		VpcIPsecGatewayNameLabel: gwName,
	}
}

// GenIPsecGwSelectors converts "key: value" selector entries into a map.
func GenIPsecGwSelectors(selectors []string) map[string]string {
	s := make(map[string]string, len(selectors))
	for _, v := range selectors {
		parts := strings.Split(strings.TrimSpace(v), ":")
		if len(parts) != 2 {
			continue
		}
		s[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return s
}

// ResolveIPsecPSKSecretRef returns namespace and key with defaults applied.
func ResolveIPsecPSKSecretRef(ref kubeovnv1.VpcIPsecPSKSecretRef, defaultNamespace string) (namespace, key string) {
	namespace = ref.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}
	key = ref.Key
	if key == "" {
		key = DefaultIPsecPSKSecretKey
	}
	return namespace, key
}

// GenIPsecGwPodAnnotations generates Pod template annotations for an IPsec gateway.
func GenIPsecGwPodAnnotations(
	userAnnotations map[string]string,
	gw *kubeovnv1.VpcIPsecGateway,
	externalNadNamespace, externalNadName, provider string,
	enableNonPrimaryCNI bool,
) (map[string]string, error) {
	p := provider
	if p == "" {
		p = OvnProvider
	}

	attachedNetworks := fmt.Sprintf("%s/%s", externalNadNamespace, externalNadName)
	result := make(map[string]string, len(userAnnotations)+5)
	maps.Copy(result, userAnnotations)

	result[nadv1.NetworkAttachmentAnnot] = attachedNetworks
	result[VpcIPsecGatewayAnnotation] = gw.Name
	result[fmt.Sprintf(LogicalSwitchAnnotationTemplate, p)] = gw.Spec.Subnet
	result[fmt.Sprintf(IPAddressAnnotationTemplate, p)] = gw.Spec.LanIP

	if p != OvnProvider {
		providerSplit := strings.Split(provider, ".")
		if len(providerSplit) != 3 || providerSplit[2] != OvnProvider {
			return nil, fmt.Errorf("name of the provider must have syntax 'name.namespace.ovn', got %s", provider)
		}
		if !enableNonPrimaryCNI {
			name, namespace := providerSplit[0], providerSplit[1]
			result[DefaultNetworkAnnotation] = fmt.Sprintf("%s/%s", namespace, name)
		}
	}
	return result, nil
}
