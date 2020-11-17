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

// getAppDatapathInterfaces returns a summary of configured datapaths, including statistics
// and a list of connected ports. The port information includes the OpenFlow
// port number, datapath port number, and the type.
//
// Reference: http://www.openvswitch.org/support/dist-docs/ovs-vswitchd.8.txt
func getAppDatapathInterfaces(db string, sock string, timeout int) ([]*OvsDatapath, []*OvsBridge, []*OvsInterface, error) {
	var app Client
	var err error
	cmd := "dpif/show"
	dps := []*OvsDatapath{}
	brs := []*OvsBridge{}
	intfs := []*OvsInterface{}
	app, err = NewClient(sock, timeout)
	if err != nil {
		app.Close()
		return dps, brs, intfs, fmt.Errorf("failed '%s' from %s: %s", cmd, db, err)
	}
	r, err := app.query(cmd, nil)
	if err != nil {
		app.Close()
		return dps, brs, intfs, fmt.Errorf("the '%s' command failed for %s: %s", cmd, db, err)
	}
	app.Close()
	response := r.String()
	if response == "" {
		return dps, brs, intfs, fmt.Errorf("the '%s' command return no data for %s", cmd, db)
	}
	lines := strings.Split(strings.Trim(response, "\""), "\\n")
	indents := []int{}
	// First, evaluate output depth
	for _, line := range lines {
		indents = append(indents, indentAnalysis(line))
	}
	depth, err := indentDepthAnalysis(indents)
	if err != nil {
		return dps, brs, intfs, fmt.Errorf("the '%s' command return for %s failed output depth analysis", cmd, db)
	}
	// Second, analyze the output
	var dpn, brn, ifn string
	for _, line := range lines {
		indent := indentAnalysis(line)
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch depth[indent] {
		case 0:
			// Datapath
			dp := &OvsDatapath{}
			i := strings.Index(line, ":")
			dpn = line[:i]
			dp.Name = dpn
			if len(line) < (i + 2) {
				return dps, brs, intfs, fmt.Errorf("the '%s' command return for %s failed output analysis: datapath string", cmd, db)
			}
			dpAttrs := line[i+2:]
			for _, dpAttrKv := range strings.Split(dpAttrs, " ") {
				kv := strings.Split(dpAttrKv, ":")
				if len(kv) != 2 {
					continue
				}
				k := kv[0]
				v, err := strconv.ParseFloat(kv[1], 64)
				if err != nil {
					continue
				}
				switch k {
				case "hit":
					dp.Lookups.Hit = v
				case "missed":
					dp.Lookups.Missed = v
				default:
					return dps, brs, intfs, fmt.Errorf("the '%s' command return for %s failed output analysis: datapath attributes", cmd, db)
				}
			}
			dps = append(dps, dp)
		case 1:
			// Bridge
			brn = strings.TrimRight(strings.TrimSpace(line), ":")
			if brn == "" {
				return dps, brs, intfs, fmt.Errorf("the '%s' command return for %s failed output analysis: bridge string", cmd, db)
			}
			br := &OvsBridge{
				Name:         brn,
				DatapathName: dpn,
			}
			brs = append(brs, br)
		case 2:
			// Interface
			intf := &OvsInterface{
				BridgeName:   brn,
				DatapathName: dpn,
			}
			// Parse interface name
			i := strings.Index(line, " ")
			if i < 0 {
				return dps, brs, intfs, fmt.Errorf("the '%s' command return for %s failed output analysis: interface string", cmd, db)
			}
			ifn = line[:i]
			intf.Name = ifn
			line = line[i:]
			// Parse interface identifiers
			i = strings.Index(line, ":")
			if i < 0 {
				return dps, brs, intfs, fmt.Errorf("the '%s' command return for %s failed output analysis: interface %s", cmd, db, ifn)
			}
			ifids := strings.Split(line[:i], "/")
			if len(ifids) != 2 {
				return dps, brs, intfs, fmt.Errorf("the '%s' command return for %s failed output analysis: interface %s identifiers", cmd, db, ifn)
			}
			if v, err := strconv.ParseFloat(strings.TrimSpace(ifids[0]), 64); err == nil {
				intf.OfPort = v
			}
			if v, err := strconv.ParseFloat(strings.TrimSpace(ifids[1]), 64); err == nil {
				intf.Index = v
			}
			// Parse interface attributes
			i = strings.Index(line, "(")
			if i < 0 {
				return dps, brs, intfs, fmt.Errorf("the '%s' command return for %s failed output analysis: interface %s attributes", cmd, db, ifn)
			}
			line = strings.TrimLeft(strings.TrimRight(line[i:], ")"), "(")
			switch line {
			case "internal":
				intf.Type = "internal"
			case "system":
				intf.Type = "system"
			default:
				if strings.HasPrefix(line, "vxlan") {
					intf.Type = "vxlan"
				} else if strings.HasPrefix(line, "geneve") {
					intf.Type = "geneve"
				} else {
					intf.Type = "unknown"
				}
			}
			intfs = append(intfs, intf)
		default:
			return dps, brs, intfs, fmt.Errorf("the '%s' command return for %s failed output analysis", cmd, db)
		}
	}
	return dps, brs, intfs, nil
}

