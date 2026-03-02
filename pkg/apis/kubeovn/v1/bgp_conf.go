package v1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type BgpConfList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []BgpConf `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// +resourceName=bgp-confs
type BgpConf struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec BgpConfSpec `json:"spec"`
}

type BgpConfSpec struct {
	LocalASN      uint32          `json:"localASN"`
	PeerASN       uint32          `json:"peerASN"`
	RouterID      string          `json:"routerId,omitempty"`
	Neighbours    []string        `json:"neighbours"`
	Password      string          `json:"password,omitempty"`
	HoldTime      metav1.Duration `json:"holdTime,omitempty"`
	KeepaliveTime metav1.Duration `json:"keepaliveTime,omitempty"`
	ConnectTime   metav1.Duration `json:"connectTime,omitempty"`
	EbgpMultiHop  bool            `json:"ebgpMultiHop,omitempty"`

	GracefulRestart bool `json:"gracefulRestart,omitempty"`
}
