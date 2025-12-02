// SPDX-License-Identifier: Apache-2.0
package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type GobgpConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []GobgpConfig `json:"items"`
}

// +genclient
// +genclient:method=GetScale,verb=get,subresource=scale,result=k8s.io/api/autoscaling/v1.Scale
// +genclient:method=UpdateScale,verb=update,subresource=scale,input=k8s.io/api/autoscaling/v1.Scale,result=k8s.io/api/autoscaling/v1.Scale
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +resourceName=gobgp-configs
// bgp edge router advertisement is used to forward the egress traffic from the VPC to the external network
type GobgpConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   GobgpConfigSpec   `json:"spec"`
	Status GobgpConfigStatus `json:"status"`
}

// If the GobgpConfig has no VPC specified in the spec, it will return the default VPC name
func (g *GobgpConfig) Subnet(subnets []string) []string {
	if len(subnets) != 0 {
		return subnets
	}
	return nil
}

type GobgpConfigSpec struct {
	BgpEdgeRouter string      `json:"bgpEdgeRouter"`
	Neighbors     []Neighbors `json:"neighbors,omitempty"`
}

type GobgpConfigStatus struct {
	Ready      bool       `json:"ready"`
	Conditions Conditions `json:"conditions,omitempty"`
}

// Neighbors defines the BGP neighbors configuration
// +k8s:openapi-gen=true
// +genclient:nonNamespaced
type Neighbors struct {
	Address     string      `json:"address"`
	ToAdvertise ToAdvertise `json:"toAdvertise"`
	ToReceive   ToReceive   `json:"toReceive"`
}

type ToAdvertise struct {
	Allowed Allowed `json:"allowed"`
}

type ToReceive struct {
	Allowed Allowed `json:"allowed"`
}

type Allowed struct {
	Mode     string   `json:"mode,omitempty"`
	Prefixes []string `json:"prefixes,omitempty"`
}
