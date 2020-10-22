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
	"fmt"
	//"github.com/davecgh/go-spew/spew"
	"io"
	"strconv"
	"sync"
)

type method struct {
	Name string
}

var methods = map[string]method{
	"echo":                 {Name: "echo"},
	"list_dbs":             {Name: "list_dbs"},
	"get_schema":           {Name: "get_schema"},
	"transact":             {Name: "transact"},
	"list-commands":        {Name: "list-commands"},
	"coverage/show":        {Name: "coverage/show"},
	"memory/show":          {Name: "memory/show"},
	"cluster/status":       {Name: "cluster/status"},
	"dpif/show":            {Name: "dpif/show"},
	"dpctl/show":           {Name: "dpctl/show"},
	"ofproto/list-tunnels": {Name: "ofproto/list-tunnels"},
	"dpctl/dump-flows":     {Name: "dpctl/dump-flows"},
}

// An ovsdbEncoder writes JSON values to an output stream.
type ovsdbEncoder struct {
	w   io.Writer
	err error
}

// newOvsdbEncoder returns a new encoder that writes to w.
func newOvsdbEncoder(w io.Writer) *ovsdbEncoder {
	return &ovsdbEncoder{w: w}
}

// An encodeState encodes JSON into a bytes.Buffer.
type encodeState struct {
	bytes.Buffer // accumulated output
	scratch      [64]byte
}

var encodeStatePool sync.Pool

func newEncodeState() *encodeState {
	if v := encodeStatePool.Get(); v != nil {
		e := v.(*encodeState)
		e.Reset()
		return e
	}
	return new(encodeState)
}

func (enc *ovsdbEncoder) Encode(v interface{}) error {
	//spew.Dump("ovsdbEncoder.Encode() - Start")
	if enc.err != nil {
		return enc.err
	}
	e := newEncodeState()
	r := v.(*clientRequest)
	if _, exists := methods[r.Method]; !exists {
		err := fmt.Errorf("encoding error: unsupported method: %s", r.Method)
		enc.err = err
		return err
	}
	//spew.Dump(r)
	e.WriteByte('{')
	if r.Method == "echo" && r.ID == 0 {
		// handle inactivity probe
		e.WriteString("\"id\":\"echo\",\"error\":null,\"result\":[]")
	} else {
		e.WriteString("\"method\":\"" + r.Method + "\",")
		e.WriteString("\"id\":" + strconv.FormatUint(r.ID, 10) + ",")
		e.WriteString("\"params\":[")
		switch r.Method {
		case "list_dbs":
			e.WriteString("[]")
		case "echo":
			if r.ID != 0 {
				s := r.Params[0].(string)
				e.WriteString(s)
			}
		case "get_schema":
			s := r.Params[0].(string)
			e.WriteString(s)
		case "transact":
			t := r.Params[0].(Transaction)
			s, err := t.ToString()
			if err != nil {
				return fmt.Errorf("encoding error: params handler: %s", r.Method)
			}
			e.WriteString(s)
			// e.WriteString("\"Open_vSwitch\",{\"op\":\"select\",\"table\":\"Open_vSwitch\",\"where\":[]}")
		case "list-commands":
		case "coverage/show":
		case "memory/show":
		case "dpif/show":
		case "dpctl/show":
		case "ofproto/list-tunnels":
		case "dpctl/dump-flows":
		case "cluster/status":
			s := r.Params[0].(string)
			e.WriteString("\"" + s + "\"")
		default:
			return fmt.Errorf("encoding error: params handler: %s", r.Method)
		}
		e.WriteString("]")
	}
	e.WriteByte('}')
	b := e.Bytes()
	//spew.Dump(b)
	_, err := enc.w.Write(b)
	if err != nil {
		enc.err = err
	}
	encodeStatePool.Put(e)
	return err
}
