// Code generated by "libovsdb.modelgen"
// DO NOT EDIT.

package ovnnb

// HAChassis defines an object in HA_Chassis table
type HAChassis struct {
	UUID        string            `ovsdb:"_uuid"`
	ChassisName string            `ovsdb:"chassis_name"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
	Priority    int               `ovsdb:"priority"`
}
