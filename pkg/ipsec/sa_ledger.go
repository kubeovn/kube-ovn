package ipsec

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"uuid"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"github.com/kubeovn/kube-ovn/pkg/fileutil"
)

type connectionClaim struct {
	InterfaceUUID string `json:"interfaceUUID"`
	LocalIP       string `json:"localIP"`
	RemoteIP      string `json:"remoteIP"`
	Reqid         uint32 `json:"reqid"`
	MarkOut       string `json:"markOut"`
	TunnelType    string `json:"tunnelType"`
}

type connectionIntent struct {
	connectionSession
	Connections map[string]connectionClaim `json:"connections"`
}

func (session connectionSession) loadIntent(owner store) (*connectionIntent, error) {
	data, err := readRegularFile(session.intentPath(owner))
	if err != nil {
		return nil, err
	}
	var intent connectionIntent
	if err := json.Unmarshal(data, &intent); err != nil {
		return nil, err
	}
	boot, bootErr := uuid.Parse(session.BootID)
	if intent.connectionSession != session || intent.Connections == nil || bootErr != nil || boot == uuid.Nil() || boot.String() != session.BootID {
		return nil, errors.New("connection intent does not bind the current runtime session")
	}
	for name, claim := range intent.Connections {
		if !strings.HasPrefix(name, session.Prefix) || !connectionName.MatchString(name) || !ovsdb.IsValidUUID(claim.InterfaceUUID) ||
			claim.Reqid != session.Reqid || claim.MarkOut != fmt.Sprintf("%d/0xffffffff", session.Mark) ||
			(claim.TunnelType != "geneve" && claim.TunnelType != "vxlan") ||
			net.ParseIP(claim.LocalIP) == nil || net.ParseIP(claim.RemoteIP) == nil {
			return nil, errors.New("invalid owned connection intent")
		}
	}
	return &intent, nil
}

var (
	connectionName = regexp.MustCompile(`^ko[A-Za-z0-9]{20,64}-.+-(?:in|out)-[1-9][0-9]*$`)
	installedChild = regexp.MustCompile(`^(.+)\{([1-9][0-9]*)\}:\s+INSTALLED, TRANSPORT, reqid ([0-9]+), ESP( in UDP)? SPIs: ([0-9a-fA-F]{8})_i ([0-9a-fA-F]{8})_o$`)
)

type childAssociation struct {
	Connection  string
	UniqueID    uint64
	InboundSPI  uint32
	OutboundSPI uint32
	UDPEncap    bool
}

// Parse only the installed ESP transport format documented by stroke_list.c.
// Unknown owned formats are errors, never a claim that the runtime has no SAs.
func (intent *connectionIntent) installedChildren(status []byte) (map[string]childAssociation, error) {
	children := make(map[string]childAssociation)
	for line := range strings.SplitSeq(string(status), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, intent.Prefix) || !strings.Contains(line, "INSTALLED,") {
			continue
		}
		matches := installedChild.FindStringSubmatch(line)
		if matches == nil {
			return nil, errors.New("unsupported owned CHILD_SA status")
		}
		claim, found := intent.Connections[matches[1]]
		if !found {
			return nil, errors.New("installed connection has no durable intent")
		}
		id, errID := strconv.ParseUint(matches[2], 10, 64)
		reqid, errReqid := strconv.ParseUint(matches[3], 10, 32)
		inbound, errIn := strconv.ParseUint(matches[5], 16, 32)
		outbound, errOut := strconv.ParseUint(matches[6], 16, 32)
		if err := errors.Join(errID, errReqid, errIn, errOut); err != nil {
			return nil, err
		}
		if uint32(reqid) != claim.Reqid || inbound == 0 || outbound == 0 {
			return nil, errors.New("installed CHILD_SA conflicts with connection intent")
		}
		key := matches[1] + "{" + matches[2] + "}"
		if _, exists := children[key]; exists {
			return nil, errors.New("duplicate installed CHILD_SA identity")
		}
		children[key] = childAssociation{Connection: matches[1], UniqueID: id, InboundSPI: uint32(inbound), OutboundSPI: uint32(outbound), UDPEncap: matches[4] != ""}
	}
	return children, nil
}

