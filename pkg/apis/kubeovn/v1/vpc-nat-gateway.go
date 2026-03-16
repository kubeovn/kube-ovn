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
	Vpc             string              `json:"vpc"`
	Subnet          string              `json:"subnet"`
	ExternalSubnets []string            `json:"externalSubnets"`
	LanIP           string              `json:"lanIp"`
	Selector        []string            `json:"selector"`
	Tolerations     []corev1.Toleration `json:"tolerations"`
	Affinity        corev1.Affinity     `json:"affinity"`
	QoSPolicy       string              `json:"qosPolicy"`
	BgpSpeaker      VpcBgpSpeaker       `json:"bgpSpeaker"`
	Routes          []Route             `json:"routes"`
	NoDefaultEIP    bool                `json:"noDefaultEIP"`
	// User-defined annotations for the StatefulSet NAT gateway Pod template.
	// Only effective at creation time; updates to this field are not detected.
	Annotations map[string]string `json:"annotations,omitempty"`
}

type VpcBgpSpeaker struct {
	Enabled               bool            `json:"enabled"`
	ASN                   uint32          `json:"asn"`
	RemoteASN             uint32          `json:"remoteAsn"`
	Neighbors             []string        `json:"neighbors"`
	HoldTime              metav1.Duration `json:"holdTime"`
	RouterID              string          `json:"routerId"`
	Password              string          `json:"password"`
	EnableGracefulRestart bool            `json:"enableGracefulRestart"`
	ExtraArgs             []string        `json:"extraArgs"`
}

// TODO: Consider removing redundant Status fields since statefulset template changes always trigger Pod recreation.
type VpcNatGatewayStatus struct {
	QoSPolicy       string              `json:"qosPolicy" patchStrategy:"merge"`
	ExternalSubnets []string            `json:"externalSubnets" patchStrategy:"merge"`
	Selector        []string            `json:"selector" patchStrategy:"merge"`
	Tolerations     []corev1.Toleration `json:"tolerations" patchStrategy:"merge"`
	Affinity        corev1.Affinity     `json:"affinity" patchStrategy:"merge"`
}

type Route struct {
	CIDR      string `json:"cidr"`
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
