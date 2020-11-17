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
	"bytes"
	"encoding/json"
	"fmt"
	//"github.com/davecgh/go-spew/spew"
	"strings"
)

// Condition represents condition for select operation,
// as described in [Notation](https://tools.ietf.org/html/rfc7047#section-5.1) section.
// The meaning of the <function> depends on the type of <column>.
type Condition struct {
	Column   string
	Function string
	Value    string
	Type     string
}

// NewCondition - DOCS-TBD
func NewCondition(s []string) (Condition, error) {
	c := Condition{}
	if err := c.Parse(strings.Join(s, "")); err != nil {
		return c, err
	}
	if strings.Contains(c.Column, "uuid") {
		c.Type = "string"
	}
	if c.Value == "true" || c.Value == "false" {
		c.Type = "bool"
	}
	if len(c.Value) >= 2 {
		value := strings.Trim(c.Value, "\"")
		if len(value) == len(c.Value)-2 {
			c.Value = value
			c.Type = "string"
		}
	}
	return c, nil
}

// Parse - DOCS-TBD
func (c *Condition) Parse(s string) error {
	data := []byte(s)
	functions := [][]byte{
		[]byte("<="),
		[]byte("=="),
		[]byte("!="),
		[]byte(">="),
		[]byte("=~"),
		[]byte(`>`),
		[]byte(`<`),
	}
	for offset := range data {
		for _, f := range functions {
			n := len(f)
			if offset < n {
				continue
			}
			if bytes.Equal(data[offset:offset+n], f) {
				//spew.Dump("***")
				//spew.Dump(data[n+1])
				//spew.Dump(f)
				//spew.Dump(data[offset : offset+n])
				v := string(data[offset+n:])
				if v == "" {
					return fmt.Errorf("invalid condition: '%s'", s)
				}
				c.Column = string(data[0:offset])
				c.Function = string(f[:])
				c.Value = v
				//spew.Dump(c)
				return nil
			}
		}
	}
	//spew.Dump(c)
	return fmt.Errorf("invalid condition: '%s'", s)
}

// MarshalJSON - DOCS-TBD
func (c Condition) MarshalJSON() ([]byte, error) {
	b := bytes.NewBuffer([]byte("["))
	column, err := json.Marshal(c.Column)
	if err != nil {
		return []byte{}, fmt.Errorf("marshal Condition.Column: %s", err)
	}
	b.Write(column)
	b.WriteString(",")
	function, err := json.Marshal(c.Function)
	if err != nil {
		return []byte{}, fmt.Errorf("marshal Condition.Function: %s", err)
	}

	b.Write(function)
	b.WriteString(",")
	switch c.Type {
	case "string":
		value, err := json.Marshal(c.Value)
		if err != nil {
			return []byte{}, fmt.Errorf("marshal Condition.Value: %s", err)
		}
		b.Write(value)
	default:
		return []byte{}, fmt.Errorf("marshal Condition.Value: no support for '%s' type", c.Type)
	}
	b.WriteString("]")
	return b.Bytes(), nil
}
