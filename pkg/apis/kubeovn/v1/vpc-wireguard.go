package v1

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

const (
	// VpcWireGuardExposureDualNIC attaches a fabric/underlay NIC via Multus, like VpcNatGateway.
	VpcWireGuardExposureDualNIC = "DualNIC"
	// VpcWireGuardExposureDNAT publishes UDP listenPort through an IptablesDnatRule on a VpcNatGateway EIP.
	VpcWireGuardExposureDNAT = "DNAT"
	// VpcWireGuardExposureFIP publishes the server LAN IP through an exclusive IptablesFIPRule.
	VpcWireGuardExposureFIP = "FIP"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VpcWireGuardList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []VpcWireGuard `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=vpc-wireguards
// +kubebuilder:resource:scope="Cluster",shortName="vpc-wg",path="vpc-wireguards",singular="vpc-wireguard"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Vpc",type="string",JSONPath=".spec.vpc"
// +kubebuilder:printcolumn:name="Subnet",type="string",JSONPath=".spec.subnet"
// +kubebuilder:printcolumn:name="Exposure",type="string",JSONPath=".spec.exposure.type"
// +kubebuilder:printcolumn:name="Endpoint",type="string",JSONPath=".status.endpoint"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:validation:XValidation:rule="self.metadata.name.size() <= 40",message="name must be no more than 40 characters"
type VpcWireGuard struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   VpcWireGuardSpec   `json:"spec"`
	Status VpcWireGuardStatus `json:"status"`
}

type VpcWireGuardSpec struct {
	// Namespace where the WireGuard StatefulSet/Pod will be created.
	// If empty, defaults to the kube-ovn controller namespace.
	Namespace string `json:"namespace,omitempty"`
	// VPC name. Immutable after creation.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="vpc is immutable"
	Vpc string `json:"vpc"`
	// Overlay subnet for the WireGuard server pod LAN NIC. Immutable after creation.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="subnet is immutable"
	Subnet string `json:"subnet"`
	// Optional static LAN IP on spec.subnet.
	LanIP string `json:"lanIp,omitempty"`
	// Overlay subnet used as the client IP pool. Immutable after creation.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="clientSubnet is immutable"
	ClientSubnet string `json:"clientSubnet"`
	// UDP listen port. Defaults to 51820.
	// +kubebuilder:default=51820
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	ListenPort int32 `json:"listenPort,omitempty"`
	// Optional WireGuard interface MTU.
	// +kubebuilder:validation:Minimum=1280
	// +kubebuilder:validation:Maximum=65535
	MTU int32 `json:"mtu,omitempty"`
	// How the server is reached from the internet.
	Exposure VpcWireGuardExposure `json:"exposure"`
	// Generate a server keypair into a Secret. Mutually exclusive with publicKey.
	GenerateServerKey bool `json:"generateServerKey,omitempty"`
	// Existing server public key when generateServerKey is false.
	PublicKey string `json:"publicKey,omitempty"`
	// Secret holding the existing server private key when generateServerKey is false.
	PrivateKeySecretRef *corev1.SecretKeySelector `json:"privateKeySecretRef,omitempty"`
	// CIDRs advertised to clients as AllowedIPs. Empty means all CIDRs of the VPC.
	AllowedIPs []string `json:"allowedIPs,omitempty"`
	// Pod selector for the WireGuard server.
	Selector []string `json:"selector,omitempty"`
	// Pod tolerations.
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// Pod affinity.
	Affinity corev1.Affinity `json:"affinity"`
}

type VpcWireGuardExposure struct {
	// Exposure mode.
	// +kubebuilder:validation:Enum=DualNIC;DNAT;FIP
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="exposure type is immutable"
	Type string `json:"type"`
	// External/underlay subnets for DualNIC (first entry is used).
	ExternalSubnets []string `json:"externalSubnets,omitempty"`
	// IptablesEIP name. Required for DNAT and FIP. Optional static hint for DualNIC.
	EIP string `json:"eip,omitempty"`
	// VpcNatGateway name that owns the EIP. Required for DNAT and FIP.
	NatGateway string `json:"natGateway,omitempty"`
}

type VpcWireGuardStatus struct {
	// Ready state of the WireGuard server.
	Ready bool `json:"ready"`
	// LAN IP of the server pod.
	LanIP string `json:"lanIp,omitempty"`
	// Public endpoint host:port clients should use.
	Endpoint string `json:"endpoint,omitempty"`
	// Server public key.
	PublicKey string `json:"publicKey,omitempty"`
	// Client CIDR from clientSubnet.
	ClientCIDR string `json:"clientCIDR,omitempty"`
	// Tunnel address assigned to wg0 from clientSubnet.
	ServerTunnelIP string `json:"serverTunnelIP,omitempty"`
	// Secret storing the generated server private key.
	ServerKeySecret string `json:"serverKeySecret,omitempty"`
	// Conditions represent the latest state of the object.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

func (s *VpcWireGuardStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}
