// SPDX-License-Identifier: Apache-2.0
package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type BgpEdgeRouterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []BgpEdgeRouter `json:"items"`
}

// +genclient
// +genclient:method=GetScale,verb=get,subresource=scale,result=k8s.io/api/autoscaling/v1.Scale
// +genclient:method=UpdateScale,verb=update,subresource=scale,input=k8s.io/api/autoscaling/v1.Scale,result=k8s.io/api/autoscaling/v1.Scale
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +resourceName=bgp-edge-routers
// bgp edge router is used to forward the egress traffic from the VPC to the external network
type BgpEdgeRouter struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   BgpEdgeRouterSpec   `json:"spec"`
	Status BgpEdgeRouterStatus `json:"status"`
}

// VPC returns the VPC name
// If the BgpEdgeRouter has no VPC specified in the spec, it will return the default VPC name
func (g *BgpEdgeRouter) VPC(defaultVPC string) string {
	if g.Spec.VPC != "" {
		return g.Spec.VPC
	}
	return defaultVPC
}

// Ready returns true if the BgpEdgeRouter has been processed successfully and is ready to serve traffic
func (g *BgpEdgeRouter) Ready() bool {
	return g.Status.Ready && g.Status.Conditions.IsReady(g.Generation)
}

type BgpEdgeRouterSpec struct {
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
	// optional traffic policy used to control the traffic routing
	// if not specified, the default traffic policy "Cluster" will be used
	// if set to "Local", traffic will be routed to the gateway pod/instance on the same node when available
	// currently it works only for the default vpc
	TrafficPolicy string `json:"trafficPolicy,omitempty"`

	// BFD configuration
	BFD BgpEdgeRouterBFDConfig `json:"bfd"`
	// egress policies
	// at least one policy must be specified
	Policies []BgpEdgeRouterPolicy `json:"policies,omitempty"`
	// optional node selector used to select the nodes where the workload will be running
	NodeSelector []BgpEdgeRouterNodeSelector `json:"nodeSelector,omitempty"`

	// Add BGP configuration
	BGP BgpEdgeRouterBGPConfig `json:"bgp"`

	// TODO, subnet to access kube-apiserver. this will be used to reconcile routes to vpc
	// KubeApiSubnet string `json:"kubeApiSubnet,omitempty"`
}

type BgpEdgeRouterBFDConfig struct {
	// whether to enable BFD
	// if set to true, the egress gateway will establish BFD session(s) with the VPC BFD LRP
	// the VPC's .spec.bfd.enabled must be set to true to enable BFD
	Enabled bool `json:"enabled"`
	// optional BFD minRX/minTX/multiplier
	MinRX      int32 `json:"minRX,omitempty"`
	MinTX      int32 `json:"minTX,omitempty"`
	Multiplier int32 `json:"multiplier,omitempty"`
}

type BgpEdgeRouterPolicy struct {
	// whether to enable SNAT/MASQUERADE for the egress traffic
	SNAT bool `json:"snat"`
	// CIDRs/subnets targeted by the egress traffic policy
	IPBlocks []string `json:"ipBlocks,omitempty"`
	Subnets  []string `json:"subnets,omitempty"`
}

type BgpEdgeRouterNodeSelector struct {
	MatchLabels      map[string]string                `json:"matchLabels,omitempty"`
	MatchExpressions []corev1.NodeSelectorRequirement `json:"matchExpressions,omitempty"`
	MatchFields      []corev1.NodeSelectorRequirement `json:"matchFields,omitempty"`
}

type BgpEdgeRouterStatus struct {
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
	Workload BgpEdgeRouterWorkload `json:"workload"`
}

type BgpEdgeRouterWorkload struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Name       string `json:"name,omitempty"`
	// nodes where the workload is running
	Nodes []string `json:"nodes,omitempty"`
}

type BgpEdgeRouterBGPConfig struct {
	// whether to enable BGP for the egress gateway
	Enabled bool `json:"enabled"`
	// optional bgp image used by the workload
	// if not specified, the default image passed in by kube-ovn-controller will be used
	Image                 string          `json:"image,omitempty"`
	ASN                   uint32          `json:"localAsn"`
	RemoteASN             uint32          `json:"remoteAsn"`
	Neighbors             []string        `json:"neighbors"`
	HoldTime              metav1.Duration `json:"holdTime"`
	RouterID              string          `json:"routerId"`
	Password              string          `json:"password"`
	EnableGracefulRestart bool            `json:"enableGracefulRestart"`
	ExtraArgs             []string        `json:"extraArgs"`
	EdgeRouterMode        bool            `json:"edgeRouterMode"`
	RouteServerClient     bool            `json:"routeServerClient"`
}
