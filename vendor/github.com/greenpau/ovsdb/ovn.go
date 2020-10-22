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

// OvnClient holds connection to all OVN databases.
type OvnClient struct {
	Database struct {
		Northbound OvsDatabase
		Southbound OvsDatabase
		Vswitch    OvsDatabase
	}
	Service struct {
		Northd   OvsDaemon
		Vswitchd OvsDaemon
	}
	Timeout int
	System  struct {
		ID       string
		RunDir   string
		Hostname string
		Type     string
		Version  string
	}
}

// NewOvnClient creates an instance of a client for OVN stack.
func NewOvnClient() *OvnClient {
	cli := OvnClient{}
	cli.Timeout = 2

	cli.System.ID = "unknown"
	cli.System.RunDir = "/var/run/openvswitch"
	cli.System.Hostname = "localhost"
	cli.System.Type = "unknown"
	cli.System.Version = "unknown"

	cli.Database.Vswitch.Process.ID = 0
	cli.Database.Vswitch.Process.User = "openvswitch"
	cli.Database.Vswitch.Process.Group = "openvswitch"
	cli.Database.Vswitch.Version = "unknown"
	cli.Database.Vswitch.Schema.Version = "unknown"
	cli.Database.Vswitch.Name = "Open_vSwitch"
	cli.Database.Vswitch.Socket.Remote = "unix:/var/run/openvswitch/db.sock"
	cli.Database.Vswitch.Socket.Control = fmt.Sprintf("unix:/var/run/openvswitch/ovsdb-server.%d.ctl", cli.Database.Vswitch.Process.ID)
	cli.Database.Vswitch.File.Data.Path = "/etc/openvswitch/conf.db"
	cli.Database.Vswitch.File.Log.Path = "/var/log/openvswitch/ovsdb-server.log"
	cli.Database.Vswitch.File.Pid.Path = "/var/run/openvswitch/ovsdb-server.pid"
	cli.Database.Vswitch.File.SystemID.Path = "/etc/openvswitch/system-id.conf"
	cli.Database.Vswitch.Port.Default = 6640
	cli.Database.Vswitch.Port.Ssl = 6630

	cli.Database.Northbound.Name = "OVN_Northbound"
	cli.Database.Northbound.Socket.Remote = "unix:/run/openvswitch/ovnnb_db.sock"
	cli.Database.Northbound.Socket.Control = "unix:/run/openvswitch/ovnnb_db.ctl"
	cli.Database.Northbound.File.Data.Path = "/var/lib/openvswitch/ovnnb_db.db"
	cli.Database.Northbound.File.Log.Path = "/var/log/openvswitch/ovsdb-server-nb.log"
	cli.Database.Northbound.File.Pid.Path = "/run/openvswitch/ovnnb_db.pid"
	cli.Database.Northbound.Process.ID = 0
	cli.Database.Northbound.Process.User = "openvswitch"
	cli.Database.Northbound.Process.Group = "openvswitch"
	cli.Database.Northbound.Port.Default = 6641
	cli.Database.Northbound.Port.Ssl = 6631
	cli.Database.Northbound.Port.Raft = 6643

	cli.Database.Southbound.Name = "OVN_Southbound"
	cli.Database.Southbound.Socket.Remote = "unix:/run/openvswitch/ovnsb_db.sock"
	cli.Database.Southbound.Socket.Control = "unix:/run/openvswitch/ovnsb_db.ctl"
	cli.Database.Southbound.File.Data.Path = "/var/lib/openvswitch/ovnsb_db.db"
	cli.Database.Southbound.File.Log.Path = "/var/log/openvswitch/ovsdb-server-sb.log"
	cli.Database.Southbound.File.Pid.Path = "/run/openvswitch/ovnsb_db.pid"
	cli.Database.Southbound.Process.ID = 0
	cli.Database.Southbound.Process.User = "openvswitch"
	cli.Database.Southbound.Process.Group = "openvswitch"
	cli.Database.Southbound.Port.Default = 6642
	cli.Database.Southbound.Port.Ssl = 6632
	cli.Database.Southbound.Port.Raft = 6644

	cli.Service.Vswitchd.Process.ID = 0
	cli.Service.Vswitchd.Process.User = "openvswitch"
	cli.Service.Vswitchd.Process.Group = "openvswitch"
	cli.Service.Vswitchd.File.Log.Path = "/var/log/openvswitch/ovs-vswitchd.log"
	cli.Service.Vswitchd.File.Pid.Path = "/var/run/openvswitch/ovs-vswitchd.pid"
	cli.Service.Vswitchd.Socket.Control = fmt.Sprintf("unix:/var/run/openvswitch/ovs-vswitchd.%d.ctl", cli.Service.Vswitchd.Process.ID)

	cli.Service.Northd.Process.ID = 0
	cli.Service.Northd.Process.User = "openvswitch"
	cli.Service.Northd.Process.Group = "openvswitch"
	cli.Service.Northd.File.Log.Path = "/var/log/openvswitch/ovn-northd.log"
	cli.Service.Northd.File.Pid.Path = "/run/openvswitch/ovn-northd.pid"
	cli.Service.Northd.Socket.Control = fmt.Sprintf("unix:/var/run/openvswitch/ovn-northd.%d.ctl", cli.Service.Northd.Process.ID)

	return &cli
}