// These types intentionally contain only public kernel metadata. Never embed
// XfrmState: its String method and JSON fields include authentication/SA keys.
type saSelector struct {
	Source          string `json:"source"`
	Destination     string `json:"destination"`
	Protocol        int    `json:"protocol"`
	SourcePort      int    `json:"sourcePort"`
	DestinationPort int    `json:"destinationPort"`
	InterfaceIndex  int    `json:"interfaceIndex"`
}

type saEncapsulation struct {
	Type            int    `json:"type"`
	SourcePort      int    `json:"sourcePort"`
	DestinationPort int    `json:"destinationPort"`
	OriginalAddress string `json:"originalAddress"`
}

type saInstance struct {
	Source           string          `json:"source"`
	Destination      string          `json:"destination"`
	SPI              uint32          `json:"spi"`
	Reqid            uint32          `json:"reqid"`
	Protocol         int             `json:"protocol"`
	Mode             int             `json:"mode"`
	InterfaceID      int             `json:"interfaceID"`
	Added            uint64          `json:"added"`
	Mark             uint32          `json:"mark"`
	MarkMask         uint32          `json:"markMask"`
	OutputMark       uint32          `json:"outputMark"`
	OutputMask       uint32          `json:"outputMask"`
	HasMark          bool            `json:"hasMark"`
	HasOutputMark    bool            `json:"hasOutputMark"`
	Selector         saSelector      `json:"selector"`
	Encapsulation    saEncapsulation `json:"encapsulation"`
	HasEncapsulation bool            `json:"hasEncapsulation"`
}

type saBinding struct {
	Connection string     `json:"connection"`
	ChildID    uint64     `json:"childID"`
	Direction  string     `json:"direction"`
	Instance   saInstance `json:"instance"`
}

type saLedger struct {
	connectionSession
	Bindings []saBinding `json:"bindings"`
}

func kernelInstance(state netlink.XfrmState) saInstance {
	instance := saInstance{Source: state.Src.String(), Destination: state.Dst.String(), SPI: stateSPI(state), Reqid: uint32(state.Reqid), Protocol: int(state.Proto), Mode: int(state.Mode), InterfaceID: state.Ifid, Added: state.Statistics.AddTime} // #nosec G115 -- reqid is validated against the positive 31-bit lease before persistence.
	if state.Mark != nil {
		instance.HasMark, instance.Mark, instance.MarkMask = true, state.Mark.Value, state.Mark.Mask
	}
	if state.OutputMark != nil {
		instance.HasOutputMark, instance.OutputMark, instance.OutputMask = true, state.OutputMark.Value, state.OutputMark.Mask
	}
	if state.Selector != nil {
		selector := state.Selector
		instance.Selector = saSelector{Source: selector.Src.String(), Destination: selector.Dst.String(), Protocol: int(selector.Proto), SourcePort: selector.SrcPort, DestinationPort: selector.DstPort, InterfaceIndex: selector.Ifindex}
	}
	if state.Encap != nil {
		encap := state.Encap
		instance.HasEncapsulation = true
		instance.Encapsulation = saEncapsulation{Type: int(encap.Type), SourcePort: encap.SrcPort, DestinationPort: encap.DstPort, OriginalAddress: encap.OriginalAddress.String()}
	}
	return instance
}

