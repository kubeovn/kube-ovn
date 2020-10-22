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

// OvsInterface represents an OVS interface. The data help by the data
// structure is the same as the output of `ovs-vsctl list Interface`
// command.
//
// Reference: http://www.openvswitch.org/support/dist-docs/ovs-vswitchd.conf.db.5.html
type OvsInterface struct {
	UUID                 string
	Name                 string
	Index                float64 // reference from ovs-appctl dpif/show, e.g. OVN `tunnel_key`
	BridgeName           string  // reference to datapath from ovs-appctl dpif/show
	DatapathName         string  // reference to datapath from ovs-appctl dpif/show
	AdminState           string
	Bfd                  map[string]string // TODO: unverified data type
	BfdStatus            map[string]string // TODO: unverified data type
	CfmFault             []string          // TODO: unverified data type
	CfmFaultStatus       []string          // TODO: unverified data type
	CfmFlapCount         []string          // TODO: unverified data type
	CfmHealth            []string          // TODO: unverified data type
	CfmMpid              []string          // TODO: unverified data type
	CfmRemoteMpids       []string          // TODO: unverified data type
	CfmRemoteOpState     []string          // TODO: unverified data type
	Duplex               string
	Error                []string // TODO: unverified data type
	ExternalIDs          map[string]string
	IfIndex              float64
	IngressPolicingBurst float64
	IngressPolicingRate  float64
	LacpCurrent          []string // TODO: unverified data type
	LinkResets           float64
	LinkSpeed            float64
	LinkState            string
	Lldp                 map[string]string // TODO: unverified data type
	Mac                  []string          // TODO: unverified data type
	MacInUse             string            // TODO: unverified data type
	Mtu                  float64
	MtuRequest           []string // TODO: unverified data type
	OfPort               float64
	OfPortRequest        []string // TODO: unverified data type
	Options              map[string]string
	OtherConfig          map[string]string // TODO: unverified data type
	Statistics           map[string]int
	Status               map[string]string
	Type                 string
}

// GetDbInterfaces returns a list of interfaces from the Interface table of OVS database.
func (cli *OvsClient) GetDbInterfaces() ([]*OvsInterface, error) {
	intfs := []*OvsInterface{}
	query := "SELECT * FROM Interface"
	result, err := cli.Database.Vswitch.Client.Transact(cli.Database.Vswitch.Name, query)
	if err != nil {
		return intfs, fmt.Errorf("The '%s' query failed: %s", query, err)
	}
	if len(result.Rows) == 0 {
		return intfs, fmt.Errorf("The '%s' query did not return any rows", query)
	}
	for _, row := range result.Rows {
		intf := &OvsInterface{}
		if r, dt, err := row.GetColumnValue("_uuid", result.Columns); err != nil {
			continue
		} else {
			if dt != "string" {
				continue
			}
			intf.UUID = r.(string)
		}

		if r, dt, err := row.GetColumnValue("name", result.Columns); err == nil {
			if dt == "string" {
				intf.Name = r.(string)
			}
		}

		if r, dt, err := row.GetColumnValue("external_ids", result.Columns); err == nil {
			if dt == "map[string]string" {
				intf.ExternalIDs = r.(map[string]string)
			}
		} else {
			intf.ExternalIDs = make(map[string]string)
		}

		if r, dt, err := row.GetColumnValue("ofport", result.Columns); err == nil {
			if dt == "integer" {
				intf.OfPort = float64(r.(int64))
			}
		}

		if r, dt, err := row.GetColumnValue("ifindex", result.Columns); err == nil {
			if dt == "integer" {
				intf.IfIndex = float64(r.(int64))
			}
		}

		if r, dt, err := row.GetColumnValue("mtu", result.Columns); err == nil {
			if dt == "integer" {
				intf.Mtu = float64(r.(int64))
			}
		}

		if r, dt, err := row.GetColumnValue("mac_in_use", result.Columns); err == nil {
			if dt == "string" {
				intf.MacInUse = r.(string)
			}
		}

		if r, dt, err := row.GetColumnValue("link_state", result.Columns); err == nil {
			if dt == "string" {
				intf.LinkState = r.(string)
			}
		}

		if r, dt, err := row.GetColumnValue("admin_state", result.Columns); err == nil {
			if dt == "string" {
				intf.AdminState = r.(string)
			}
		}

		if r, dt, err := row.GetColumnValue("ingress_policing_burst", result.Columns); err == nil {
			if dt == "integer" {
				intf.IngressPolicingBurst = float64(r.(int64))
			}
		}

		if r, dt, err := row.GetColumnValue("ingress_policing_rate", result.Columns); err == nil {
			if dt == "integer" {
				intf.IngressPolicingRate = float64(r.(int64))
			}
		}

		if r, dt, err := row.GetColumnValue("statistics", result.Columns); err == nil {
			if dt == "map[string]integer" {
				intf.Statistics = r.(map[string]int)
			}
		} else {
			intf.Statistics = make(map[string]int)
		}

		if r, dt, err := row.GetColumnValue("status", result.Columns); err == nil {
			if dt == "map[string]string" {
				intf.Status = r.(map[string]string)
			}
		} else {
			intf.Status = make(map[string]string)
		}

		if r, dt, err := row.GetColumnValue("options", result.Columns); err == nil {
			if dt == "map[string]string" {
				intf.Options = r.(map[string]string)
			}
		} else {
			intf.Options = make(map[string]string)
		}

		if r, dt, err := row.GetColumnValue("type", result.Columns); err == nil {
			if dt == "string" {
				intf.Type = r.(string)
			}
		}

		if r, dt, err := row.GetColumnValue("duplex", result.Columns); err == nil {
			if dt == "string" {
				intf.Duplex = r.(string)
			}
		}

		intfs = append(intfs, intf)
	}
	return intfs, nil

}
