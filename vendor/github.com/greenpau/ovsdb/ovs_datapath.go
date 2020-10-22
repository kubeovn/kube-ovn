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

// OvsDatapath represents an OVS datapath. A datapath is a collection
// of the ports attached to bridges. Each datapath also has associated
// with it a flow table that userspace populates with flows that map
// from keys based on packet headers and metadata to sets of actions.
// Importantly, a datapath is a userspace concept.
type OvsDatapath struct {
	Name    string
	Lookups struct {
		Hit    float64
		Missed float64
		Lost   float64
	}
	Flows float64
	Masks struct {
		Hit      float64
		Total    float64
		HitRatio float64
	}
}
