package rpc

import (
	"fmt"
	"net/rpc"
)

type Client struct {
	*rpc.Client
}

func (c *Client) Close() error {
	return c.Client.Close()
}

func (c *Client) Call(method string, args, reply any) error {
	return c.Client.Call(method, args, reply)
}

func NewClient(addr string) (*Client, error) {
	client, err := rpc.DialHTTP("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to dial %q: %w", addr, err)
	}

	return &Client{client}, nil
}

func Call(addr, method string, args, reply any) error {
	client, err := NewClient(addr)
	if err != nil {
		return fmt.Errorf("failed to create rpc client for %q: %w", addr, err)
	}
	defer client.Close()

	return client.Call(method, args, reply)
}