// Connect initiates connections to OVN databases.
func (cli *OvnClient) Connect() error {
	errMsgs := []string{}
	if cli.Database.Vswitch.Client == nil {
		ovs, err := NewClient(cli.Database.Vswitch.Socket.Remote, cli.Timeout)
		cli.Database.Vswitch.Client = &ovs
		if err != nil {
			cli.Database.Vswitch.Client.closed = true
			errMsgs = append(errMsgs, fmt.Sprintf("failed connecting to %s via %s: %s", cli.Database.Vswitch.Name, cli.Database.Vswitch.Socket.Remote, err))
		}
	}
	if cli.Database.Northbound.Client == nil {
		nb, err := NewClient(cli.Database.Northbound.Socket.Remote, cli.Timeout)
		cli.Database.Northbound.Client = &nb
		if err != nil {
			cli.Database.Northbound.Client.closed = true
			errMsgs = append(errMsgs, fmt.Sprintf("failed connecting to %s via %s: %s", cli.Database.Northbound.Name, cli.Database.Northbound.Socket.Remote, err))
		}
	}
	if cli.Database.Southbound.Client == nil {
		sb, err := NewClient(cli.Database.Southbound.Socket.Remote, cli.Timeout)
		cli.Database.Southbound.Client = &sb
		if err != nil {
			cli.Database.Southbound.Client.closed = true
			errMsgs = append(errMsgs, fmt.Sprintf("failed connecting to %s via %s: %s", cli.Database.Southbound.Name, cli.Database.Southbound.Socket.Remote, err))
		}
	}
	if len(errMsgs) > 0 {
		return fmt.Errorf("%s", errMsgs)
	}
	return nil
}

// Close closes connections to OVN databases.
func (cli *OvnClient) Close() {
	if cli.Database.Southbound.Client != nil {
		cli.Database.Southbound.Client.Close()
	}
	if cli.Database.Northbound.Client != nil {
		cli.Database.Northbound.Client.Close()
	}
	if cli.Database.Vswitch.Client != nil {
		cli.Database.Vswitch.Client.Close()
	}
}

func (cli *OvnClient) updateRefs() {
	cli.Database.Vswitch.Socket.Control = fmt.Sprintf("unix:/var/run/openvswitch/ovsdb-server.%d.ctl", cli.Database.Vswitch.Process.ID)
	cli.Service.Vswitchd.Socket.Control = fmt.Sprintf("unix:/var/run/openvswitch/ovs-vswitchd.%d.ctl", cli.Service.Vswitchd.Process.ID)
	cli.Service.Northd.Socket.Control = fmt.Sprintf("unix:/var/run/openvswitch/ovn-northd.%d.ctl", cli.Service.Northd.Process.ID)
}