func (intent *connectionIntent) bindChild(child childAssociation, direction string, states []netlink.XfrmState) (saBinding, error) {
	if direction != "in" && direction != "out" {
		return saBinding{}, errors.New("invalid CHILD_SA direction")
	}
	claim := intent.Connections[child.Connection]
	source, destination, spi := claim.RemoteIP, claim.LocalIP, child.InboundSPI
	if direction == "out" {
		source, destination, spi = claim.LocalIP, claim.RemoteIP, child.OutboundSPI
	}
	sourceIP, destinationIP := net.ParseIP(source), net.ParseIP(destination)
	var matched *saInstance
	for _, state := range states {
		if stateSPI(state) != spi || !state.Src.Equal(sourceIP) || !state.Dst.Equal(destinationIP) {
			continue
		}
		if state.Proto != netlink.XFRM_PROTO_ESP || state.Mode != netlink.XFRM_MODE_TRANSPORT || state.Reqid != int(intent.Reqid) || state.Ifid != 0 || state.Statistics.AddTime == 0 || (state.Encap != nil) != child.UDPEncap {
			return saBinding{}, errors.New("CHILD_SA has conflicting kernel metadata")
		}
		if state.Encap != nil && (state.Encap.Type != netlink.XFRM_ENCAP_ESPINUDP || state.Encap.SrcPort <= 0 || state.Encap.SrcPort > 65535 || state.Encap.DstPort <= 0 || state.Encap.DstPort > 65535) {
			return saBinding{}, errors.New("CHILD_SA has unsupported UDP encapsulation")
		}
		if direction == "out" && (state.Mark == nil || state.Mark.Value != intent.Mark || state.Mark.Mask != ^uint32(0)) {
			return saBinding{}, errors.New("CHILD_SA does not use the reserved output mark")
		}
		if (direction == "in" && state.Mark != nil) || state.OutputMark != nil {
			return saBinding{}, errors.New("CHILD_SA has unconfigured kernel marks")
		}
		instance := kernelInstance(state)
		if !matchesTransportSelector(instance.Selector, claim, child.Connection, direction) {
			return saBinding{}, errors.New("CHILD_SA lacks the intended transport UDP selector")
		}
		if matched != nil {
			return saBinding{}, errors.New("ambiguous CHILD_SA kernel instance")
		}
		matched = &instance
	}
	if matched == nil {
		return saBinding{}, errors.New("CHILD_SA kernel instance changed during observation")
	}
	return saBinding{Connection: child.Connection, ChildID: child.UniqueID, Direction: direction, Instance: *matched}, nil
}

func stateSPI(state netlink.XfrmState) uint32 {
	// Netlink exposes the unsigned 32-bit SPI in an int. On 32-bit systems,
	// converting it back must preserve the original wire representation.
	return uint32(state.Spi) // #nosec G115 -- restores an unsigned 32-bit netlink field, not an arithmetic result.
}

func matchesTransportSelector(selector saSelector, claim connectionClaim, connection, direction string) bool {
	port := 6081
	if claim.TunnelType == "vxlan" {
		port = 4789
	}
	stem, _, _ := strings.CutLast(connection, "-")
	localPort, remotePort := 0, port
	if strings.HasSuffix(stem, "-in") {
		localPort, remotePort = port, 0
	}
	source, destination, sourcePort, destinationPort := claim.RemoteIP, claim.LocalIP, remotePort, localPort
	if direction == "out" {
		source, destination, sourcePort, destinationPort = claim.LocalIP, claim.RemoteIP, localPort, remotePort
	}
	return selector.Protocol == unix.IPPROTO_UDP && selector.InterfaceIndex == 0 &&
		selector.SourcePort == sourcePort && selector.DestinationPort == destinationPort &&
		exactHostSelector(selector.Source, source) && exactHostSelector(selector.Destination, destination)
}

func exactHostSelector(selector, address string) bool {
	_, network, err := net.ParseCIDR(selector)
	if err != nil {
		return false
	}
	ones, bits := network.Mask.Size()
	return ones == bits && network.IP.Equal(net.ParseIP(address))
}

func (session connectionSession) loadLedger(owner store) (*saLedger, error) {
	ledger := &saLedger{connectionSession: session}
	data, err := readRegularFile(filepath.Join(filepath.Dir(session.intentPath(owner)), "sa-ledger.json"))
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return ledger, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, ledger); err != nil {
		return nil, err
	}
	if ledger.connectionSession != session {
		return nil, errors.New("SA ledger belongs to a different runtime session")
	}
	return ledger, nil
}

func (ledger *saLedger) save(owner store) error {
	data, err := json.Marshal(ledger)
	if err != nil {
		return err
	}
	if len(data) > 1<<20 {
		return errors.New("IPsec SA ledger exceeds the private storage limit")
	}
	return fileutil.AtomicWriteFile(filepath.Join(filepath.Dir(ledger.intentPath(owner)), "sa-ledger.json"), data, 0o600)
}
