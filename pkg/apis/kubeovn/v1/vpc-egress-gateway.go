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
	// +kubebuilder:validation:XValidation:rule="type(self) == int ? self >= 0 : true",message="bandwidth must not be negative"
	Ingress *BandwidthRate `json:"ingress,omitempty"`
	// egress bandwidth limit, specified as an integer in Mbps or a Kubernetes quantity such as 100M or 1Gi in bits per second
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:validation:XIntOrString
	// +kubebuilder:validation:Pattern=`^([0-9]+|([0-9]+(\.[0-9]+)?|\.[0-9]+)(M|Mi|G|Gi))$`
	// +kubebuilder:validation:XValidation:rule="type(self) == int ? self >= 0 : true",message="bandwidth must not be negative"
	Egress *BandwidthRate `json:"egress,omitempty"`
}

const (
	bandwidthRatePattern = `^([0-9]+|([0-9]+(\.[0-9]+)?|\.[0-9]+)(M|Mi|G|Gi))$`
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
	maxQuantity := resource.NewScaledQuantity(math.MaxInt64, resource.Mega)
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

type VpcEgressGatewaySpec struct {
	// optional VPC name
	// if not specified, the default VPC will be used
	VPC string `json:"vpc,omitempty"`
	// workload replicas
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
	ExternalSubnet string `json:"externalSubnet"`
	// optional internal/external IPs used to create the workload
	// these IPs must be in the internal/external subnet
	// the IPs count must NOT be less than the replicas count
	InternalIPs []string `json:"internalIPs,omitempty"`
	ExternalIPs []string `json:"externalIPs,omitempty"`
	// namespace/pod selectors
	Selectors []VpcEgressGatewaySelector `json:"selectors,omitempty"`
	// optional traffic policy used to control the traffic routing
	// if not specified, the default traffic policy "Cluster" will be used
	// if set to "Local", traffic will be routed to the gateway pod/instance on the same node when available
	// currently it works only for the default vpc
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
	Enabled bool `json:"enabled"`
	// optional BFD minRX/minTX/multiplier
	MinRX      int32 `json:"minRX,omitempty"`
	MinTX      int32 `json:"minTX,omitempty"`
	Multiplier int32 `json:"multiplier,omitempty"`
}

type VpcEgressGatewayPolicy struct {
	// whether to enable SNAT/MASQUERADE for the egress traffic
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
	Replicas      int32  `json:"replicas,omitempty"`
	LabelSelector string `json:"labelSelector,omitempty"`

	// whether the egress gateway is ready
	Ready bool  `json:"ready"`
	Phase Phase `json:"phase"`
	// internal/external IPs used by the workload
	InternalIPs []string   `json:"internalIPs,omitempty"`
	ExternalIPs []string   `json:"externalIPs,omitempty"`
	Conditions  Conditions `json:"conditions,omitempty"`

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
