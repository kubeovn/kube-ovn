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
	"strconv"
	"strings"
)

func getAppMemoryMetrics(db string, sock string, timeout int) (map[string]float64, error) {
	var app Client
	var err error
	cmd := "memory/show"
	metrics := make(map[string]float64)
	app, err = NewClient(sock, timeout)
	if err != nil {
		app.Close()
		return metrics, fmt.Errorf("failed '%s' from %s: %s", cmd, db, err)
	}
	r, err := app.query(cmd, nil)
	if err != nil {
		app.Close()
		return metrics, fmt.Errorf("the '%s' command failed for %s: %s", cmd, db, err)
	}
	app.Close()
	response := r.String()
	if response == "" {
		return metrics, fmt.Errorf("the '%s' command return no data for %s", cmd, db)
	}
	response = strings.Trim(response, "\"")
	lines := strings.Split(response, "\\n")
	for _, line := range lines {
		lineArray := strings.Split(strings.Join(strings.Fields(line), " "), " ")
		for _, item := range lineArray {
			if item == "" {
				continue
			}
			itemArray := strings.Split(item, ":")
			if len(itemArray) != 2 {
				continue
			}
			if value, err := strconv.ParseFloat(itemArray[1], 64); err == nil {
				metrics[itemArray[0]] = value
			}
		}
	}
	return metrics, nil
}

// GetAppMemoryMetrics returns memory usage counters.
func (cli *OvnClient) GetAppMemoryMetrics(db string) (map[string]float64, error) {
	cli.updateRefs()
	cmd := "memory/show"
	switch db {
	case "ovsdb-server-northbound":
		return getAppMemoryMetrics(db, cli.Database.Northbound.Socket.Control, cli.Timeout)
	case "ovsdb-server-southbound":
		return getAppMemoryMetrics(db, cli.Database.Southbound.Socket.Control, cli.Timeout)
	case "ovsdb-server":
		return getAppMemoryMetrics(db, cli.Database.Vswitch.Socket.Control, cli.Timeout)
	default:
		return nil, fmt.Errorf("The '%s' database is unsupported for '%s'", db, cmd)
	}
}

// GetAppMemoryMetrics returns memory usage counters.
func (cli *OvsClient) GetAppMemoryMetrics(db string) (map[string]float64, error) {
	cli.updateRefs()
	cmd := "memory/show"
	switch db {
	case "ovsdb-server":
		return getAppMemoryMetrics(db, cli.Database.Vswitch.Socket.Control, cli.Timeout)
	case "vswitchd-service":
		return getAppMemoryMetrics(db, cli.Service.Vswitchd.Socket.Control, cli.Timeout)
	default:
		return nil, fmt.Errorf("The '%s' database is unsupported for '%s'", db, cmd)
	}
}
