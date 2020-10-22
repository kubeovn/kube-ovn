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
	//"encoding/base64"
	"encoding/json"
	//"fmt"
	//"github.com/davecgh/go-spew/spew"
	//"strings"
)

// Response - TODO
type Response struct {
	Result json.RawMessage `json:"result"`
	Error
	Seq uint64 `json:"id"`
}

// UnmarshalJSON - TODO
func (r *Response) UnmarshalJSON(b []byte) error {
	if bytes.HasPrefix(b, []byte(`[{"`)) {
		b = bytes.TrimLeft(b, "[")
		b = bytes.TrimRight(b, "]")
	}
	if err := json.Unmarshal(b, &r.Result); err != nil {
		return err
	}
	if bytes.HasPrefix(b, []byte(`{"error":`)) {
		if err := json.Unmarshal(b, &r.Error); err != nil {
			return err
		}
	}
	return nil
}

// Databases - TODO
func (r *Response) Databases() ([]string, error) {
	body := []string{}
	err := json.Unmarshal(r.Result, &body)
	if err != nil {
		return body, err
	}
	return body, nil
}

// GetSchema TODO
func (r *Response) GetSchema() (Schema, error) {
	var s Schema
	err := json.Unmarshal(r.Result, &s)
	if err != nil {
		return s, err
	}
	return s, nil
}

// String() returns response payload as a string.
func (r *Response) String() string {
	if len(r.Result) < 1 {
		return ""
	}
	return string(r.Result[:])
}
