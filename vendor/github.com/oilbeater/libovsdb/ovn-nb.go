package libovsdb

import (
	"encoding/json"
)

type LogicalSwitch struct {
	Version     UUID   `json:"version,omitempty" structs:"-"`
	UUID        UUID   `json:"uuid,omitempty" structs:"-"`
	Name        string `json:"name" structs:"name,omitempty"`
	Ports       []UUID `json:"ports,omitempty" structs:"ports,omitempty"`
	OtherConfig OvsMap `json:"other_config,omitempty" structs:"other_config,omitempty,omitnested"`
}

func (ls *LogicalSwitch) UnmarshalJSON(b []byte) (err error) {
	var ols map[string]interface{}
	if err := json.Unmarshal(b, &ols); err == nil {
		ls.Name = ols["name"].(string)
		ls.Version.GoUUID = ols["_version"].([]interface{})[1].(string)
		ls.UUID.GoUUID = ols["_uuid"].([]interface{})[1].(string)

		psType := ols["ports"].([]interface{})[0].(string)
		switch psType {
		case "set":
			ls.Ports = []UUID{}
		case "uuid":
			ls.Ports = make([]UUID, 0, len(ols["ports"].([]interface{}))-1)
			for i, p := range ols["ports"].([]interface{}) {
				if i == 0 {
					continue
				}
				ls.Ports = append(ls.Ports, UUID{p.(string)})
			}
		}
	}
	return err
}

type LogicalSwitchPort struct {
	UUID        UUID   `json:"_uuid,omitempty" structs:"-"`
	Name        string `json:"name" structs:"name,omitempty"`
	DynamicAddresses string `json:"dynamic_addresses" structs:"dynamic_addresses,omitempty"`
}

func (lsp *LogicalSwitchPort) UnmarshalJSON(b []byte) (err error) {
	var ols map[string]interface{}
	if err := json.Unmarshal(b, &ols); err == nil {
		lsp.UUID.GoUUID = ols["_uuid"].([]interface{})[1].(string)
		lsp.Name = ols["name"].(string)
		lsp.DynamicAddresses = ols["dynamic_addresses"].(string)
	}
	return err
}