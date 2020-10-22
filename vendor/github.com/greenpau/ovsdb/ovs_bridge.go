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

// OvsBridge represents an OVS bridge. The data help by the data
// structure is the same as the output of `ovs-vsctl list Bridge`
// command.
type OvsBridge struct {
	UUID                string
	Name                string
	AutoAttach          []string // TODO: unverified data type
	Controller          []string // TODO: unverified data type
	DatapathName        string   // reference from ovs-appctl dpif/show
	DatapathID          string
	DatapathType        string
	DatapathVersion     string
	ExternalIDs         map[string]string
	FailMode            string
	FloodVlans          []string          // TODO: unverified data type
	FlowTables          map[string]string // TODO: unverified data type
	Ipfix               []string          // TODO: unverified data type
	McastSnoopingEnable bool
	Mirrors             []string // TODO: unverified data type
	Netflow             []string // TODO: unverified data type
	OtherConfig         map[string]string
	Ports               []string
	Protocols           []string // TODO: unverified data type
	RstpEnable          bool
	RstpStatus          map[string]string // TODO: unverified data type
	Sflow               []string          // TODO: unverified data type
	Status              map[string]string // TODO: unverified data type
	StpEnable           bool
}
