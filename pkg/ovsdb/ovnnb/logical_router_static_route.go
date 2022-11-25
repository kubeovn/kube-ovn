// Code generated by "libovsdb.modelgen"
// DO NOT EDIT.

package ovnnb

const LogicalRouterStaticRouteTable = "Logical_Router_Static_Route"

type (
	LogicalRouterStaticRoutePolicy = string
)

var (
	LogicalRouterStaticRoutePolicyDstIP LogicalRouterStaticRoutePolicy = "dst-ip"
	LogicalRouterStaticRoutePolicySrcIP LogicalRouterStaticRoutePolicy = "src-ip"
)

// LogicalRouterStaticRoute defines an object in Logical_Router_Static_Route table
type LogicalRouterStaticRoute struct {
	UUID        string                          `ovsdb:"_uuid"`
	BFD         *string                         `ovsdb:"bfd"`
	ExternalIDs map[string]string               `ovsdb:"external_ids"`
	IPPrefix    string                          `ovsdb:"ip_prefix"`
	Nexthop     string                          `ovsdb:"nexthop"`
	Options     map[string]string               `ovsdb:"options"`
	OutputPort  *string                         `ovsdb:"output_port"`
	Policy      *LogicalRouterStaticRoutePolicy `ovsdb:"policy"`
	RouteTable  string                          `ovsdb:"route_table"`
}
