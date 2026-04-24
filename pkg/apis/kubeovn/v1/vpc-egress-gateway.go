package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	TrafficPolicyLocal   = "Local"
	TrafficPolicyCluster = "Cluster"
)

// Phase represents resource phase
type Phase string

const (
	// PhasePending means the resource is pending and not processed yet
	PhasePending Phase = "Pending"
	// PhaseProcessing means the resource is being processed
	PhaseProcessing Phase = "Processing"
	// PhaseCompleted means the resource has been processed successfully
	PhaseCompleted Phase = "Completed"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VpcEgressGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []VpcEgressGateway `json:"items"`
}

// +genclient
// +genclient:method=GetScale,verb=get,subresource=scale,result=k8s.io/api/autoscaling/v1.Scale
// +genclient:method=UpdateScale,verb=update,subresource=scale,input=k8s.io/api/autoscaling/v1.Scale,result=k8s.io/api/autoscaling/v1.Scale
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +resourceName=vpc-egress-gateways
// +kubebuilder:resource:scope="Namespaced",shortName={"vpc-egress-gw","veg"},path="vpc-egress-gateways",singular="vpc-egress-gateway"
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.labelSelector
// +kubebuilder:printcolumn:name="Vpc",type="string",JSONPath=".spec.vpc"
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".spec.replicas"
// +kubebuilder:printcolumn:name="bfd",type="boolean",JSONPath=".spec.bfd.enabled"
// +kubebuilder:printcolumn:name="External Subnet",type="string",JSONPath=".spec.externalSubnet"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready"
// +kubebuilder:printcolumn:name="Internal IPs",type="string",JSONPath=".status.internalIPs",priority=1
// +kubebuilder:printcolumn:name="External IPs",type="string",JSONPath=".status.externalIPs",priority=1
// +kubebuilder:printcolumn:name="Working Nodes",type="string",JSONPath=".status.workload.nodes",priority=1
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// vpc egress gateway is used to forward the egress traffic from the VPC to the external network
type VpcEgressGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   VpcEgressGatewaySpec   `json:"spec"`
	Status VpcEgressGatewayStatus `json:"status"`
}

// VPC returns the VPC name
// If the VpcEgressGateway has no VPC specified in the spec, it will return the default VPC name
func (g *VpcEgressGateway) VPC(defaultVPC string) string {
	if g.Spec.VPC != "" {
		return g.Spec.VPC
	}
	return defaultVPC
}

// Ready returns true if the VpcEgressGateway has been processed successfully and is ready to serve traffic
func (g *VpcEgressGateway) Ready() bool {
	return g.Status.Ready && g.Status.Conditions.IsReady(g.Generation)
}

// BandwidthLimit represents the bandwidth limit for the egress gateway in both ingress and egress directions.
// The bandwidth is specified in Mbps. If not specified, there will be no bandwidth limit.
type BandwidthLimit struct {
	// ingress bandwidth limit in Mbps
	Ingress int64 `json:"ingress,omitempty"`
	// egress bandwidth limit in Mbps
	Egress int64 `json:"egress,omitempty"`
}

