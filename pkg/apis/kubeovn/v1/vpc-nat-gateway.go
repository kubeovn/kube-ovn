package v1

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VpcNatGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []VpcNatGateway `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=vpc-nat-gateways
// +kubebuilder:resource:scope="Cluster",shortName="vpc-nat-gw",path="vpc-nat-gateways",singular="vpc-nat-gateway"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Vpc",type="string",JSONPath=".spec.vpc"
// +kubebuilder:printcolumn:name="Subnet",type="string",JSONPath=".spec.subnet"
// +kubebuilder:printcolumn:name="LanIP",type="string",JSONPath=".spec.lanIp"
//
// VpcNatGateway represents a NAT gateway for a VPC, implemented as a StatefulSet Pod.
//
// Architecture note:
// The NAT gateway Pod does NOT support hot updates. Any changes to Spec fields (ExternalSubnets,
// Selector, Tolerations, Affinity, etc.) will trigger a StatefulSet template update,
// which causes the Pod to be recreated via RollingUpdate strategy. This is by design because:
//  1. Network configuration (routes, iptables rules) is initialized at Pod startup
//  2. Runtime state (vpc_cidrs, init status) is managed by separate handlers and will be
//     automatically restored after Pod recreation through the normal reconciliation flow
//
// The only exception is QoSPolicy, which can be updated without Pod restart.
type VpcNatGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   VpcNatGatewaySpec   `json:"spec"`
	Status VpcNatGatewayStatus `json:"status"`
}

type VpcNatGatewaySpec struct {
	// Namespace where the NAT gateway StatefulSet/Pod will be created.
	// If empty, defaults to the kube-ovn controller's own namespace (typically kube-system).
	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty"`
	// VPC name for the NAT gateway. This field is immutable after creation.
	Vpc string `json:"vpc"`
	// Subnet name for the NAT gateway. This field is immutable after creation.
	Subnet string `json:"subnet"`
	// External subnets accessible through the NAT gateway
	ExternalSubnets []string `json:"externalSubnets"`
	// LAN IP address for the NAT gateway. This field is immutable after creation.
	LanIP string `json:"lanIp"`
	// Pod selector for the NAT gateway
	Selector    []string            `json:"selector"`
	Tolerations []corev1.Toleration `json:"tolerations"`
	Affinity    corev1.Affinity     `json:"affinity"`
	// QoS policy name to apply to the NAT gateway
	QoSPolicy string `json:"qosPolicy"`
	// BGP speaker configuration
	BgpSpeaker VpcBgpSpeaker `json:"bgpSpeaker"`
	// Static routes for the NAT gateway
	Routes []Route `json:"routes"`
	// Disable default EIP assignment
	NoDefaultEIP bool `json:"noDefaultEIP"`
	// User-defined annotations for the StatefulSet NAT gateway Pod template.
	// Only effective at creation time; updates to this field are not detected.
	Annotations map[string]string `json:"annotations,omitempty"`
}

type VpcBgpSpeaker struct {
	// Whether to enable BGP speaker
	Enabled bool `json:"enabled"`
	// BGP ASN
	ASN uint32 `json:"asn"`
	// BGP remote ASN
	RemoteASN uint32 `json:"remoteAsn"`
	// BGP neighbors
	Neighbors []string `json:"neighbors"`
	// BGP hold time
	HoldTime metav1.Duration `json:"holdTime"`
	// BGP router ID
	RouterID string `json:"routerId"`
	// BGP password
	Password string `json:"password"` // #nosec G117
	// Enable graceful restart
	EnableGracefulRestart bool `json:"enableGracefulRestart"`
	// Extra arguments for BGP speaker
	ExtraArgs []string `json:"extraArgs"`
}

// TODO: Consider removing redundant Status fields since statefulset template changes always trigger Pod recreation.
type VpcNatGatewayStatus struct {
	// QoS policy applied to the NAT gateway
	QoSPolicy string `json:"qosPolicy" patchStrategy:"merge"`
	// External subnets configured for the NAT gateway
	ExternalSubnets []string `json:"externalSubnets" patchStrategy:"merge"`
	// Pod selector configured for the NAT gateway
	Selector    []string            `json:"selector" patchStrategy:"merge"`
	Tolerations []corev1.Toleration `json:"tolerations" patchStrategy:"merge"`
	Affinity    corev1.Affinity     `json:"affinity" patchStrategy:"merge"`
}

type Route struct {
	// Route CIDR
	CIDR string `json:"cidr"`
	// Next hop IP
	NextHopIP string `json:"nextHopIP"`
}

func (s *VpcNatGatewayStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}
