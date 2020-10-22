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

// Package ovsdb implements OVSDB (JSON-RPC 1.0) Client
// per RFC 7047.
package ovsdb

import (
	"encoding/json"
	"fmt"
	//"github.com/davecgh/go-spew/spew"
	"io"
	//"math/rand"
	"net"
	"net/rpc"
	"reflect"
	"sync"
	"time"
)

// Client DOCS-TBD
type Client struct {
	mux        sync.Mutex
	Endpoint   string
	Timeout    int
	MaxRetries int
	Schemas    map[string]Schema
	References map[string]map[string]map[string]string
	txQueue    chan Request
	rxQueue    chan Response
	errQueue   chan error
	closed     bool
}

// NewClient TODO
func NewClient(s string, t int) (Client, error) {
	cli := Client{}
	cli.Endpoint = s
	cli.Timeout = t
	cli.MaxRetries = 2
	cli.Schemas = make(map[string]Schema)
	cli.References = make(map[string]map[string]map[string]string)
	// send only channel
	cli.txQueue = make(chan Request, 1)
	// receive only channels
	cli.rxQueue = make(chan Response, 1)
	cli.errQueue = make(chan error, 1)
	go ovsdbMessenger(cli.Endpoint, cli.Timeout, cli.txQueue, cli.rxQueue, cli.errQueue)
	err := <-cli.errQueue
	return cli, err
}

// Close TODO
func (cli *Client) Close() error {
	_, err := cli.query("shutdown", nil)
	return err
}

func (cli *Client) query(method string, param interface{}) (*Response, error) {
	if cli == nil {
		return nil, fmt.Errorf("client was not initialized")
	}
	if method == "shutdown" && cli.closed {
		return nil, nil
	}
	cli.mux.Lock()
	defer cli.mux.Unlock()
	errMsgs := []string{}
	req := Request{
		Method: method,
		Params: param,
	}
	for {
		if cli.closed == false {
			cli.txQueue <- req
			select {
			case err := <-cli.errQueue:
				cli.closed = true
				if method == "shutdown" {
					return nil, nil
				}
				errMsgs = append(errMsgs, err.Error())
			case resp := <-cli.rxQueue:
				return &resp, nil
			}
		}
		retryAttempts := cli.MaxRetries
		for {
			if cli.closed {
				go ovsdbMessenger(cli.Endpoint, cli.Timeout, cli.txQueue, cli.rxQueue, cli.errQueue)
				err := <-cli.errQueue
				if err == nil {
					cli.closed = false
					break
				}
			}
			if retryAttempts < 1 {
				if len(errMsgs) == 0 {
					return nil, fmt.Errorf("client unavailable")
				}
				return nil, fmt.Errorf("%s", errMsgs)
			}
			retryAttempts--
		}
	}
	return nil, fmt.Errorf("%s", errMsgs)
}

func (cli *Client) getColumns(db, table string) (map[string]string, error) {
	if _, dbExists := cli.References[db]; dbExists {
		if _, tblExists := cli.References[db][table]; tblExists {
			return cli.References[db][table], nil
		}
	}
	schema, err := cli.GetSchema(db)
	if err != nil {
		return make(map[string]string), err
	}
	cli.References[db] = make(map[string]map[string]string)
	columns, err := schema.GetColumnsTypes(table)
	if err != nil {
		return columns, err
	}
	cli.References[db][table] = columns
	return columns, nil
}

type ovsdbCodec struct {
	dec *json.Decoder // for reading JSON values
	enc *ovsdbEncoder // for writing OVSDB requests
	c   io.Closer

	// temporary work space
	req  clientRequest
	resp clientResponse

	// JSON-RPC responses include the request id but not the request method.
	// Package rpc expects both.
	// We save the request method in pending when sending a request
	// and then look it up by request ID when filling out the rpc Response.
	mutex   sync.Mutex        // protects pending
	pending map[uint64]string // map request id to method name
}

// newClientCodec returns a new rpc.ClientCodec using JSON-RPC on conn.
func newClientCodec(conn io.ReadWriteCloser) rpc.ClientCodec {
	return &ovsdbCodec{
		dec:     json.NewDecoder(conn),
		enc:     newOvsdbEncoder(conn),
		c:       conn,
		pending: make(map[uint64]string),
	}
}

type clientRequest struct {
	Method string `json:"method"`
	ID     uint64 `json:"id"`
	//ID     interface{}    `json:"id"`
	Params [1]interface{} `json:"params"`
}