type VpcEgressGatewaySpec struct {
	// optional VPC name
	// if not specified, the default VPC will be used
	VPC string `json:"vpc,omitempty"`
	// optional BGP configuration name
	// it references a cluster-scoped BgpConf resource
	BgpConf string `json:"bgpConf,omitempty"`
	// optional EVPN configuration name
	// it references a cluster-scoped EvpnConf resource
	EvpnConf string `json:"evpnConf,omitempty"`
	// workload replicas
	// +kubebuilder:default=1
	Replicas int32 `json:"replicas,omitempty"`
	// optional name prefix used to generate the workload
	// the workload name will be generated as <prefix><vpc-egress-gateway-name>
	Prefix string `json:"prefix,omitempty"`
	// optional image used by the workload
	// if not specified, the default image passed in by kube-ovn-controller will be used
	Image string `json:"image,omitempty"`
	// optional internal subnet used to create the workload
	// if not specified, the workload will be created in the default subnet of the VPC
	InternalSubnet string `json:"internalSubnet,omitempty"`
	// external subnet used to create the workload
	// +kubebuilder:validation:Required
	ExternalSubnet string `json:"externalSubnet"`
	// optional internal/external IPs used to create the workload
	// these IPs must be in the internal/external subnet
	// the IPs count must NOT be less than the replicas count
	InternalIPs []string `json:"internalIPs,omitempty"`
	// External IP addresses for the egress gateway
	ExternalIPs []string `json:"externalIPs,omitempty"`
	// namespace/pod selectors
	Selectors []VpcEgressGatewaySelector `json:"selectors,omitempty"`
	// optional traffic policy used to control the traffic routing
	// if not specified, the default traffic policy "Cluster" will be used
	// if set to "Local", traffic will be routed to the gateway pod/instance on the same node when available
	// currently it works only for the default vpc
	// +kubebuilder:default=Cluster
	TrafficPolicy string `json:"trafficPolicy,omitempty"`

	// BFD configuration
	BFD VpcEgressGatewayBFDConfig `json:"bfd"`
	// egress policies
	// at least one policy must be specified
	Policies []VpcEgressGatewayPolicy `json:"policies,omitempty"`
	// optional node selector used to select the nodes where the workload will be running
	NodeSelector []VpcEgressGatewayNodeSelector `json:"nodeSelector,omitempty"`
	// optional tolerations applied to the workload pods
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Compute Resources required for the container. If not specified, the controller will set a default value.
	// If specified, the controller will not set any default value and use the specified value directly.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Optional bandwidth limit for each egress gateway instance in both ingress and egress directions.
	// If not specified, there will be no bandwidth limit.
	Bandwidth *BandwidthLimit `json:"bandwidth,omitempty"`
}

type VpcEgressGatewaySelector struct {
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
	PodSelector       *metav1.LabelSelector `json:"podSelector,omitempty"`
}

type VpcEgressGatewayBFDConfig struct {
	// whether to enable BFD
	// if set to true, the egress gateway will establish BFD session(s) with the VPC BFD LRP
	// the VPC's .spec.bfd.enabled must be set to true to enable BFD
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`
	// optional BFD minRX/minTX/multiplier
	// +kubebuilder:default=1000
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=3600000
	MinRX int32 `json:"minRX,omitempty"`
	// +kubebuilder:default=1000
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=3600000
	MinTX int32 `json:"minTX,omitempty"`
	// +kubebuilder:default=3
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=3600000
	Multiplier int32 `json:"multiplier,omitempty"`
}

type VpcEgressGatewayPolicy struct {
	// whether to enable SNAT/MASQUERADE for the egress traffic
	// +kubebuilder:default=false
	SNAT bool `json:"snat"`
	// CIDRs/subnets targeted by the egress traffic policy
	IPBlocks []string `json:"ipBlocks,omitempty"`
	Subnets  []string `json:"subnets,omitempty"`
}

type VpcEgressGatewayNodeSelector struct {
	MatchLabels      map[string]string                `json:"matchLabels,omitempty"`
	MatchExpressions []corev1.NodeSelectorRequirement `json:"matchExpressions,omitempty"`
	MatchFields      []corev1.NodeSelectorRequirement `json:"matchFields,omitempty"`
}

type VpcEgressGatewayStatus struct {
	// used by the scale subresource
	Replicas int32 `json:"replicas,omitempty"`
	// Label selector for the egress gateway
	LabelSelector string `json:"labelSelector,omitempty"`

	// whether the egress gateway is ready
	// +kubebuilder:default=false
	Ready bool `json:"ready"`
	// Current phase of the egress gateway (Pending, Processing, or Completed)
	// +kubebuilder:default=Pending
	// +kubebuilder:validation:Required
	Phase Phase `json:"phase"`
	// internal/external IPs used by the workload
	InternalIPs []string `json:"internalIPs,omitempty"`
	// External IP addresses assigned to the egress gateway
	ExternalIPs []string `json:"externalIPs,omitempty"`
	// Conditions represent the latest available observations of the egress gateway's current state
	// +kubebuilder:validation:Required
	Conditions Conditions `json:"conditions,omitempty"`

	// workload information
	Workload VpcEgressWorkload `json:"workload"`
}

type VpcEgressWorkload struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Name       string `json:"name,omitempty"`
	// nodes where the workload is running
	Nodes []string `json:"nodes,omitempty"`
}
