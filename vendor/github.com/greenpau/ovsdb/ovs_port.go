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

// OvsPort represents an OVS bridge. The data help by the data
// structure is the same as the output of `ovs-vsctl list Port`
// command.
type OvsPort struct {
	UUID            string
	Name            string
	BondActiveSlave []string // TODO: unverified data type
	BondDowndelay   float64
	BondFakeIface   bool
	BondMode        []string // TODO: unverified data type
	BondUpdelay     float64
	Cvlans          []string // TODO: unverified data type
	ExternalIDs     map[string]string
	FakeBridge      bool
	Interfaces      []string
	Lacp            []string          // TODO: unverified data type
	Mac             []string          // TODO: unverified data type
	OtherConfig     map[string]string // TODO: unverified data type
	Protected       bool
	Qos             []string           // TODO: unverified data type
	RstpStatistics  map[string]float64 // TODO: unverified data type
	RstpStatus      map[string]string  // TODO: unverified data type
	Statistics      map[string]float64 // TODO: unverified data type
	Status          map[string]string  // TODO: unverified data type
	Tag             []string           // TODO: unverified data type
	Trunks          []string           // TODO: unverified data type
	VlanMode        []string           // TODO: unverified data type
}
