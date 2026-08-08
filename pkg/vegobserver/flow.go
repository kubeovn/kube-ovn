package vegobserver

import (
	"sort"
	"time"

	"github.com/ti-mo/conntrack"
	"golang.org/x/sys/unix"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

type flowKey struct {
	zone uint16
	id   uint32
}

type flowTuple struct {
	SourceIP        string `json:"sourceIP"`
	SourcePort      uint16 `json:"sourcePort,omitempty"`
	DestinationIP   string `json:"destinationIP"`
	DestinationPort uint16 `json:"destinationPort,omitempty"`
}

type flowCounters struct {
	OriginalPackets *uint64 `json:"originalPackets,omitempty"`
	OriginalBytes   *uint64 `json:"originalBytes,omitempty"`
	ReplyPackets    *uint64 `json:"replyPackets,omitempty"`
	ReplyBytes      *uint64 `json:"replyBytes,omitempty"`
}

type flowRecord struct {
	SchemaVersion  string        `json:"schemaVersion"`
	Timestamp      time.Time     `json:"timestamp"`
	Event          string        `json:"event"`
	ConntrackID    uint32        `json:"conntrackID"`
	Zone           uint16        `json:"zone"`
	Namespace      string        `json:"namespace"`
	Name           string        `json:"name"`
	Pod            string        `json:"pod"`
	Node           string        `json:"node"`
	AddressFamily  string        `json:"addressFamily"`
	Protocol       string        `json:"protocol"`
	ProtocolNumber uint8         `json:"protocolNumber"`
	NatType        []string      `json:"natType"`
	Original       flowTuple     `json:"original"`
	Translated     flowTuple     `json:"translated"`
	Counters       *flowCounters `json:"counters,omitempty"`
}

func recordFromFlow(flow *conntrack.Flow, identity observerIdentity) (flowRecord, bool) {
	if flow == nil {
		return flowRecord{}, false
	}
	original := flowTuple{
		SourceIP: flow.TupleOrig.IP.SourceAddress.String(), SourcePort: flow.TupleOrig.Proto.SourcePort,
		DestinationIP: flow.TupleOrig.IP.DestinationAddress.String(), DestinationPort: flow.TupleOrig.Proto.DestinationPort,
	}
	translated := flowTuple{
		SourceIP: flow.TupleReply.IP.DestinationAddress.String(), SourcePort: flow.TupleReply.Proto.DestinationPort,
		DestinationIP: flow.TupleReply.IP.SourceAddress.String(), DestinationPort: flow.TupleReply.Proto.SourcePort,
	}
	natTypes := make([]string, 0, 2)
	if flow.Status.SrcNAT() || original.SourceIP != translated.SourceIP || original.SourcePort != translated.SourcePort {
		natTypes = append(natTypes, apiv1.ObservabilityNatTypeSNAT)
	}
	if flow.Status.DstNAT() || original.DestinationIP != translated.DestinationIP || original.DestinationPort != translated.DestinationPort {
		natTypes = append(natTypes, apiv1.ObservabilityNatTypeDNAT)
	}
	if len(natTypes) == 0 {
		return flowRecord{}, false
	}
	sort.Strings(natTypes)
	addressFamily := apiv1.ObservabilityAddressFamilyIPv6
	if flow.TupleOrig.IP.SourceAddress.Is4() {
		addressFamily = apiv1.ObservabilityAddressFamilyIPv4
	}
	return flowRecord{
		SchemaVersion: "v1", Timestamp: time.Now().UTC(), ConntrackID: flow.ID, Zone: flow.Zone,
		Namespace: identity.namespace, Name: identity.name, Pod: identity.pod, Node: identity.node,
		AddressFamily: addressFamily, Protocol: protocolName(flow.TupleOrig.Proto.Protocol), ProtocolNumber: flow.TupleOrig.Proto.Protocol,
		NatType: natTypes, Original: original, Translated: translated, Counters: countersFromFlow(flow),
	}, true
}

func protocolName(number uint8) string {
	switch number {
	case unix.IPPROTO_TCP:
		return apiv1.ObservabilityProtocolTCP
	case unix.IPPROTO_UDP:
		return apiv1.ObservabilityProtocolUDP
	case unix.IPPROTO_SCTP:
		return apiv1.ObservabilityProtocolSCTP
	case unix.IPPROTO_ICMP:
		return apiv1.ObservabilityProtocolICMP
	case unix.IPPROTO_ICMPV6:
		return apiv1.ObservabilityProtocolICMPv6
	default:
		return apiv1.ObservabilityProtocolOther
	}
}

func countersFromFlow(flow *conntrack.Flow) *flowCounters {
	if !accountingAvailable(flow) {
		return nil
	}
	return &flowCounters{
		OriginalPackets: new(flow.CountersOrig.Packets), OriginalBytes: new(flow.CountersOrig.Bytes),
		ReplyPackets: new(flow.CountersReply.Packets), ReplyBytes: new(flow.CountersReply.Bytes),
	}
}

func accountingAvailable(flow *conntrack.Flow) bool {
	return flow.CountersOrig.Packets != 0 || flow.CountersOrig.Bytes != 0 || flow.CountersReply.Packets != 0 || flow.CountersReply.Bytes != 0
}

func metricNatType(natTypes []string) string {
	if len(natTypes) == 2 {
		return apiv1.ObservabilityNatTypeSNATDNAT
	}
	return natTypes[0]
}
