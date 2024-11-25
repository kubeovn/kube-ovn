package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Phase represents resource phase
type Phase string

const (
	PhasePending    Phase = "Pending"
	PhaseProcessing Phase = "Processing"
	PhaseCompleted  Phase = "Completed"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VpcEgressGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []VpcEgressGateway `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +resourceName=vpc-egress-gateways
type VpcEgressGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VpcEgressGatewaySpec   `json:"spec"`
	Status VpcEgressGatewayStatus `json:"status,omitempty"`
}

func (g *VpcEgressGateway) Ready() bool {
	return g.Status.Ready && g.Status.Conditions.IsReady(g.Generation)
}

type VpcEgressGatewaySpec struct {
	VPC            string   `json:"vpc,omitempty"`
	Replicas       int32    `json:"replicas,omitempty"`
	Prefix         string   `json:"prefix,omitempty"`
	Image          string   `json:"image,omitempty"`
	InternalSubnet string   `json:"internalSubnet,omitempty"`
	ExternalSubnet string   `json:"externalSubnet"`
	InternalIPs    []string `json:"internalIPs,omitempty"`
	ExternalIPs    []string `json:"externalIPs,omitempty"`

	BFD          VpcEgressGatewayBFDConfig      `json:"bfd,omitempty"`
	Policies     []VpcEgressGatewayPolicy       `json:"policies,omitempty"`
	NodeSelector []VpcEgressGatewayNodeSelector `json:"nodeSelector,omitempty"`
}

type VpcEgressGatewayBFDConfig struct {
	Enabled    bool  `json:"enabled"`
	MinRX      int32 `json:"minRX,omitempty"`
	MinTX      int32 `json:"minTX,omitempty"`
	Multiplier int32 `json:"multiplier,omitempty"`
}

type VpcEgressGatewayPolicy struct {
	SNAT     bool     `json:"snat"`
	IPBlocks []string `json:"ipBlocks,omitempty"`
	Subnets  []string `json:"subnets,omitempty"`
}

type VpcEgressGatewayNodeSelector struct {
	MatchLabels      map[string]string                `json:"matchLabels,omitempty"`
	MatchExpressions []corev1.NodeSelectorRequirement `json:"matchExpressions,omitempty"`
	MatchFields      []corev1.NodeSelectorRequirement `json:"matchFields,omitempty"`
}

type VpcEgressGatewayStatus struct {
	Ready       bool       `json:"ready"`
	Phase       Phase      `json:"phase"`
	InternalIPs []string   `json:"internalIPs,omitempty"`
	ExternalIPs []string   `json:"externalIPs,omitempty"`
	Conditions  Conditions `json:"conditions,omitempty"`

	Workload VpcEgressWorkload `json:"workload,omitempty"`
}

type VpcEgressWorkload struct {
	APIVersion string   `json:"apiVersion,omitempty"`
	Kind       string   `json:"kind,omitempty"`
	Name       string   `json:"name,omitempty"`
	Nodes      []string `json:"nodes,omitempty"`
}
