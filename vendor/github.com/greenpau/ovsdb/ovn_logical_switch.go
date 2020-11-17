// Copyright 2018 Paul Greenberg (greenpau@outlook.com)
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ovsdb

import (
	"fmt"
	//"github.com/davecgh/go-spew/spew"
)

// OvnLogicalSwitch holds basic information about a logical switch.
type OvnLogicalSwitch struct {
	UUID        string `json:"uuid" yaml:"uuid"`
	Name        string `json:"name" yaml:"name"`
	TunnelKey   uint64 `json:"tunnel_key" yaml:"tunnel_key"`
	DatapathID  string
	ExternalIDs map[string]string
	Ports       []string `json:"ports" yaml:"ports"`
}

// GetLogicalSwitches returns a list of OVN logical switches.
func (cli *OvnClient) GetLogicalSwitches() ([]*OvnLogicalSwitch, error) {
	switches := []*OvnLogicalSwitch{}
	// First, get basic information about OVN logical switches.
	query := "SELECT _uuid, external_ids, name, ports FROM Logical_Switch"
	result, err := cli.Database.Northbound.Client.Transact(cli.Database.Northbound.Name, query)
	if err != nil {
		return nil, fmt.Errorf("%s: '%s' table error: %s", cli.Database.Northbound.Name, "Logical_Switch", err)
	}
	if len(result.Rows) == 0 {
		return nil, fmt.Errorf("%s: no switch found", cli.Database.Northbound.Name)
	}
	for _, row := range result.Rows {
		sw := &OvnLogicalSwitch{}
		if r, dt, err := row.GetColumnValue("_uuid", result.Columns); err != nil {
			continue
		} else {
			if dt != "string" {
				continue
			}
			sw.UUID = r.(string)
		}
		if r, dt, err := row.GetColumnValue("name", result.Columns); err != nil {
			continue
		} else {
			if dt != "string" {
				continue
			}
			sw.Name = r.(string)
		}
		if r, dt, err := row.GetColumnValue("ports", result.Columns); err != nil {
			continue
		} else {
			switch dt {
			case "string":
				sw.Ports = append(sw.Ports, r.(string))
			case "[]string":
				sw.Ports = r.([]string)
			default:
				continue
			}
		}
		if r, dt, err := row.GetColumnValue("external_ids", result.Columns); err != nil {
			sw.ExternalIDs = make(map[string]string)
		} else {
			if dt == "map[string]string" {
				sw.ExternalIDs = r.(map[string]string)
			}
		}
		switches = append(switches, sw)
	}

	// Next, obtain a tunnel key for the datapath associated with the switch.
	query = "SELECT _uuid, external_ids, tunnel_key FROM Datapath_Binding"
	result, err = cli.Database.Southbound.Client.Transact(cli.Database.Southbound.Name, query)
	if err != nil {
		return nil, fmt.Errorf("%s: '%s' table error: %s", cli.Database.Southbound.Name, "Datapath_Binding", err)
	}
	if len(result.Rows) == 0 {
		return nil, fmt.Errorf("%s: no datapath binding found", cli.Database.Southbound.Name)
	}
	for _, row := range result.Rows {
		var bindUUID string
		var bindExternalIDs map[string]string
		var bindTunnelKey uint64
		if r, dt, err := row.GetColumnValue("_uuid", result.Columns); err != nil {
			continue
		} else {
			if dt != "string" {
				continue
			}
			bindUUID = r.(string)
		}
		if r, dt, err := row.GetColumnValue("tunnel_key", result.Columns); err != nil {
			continue
		} else {
			if dt != "integer" {
				continue
			}
			bindTunnelKey = uint64(r.(int64))
		}
		if r, dt, err := row.GetColumnValue("external_ids", result.Columns); err != nil {
			continue
		} else {
			if dt != "map[string]string" {
				continue
			}
			bindExternalIDs = r.(map[string]string)
		}
		if len(bindExternalIDs) < 1 {
			continue
		}
		if _, exists := bindExternalIDs["logical-switch"]; !exists {
			continue
		}
		for _, sw := range switches {
			if bindExternalIDs["logical-switch"] == sw.UUID {
				sw.TunnelKey = bindTunnelKey
				sw.DatapathID = bindUUID
				break
			}
		}
	}
	return switches, nil
}

// MapPortToSwitch update logical switch ports with the entries from the
// logical switches associated with the ports.
func (cli *OvnClient) MapPortToSwitch(logicalSwitches []*OvnLogicalSwitch, logicalSwitchPorts []*OvnLogicalSwitchPort) {
	portRef := make(map[string]string)
	portMap := make(map[string]*OvnLogicalSwitch)
	for _, logicalSwitch := range logicalSwitches {
		for _, port := range logicalSwitch.Ports {
			portRef[port] = logicalSwitch.UUID
			portMap[port] = logicalSwitch
		}
	}
	for _, logicalSwitchPort := range logicalSwitchPorts {
		if _, exists := portRef[logicalSwitchPort.UUID]; !exists {
			continue
		}
		logicalSwitchPort.LogicalSwitchUUID = portMap[logicalSwitchPort.UUID].UUID
		logicalSwitchPort.LogicalSwitchName = portMap[logicalSwitchPort.UUID].Name
		for k, v := range portMap[logicalSwitchPort.UUID].ExternalIDs {
			logicalSwitchPort.ExternalIDs[k] = v
		}
	}
}
