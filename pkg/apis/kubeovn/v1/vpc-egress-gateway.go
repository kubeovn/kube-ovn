package v1

import (
	"bytes"
	"encoding/json"
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

	ObservabilityConfigured ConditionType = "ObservabilityConfigured"
	ServiceMonitorReady     ConditionType = "ServiceMonitorReady"

	ObservabilityEventStart = "start"
	ObservabilityEventEnd   = "end"

	ObservabilityProtocolTCP    = "tcp"
	ObservabilityProtocolUDP    = "udp"
	ObservabilityProtocolSCTP   = "sctp"
	ObservabilityProtocolICMP   = "icmp"
	ObservabilityProtocolICMPv6 = "icmpv6"
	ObservabilityProtocolOther  = "other"

	ObservabilityAddressFamilyIPv4 = "ipv4"
	ObservabilityAddressFamilyIPv6 = "ipv6"

	ObservabilityNatTypeSNAT     = "snat"
	ObservabilityNatTypeDNAT     = "dnat"
	ObservabilityNatTypeSNATDNAT = "snat_dnat"
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
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:validation:XIntOrString
	// +kubebuilder:validation:Pattern=`^([0-9]+|([0-9]+(\.[0-9]+)?|\.[0-9]+)(M|Mi|G|Gi))$`
	// +kubebuilder:validation:XValidation:rule="type(self) == int ? self >= 0 && self <= 9223372036854 : true",message="integer bandwidth must be between 0 and 9223372036854 Mbps"
	Ingress *BandwidthRate `json:"ingress,omitempty"`
	// egress bandwidth limit, specified as an integer in Mbps or a Kubernetes quantity such as 100M or 1Gi in bits per second
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:validation:XIntOrString
	// +kubebuilder:validation:Pattern=`^([0-9]+|([0-9]+(\.[0-9]+)?|\.[0-9]+)(M|Mi|G|Gi))$`
	// +kubebuilder:validation:XValidation:rule="type(self) == int ? self >= 0 && self <= 9223372036854 : true",message="integer bandwidth must be between 0 and 9223372036854 Mbps"
	Egress *BandwidthRate `json:"egress,omitempty"`
}

const (
	bandwidthRatePattern = `^([0-9]+|([0-9]+(\.[0-9]+)?|\.[0-9]+)(M|Mi|G|Gi))$`
	// MaxBandwidthMbps is the largest whole Mbps value that can be converted to
	// the signed int64 bits-per-second value used by OVS without overflowing.
	MaxBandwidthMbps int64 = math.MaxInt64 / 1_000_000
)

var bandwidthRateRegexp = regexp.MustCompile(bandwidthRatePattern)

// BandwidthRate holds either an int64 Mbps value or a Kubernetes quantity string.
type BandwidthRate struct {
	Type   intstr.Type `json:"-"`
	IntVal int64       `json:"-"`
	StrVal string      `json:"-"`
}

// BandwidthRateFromInt64 returns a BandwidthRate containing an integer Mbps value.
func BandwidthRateFromInt64(value int64) *BandwidthRate {
	return &BandwidthRate{Type: intstr.Int, IntVal: value}
}

// BandwidthRateFromString returns a BandwidthRate containing a quantity string.
func BandwidthRateFromString(value string) *BandwidthRate {
	return &BandwidthRate{Type: intstr.String, StrVal: value}
}

// UnmarshalJSON implements json.Unmarshaller.
func (rate *BandwidthRate) UnmarshalJSON(value []byte) error {
	value = bytes.TrimSpace(value)
	if len(value) == 0 {
		return errors.New("cannot unmarshal empty JSON as bandwidth rate")
	}
	if value[0] == '"' {
		var stringValue string
		if err := json.Unmarshal(value, &stringValue); err != nil {
			return err
		}
		*rate = *BandwidthRateFromString(stringValue)
		return nil
	}

	if (value[0] < '0' || value[0] > '9') && value[0] != '-' {
		return errors.New("bandwidth rate must be a JSON integer or string")
	}
	var integerValue int64
	if err := json.Unmarshal(value, &integerValue); err != nil {
		return fmt.Errorf("bandwidth rate must be a JSON integer or string: %w", err)
	}
	*rate = *BandwidthRateFromInt64(integerValue)
	return nil
}

// MarshalJSON implements json.Marshaller.
func (rate BandwidthRate) MarshalJSON() ([]byte, error) {
	switch rate.Type {
	case intstr.Int:
		return json.Marshal(rate.IntVal)
	case intstr.String:
		return json.Marshal(rate.StrVal)
	default:
		return nil, fmt.Errorf("unsupported bandwidth rate type %d", rate.Type)
	}
}

