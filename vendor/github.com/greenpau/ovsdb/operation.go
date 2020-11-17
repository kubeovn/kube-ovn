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
	"text/scanner"
)

// Operation represents Transact Method, as described in
// https://tools.ietf.org/html/rfc7047#section-4.1.3
type Operation struct {
	Name       string      `json:"op"`
	Table      string      `json:"table"`
	Conditions []Condition `json:"where"`
	Columns    []string    `json:"columns,omitempty"`
}

// NewOperation - TODO
func NewOperation(query string) (Operation, error) {
	t := Operation{}
	t.Conditions = []Condition{}
	if err := t.Parse(query); err != nil {
		return t, err
	}
	if err := t.Validate(); err != nil {
		return t, err
	}
	return t, nil
}

// Parse - TODO
func (t *Operation) Parse(i string) error {
	var s scanner.Scanner
	s.Init(strings.NewReader(i))
	var tok rune
	stage := "operation"
	conditions := []string{}
	for tok != scanner.EOF {
		tok = s.Scan()
		if s.TokenText() == "" {
			continue
		}
		switch stage {
		case "operation":
			t.Name = strings.ToLower(s.TokenText())
			stage = "columns"
		case "columns":
			if s.TokenText() == "FROM" {
				stage = "table"
				continue
			}
			if s.TokenText() != "," && s.TokenText() != "*" {
				t.Columns = append(t.Columns, s.TokenText())
			}
		case "table":
			t.Table = s.TokenText()
			stage = "where"
		case "where":
			if s.TokenText() != "WHERE" {
				return fmt.Errorf("parser error: expected WHERE clause")
			}
			stage = "conditions"
		case "conditions":
			if t.Table == "" {
				return fmt.Errorf("parser error: expected FROM clause followed by a table name")
			}
			if s.TokenText() == "LIMIT" {
				stage = "lmits"
				continue
			}
			if s.TokenText() == "," {
				cond, err := NewCondition(conditions)
				if err != nil {
					return fmt.Errorf("parser error: %s for: %s", err, i)
				}
				t.Conditions = append(t.Conditions, cond)
				conditions = conditions[:0]
				continue
			}
			conditions = append(conditions, s.TokenText())
		case "limits":
			return fmt.Errorf("parser error: unsupported LIMIT clause")
		default:
			return fmt.Errorf("parser error: unknown stage: %s", stage)
		}
	}
	if len(conditions) > 0 {
		cond, err := NewCondition(conditions)
		if err != nil {
			return fmt.Errorf("parser error: invalid condition: %s", conditions)
		}
		t.Conditions = append(t.Conditions, cond)
	}
	//spew.Dump(t)
	return nil
}

// Validate - TODO
func (t *Operation) Validate() error {
	if t.Name == "" {
		return fmt.Errorf("validation error: operation is empty")
	}
	op, exists := operations[t.Name]
	if !exists {
		return fmt.Errorf("validation error: unsupported operation: %s", t.Name)
	}

	for _, m := range op.Members {
		switch m.Name {
		case "op":
			if t.Name == "" && m.Required {
				return fmt.Errorf("validation error: no operation")
			}
			t.Name = strings.ToLower(t.Name)
		case "table":
			if t.Table == "" && m.Required {
				return fmt.Errorf("validation error: no table")
			}
		case "where":
			if len(t.Conditions) == 0 && !m.Autofill && m.Required {
				return fmt.Errorf("validation error: no conditions")
			}
		case "columns":
			if len(t.Columns) == 0 && m.Required {
				return fmt.Errorf("validation error: no columns")
			}
		default:
			return fmt.Errorf("validation error: unsupported transaction member: %s", m.Name)
		}
	}
	return nil
}

type member struct {
	Name     string
	Required bool
	Autofill bool
}

type operationConfiguration struct {
	Name    string
	Members map[string]member
}

var operations = map[string]operationConfiguration{
	"select": {
		Name: "select",
		Members: map[string]member{
			"op": {
				Name:     "op",
				Required: true,
			},
			"table": {
				Name:     "table",
				Required: true,
			},
			"where": {
				Name:     "where",
				Required: true,
				Autofill: true,
			},
			"columns": {
				Name:     "columns",
				Required: false,
			},
		},
	},
}
