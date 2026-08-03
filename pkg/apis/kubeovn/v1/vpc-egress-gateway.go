package v1

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	TrafficPolicyLocal   = "Local"
	TrafficPolicyCluster = "Cluster"

	PodAntiAffinityRequired  = "Required"
	PodAntiAffinityPreferred = "Preferred"
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
// Integer values and numeric strings are specified in Mbps. Kubernetes quantity strings such as 100M or 1Gi are specified in bits per second.
// If not specified, there will be no bandwidth limit.
type BandwidthLimit struct {
	// ingress bandwidth limit, specified as an integer in Mbps or a Kubernetes quantity such as 100M or 1Gi in bits per second
	// +kubebuilder:validation:XIntOrString
	// +kubebuilder:validation:Pattern=`^([0-9]+|([0-9]+(\.[0-9]+)?|\.[0-9]+)(M|Mi|G|Gi))$`
	Ingress intstr.IntOrString `json:"ingress,omitempty"`
	// egress bandwidth limit, specified as an integer in Mbps or a Kubernetes quantity such as 100M or 1Gi in bits per second
	// +kubebuilder:validation:XIntOrString
	// +kubebuilder:validation:Pattern=`^([0-9]+|([0-9]+(\.[0-9]+)?|\.[0-9]+)(M|Mi|G|Gi))$`
	Egress intstr.IntOrString `json:"egress,omitempty"`
}

const (
	maxBandwidthMbps     = math.MaxInt64 / 1_000_000
	bandwidthRatePattern = `^([0-9]+|([0-9]+(\.[0-9]+)?|\.[0-9]+)(M|Mi|G|Gi))$`
)

var bandwidthRateRegexp = regexp.MustCompile(bandwidthRatePattern)

func bandwidthRateToMbps(rate intstr.IntOrString) (int64, error) {
	if rate.Type == intstr.Int {
		if rate.IntVal < 0 {
			return 0, errors.New("bandwidth must not be negative")
		}
		return int64(rate.IntVal), nil
	}
	if rate.Type != intstr.String {
		return 0, fmt.Errorf("unsupported IntOrString type %d", rate.Type)
	}

	value := rate.StrVal
	if value == "" {
		return 0, errors.New("bandwidth must not be empty")
	}
	if strings.HasPrefix(value, "-") {
		return 0, errors.New("bandwidth must not be negative")
	}
	if !bandwidthRateRegexp.MatchString(value) {
		return 0, fmt.Errorf("bandwidth %q must be an integer in Mbps or a quantity with unit M, Mi, G, or Gi", value)
	}
	if mbps, err := strconv.ParseInt(value, 10, 64); err == nil {
		if mbps > maxBandwidthMbps {
			return 0, fmt.Errorf("bandwidth %q exceeds the supported maximum", value)
		}
		return mbps, nil
	}

	quantity, err := resource.ParseQuantity(value)
	if err != nil {
		return 0, fmt.Errorf("invalid Kubernetes quantity %q: %w", value, err)
	}
	if quantity.Sign() < 0 {
		return 0, errors.New("bandwidth must not be negative")
	}
	maxQuantity := resource.NewScaledQuantity(maxBandwidthMbps, resource.Mega)
	if quantity.Cmp(*maxQuantity) > 0 {
		return 0, fmt.Errorf("bandwidth %q exceeds the supported maximum", value)
	}
	return quantity.ScaledValue(resource.Mega), nil
}

// Mbps returns the ingress and egress limits normalized to whole Mbps.
// Positive quantities below a whole Mbps are rounded up so they do not silently disable the limit.
func (b *BandwidthLimit) Mbps() (int64, int64, error) {
	if b == nil {
		return 0, 0, nil
	}
	ingress, err := bandwidthRateToMbps(b.Ingress)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid ingress bandwidth: %w", err)
	}
	egress, err := bandwidthRateToMbps(b.Egress)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid egress bandwidth: %w", err)
	}
	return ingress, egress, nil
}