// Mbps returns the rate normalized to whole Mbps.
func (rate *BandwidthRate) Mbps() (int64, error) {
	if rate == nil {
		return 0, nil
	}
	if rate.Type == intstr.Int {
		if rate.IntVal < 0 {
			return 0, errors.New("bandwidth must not be negative")
		}
		if rate.IntVal > MaxBandwidthMbps {
			return 0, fmt.Errorf("bandwidth %d exceeds the supported maximum of %d Mbps", rate.IntVal, MaxBandwidthMbps)
		}
		return rate.IntVal, nil
	}
	if rate.Type != intstr.String {
		return 0, fmt.Errorf("unsupported bandwidth rate type %d", rate.Type)
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
	if value[0] >= '0' && value[0] <= '9' && !strings.ContainsAny(value, ".MGi") {
		mbps, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("bandwidth %q exceeds the supported maximum of %d Mbps", value, MaxBandwidthMbps)
		}
		if mbps > MaxBandwidthMbps {
			return 0, fmt.Errorf("bandwidth %q exceeds the supported maximum of %d Mbps", value, MaxBandwidthMbps)
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
	maxQuantity := resource.NewScaledQuantity(MaxBandwidthMbps, resource.Mega)
	if quantity.Cmp(*maxQuantity) > 0 {
		return 0, fmt.Errorf("bandwidth %q exceeds the supported maximum of %d Mbps", value, MaxBandwidthMbps)
	}
	return quantity.ScaledValue(resource.Mega), nil
}

// Mbps returns the ingress and egress limits normalized to whole Mbps.
// Positive quantities below a whole Mbps are rounded up so they do not silently disable the limit.
func (b *BandwidthLimit) Mbps() (int64, int64, error) {
	if b == nil {
		return 0, 0, nil
	}
	ingress, err := b.Ingress.Mbps()
	if err != nil {
		return 0, 0, fmt.Errorf("invalid ingress bandwidth: %w", err)
	}
	egress, err := b.Egress.Mbps()
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

	// Optional observability configuration for the gateway workload.
	Observability *VpcEgressGatewayObservability `json:"observability,omitempty"`
}

// VpcEgressGatewayObservability configures the native observability sidecar.
type VpcEgressGatewayObservability struct {
	// Compute resources required by the observability sidecar.
	Resources        corev1.ResourceRequirements            `json:"resources,omitempty"`
	InterfaceMetrics VpcEgressGatewayObservabilityFeature   `json:"interfaceMetrics,omitempty"`
	Conntrack        VpcEgressGatewayConntrackObservability `json:"conntrack,omitempty"`
	ServiceMonitor   VpcEgressGatewayServiceMonitor         `json:"serviceMonitor,omitempty"`
}

// VpcEgressGatewayObservabilityFeature enables an observability collector.
type VpcEgressGatewayObservabilityFeature struct {
	Enabled bool `json:"enabled,omitempty"`
}

// VpcEgressGatewayConntrackObservability configures conntrack metrics and flow logs.
type VpcEgressGatewayConntrackObservability struct {
	Metrics VpcEgressGatewayObservabilityFeature `json:"metrics,omitempty"`
	Log     VpcEgressGatewayConntrackLog         `json:"log,omitempty"`
}

// VpcEgressGatewayConntrackLog configures JSON Lines flow logging to stdout.
type VpcEgressGatewayConntrackLog struct {
	Enabled bool `json:"enabled,omitempty"`
	// flow lifecycle events to log; start and end are used when omitted
	// +listType=set
	// +kubebuilder:validation:MaxItems=2
	// +kubebuilder:validation:items:Enum=start;end
	Events    []string                              `json:"events,omitempty"`
	RateLimit VpcEgressGatewayConntrackLogRateLimit `json:"rateLimit,omitempty"`
	Filters   VpcEgressGatewayConntrackLogFilters   `json:"filters,omitempty"`
}

// VpcEgressGatewayConntrackLogFilters selects flow records. Exclude rules take precedence.
type VpcEgressGatewayConntrackLogFilters struct {
	// +kubebuilder:validation:MaxItems=64
	Include []VpcEgressGatewayConntrackLogFilter `json:"include,omitempty"`
	// +kubebuilder:validation:MaxItems=64
	Exclude []VpcEgressGatewayConntrackLogFilter `json:"exclude,omitempty"`
}

// VpcEgressGatewayConntrackLogFilter matches all configured fields in a rule.
type VpcEgressGatewayConntrackLogFilter struct {
	// +listType=set
	// +kubebuilder:validation:MaxItems=2
	// +kubebuilder:validation:items:Enum=ipv4;ipv6
	AddressFamilies []string `json:"addressFamilies,omitempty"`
	// +listType=set
	// +kubebuilder:validation:MaxItems=6
	// +kubebuilder:validation:items:Enum=tcp;udp;sctp;icmp;icmpv6;other
	Protocols []string `json:"protocols,omitempty"`
	// +listType=set
	// +kubebuilder:validation:MaxItems=3
	// +kubebuilder:validation:items:Enum=snat;dnat;snat_dnat
	NatTypes   []string                             `json:"natTypes,omitempty"`
	Original   VpcEgressGatewayConntrackTupleFilter `json:"original,omitempty"`
	Translated VpcEgressGatewayConntrackTupleFilter `json:"translated,omitempty"`
}

// VpcEgressGatewayConntrackTupleFilter matches tuple addresses and ports.
type VpcEgressGatewayConntrackTupleFilter struct {
	// +listType=set
	// +kubebuilder:validation:MaxItems=64
	SourceCIDRs []string `json:"sourceCIDRs,omitempty"`
	// +listType=set
	// +kubebuilder:validation:MaxItems=64
	DestinationCIDRs []string `json:"destinationCIDRs,omitempty"`
	// +kubebuilder:validation:MaxItems=64
	SourcePorts []VpcEgressGatewayPortRange `json:"sourcePorts,omitempty"`
	// +kubebuilder:validation:MaxItems=64
	DestinationPorts []VpcEgressGatewayPortRange `json:"destinationPorts,omitempty"`
}

// VpcEgressGatewayPortRange is an inclusive transport port range.
// +kubebuilder:validation:XValidation:rule="self.start <= self.end",message="start must not be greater than end"
type VpcEgressGatewayPortRange struct {
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=65535
	Start int32 `json:"start"`
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=65535
	End int32 `json:"end"`
}

// VpcEgressGatewayConntrackLogRateLimit limits flow log records per gateway pod.
type VpcEgressGatewayConntrackLogRateLimit struct {
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100000
	RecordsPerSecond int32 `json:"recordsPerSecond,omitempty"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000000
	Burst int32 `json:"burst,omitempty"`
}

// VpcEgressGatewayServiceMonitor configures metadata for the per-gateway ServiceMonitor.
type VpcEgressGatewayServiceMonitor struct {
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
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
