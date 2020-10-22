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
	"reflect"
)

// Result - TODO
type Result struct {
	Rows     []Row `json:"rows"`
	Database string
	Table    string
	Columns  map[string]string
}

// Row - TODO
type Row map[string]interface{}

// GetColumnValue - TODO
func (r *Row) GetColumnValue(column string, columns map[string]string) (interface{}, string, error) {
	data := (*r)[column]
	dataType := reflect.TypeOf(data).Kind().String()
	switch dataType {
	case "string":
		return data, dataType, nil
	case "bool":
		return data, dataType, nil
	case "int":
		return data, "integer", nil
	case "float64":
		return int64(data.(float64)), "integer", nil
	case "slice":
		sliceData := []string{}
		sliceDataKey := reflect.ValueOf(data).Index(0).Interface().(string)
		sliceDataValue := reflect.ValueOf(data).Index(1).Interface()
		switch sliceDataKey {
		case "uuid":
			return sliceDataValue.(string), "string", nil
		case "set":
			for _, x := range sliceDataValue.([]interface{}) {
				k := reflect.ValueOf(x).Index(0).Interface().(string)
				if k == "uuid" {
					v := reflect.ValueOf(x).Index(1).Interface().(string)
					sliceData = append(sliceData, v)
				} else {
					return sliceData, "", fmt.Errorf("Column %s contains %s, but []%s is not supported: %v", column, dataType, k, data)
				}
			}
			if len(sliceData) > 0 {
				return sliceData, "[]string", nil
			}
			// Note: in some instances the data type of a column is integer or string,
			// but because the column is empty, it will be identified as a set.
			return sliceData, "[]string", nil
		case "map":
			var mapType string
			kv := make(map[string]interface{})
			for _, x := range sliceDataValue.([]interface{}) {
				if reflect.ValueOf(x).Kind() == reflect.Slice {
					xData := reflect.ValueOf(x)
					if xData.Len() < 2 {
						continue
					}
					xDataKey := xData.Index(0).Interface()
					xDataValue := xData.Index(1).Interface()
					xDataKeyType := reflect.ValueOf(xDataKey).Kind()
					xDataValueType := reflect.ValueOf(xDataValue).Kind()
					if mapType == "" || mapType == "map[]" {
						mapType = fmt.Sprintf("map[%s]%s", xDataKeyType, xDataValueType)
					}
					mapTypeCurrent := fmt.Sprintf("map[%s]%s", xDataKeyType, xDataValueType)
					if mapType != mapTypeCurrent {
						return nil, "", fmt.Errorf("Column %s contains mixed type map: %s vs. %s : %v", column, mapType, mapTypeCurrent, data)
					}
					if xDataKeyType != reflect.String {
						return nil, "", fmt.Errorf("Column %s does not contain map with string keys: %v", column, data)
					}
					kv[xData.Index(0).Interface().(string)] = xData.Index(1).Interface()
				}
			}
			switch mapType {
			case "map[string]string":
				rkv := make(map[string]string)
				for k, v := range kv {
					rkv[k] = v.(string)
				}
				return rkv, mapType, nil
			case "map[string]float64":
				if columns[column] == "map[string]integer" {
					rkv := make(map[string]int)
					for k, v := range kv {
						rkv[k] = int(v.(float64))
					}
					return rkv, columns[column], nil
				}
			}
			if mapType == "" {
				switch columns[column] {
				case "map[string]string":
					rkv := make(map[string]string)
					return rkv, columns[column], nil
				case "map[string]integer":
					rkv := make(map[string]int)
					return rkv, columns[column], nil
				}
			}
			return nil, "", fmt.Errorf("Column '%s' contains unsupported slice map: %s: %v", column, mapType, data)
		}
		return nil, "", fmt.Errorf("Column '%s' contains unsupported slice: %s: %v", column, sliceDataKey, data)
	}
	return nil, "", fmt.Errorf("Column '%s' contains unsupported data type: %s, %v", column, dataType, data)
}