func getAppDatapath(db string, sock string, timeout int) ([]*OvsDatapath, error) {
	var app Client
	var err error
	cmd := "dpctl/show"
	dps := []*OvsDatapath{}
	app, err = NewClient(sock, timeout)
	if err != nil {
		app.Close()
		return dps, fmt.Errorf("failed '%s' from %s: %s", cmd, db, err)
	}
	r, err := app.query(cmd, nil)
	if err != nil {
		app.Close()
		return dps, fmt.Errorf("the '%s' command failed for %s: %s", cmd, db, err)
	}
	app.Close()
	response := r.String()
	if response == "" {
		return dps, fmt.Errorf("the '%s' command return no data for %s", cmd, db)
	}
	lines := strings.Split(strings.Trim(response, "\""), "\\n")
	indents := []int{}
	// First, evaluate output depth
	for _, line := range lines {
		indents = append(indents, indentAnalysis(line))
	}
	depth, err := indentDepthAnalysis(indents)
	if err != nil {
		return dps, fmt.Errorf("the '%s' command return for %s failed output depth analysis", cmd, db)
	}
	// Second, analyze the output
	var dpn string
	var dp *OvsDatapath
	for _, line := range lines {
		indent := indentAnalysis(line)
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch depth[indent] {
		case 0:
			// Datapath
			if dp != nil {
				dps = append(dps, dp)
			}
			dp = &OvsDatapath{}
			i := strings.Index(line, ":")
			dpn = line[:i]
			dp.Name = dpn
		case 1:
			if dp == nil {
				continue
			}
			// Counters and Interfaces
			i := strings.Index(line, ":")
			prefix := line[:i]
			switch prefix {
			case "lookups":
				for _, lookupAttrKv := range strings.Split(line[i+2:], " ") {
					kv := strings.Split(lookupAttrKv, ":")
					if len(kv) != 2 {
						continue
					}
					k := kv[0]
					v, err := strconv.ParseFloat(kv[1], 64)
					if err != nil {
						continue
					}
					switch k {
					case "hit":
						dp.Lookups.Hit = v
					case "missed":
						dp.Lookups.Missed = v
					case "lost":
						dp.Lookups.Lost = v
					default:
						return dps, fmt.Errorf("the '%s' command return for %s failed output analysis: datapath lookup counters", cmd, db)
					}
				}
			case "flows":
				if v, err := strconv.ParseFloat(strings.TrimSpace(line[i+2:]), 64); err == nil {
					dp.Flows = v
				}
			case "masks":
				for _, lookupAttrKv := range strings.Split(line[i+2:], " ") {
					kv := strings.Split(lookupAttrKv, ":")
					if len(kv) != 2 {
						continue
					}
					k := kv[0]
					v, err := strconv.ParseFloat(kv[1], 64)
					if err != nil {
						continue
					}
					switch k {
					case "hit":
						dp.Masks.Hit = v
					case "total":
						dp.Masks.Total = v
					case "hit/pkt":
						dp.Masks.HitRatio = v
					default:
						return dps, fmt.Errorf("the '%s' command return for %s failed output analysis: datapath masks counters", cmd, db)
					}
				}
			default:
				// do nothing
			}
		default:
			return dps, fmt.Errorf("the '%s' command return for %s failed output analysis", cmd, db)
		}
	}
	if dp != nil {
		dps = append(dps, dp)
	}
	return dps, nil
}

// GetAppDatapath returns the information about available datapaths.
func (cli *OvsClient) GetAppDatapath(db string) ([]*OvsDatapath, []*OvsBridge, []*OvsInterface, error) {
	cli.updateRefs()
	dps := []*OvsDatapath{}
	brs := []*OvsBridge{}
	intfs := []*OvsInterface{}
	var err error
	switch db {
	case "vswitchd-service":
		dps, brs, intfs, err = getAppDatapathInterfaces(db, cli.Service.Vswitchd.Socket.Control, cli.Timeout)
		if err != nil {
			return dps, brs, intfs, err
		}
		dps, err = getAppDatapath(db, cli.Service.Vswitchd.Socket.Control, cli.Timeout)
		if err != nil {
			return dps, brs, intfs, err
		}
	default:
		return dps, brs, intfs, fmt.Errorf("The '%s' database is unsupported for '%s'", db, "dpif/show")
	}
	return dps, brs, intfs, nil
}
