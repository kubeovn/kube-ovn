package v1

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VpcIPsecGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []VpcIPsecGateway `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=vpc-ipsec-gateways
// +kubebuilder:resource:scope="Cluster",shortName="vpc-ipsec-gw",path="vpc-ipsec-gateways",singular="vpc-ipsec-gateway"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Namespace",type="string",JSONPath=".spec.namespace"
// +kubebuilder:printcolumn:name="Vpc",type="string",JSONPath=".spec.vpc"
// +kubebuilder:printcolumn:name="Subnet",type="string",JSONPath=".spec.subnet"
// +kubebuilder:printcolumn:name="Remote",type="string",JSONPath=".spec.remoteEndpoint"
// +kubebuilder:printcolumn:name="LanIP",type="string",JSONPath=".status.lanIp"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
//
// VpcIPsecGateway represents a site-to-site IPsec gateway for a VPC.
// The controller provisions a privileged StatefulSet Pod that establishes an
// IPsec tunnel (strongSwan) to a remote endpoint and routes traffic between
// VPC subnets and the remote CIDRs through the tunnel.
type VpcIPsecGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   VpcIPsecGatewaySpec   `json:"spec"`
	Status VpcIPsecGatewayStatus `json:"status"`
}

// VpcIPsecGatewaySpec defines the desired state of VpcIPsecGateway.
type VpcIPsecGatewaySpec struct {
	// Namespace where the IPsec gateway StatefulSet/Pod will be created.
	// If empty, defaults to the kube-ovn controller's own namespace (typically kube-system).
	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty"`
	// VPC name for the IPsec gateway. Immutable after creation.
	// +kubebuilder:validation:Required
	Vpc string `json:"vpc"`
	// Internal (LAN) subnet name for the gateway Pod. Immutable after creation.
	// +kubebuilder:validation:Required
	Subnet string `json:"subnet"`
	// External subnet used for Multus attachment (WAN / underlay path to the remote peer).
	// +kubebuilder:validation:Required
	ExternalSubnet string `json:"externalSubnet"`
	// Static LAN IP for the gateway Pod on the internal subnet. Immutable after creation.
	// +kubebuilder:validation:Required
	LanIP string `json:"lanIp"`
	// Remote IPsec peer address (public/underlay IP or hostname).
	// +kubebuilder:validation:Required
	RemoteEndpoint string `json:"remoteEndpoint"`
	// Remote private CIDRs reachable through the tunnel.
	// OVN static routes for these CIDRs are installed pointing at the gateway LanIP.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	RemoteCIDRs []string `json:"remoteCIDRs"`
	// Local CIDRs advertised as traffic selectors.
	// When empty, the CIDR of Spec.Subnet is used.
	// +kubebuilder:validation:Optional
	LocalCIDRs []string `json:"localCIDRs,omitempty"`
	// Reference to a Secret that holds the pre-shared key.
	// +kubebuilder:validation:Required
	PSKSecretRef VpcIPsecPSKSecretRef `json:"pskSecretRef"`
	// Pod node selector entries in "key: value" form.
	Selector []string `json:"selector,omitempty"`
	// Pod tolerations.
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// Pod affinity.
	Affinity corev1.Affinity `json:"affinity,omitempty"`
	// User-defined annotations for the StatefulSet Pod template.
	// Only effective at creation time; updates to this field are not detected.
	Annotations map[string]string `json:"annotations,omitempty"`
}

// VpcIPsecPSKSecretRef references a Kubernetes Secret containing the IPsec PSK.
type VpcIPsecPSKSecretRef struct {
	// Name of the Secret.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// Namespace of the Secret. Defaults to the gateway workload namespace.
	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty"`
	// Key in the Secret data that holds the PSK. Defaults to "psk".
	// +kubebuilder:default=psk
	// +kubebuilder:validation:Optional
	Key string `json:"key,omitempty"`
}

// VpcIPsecGatewayStatus defines the observed state of VpcIPsecGateway.
type VpcIPsecGatewayStatus struct {
	// Ready is true when the gateway Pod is running and routes are programmed.
	Ready bool `json:"ready"`
	// LAN IP address of the gateway Pod.
	LanIP string `json:"lanIp,omitempty"`
	// Remote peer endpoint currently programmed.
	RemoteEndpoint string `json:"remoteEndpoint,omitempty"`
	// Remote CIDRs currently programmed into OVN.
	RemoteCIDRs []string `json:"remoteCIDRs,omitempty" patchStrategy:"merge"`
	// External subnet currently attached.
	ExternalSubnet string `json:"externalSubnet,omitempty"`
	// Local CIDRs used as traffic selectors.
	LocalCIDRs []string `json:"localCIDRs,omitempty" patchStrategy:"merge"`
	// Pod selector configured for the gateway.
	Selector []string `json:"selector,omitempty" patchStrategy:"merge"`
	Tolerations []corev1.Toleration `json:"tolerations,omitempty" patchStrategy:"merge"`
	Affinity    corev1.Affinity     `json:"affinity,omitempty" patchStrategy:"merge"`
	// Workload information for the underlying StatefulSet.
	Workload VpcIPsecWorkload `json:"workload,omitempty"`
	// Human-readable phase (e.g. Pending, Ready, Error).
	Phase string `json:"phase,omitempty"`
	// Message elaborates on Phase.
	Message string `json:"message,omitempty"`
}

// VpcIPsecWorkload contains information about the underlying StatefulSet.
type VpcIPsecWorkload struct {
	APIVersion string   `json:"apiVersion,omitempty"`
	Kind       string   `json:"kind,omitempty"`
	Name       string   `json:"name,omitempty"`
	Nodes      []string `json:"nodes,omitempty"`
}

func (s *VpcIPsecGatewayStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}
