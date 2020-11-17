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
	"strconv"
	"strings"
)

// OvsFlow is a datapath flow.
type OvsFlow struct {
	EthType    string
	Statistics struct {
		Packets float64
		Bytes   float64
		Used    float64
	}
	Flags string
	Raw   string
}

// NewOvsFlowFromString retuns OvsFlow instance from an input string.
func NewOvsFlowFromString(line string) (*OvsFlow, error) {
	if line == "" {
		return nil, fmt.Errorf("empty input")
	}
	f := &OvsFlow{
		Raw: line,
	}
	for _, element := range strings.Split(line, ", ") {
		// First, parse out statistics and flags
		pairs := strings.Split(element, ":")
		if len(pairs) == 2 {
			switch pairs[0] {
			case "packets":
				if v, err := strconv.ParseFloat(pairs[1], 64); err == nil {
					f.Statistics.Packets = v
				} else {
					return f, fmt.Errorf("failed to parse %s", element)
				}
				continue
			case "bytes":
				if v, err := strconv.ParseFloat(pairs[1], 64); err == nil {
					f.Statistics.Bytes = v
				} else {
					return f, fmt.Errorf("failed to parse %s", element)
				}
				continue
			case "used":
				t, err := parseTimeUsed(pairs[1])
				if err != nil {
					return f, fmt.Errorf("failed to parse %s", element)
				}
				f.Statistics.Used = t
				continue
			case "flags":
				f.Flags = pairs[1]
				continue
			}
		}
		// Next, parse out actions
		// TODO:
		// Then, parse out matching conditions
		// TODO:
		//spew.Dump(element)
	}
	//spew.Dump(*f)
	return f, nil
}

// GetOvsFlows returns a list of datapath flows of an OVS instance.
func (cli *OvsClient) GetOvsFlows() ([]*OvsFlow, error) {
	cli.updateRefs()
	db := "vswitchd-service"
	cmd := "dpctl/dump-flows"
	flows := []*OvsFlow{}
	app, err := NewClient(cli.Service.Vswitchd.Socket.Control, cli.Timeout)
	if err != nil {
		app.Close()
		return flows, fmt.Errorf("failed '%s' from %s: %s", cmd, db, err)
	}
	r, err := app.query(cmd, nil)
	if err != nil {
		app.Close()
		return flows, fmt.Errorf("the '%s' command failed for %s: %s", cmd, db, err)
	}
	app.Close()
	response := r.String()
	if response == "" {
		return flows, fmt.Errorf("the '%s' command return no data for %s", cmd, db)
	}
	lines := strings.Split(strings.Trim(response, "\""), "\\n")
	// Analyze the output
	for _, line := range lines {
		if line == "" {
			continue
		}
		f, err := NewOvsFlowFromString(line)
		if err != nil {
			return flows, fmt.Errorf("the '%s' command returned data for %s, but erred: %s", cmd, db, err)
		}
		flows = append(flows, f)
	}
	return flows, nil
}
