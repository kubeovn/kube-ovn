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
	"bufio"
	"fmt"
	//"github.com/davecgh/go-spew/spew"
	"os"
	"strconv"
	"strings"
)

// IsDefaultPortUp returns the TCP port used for database connection.
// If the value if greater than 0, then the port is in LISTEN state.
func (cli *OvnClient) IsDefaultPortUp(db string) (int, error) {
	var port int
	var pid int
	switch db {
	case "ovsdb-server-northbound":
		port = cli.Database.Northbound.Port.Default
		pid = cli.Database.Northbound.Process.ID
	case "ovsdb-server-southbound":
		port = cli.Database.Southbound.Port.Default
		pid = cli.Database.Southbound.Process.ID
	default:
		return 0, fmt.Errorf("The '%s' database is unsupported", db)
	}
	if !isPortUp(pid, port) {
		port = 0
	}
	return port, nil
}

// IsDefaultPortUp returns the TCP port used for database connection.
// If the value if greater than 0, then the port is in LISTEN state.
func (cli *OvsClient) IsDefaultPortUp(db string) (int, error) {
	var port int
	var pid int
	switch db {
	case "ovsdb-server":
		port = cli.Database.Vswitch.Port.Default
		pid = cli.Database.Vswitch.Process.ID
	default:
		return 0, fmt.Errorf("The '%s' database is unsupported", db)
	}
	if !isPortUp(pid, port) {
		port = 0
	}
	return port, nil
}

// IsSslPortUp returns the TCP port used for secure database connection.
// If the value if greater than 0, then the port is in LISTEN state.
func (cli *OvnClient) IsSslPortUp(db string) (int, error) {
	var port int
	var pid int
	switch db {
	case "ovsdb-server-northbound":
		port = cli.Database.Northbound.Port.Ssl
		pid = cli.Database.Northbound.Process.ID
	case "ovsdb-server-southbound":
		port = cli.Database.Southbound.Port.Ssl
		pid = cli.Database.Southbound.Process.ID
	default:
		return 0, fmt.Errorf("The '%s' database is unsupported", db)
	}
	if !isPortUp(pid, port) {
		port = 0
	}
	return port, nil
}

// IsSslPortUp returns the TCP port used for secure database connection.
// If the value if greater than 0, then the port is in LISTEN state.
func (cli *OvsClient) IsSslPortUp(db string) (int, error) {
	var port int
	var pid int
	switch db {
	case "ovsdb-server":
		port = cli.Database.Vswitch.Port.Ssl
		pid = cli.Database.Vswitch.Process.ID
	default:
		return 0, fmt.Errorf("The '%s' database is unsupported", db)
	}
	if !isPortUp(pid, port) {
		port = 0
	}
	return port, nil
}

// IsRaftPortUp returns the TCP port used for clustering (raft).
// If the value if greater than 0, then the port is in LISTEN state.
func (cli *OvnClient) IsRaftPortUp(db string) (int, error) {
	var port int
	var pid int
	switch db {
	case "ovsdb-server-northbound":
		port = cli.Database.Northbound.Port.Raft
		pid = cli.Database.Northbound.Process.ID
	case "ovsdb-server-southbound":
		port = cli.Database.Southbound.Port.Raft
		pid = cli.Database.Southbound.Process.ID
	default:
		return 0, fmt.Errorf("The '%s' database is unsupported", db)
	}
	if !isPortUp(pid, port) {
		port = 0
	}
	return port, nil
}

func isPortUp(pid int, port int) bool {
	if pid == 0 || port == 0 {
		return false
	}
	localAddress := "00000000:" + strings.ToUpper(strconv.FormatInt(int64(port), 16))
	f := "/proc/" + strconv.Itoa(pid) + "/net/tcp"
	file, err := os.Open(f)
	if err != nil {
		return false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		arr := strings.Fields(strings.TrimSpace(scanner.Text()))
		if len(arr) < 10 {
			continue
		}
		// State:
		//   - 0A means "LISTEN"
		//   - 01 means "ESTABLISHED"
		//   - 06 means "TIME_WAIT"
		if arr[3] == "0A" && arr[1] == localAddress && arr[2] == "00000000:0000" {
			return true
		}
	}
	if err := scanner.Err(); err != nil {
		return false
	}
	return false
}