// +kubebuilder:validation:XValidation:rule="!has(self.internalIPs) || size(self.internalIPs) == 0 || size(self.internalIPs) >= self.replicas",message="Size of Internal IPs MUST be equal to or greater than Replicas",fieldPath=".internalIPs"
// +kubebuilder:validation:XValidation:rule="!has(self.externalIPs) || size(self.externalIPs) == 0 || size(self.externalIPs) >= self.replicas",message="Size of External IPs MUST be equal to or greater than Replicas",fieldPath=".externalIPs"
// +kubebuilder:validation:XValidation:rule="(has(self.policies) && size(self.policies) != 0) || (has(self.selectors) && size(self.selectors) != 0)",message="Each VPC Egress Gateway MUST have at least one policy or selector"
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
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=10
	Replicas int32 `json:"replicas,omitempty"`
	// optional name prefix used to generate the workload
	// the workload name will be generated as <prefix><vpc-egress-gateway-name>
	// +kubebuilder:validation:Pattern=`^$|^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*[-\.]?$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="This field is immutable."
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
	// when specified, the IPs count must NOT be less than the replicas count
	// +listType=set
	InternalIPs []string `json:"internalIPs,omitempty"`
	// External IP addresses for the egress gateway
	// +listType=set
	ExternalIPs []string `json:"externalIPs,omitempty"`
	// namespace/pod selectors
	Selectors []VpcEgressGatewaySelector `json:"selectors,omitempty"`
	// optional traffic policy used to control the traffic routing
	// if not specified, the default traffic policy "Cluster" will be used
	// if set to "Local", traffic will be routed to the gateway pod/instance on the same node when available
	// currently it works only for the default vpc
	// +kubebuilder:default=Cluster
	// +kubebuilder:validation:Enum=Local;Cluster
	TrafficPolicy string `json:"trafficPolicy,omitempty"`
	// Pod anti-affinity mode for gateway workload replicas.
	// Required spreads replicas across nodes and is the default. Preferred allows
	// co-located replicas but does not provide node-level HA. Changing from
	// Preferred to Required only takes effect when pods are recreated.
	// +kubebuilder:default=Required
	// +kubebuilder:validation:Enum=Required;Preferred
	PodAntiAffinity string `json:"podAntiAffinity,omitempty"`

	// BFD configuration
	BFD VpcEgressGatewayBFDConfig `json:"bfd"`
	// egress policies
	// at least one policy or selector must be specified
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
	// +kubebuilder:validation:XValidation:rule="has(self.matchLabels) || has(self.matchExpressions)",message="Each namespace selector MUST have at least one matchLabels or matchExpressions"
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
	// +kubebuilder:validation:XValidation:rule="has(self.matchLabels) || has(self.matchExpressions)",message="Each pod selector MUST have at least one matchLabels or matchExpressions"
	PodSelector *metav1.LabelSelector `json:"podSelector,omitempty"`
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

// +kubebuilder:validation:XValidation:rule="has(self.ipBlocks) || has(self.subnets)",message="Each policy MUST have at least one ipBlocks or subnets"
type VpcEgressGatewayPolicy struct {
	// whether to enable SNAT/MASQUERADE for the egress traffic
	// +kubebuilder:default=false
	SNAT bool `json:"snat"`
	// CIDRs/subnets targeted by the egress traffic policy
	// +listType=set
	IPBlocks []string `json:"ipBlocks,omitempty"`
	// +listType=set
	Subnets []string `json:"subnets,omitempty"`
}

type VpcEgressGatewayNodeSelector struct {
	MatchLabels      map[string]string                `json:"matchLabels,omitempty"`
	MatchExpressions []corev1.NodeSelectorRequirement `json:"matchExpressions,omitempty"`
	MatchFields      []corev1.NodeSelectorRequirement `json:"matchFields,omitempty"`
}

type VpcEgressGatewayStatus struct {
	// used by the scale subresource
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=10
	Replicas int32 `json:"replicas,omitempty"`
	// Label selector for the egress gateway
	LabelSelector string `json:"labelSelector,omitempty"`

	// whether the egress gateway is ready
	// +kubebuilder:default=false
	Ready bool `json:"ready"`
	// Current phase of the egress gateway (Pending, Processing, or Completed)
	// +kubebuilder:default=Pending
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Pending;Processing;Completed
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
