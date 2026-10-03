package request

import (
	"fmt"
	"net/http"

	"github.com/containernetworking/cni/pkg/types"
	"github.com/parnurzeal/gorequest"
)

// CniServerClient is the client to visit cniserver
type CniServerClient struct {
	*gorequest.SuperAgent
}

// Route represents a requested route
type Route struct {
	Destination string `json:"dst,omitempty"`
	Gateway     string `json:"gw,omitempty"`
}

// IPConfig represents a single IP configuration (one per protocol)
type IPConfig struct {
	Protocol string `json:"protocol"`
	IP       string `json:"ip"`
	CIDR     string `json:"cidr"`
	Gateway  string `json:"gateway,omitempty"`
}

// CniRequest is the cniserver request format
type CniRequest struct {
	CniType      string    `json:"cni_type"`
	PodName      string    `json:"pod_name"`
	PodNamespace string    `json:"pod_namespace"`
	ContainerID  string    `json:"container_id"`
	NetNs        string    `json:"net_ns"`
	IfName       string    `json:"if_name"`
	Provider     string    `json:"provider"`
	Routes       []Route   `json:"routes"`
	DNS          types.DNS `json:"dns"`
	VfDriver     string    `json:"vf_driver"`
	// PciAddrs in case of using sriov
	DeviceID string `json:"deviceID"`
	// dpdk
	// empty dir volume for sharing vhost user unix socket
	VhostUserSocketVolumeName  string              `json:"vhost_user_socket_volume_name"`
	VhostUserSocketName        string              `json:"vhost_user_socket_name"`
	VhostUserSocketConsumption string              `json:"vhost_user_socket_consumption"`
	PrepareOnly                bool                `json:"prepare_only,omitempty"`
	Plan                       *CNIPlan            `json:"plan,omitempty"`
	Execution                  *CNIExecutionResult `json:"execution,omitempty"`
}

// CNIPlan contains the validated inputs needed by the privileged CNI executor.
// It deliberately contains network intent rather than shell commands or paths.
type CNIPlan struct {
	CniType                    string    `json:"cni_type"`
	PodName                    string    `json:"pod_name"`
	PodNamespace               string    `json:"pod_namespace"`
	ContainerID                string    `json:"container_id"`
	NetNs                      string    `json:"net_ns"`
	IfName                     string    `json:"if_name"`
	Provider                   string    `json:"provider"`
	Routes                     []Route   `json:"routes,omitempty"`
	DNS                        types.DNS `json:"dns"`
	VfDriver                   string    `json:"vf_driver,omitempty"`
	DeviceID                   string    `json:"device_id,omitempty"`
	VhostUserSocketVolumeName  string    `json:"vhost_user_socket_volume_name,omitempty"`
	VhostUserSocketName        string    `json:"vhost_user_socket_name,omitempty"`
	VhostUserSocketConsumption string    `json:"vhost_user_socket_consumption,omitempty"`
	ShortSharedDir             string    `json:"short_shared_dir,omitempty"`
	OriginSharedDir            string    `json:"origin_shared_dir,omitempty"`
	MacAddress                 string    `json:"mac_address,omitempty"`
	IP                         string    `json:"ip,omitempty"`
	IPAddr                     string    `json:"ip_addr,omitempty"`
	CIDR                       string    `json:"cidr,omitempty"`
	Gateway                    string    `json:"gateway,omitempty"`
	MTU                        int       `json:"mtu,omitempty"`
	IsDefaultRoute             bool      `json:"is_default_route,omitempty"`
	VMMigration                bool      `json:"vm_migration,omitempty"`
	RoutedSubnet               bool      `json:"routed_subnet,omitempty"`
	IPAMOnly                   bool      `json:"ipam_only,omitempty"`
	GatewayCheckMode           int       `json:"gateway_check_mode,omitempty"`
	U2OInterconnectionIP       string    `json:"u2o_interconnection_ip,omitempty"`
	OldPodName                 string    `json:"old_pod_name,omitempty"`
	EncapIP                    string    `json:"encap_ip,omitempty"`
	LocalnetSubnet             string    `json:"localnet_subnet,omitempty"`
	AppendIfName               bool      `json:"append_if_name,omitempty"`
	Ingress                    string    `json:"ingress,omitempty"`
	Egress                     string    `json:"egress,omitempty"`
	IngressBurst               string    `json:"ingress_burst,omitempty"`
	EgressBurst                string    `json:"egress_burst,omitempty"`
	Latency                    string    `json:"latency,omitempty"`
	Limit                      string    `json:"limit,omitempty"`
	Loss                       string    `json:"loss,omitempty"`
	Jitter                     string    `json:"jitter,omitempty"`
	NicType                    string    `json:"nic_type,omitempty"`
	Subnet                     string    `json:"subnet,omitempty"`
	MirrorEnabled              bool      `json:"mirror_enabled,omitempty"`
	MirrorControl              string    `json:"mirror_control,omitempty"`
	Delete                     bool      `json:"delete,omitempty"`
	EnableArpDetectIPConflict  bool      `json:"enable_arp_detect_ip_conflict,omitempty"`
}

// CNIExecutionResult reports facts observed after privileged execution.
type CNIExecutionResult struct {
	Routes           []Route `json:"routes,omitempty"`
	HostNicName      string  `json:"host_nic_name,omitempty"`
	ContainerNicName string  `json:"container_nic_name,omitempty"`
}

// CniResponse is the cniserver response format
type CniResponse struct {
	IPs        []IPConfig `json:"ips"`
	MacAddress string     `json:"mac_address"`
	Routes     []Route    `json:"routes"`
	Mtu        int        `json:"mtu"`
	PodNicName string     `json:"nicname"`
	DNS        types.DNS  `json:"dns"`
	Err        string     `json:"error"`
	Plan       *CNIPlan   `json:"plan,omitempty"`
}

// Add pod request
func (csc CniServerClient) Add(podRequest CniRequest) (*CniResponse, error) {
	resp := CniResponse{}
	res, _, errors := csc.Post("http://dummy/api/v1/add").Send(podRequest).EndStruct(&resp)
	if len(errors) != 0 {
		return nil, errors[0]
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request ip return %d %s", res.StatusCode, resp.Err)
	}
	return &resp, nil
}

// Del pod request
func (csc CniServerClient) Del(podRequest CniRequest) error {
	res, body, errors := csc.Post("http://dummy/api/v1/del").Send(podRequest).End()
	if len(errors) != 0 {
		return errors[0]
	}
	if res.StatusCode != http.StatusNoContent {
		return fmt.Errorf("delete ip return %d %s", res.StatusCode, body)
	}
	return nil
}

// PrepareDelete obtains a deletion plan without changing host networking.
func (csc CniServerClient) PrepareDelete(podRequest CniRequest) (*CniResponse, error) {
	response := CniResponse{}
	res, _, errors := csc.Post("http://dummy/api/v1/del").Send(podRequest).EndStruct(&response)
	if len(errors) != 0 {
		return nil, errors[0]
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prepare delete return %d %s", res.StatusCode, response.Err)
	}
	return &response, nil
}

// Commit reports successful local execution after a prepare-only request.
func (csc CniServerClient) Commit(podRequest CniRequest) error {
	res, body, errors := csc.Post("http://dummy/api/v1/commit").Send(podRequest).End()
	if len(errors) != 0 {
		return errors[0]
	}
	if res.StatusCode != http.StatusNoContent {
		return fmt.Errorf("commit ip return %d %s", res.StatusCode, body)
	}
	return nil
}
