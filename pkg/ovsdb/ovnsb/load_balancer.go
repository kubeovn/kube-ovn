// Code generated by "libovsdb.modelgen"
// DO NOT EDIT.

package ovnsb

const LoadBalancerTable = "Load_Balancer"

type (
	LoadBalancerProtocol = string
)

var (
	LoadBalancerProtocolTCP  LoadBalancerProtocol = "tcp"
	LoadBalancerProtocolUDP  LoadBalancerProtocol = "udp"
	LoadBalancerProtocolSCTP LoadBalancerProtocol = "sctp"
)

// LoadBalancer defines an object in Load_Balancer table
type LoadBalancer struct {
	UUID            string                `ovsdb:"_uuid"`
	DatapathGroup   *string               `ovsdb:"datapath_group"`
	Datapaths       []string              `ovsdb:"datapaths"`
	ExternalIDs     map[string]string     `ovsdb:"external_ids"`
	LrDatapathGroup *string               `ovsdb:"lr_datapath_group"`
	LsDatapathGroup *string               `ovsdb:"ls_datapath_group"`
	Name            string                `ovsdb:"name"`
	Options         map[string]string     `ovsdb:"options"`
	Protocol        *LoadBalancerProtocol `ovsdb:"protocol"`
	Vips            map[string]string     `ovsdb:"vips"`
}
