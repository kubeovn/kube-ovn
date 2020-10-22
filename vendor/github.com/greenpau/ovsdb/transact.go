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
	"encoding/json"
	"fmt"
	//"github.com/davecgh/go-spew/spew"
)

// Transaction - TODO
type Transaction struct {
	Database   string
	Operations []Operation
}

// ToBytes - TODO
func (t *Transaction) ToBytes() ([]byte, int, error) {
	out := []byte{}
	b, err := json.Marshal(t.Database)
	if err != nil {
		return []byte{}, 0, err
	}
	out = append(out, b...)
	for _, op := range t.Operations {
		b, err := json.Marshal(op)
		if err != nil {
			return []byte{}, 0, err
		}
		out = append(out, []byte(",")...)
		out = append(out, b...)
	}
	return out, len(out), nil
}

// ToString - TODO
func (t *Transaction) ToString() (string, error) {
	b, n, err := t.ToBytes()
	if err != nil {
		return "", err
	}
	return string(b[:n]), nil
}

// Transact - TODO
func (c *Client) Transact(db string, query string) (Result, error) {
	if c == nil {
		return Result{}, fmt.Errorf("interface is unavailable")
	}
	op, err := NewOperation(query)
	if err != nil {
		return Result{}, err
	}
	params := Transaction{
		Database:   db,
		Operations: []Operation{},
	}
	params.Operations = append(params.Operations, op)
	method := "transact"
	response, err := c.query(method, params)
	if err != nil {
		return Result{}, fmt.Errorf("'%s' method, query: '%s' failed: %v", method, query, err)
	}
	var r Result
	if err := json.Unmarshal(response.Result, &r); err != nil {
		return Result{}, fmt.Errorf("'%s' method, query: '%s' failed: %v", method, query, err)
	}
	r.Database = db
	r.Table = op.Table
	columns, err := c.getColumns(db, op.Table)
	if err != nil {
		return Result{}, fmt.Errorf("'%s' method, query: '%s' failed: %v", method, query, err)
	}
	r.Columns = columns
	return r, nil
}
