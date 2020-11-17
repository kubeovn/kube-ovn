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
	"net"
	"strconv"
	"strings"
)

// OvsTunnel is a remote tunnel endpoint.
type OvsTunnel struct {
	Name          string
	Encapsulation string
	ID            uint64
	Key           string
	PacketType    string
	TTL           uint64
	Checksum      bool
	RemoteIP      string
	LocalIP       string
	InKey         string
	OutKey        string
	DstPort       string
	Tos           string
	DfDefault     bool
	EgressPktMark string
	Extensions    string
	Raw           string
}

// NewOvsTunnelFromString retuns OvsTunnel instance from an input string.
func NewOvsTunnelFromString(line string) (*OvsTunnel, error) {
	t := &OvsTunnel{
		Raw: line,
	}
	i := strings.Index(line, ":")
	if i < 0 {
		return t, fmt.Errorf("invalid input")
	}
	if !strings.HasPrefix(line[:i], "port ") {
		return t, fmt.Errorf("invalid input port: %s", line[:i])
	}
	port, err := strconv.ParseUint(strings.TrimLeft(line[:i], "port "), 10, 64)
	if err != nil {
		return t, fmt.Errorf("invalid input port: %d, %s", port, err)
	}
	t.ID = port
	if len(line) < (i + 2) {
		return t, fmt.Errorf("invalid input: port %d", port)
	}
	line = line[i+2:]
	i = strings.Index(line, " ")
	if i < 0 {
		return t, fmt.Errorf("invalid input: port %d, port name not found", port)
	}
	t.Name = line[:i]
	if len(line) < (i + 1) {
		return t, fmt.Errorf("invalid input: port %d, port attributes not found", port)
	}
	line = line[i+1:]
	if !strings.HasPrefix(line, "(") || !strings.HasSuffix(line, ")") {
		return t, fmt.Errorf("invalid input: port %d, invalid port attributes", port)
	}
	line = strings.TrimLeft(line, "(")
	line = strings.TrimRight(line, ")")

	for _, attrKv := range strings.Split(line, ",") {
		attrKv = strings.TrimSpace(attrKv)
		kv := strings.Split(attrKv, "=")
		if len(kv) != 2 {
			switch attrKv {
			case "legacy_l2":
				t.PacketType = attrKv
				continue
			case "legacy_l3":
				t.PacketType = attrKv
				continue
			case "ptap":
				t.PacketType = attrKv
				continue
			}
			i := strings.Index(attrKv, ":")
			if i < 0 {
				return t, fmt.Errorf("invalid input: port %d, invalid port attribute %s", port, attrKv)
			}
			encaps := attrKv[:i]
			switch encaps {
			case "geneve":
			case "gre":
			case "vxlan":
			case "lisp":
			case "stt":
			default:
				return t, fmt.Errorf("invalid input: port %d, invalid tunnel encapsulation %s", port, attrKv)
			}
			t.Encapsulation = encaps
			if len(attrKv) < (i + 2) {
				continue
			}
			attrKv = attrKv[i+2:]
			if strings.Contains(attrKv, "->") {
				ipPair := strings.Split(attrKv, "->")
				if len(ipPair) == 2 {
					var localIP net.IP
					var remoteIP net.IP
					if ipPair[0] == "::" {
						localIP = net.ParseIP("0.0.0.0")
					} else {
						localIP = net.ParseIP(ipPair[0])
						if localIP.String() != ipPair[0] {
							return t, fmt.Errorf("invalid input: port %d, invalid local ip: %s", port, attrKv)
						}
					}
					remoteIP = net.ParseIP(ipPair[1])
					if remoteIP.String() != ipPair[1] {
						return t, fmt.Errorf("invalid input: port %d, invalid remote ip: %s", port, attrKv)
					}
					t.LocalIP = localIP.String()
					t.RemoteIP = remoteIP.String()
				}
			}
			continue
		}
		k := kv[0]
		v := kv[1]
		switch k {
		case "key":
			t.Key = v
		case "dst_port":
			t.DstPort = v
		case "in_key":
			t.InKey = v
		case "out_key":
			t.OutKey = v
		case "tos":
			t.Tos = v
		case "egress_pkt_mark":
			t.EgressPktMark = v
		case "exts":
			t.Extensions = v
		case "csum":
			if v == "true" {
				t.Checksum = true
			} else {
				t.Checksum = false
			}
		case "df_default":
			if v == "true" {
				t.DfDefault = true
			} else {
				t.DfDefault = false
			}
		case "ttl":
			if ttl, err := strconv.ParseUint(v, 10, 64); err == nil {
				t.TTL = ttl
			}
		case "dp port":
			if dpPort, err := strconv.ParseUint(v, 10, 64); err == nil {
				if dpPort != port {
					return t, fmt.Errorf("invalid input: port %d, dp port mismatch: %d", port, dpPort)
				}
			}
		default:
			return t, fmt.Errorf("invalid input: port %d, unsupported port attribute: %s", port, attrKv)
		}
	}
	return t, nil
}

// GetTunnels returns a list of tunnels originating from an OVS instance.
func (cli *OvsClient) GetTunnels() ([]*OvsTunnel, error) {
	cli.updateRefs()
	db := "vswitchd-service"
	cmd := "ofproto/list-tunnels"
	tunnels := []*OvsTunnel{}
	app, err := NewClient(cli.Service.Vswitchd.Socket.Control, cli.Timeout)
	if err != nil {
		app.Close()
		return tunnels, fmt.Errorf("failed '%s' from %s: %s", cmd, db, err)
	}
	r, err := app.query(cmd, nil)
	if err != nil {
		app.Close()
		return tunnels, fmt.Errorf("the '%s' command failed for %s: %s", cmd, db, err)
	}
	app.Close()
	response := r.String()
	if response == "" {
		return tunnels, fmt.Errorf("the '%s' command return no data for %s", cmd, db)
	}
	lines := strings.Split(strings.Trim(response, "\""), "\\n")
	// Analyze the output
	for _, line := range lines {
		if line == "" {
			continue
		}
		t, err := NewOvsTunnelFromString(line)
		if err != nil {
			return tunnels, fmt.Errorf("the '%s' command returned data for %s, but erred: %s", cmd, db, err)
		}
		tunnels = append(tunnels, t)
	}
	return tunnels, nil
}