func (c *ovsdbCodec) WriteRequest(r *rpc.Request, param interface{}) error {
	c.req.Method = r.ServiceMethod
	c.req.ID = r.Seq
	c.req.Params[0] = param
	if r.ServiceMethod != "echo" && r.Seq != 0 {
		c.mutex.Lock()
		c.pending[r.Seq] = r.ServiceMethod
		c.mutex.Unlock()
	}
	return c.enc.Encode(&c.req)
}

type clientResponse struct {
	//ID     uint64           `json:"id"`
	ID     interface{}      `json:"id"`
	Result *json.RawMessage `json:"result"`
	Error  interface{}      `json:"error"`
}

func (r *clientResponse) reset() {
	r.ID = 0
	r.Result = nil
	r.Error = nil
}

func (c *ovsdbCodec) ReadResponseHeader(r *rpc.Response) error {
	c.resp.reset()
	if err := c.dec.Decode(&c.resp); err != nil {
		if err.Error() == "EOF" {
			return nil
		}
		return fmt.Errorf("codec decode: %v", err)
	}
	//spew.Dump(c.resp)

	if reflect.ValueOf(c.resp.ID).Kind() == reflect.Float64 {
		c.mutex.Lock()
		r.Seq = uint64(c.resp.ID.(float64))
		r.ServiceMethod = c.pending[r.Seq]
		delete(c.pending, r.Seq)
		c.mutex.Unlock()
	} else {
		//spew.Dump(c)
		r.ServiceMethod = "echo"
		r.Seq = 0
		return nil
	}

	r.Error = ""
	if c.resp.Error != nil || c.resp.Result == nil {
		x, ok := c.resp.Error.(string)
		if !ok {
			return fmt.Errorf("invalid error %v", c.resp.Error)
		}
		if x == "" {
			x = "unspecified error"
		}
		r.Error = x
	}
	return nil
}

func (c *ovsdbCodec) ReadResponseBody(x interface{}) error {
	if x == nil {
		return nil
	}
	return json.Unmarshal(*c.resp.Result, x)
}

func (c *ovsdbCodec) Close() error {
	return c.c.Close()
}

func ovsdbMessenger(s string, t int, rxQueue <-chan Request, txQueue chan<- Response, errQueue chan<- error) {
	var counter uint64 = 1
	var resp rpc.Response
	var respMsg Response
	if t == 0 {
		t = 2
	}
	serverProto, serverAddr, err := parseSocket(s)
	if err != nil {
		errQueue <- err
		return
	}
	dialer := net.Dialer{
		Timeout: time.Second * time.Duration(t),
	}
	conn, err := dialer.Dial(serverProto, serverAddr)
	if err != nil {
		errQueue <- err
		return
	}
	errQueue <- nil
	cli := newClientCodec(conn)
	for {
		select {
		case reqMsg := <-rxQueue:
			var req rpc.Request
			if reqMsg.Method == "shutdown" {
				cli.Close()
				errQueue <- nil
				return
			}
			req.ServiceMethod = reqMsg.Method
			req.Seq = counter
			if err := cli.WriteRequest(&req, reqMsg.Params); err != nil {
				errQueue <- err
				return
			}
		}
		if err := cli.ReadResponseHeader(&resp); err != nil {
			errQueue <- err
			return
		}
		if resp.ServiceMethod == "echo" && resp.Seq == 0 {
			// handling server echo
			var req rpc.Request
			req.ServiceMethod = resp.ServiceMethod
			req.Seq = resp.Seq
			if err := cli.WriteRequest(&req, nil); err != nil {
				errQueue <- err
				return
			}
			if err := cli.ReadResponseHeader(&resp); err != nil {
				errQueue <- err
				return
			}
		} else {
			if resp.Seq != counter {
				errQueue <- fmt.Errorf("sequence mismatch: %v (request) vs. %v (response)", counter, resp.Seq)
				return
			}
			counter++
		}
		if resp.Error != "" {
			errQueue <- fmt.Errorf("error in response header: %s", resp.Error)
			return
		}
		if err := cli.ReadResponseBody(&respMsg); err != nil {
			errQueue <- fmt.Errorf("decode body error: %v", err)
			return
		}
		if respMsg.Error.Message != "" {
			errQueue <- fmt.Errorf("error in response body: %s", respMsg.Error.String())
			return
		}
		txQueue <- respMsg
	}
	return
}
