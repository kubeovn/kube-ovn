// SPDX-License-Identifier: Apache-2.0
package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type BgpEdgeRouterAdvertisementList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []BgpEdgeRouterAdvertisement `json:"items"`
}

// +genclient
// +genclient:method=GetScale,verb=get,subresource=scale,result=k8s.io/api/autoscaling/v1.Scale
// +genclient:method=UpdateScale,verb=update,subresource=scale,input=k8s.io/api/autoscaling/v1.Scale,result=k8s.io/api/autoscaling/v1.Scale
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +resourceName=bgp-edge-router-advertisements
// bgp edge router advertisement is used to forward the egress traffic from the VPC to the external network
type BgpEdgeRouterAdvertisement struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   BgpEdgeRouterAdvertisementSpec   `json:"spec"`
	Status BgpEdgeRouterAdvertisementStatus `json:"status"`
}

// If the BgpEdgeRouter has no VPC specified in the spec, it will return the default VPC name
func (g *BgpEdgeRouterAdvertisement) Subnet(subnets []string) []string {
	if len(subnets) != 0 {
		return subnets
	}
	return nil
}

// Ready returns true if the BgpEdgeRouter has been processed successfully and is ready to serve traffic
// func (g *BgpEdgeRouterAdvertisement) Ready() bool {
// 	return g.Status.Ready && g.Status.Conditions.IsReady(g.Generation)
// }

type BgpEdgeRouterAdvertisementSpec struct {
	Subnet        []string `json:"subnet,omitempty"`
	BgpEdgeRouter string   `json:"bgpEdgeRouter,omitempty"`
}

type BgpEdgeRouterAdvertisementStatus struct {
	Ready      bool       `json:"ready"`
	Conditions Conditions `json:"conditions,omitempty"`
}
