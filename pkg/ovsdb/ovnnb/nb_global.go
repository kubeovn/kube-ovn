// Code generated by "libovsdb.modelgen"
// DO NOT EDIT.

package ovnnb

// NBGlobal defines an object in NB_Global table
type NBGlobal struct {
	UUID           string            `ovsdb:"_uuid"`
	Connections    []string          `ovsdb:"connections"`
	ExternalIDs    map[string]string `ovsdb:"external_ids"`
	HvCfg          int               `ovsdb:"hv_cfg"`
	HvCfgTimestamp int               `ovsdb:"hv_cfg_timestamp"`
	Ipsec          bool              `ovsdb:"ipsec"`
	Name           string            `ovsdb:"name"`
	NbCfg          int               `ovsdb:"nb_cfg"`
	NbCfgTimestamp int               `ovsdb:"nb_cfg_timestamp"`
	Options        map[string]string `ovsdb:"options"`
	SbCfg          int               `ovsdb:"sb_cfg"`
	SbCfgTimestamp int               `ovsdb:"sb_cfg_timestamp"`
	SSL            *string           `ovsdb:"ssl"`
}
