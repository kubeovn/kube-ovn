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

// OvsClient holds connection to OVS databases.
type OvsClient struct {
	Database struct {
		Vswitch OvsDatabase
	}
	Service struct {
		OvnController OvsDaemon
		Vswitchd      OvsDaemon
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

// NewOvsClient creates an instance of a client for OVS stack.
func NewOvsClient() *OvsClient {
	cli := OvsClient{}
	cli.Timeout = 2

	cli.System.ID = "unknown"
	cli.System.RunDir = "/var/run/openvswitch"
	cli.System.Hostname = "localhost"
	cli.System.Type = "unknown"
	cli.System.Version = "unknown"

	cli.Database.Vswitch.Process.ID = 0
	cli.Database.Vswitch.Process.Group = "openvswitch"
	cli.Database.Vswitch.Process.User = "openvswitch"
	cli.Database.Vswitch.Version = "unknown"
	cli.Database.Vswitch.Schema.Version = "unknown"
	cli.Database.Vswitch.Name = "Open_vSwitch"
	cli.Database.Vswitch.Socket.Remote = "unix:/var/run/openvswitch/db.sock"
	cli.Database.Vswitch.Socket.Control = fmt.Sprintf("/var/run/openvswitch/ovsdb-server.%d.ctl", cli.Database.Vswitch.Process.ID)
	cli.Database.Vswitch.File.Data.Path = "/etc/openvswitch/conf.db"
	cli.Database.Vswitch.File.Log.Path = "/var/log/openvswitch/ovsdb-server.log"
	cli.Database.Vswitch.File.Pid.Path = "/var/run/openvswitch/ovsdb-server.pid"
	cli.Database.Vswitch.File.SystemID.Path = "/etc/openvswitch/system-id.conf"
	cli.Database.Vswitch.Port.Default = 6640
	cli.Database.Vswitch.Port.Ssl = 6630

	cli.Service.Vswitchd.Process.ID = 0
	cli.Service.Vswitchd.Process.User = "openvswitch"
	cli.Service.Vswitchd.Process.Group = "openvswitch"
	cli.Service.Vswitchd.File.Log.Path = "/var/log/openvswitch/ovs-vswitchd.log"
	cli.Service.Vswitchd.File.Pid.Path = "/var/run/openvswitch/ovs-vswitchd.pid"
	cli.Service.Vswitchd.Socket.Control = fmt.Sprintf("/var/run/openvswitch/ovs-vswitchd.%d.ctl", cli.Service.Vswitchd.Process.ID)

	cli.Service.OvnController.Process.ID = 0
	cli.Service.OvnController.Process.User = "openvswitch"
	cli.Service.OvnController.Process.Group = "openvswitch"
	cli.Service.OvnController.File.Log.Path = "/var/log/openvswitch/ovn-controller.log"
	cli.Service.OvnController.File.Pid.Path = "/var/run/openvswitch/ovn-controller.pid"
	cli.Service.OvnController.Socket.Control = fmt.Sprintf("/var/run/openvswitch/ovn-controller.%d.ctl", cli.Service.OvnController.Process.ID)

	return &cli
}

// Connect initiates connections to OVS database.
func (cli *OvsClient) Connect() error {
	if cli.Database.Vswitch.Client == nil {
		ovs, err := NewClient(cli.Database.Vswitch.Socket.Remote, cli.Timeout)
		cli.Database.Vswitch.Client = &ovs
		if err != nil {
			cli.Database.Vswitch.Client.closed = true
			return fmt.Errorf("failed connecting to %s via %s: %s", cli.Database.Vswitch.Name, cli.Database.Vswitch.Socket.Remote, err)
		}
	}
	return nil
}

// Close closes connections to OVS database.
func (cli *OvsClient) Close() {
	if cli.Database.Vswitch.Client != nil {
		cli.Database.Vswitch.Client.Close()
	}
}

func (cli *OvsClient) updateRefs() {
	cli.Database.Vswitch.Socket.Control = fmt.Sprintf("unix:/var/run/openvswitch/ovsdb-server.%d.ctl", cli.Database.Vswitch.Process.ID)
	cli.Service.Vswitchd.Socket.Control = fmt.Sprintf("unix:/var/run/openvswitch/ovs-vswitchd.%d.ctl", cli.Service.Vswitchd.Process.ID)
	cli.Service.OvnController.Socket.Control = fmt.Sprintf("unix:/var/run/openvswitch/ovn-controller.%d.ctl", cli.Service.OvnController.Process.ID)
}
