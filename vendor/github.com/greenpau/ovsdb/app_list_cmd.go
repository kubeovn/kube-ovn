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
	"strings"
)

func appListCommands(db string, sock string, timeout int) (map[string]bool, error) {
	var app Client
	var err error
	cmd := "list-commands"
	cmds := make(map[string]bool)
	app, err = NewClient(sock, timeout)
	if err != nil {
		return cmds, fmt.Errorf("failed '%s' from %s: %s", cmd, db, err)
	}
	r, err := app.query(cmd, nil)
	if err != nil {
		app.Close()
		return cmds, fmt.Errorf("the '%s' command failed for %s: %s", cmd, db, err)
	}
	app.Close()
	response := r.String()
	if response == "" {
		return cmds, fmt.Errorf("the '%s' command return no data for %s", cmd, db)
	}
	lines := strings.Split(response, "\\n")
	for _, line := range lines {
		if !strings.HasPrefix(line, " ") {
			continue
		}
		cmds[strings.Join(strings.Fields(line), " ")] = true
	}
	return cmds, nil
}

// AppListCommands returns the list of commands supported by
// ovs-appctl tool and the database.
func (cli *OvnClient) AppListCommands(db string) (map[string]bool, error) {
	cmd := "list-commands"
	cli.updateRefs()
	switch db {
	case "ovsdb-server-northbound":
		return appListCommands(db, cli.Database.Northbound.Socket.Control, cli.Timeout)
	case "ovsdb-server-southbound":
		return appListCommands(db, cli.Database.Southbound.Socket.Control, cli.Timeout)
	case "ovsdb-server":
		return appListCommands(db, cli.Database.Vswitch.Socket.Control, cli.Timeout)
	default:
		return nil, fmt.Errorf("The '%s' database is unsupported for '%s'", db, cmd)
	}
}

// AppListCommands returns the list of commands supported by
// ovs-appctl tool and the database.
func (cli *OvsClient) AppListCommands(db string) (map[string]bool, error) {
	cli.updateRefs()
	cmd := "list-commands"
	switch db {
	case "ovsdb-server":
		return appListCommands(db, cli.Database.Vswitch.Socket.Control, cli.Timeout)
	case "vswitchd-service":
		return appListCommands(db, cli.Service.Vswitchd.Socket.Control, cli.Timeout)
	default:
		return nil, fmt.Errorf("The '%s' database is unsupported for '%s'", db, cmd)
	}
}
