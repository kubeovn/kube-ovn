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
	//"encoding/json"
	"fmt"
	//"github.com/davecgh/go-spew/spew"
	"reflect"
	"sort"
)

// Schema - TODO
type Schema struct {
	Tables   map[string]Table `json:"tables"`
	Checksum string           `json:"cksum"`
	Name     string           `json:"name"`
	Version  string           `json:"version"`
}

// Table - TODO
type Table struct {
	Columns map[string]Column `json:"columns"`
	Indexes []interface{}     `json:"indexes"`
	MaxRows int               `json:"maxRows"`
	IsRoot  bool              `json:"isRoot"`
}

// Column - TODO
type Column struct {
	Type      interface{} `json:"type"`
	Ephemeral bool        `json:"ephemeral"`
	Mutable   bool        `json:"mutable"`
}

// GetSchema - TODO
func (c *Client) GetSchema(s string) (Schema, error) {
	if _, exists := c.Schemas[s]; exists {
		return c.Schemas[s], nil
	}
	method := "get_schema"
	js, err := encodeString(s)
	if err != nil {
		fmt.Errorf("'%s' method failed: %v", method, err)
	}
	response, err := c.query(method, js)
	if err != nil {
		return Schema{}, fmt.Errorf("'%s' method failed for '%s' database: %v", method, s, err)
	}
	schema, err := response.GetSchema()
	if err != nil {
		return Schema{}, fmt.Errorf("'%s' method failed for '%s' database: %v", method, s, err)
	}
	c.Schemas[s] = schema
	return c.Schemas[s], nil
}

// GetTables - TODO
func (sc *Schema) GetTables() []string {
	var tables []string
	for table := range sc.Tables {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	return tables
}

// GetColumns - TODO
func (sc *Schema) GetColumns(s string) []string {
	var columns []string
	if _, exists := sc.Tables[s]; !exists {
		return columns
	}
	for column := range sc.Tables[s].Columns {
		columns = append(columns, column)
	}
	sort.Strings(columns)
	return columns
}

// GetColumnsTypes - TODO
func (sc *Schema) GetColumnsTypes(table string) (map[string]string, error) {
	r := make(map[string]string)
	columns := sc.GetColumns(table)
	columns = append(columns, "_uuid")
	columns = append(columns, "_version")
	for _, column := range columns {
		columnType, err := sc.GetColumnType(table, column)
		if err != nil {
			return map[string]string{}, err
		}
		r[column] = columnType
	}
	return r, nil
}

// GetColumnType - TODO
func (sc *Schema) GetColumnType(table, column string) (string, error) {
	if column == "_uuid" || column == "_version" {
		return "uuid", nil
	}
	var columnType string
	if _, exists := sc.Tables[table]; !exists {
		return "", fmt.Errorf("Table %s not found", table)
	}
	if _, exists := sc.Tables[table].Columns[column]; !exists {
		return "", fmt.Errorf("Column %s not found in Table %s", column, table)
	}
	t := sc.Tables[table].Columns[column].Type
	k := reflect.ValueOf(t).Kind()

	switch k {
	case reflect.Map:
		//if table == "Port_Binding" && column == "chassis" {
		//	spew.Dump("***** start ******")
		//	spew.Dump(table)
		//	spew.Dump(column)
		//	spew.Dump("      *****")
		//	spew.Dump(t)
		//	spew.Dump(k)
		//	spew.Dump("***** end ******")
		//}
		m := t.(map[string]interface{})
		mapKey, mapKeyExists := m["key"]
		mapValue, mapValueExists := m["value"]
		if mapKeyExists && mapValueExists {
			if reflect.ValueOf(mapKey).Kind() == reflect.String {
				if reflect.ValueOf(mapValue).Kind() == reflect.String {
					return fmt.Sprintf("map[%s]%s", mapKey.(string), mapValue.(string)), nil
				}
			}
		}
		if !mapKeyExists {
			return "", fmt.Errorf("Table %s Column %s: unsupported map, 'key' not found: %v", table, column, m)
		}
		mapKeyType := reflect.ValueOf(mapKey).Kind()
		switch mapKeyType {
		case reflect.Map:
			mtt := mapKey.(map[string]interface{})
			if _, exists := mtt["type"]; !exists {
				return "", fmt.Errorf("Table %s Column %s: unsupported column type: %s-%s: %v", table, column, k, mapKeyType, t)
			}
			if reflect.ValueOf(mtt["type"]).Kind() != reflect.String {
				return "", fmt.Errorf("Table %s Column %s: unsupported column type: %s-%s: %v", table, column, k, mapKeyType, t)
			}
			_, refTableExists := mtt["refTable"]
			if refTableExists {
				// TODO: handle ref table
				//spew.Dump("***** start ******")
				//spew.Dump(table)
				//spew.Dump(column)
				//spew.Dump(t)
				//spew.Dump(k)
				//spew.Dump("***** end ******")
				columnType = fmt.Sprintf("map[string]%s", mtt["type"].(string))
			} else {
				columnType = mtt["type"].(string)
			}
		case reflect.String:
			columnType = mapKey.(string)
		default:
			return "", fmt.Errorf("Table %s Column %s: unsupported column type: %s-%s", table, column, k, mapKeyType)
		}
	case reflect.String:
		columnType = t.(string)
	default:
		return "", fmt.Errorf("Table %s Column %s: unsupported column type: %s", table, column, k)
	}
	return columnType, nil
}
