package ovnicsb

import "github.com/ovn-kubernetes/libovsdb/model"

// DatabaseName is the OVN IC southbound database name.
const DatabaseName = "OVN_IC_Southbound"

const (
	// AvailabilityZoneTable is the IC SB availability-zone table.
	AvailabilityZoneTable = "Availability_Zone"
	// GatewayTable is the IC SB gateway table.
	GatewayTable = "Gateway"
	// RouteTable is the IC SB route table.
	RouteTable = "Route"
	// PortBindingTable is the IC SB port-binding table.
	PortBindingTable = "Port_Binding"
)

// AvailabilityZone identifies an interconnection availability zone.
type AvailabilityZone struct {
	UUID    string `ovsdb:"_uuid"`
	Name    string `ovsdb:"name"`
	NbIcCfg int    `ovsdb:"nb_ic_cfg"`
}

// Gateway identifies an IC gateway and its availability zone.
type Gateway struct {
	UUID             string            `ovsdb:"_uuid"`
	Name             string            `ovsdb:"name"`
	AvailabilityZone string            `ovsdb:"availability_zone"`
	Hostname         string            `ovsdb:"hostname"`
	Encaps           []string          `ovsdb:"encaps"`
	ExternalIDs      map[string]string `ovsdb:"external_ids"`
}

// Route identifies a route advertised by an IC availability zone.
type Route struct {
	UUID             string            `ovsdb:"_uuid"`
	TransitSwitch    string            `ovsdb:"transit_switch"`
	AvailabilityZone string            `ovsdb:"availability_zone"`
	RouteTable       string            `ovsdb:"route_table"`
	IPPrefix         string            `ovsdb:"ip_prefix"`
	Nexthop          string            `ovsdb:"nexthop"`
	Origin           string            `ovsdb:"origin"`
	Options          map[string]string `ovsdb:"options"`
	ExternalIDs      map[string]string `ovsdb:"external_ids"`
}

// PortBinding identifies a logical port in the IC southbound database.
type PortBinding struct {
	UUID             string            `ovsdb:"_uuid"`
	LogicalPort      string            `ovsdb:"logical_port"`
	TransitSwitch    string            `ovsdb:"transit_switch"`
	AvailabilityZone string            `ovsdb:"availability_zone"`
	TunnelKey        int               `ovsdb:"tunnel_key"`
	Gateway          string            `ovsdb:"gateway"`
	Encap            *string           `ovsdb:"encap"`
	Address          string            `ovsdb:"address"`
	Type             string            `ovsdb:"type"`
	NbIcUUID         *string           `ovsdb:"nb_ic_uuid"`
	ExternalIDs      map[string]string `ovsdb:"external_ids"`
}

// FullDatabaseModel returns the IC SB tables used for availability-zone
// cleanup. The remaining IC SB tables are deliberately left unmonitored.
func FullDatabaseModel() (model.ClientDBModel, error) {
	return model.NewClientDBModel(DatabaseName, map[string]model.Model{
		AvailabilityZoneTable: &AvailabilityZone{},
		GatewayTable:          &Gateway{},
		RouteTable:            &Route{},
		PortBindingTable:      &PortBinding{},
	})
}
