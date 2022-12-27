package iproute

import (
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"strings"

	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
)

type LinkInfo struct {
	InfoKind string `json:"info_kind"`
}

type AddrInfo struct {
	Family            string `json:"family"`
	Local             string `json:"local"`
	PrefixLen         int    `json:"prefixlen"`
	Broadcast         string `json:"broadcast,omitempty"`
	Scope             string `json:"scope"`
	Label             string `json:"label,omitempty"`
	ValidLifeTime     int64  `json:"valid_life_time"`
	PreferredLifeTime int64  `json:"preferred_life_time"`
	NoDAD             bool   `json:"nodad,omitempty"`
}

type Link struct {
	IfIndex     int        `json:"ifindex"`
	LinkIndex   int        `json:"link_index"`
	IfName      string     `json:"ifname"`
	Flags       []string   `json:"flags"`
	Mtu         int        `json:"mtu"`
	Qdisc       string     `json:"qdisc"`
	Master      string     `json:"master"`
	OperState   string     `json:"operstate"`
	Group       string     `json:"group"`
	LinkType    string     `json:"link_type"`
	Address     string     `json:"address"`
	Broadcast   string     `json:"broadcast"`
	LinkNetnsID int        `json:"link_netnsid"`
	Promiscuity int        `json:"promiscuity"`
	MinMtu      int        `json:"min_mtu"`
	MaxMtu      int        `json:"max_mtu"`
	LinkInfo    LinkInfo   `json:"linkinfo"`
	NumTxQueues int        `json:"num_tx_queues"`
	NumRxQueues int        `json:"num_rx_queues"`
	GsoMaxSize  int        `json:"gso_max_size"`
	GsoMaxSegs  int        `json:"gso_max_segs"`
	AddrInfo    []AddrInfo `json:"addr_info"`
}

func (l *Link) NonLinkLocalAddresses() []string {
	var result []string
	for _, addr := range l.AddrInfo {
		if !net.ParseIP(addr.Local).IsLinkLocalUnicast() {
			result = append(result, fmt.Sprintf("%s/%d", addr.Local, addr.PrefixLen))
		}
	}
	return result
}

type Route struct {
	Type     string        `json:"type"`
	Dst      string        `json:"dst"`
	Gateway  string        `json:"gateway,omitempty"`
	Dev      string        `json:"dev"`
	Protocol string        `json:"protocol"`
	Scope    string        `json:"scope"`
	Metric   int           `json:"metric"`
	Flags    []interface{} `json:"flags"`
	PrefSrc  string        `json:"prefsrc,omitempty"`
	Pref     string        `json:"pref"`
}

type Rule struct {
	Priority int    `json:"priority"`
	Src      string `json:"src"`
	Table    string `json:"table"`
	Protocol string `json:"protocol"`
	SrcLen   int    `json:"srclen,omitempty"`
}

type ExecFunc func(cmd ...string) (stdout, stderr []byte, err error)

type execer struct {
	fn            ExecFunc
	ignoredErrors []reflect.Type
}

func (e *execer) exec(cmd string, result interface{}) error {
	stdout, stderr, err := e.fn(strings.Fields(cmd)...)
	if err != nil {
		t := reflect.TypeOf(err)
		for _, err := range e.ignoredErrors {
			if t == err {
				return nil
			}
		}
		return fmt.Errorf("failed to exec cmd %q: %v\nstdout:\n%s\nstderr:\n%s", cmd, err, stdout, stderr)
	}

	if err = json.Unmarshal(stdout, result); err != nil {
		return fmt.Errorf("failed to decode json %q: %v", string(stdout), err)
	}

	return nil
}

func devArg(device string) string {
	if device == "" {
		return ""
	}
	return " dev " + device
}

func AddressShow(device string, execFunc ExecFunc) ([]Link, error) {
	var links []Link
	e := execer{fn: execFunc}
	if err := e.exec("ip -d -j address show"+devArg(device), &links); err != nil {
		return nil, err
	}

	return links, nil
}

func RouteShow(table, device string, execFunc ExecFunc) ([]Route, error) {
	e := execer{fn: execFunc}
	var args string
	if table != "" {
		// ignore the following error:
		// Error: ipv4/ipv6: FIB table does not exist.
		// Dump terminated
		e.ignoredErrors = append(e.ignoredErrors, reflect.TypeOf(docker.ErrNonZeroExitCode{}))
		args = " table " + table
	}
	args += devArg(device)

	var routes []Route
	if err := e.exec("ip -d -j route show"+args, &routes); err != nil {
		return nil, err
	}

	var routes6 []Route
	if err := e.exec("ip -d -j -6 route show"+args, &routes6); err != nil {
		return nil, err
	}

	return append(routes, routes6...), nil
}

func RuleShow(device string, execFunc ExecFunc) ([]Rule, error) {
	e := execer{fn: execFunc}

	var rules []Rule
	if err := e.exec("ip -d -j rule show"+devArg(device), &rules); err != nil {
		return nil, err
	}

	var rules6 []Rule
	if err := e.exec("ip -d -j -6 rule show"+devArg(device), &rules6); err != nil {
		return nil, err
	}
	return append(rules, rules6...), nil
}
