package v1

import (
	"encoding/json"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

type QoSPolicyBindingType string

const (
	QoSBindingTypeEIP   QoSPolicyBindingType = "EIP"
	QoSBindingTypeNatGw QoSPolicyBindingType = "NATGW"
)

type QoSPolicyRuleDirection string

const (
	QoSDirectionIngress QoSPolicyRuleDirection = "ingress"
	QoSDirectionEgress  QoSPolicyRuleDirection = "egress"
)

type QoSPolicyRuleMatchType string

const QoSMatchTypeIP QoSPolicyRuleMatchType = "ip"

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type QoSPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []QoSPolicy `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=qos-policies
// +kubebuilder:resource:scope="Cluster",shortName="qos",path="qos-policies"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Shared",type="string",JSONPath=".spec.shared"
// +kubebuilder:printcolumn:name="BindingType",type="string",JSONPath=".spec.bindingType"
type QoSPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   QoSPolicySpec   `json:"spec"`
	Status QoSPolicyStatus `json:"status"`
}
type QoSPolicySpec struct {
	// Bandwidth limit rules to apply
	BandwidthLimitRules QoSPolicyBandwidthLimitRules `json:"bandwidthLimitRules"`
	// Whether the QoS policy is shared across multiple pods
	Shared bool `json:"shared"`
	// Binding type (e.g., pod, namespace)
	BindingType QoSPolicyBindingType `json:"bindingType"`
}

// BandwidthLimitRule describes the rule of a bandwidth limit.
type QoSPolicyBandwidthLimitRule struct {
	// Rule name
	Name string `json:"name"`
	// Interface name
	Interface string `json:"interface,omitempty"`
	// Maximum rate in Mbps (e.g., 100 or 0.5 for 500Kbps)
	RateMax string `json:"rateMax,omitempty"`
	// Maximum burst in MB (e.g., 10 or 0.5 for 500KB)
	BurstMax string `json:"burstMax,omitempty"`
	// Rule priority
	Priority int `json:"priority,omitempty"`
	// Traffic direction (ingress/egress)
	Direction QoSPolicyRuleDirection `json:"direction,omitempty"`
	// Match type
	MatchType QoSPolicyRuleMatchType `json:"matchType,omitempty"`
	// Match value
	MatchValue string `json:"matchValue,omitempty"`
}

type QoSPolicyBandwidthLimitRules []QoSPolicyBandwidthLimitRule

func (s QoSPolicyBandwidthLimitRules) Strings() string {
	var resultNames []string
	for _, rule := range s {
		resultNames = append(resultNames, rule.Name)
	}
	return strings.Join(resultNames, ",")
}

type QoSPolicyStatus struct {
	// Active bandwidth limit rules
	BandwidthLimitRules QoSPolicyBandwidthLimitRules `json:"bandwidthLimitRules" patchStrategy:"merge"`
	// Whether the QoS policy is shared
	Shared bool `json:"shared" patchStrategy:"merge"`
	// Binding type of the QoS policy
	BindingType QoSPolicyBindingType `json:"bindingType"`

	// Conditions represents the latest state of the object
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

func (s *QoSPolicyStatus) Bytes() ([]byte, error) {
	bytes, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	newStr := fmt.Sprintf(`{"status": %s}`, string(bytes))
	klog.V(5).Info("status body", newStr)
	return []byte(newStr), nil
}
