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
)

// Echo - TODO
func (c *Client) Echo(s string) error {
	method := "echo"
	js, err := encodeString(s)
	if err != nil {
		fmt.Errorf("'%s' method failed: %v", method, err)
	}
	response, err := c.query(method, js)
	if err != nil {
		return fmt.Errorf("'%s' method failed: %v", method, err)
	}
	if err := matchRequestResponse(s, response); err != nil {
		return fmt.Errorf("'%s' method failed: %v", method, err)
	}
	return nil
}

func matchRequestResponse(req string, resp *Response) error {
	var respBody []string
	if err := json.Unmarshal(resp.Result, &respBody); err != nil {
		return fmt.Errorf("response (%s) decoding failed: %s",
			resp.Result, err)
	}
	if len(respBody) != 1 {
		return fmt.Errorf("parameters mismatch, request (%s) did not match response (%s)",
			req, respBody)
	}
	if req != respBody[0] {
		return fmt.Errorf("parameters mismatch, request (%s) did not match response (%s)",
			req, respBody)
	}
	return nil
}
