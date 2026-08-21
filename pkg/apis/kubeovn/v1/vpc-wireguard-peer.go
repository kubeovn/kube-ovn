package v1

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VpcWireGuardPeerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []VpcWireGuardPeer `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=vpc-wireguard-peers
// +kubebuilder:resource:scope="Cluster",shortName="vpc-wg-peer",path="vpc-wireguard-peers",singular="vpc-wireguard-peer"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="WireGuard",type="string",JSONPath=".spec.wireGuard"
// +kubebuilder:printcolumn:name="ClientIP",type="string",JSONPath=".status.clientIP"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type VpcWireGuardPeer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   VpcWireGuardPeerSpec   `json:"spec"`
	Status VpcWireGuardPeerStatus `json:"status"`
}

type VpcWireGuardPeerSpec struct {
	// VpcWireGuard name this peer belongs to. Immutable after creation.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="wireGuard is immutable"
	WireGuard string `json:"wireGuard"`
	// Client public key. Required when generateKey is false.
	PublicKey string `json:"publicKey,omitempty"`
	// Generate a client keypair into a Secret. Mutually exclusive with publicKey.
	GenerateKey bool `json:"generateKey,omitempty"`
	// Optional preshared key secret.
	PresharedKeySecretRef *corev1.SecretKeySelector `json:"presharedKeySecretRef,omitempty"`
	// Optional static client IP in the WireGuard clientSubnet. Empty means allocate.
	ClientIP string `json:"clientIP,omitempty"`
	// PersistentKeepalive interval in seconds. 0 disables it.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=65535
	PersistentKeepalive int32 `json:"persistentKeepalive,omitempty"`
}

type VpcWireGuardPeerStatus struct {
	// Ready state of the peer.
	Ready bool `json:"ready"`
	// Allocated or requested client tunnel IP.
	ClientIP string `json:"clientIP,omitempty"`
	// Client public key actually programmed on the server.
	PublicKey string `json:"publicKey,omitempty"`
	// Server public key copied from the gateway.
	ServerPublicKey string `json:"serverPublicKey,omitempty"`
	// Public endpoint copied from the gateway.
	Endpoint string `json:"endpoint,omitempty"`
	// Secret containing wg-quick.conf when generateKey is true.
	ConfigSecret string `json:"configSecret,omitempty"`
	// Conditions represent the latest state of the object.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

func (s *VpcWireGuardPeerStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}
