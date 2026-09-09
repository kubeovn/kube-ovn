package ovnicnb

import (
	"github.com/ovn-kubernetes/libovsdb/model"
)

// DatabaseName is the OVN IC northbound database name.
const DatabaseName = "OVN_IC_Northbound"

// TransitSwitchTable is the IC northbound transit-switch table.
const TransitSwitchTable = "Transit_Switch"

// TransitSwitch is the IC northbound transit switch row used by the
// interconnection controller.
type TransitSwitch struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	Ports       []string          `ovsdb:"ports"`
	OtherConfig map[string]string `ovsdb:"other_config"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

// FullDatabaseModel returns the subset of the IC NB schema used by kube-ovn.
// A partial model is intentional: the controller only monitors Transit_Switch
// rows and does not need to own unrelated IC database tables.
func FullDatabaseModel() (model.ClientDBModel, error) {
	return model.NewClientDBModel(DatabaseName, map[string]model.Model{
		TransitSwitchTable: &TransitSwitch{},
	})
}
