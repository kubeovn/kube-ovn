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
)

// OvsDatabase represents an instance of OVS DB.
type OvsDatabase struct {
	Client *Client
	Name   string
	Socket struct {
		Remote  string
		Control string
		Raft    string
	}
	Port struct {
		Default int
		Ssl     int
		Raft    int
	}
	File struct {
		Log      OvsDataFile
		Data     OvsDataFile
		Pid      OvsDataFile
		SystemID OvsDataFile
	}
	Process OvsProcess
	Version string
	Schema  struct {
		Version string
	}
	connected bool
}

// Databases - DOCS-TBD
func (c *Client) Databases() ([]string, error) {
	method := "list_dbs"
	response, err := c.query(method, nil)
	if err != nil {
		return nil, fmt.Errorf("'%s' method failed: %v", method, err)
	}
	dbs, err := response.Databases()
	if err != nil {
		return nil, fmt.Errorf("'%s' method failed: %v", method, err)
	}
	return dbs, nil
}

// DatabaseExists - DOCS-TBD
func (c *Client) DatabaseExists(dbName string) error {
	databases, err := c.Databases()
	if err != nil {
		return err
	}
	for _, name := range databases {
		if name == dbName {
			return nil
		}
	}
	return fmt.Errorf("database '%s' not found", dbName)
}
