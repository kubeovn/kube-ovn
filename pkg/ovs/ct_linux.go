package ovs

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// CTEntry represents a conntrack entry as reported by dpctl/dump-conntrack.
type CTEntry struct {
	Zone     int
	Proto    int // IANA protocol number
	SrcIP    net.IP
	DstIP    net.IP
	SrcPort  int // 0 for non-TCP/UDP/ICMP-id
	DstPort  int // 0 for non-TCP/UDP
	ICMPType int // -1 if not ICMP
	ICMPCode int // -1 if not ICMP
	ICMPID   int // -1 if not ICMP; used as ct_tp_src in flush tuple
}

// GetCTZones returns the OVN conntrack zone mapping: logical switch name → zone ID.
// It calls ovn-appctl -t ovn-controller ct-zone-list.
func GetCTZones() (map[string]int, error) {
	output, err := Appctl(OvnController, "ct-zone-list")
	if err != nil {
		return nil, fmt.Errorf("failed to get ct-zone-list: %w", err)
	}

	zones := make(map[string]int)
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Format: "<datapath-name> <zone-id>"
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		zoneID, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		zones[fields[0]] = zoneID
	}
	return zones, nil
}

// DumpCTEntriesForZone returns all conntrack entries for the given zone.
func DumpCTEntriesForZone(zone int) ([]CTEntry, error) {
	output, err := Appctl(OvsVswitchd, "dpctl/dump-conntrack", fmt.Sprintf("zone=%d", zone))
	if err != nil {
		return nil, fmt.Errorf("failed to dump conntrack for zone %d: %w", zone, err)
	}
	return parseCTEntries(zone, output), nil
}

// FlushCTEntry removes a specific conntrack entry by its precise tuple within a zone.
// The tuple is passed as a single comma-separated argument per the dpctl syntax:
//
//	dpctl/flush-conntrack zone=N "ct_nw_src=X,ct_nw_dst=Y,ct_nw_proto=P[,ct_tp_src=S,ct_tp_dst=D]"
//
// Partial matching is supported: any fields omitted will match all values.
func FlushCTEntry(entry CTEntry) error {
	tuple := buildTupleArg(entry)
	_, err := Appctl(OvsVswitchd, "dpctl/flush-conntrack",
		fmt.Sprintf("zone=%d", entry.Zone),
		tuple,
	)
	if err != nil {
		return fmt.Errorf("failed to flush conntrack entry: %w", err)
	}
	return nil
}

// buildTupleArg builds the comma-separated tuple argument for dpctl/flush-conntrack.
// Fields are joined as a single argument: "ct_nw_src=X,ct_nw_dst=Y,ct_nw_proto=P,..."
func buildTupleArg(e CTEntry) string {
	var fields []string

	isIPv6 := e.SrcIP.To4() == nil
	if isIPv6 {
		fields = append(fields, fmt.Sprintf("ct_ipv6_src=%s", e.SrcIP))
		fields = append(fields, fmt.Sprintf("ct_ipv6_dst=%s", e.DstIP))
	} else {
		fields = append(fields, fmt.Sprintf("ct_nw_src=%s", e.SrcIP))
		fields = append(fields, fmt.Sprintf("ct_nw_dst=%s", e.DstIP))
	}
	fields = append(fields, fmt.Sprintf("ct_nw_proto=%d", e.Proto))

	if e.ICMPType >= 0 {
		fields = append(fields, fmt.Sprintf("icmp_type=%d", e.ICMPType))
		fields = append(fields, fmt.Sprintf("icmp_code=%d", e.ICMPCode))
		if e.ICMPID >= 0 {
			fields = append(fields, fmt.Sprintf("icmp_id=%d", e.ICMPID))
		}
	} else {
		if e.SrcPort != 0 {
			fields = append(fields, fmt.Sprintf("ct_tp_src=%d", e.SrcPort))
		}
		if e.DstPort != 0 {
			fields = append(fields, fmt.Sprintf("ct_tp_dst=%d", e.DstPort))
		}
	}

	return strings.Join(fields, ",")
}

func parseCTEntries(zone int, output string) []CTEntry {
	var entries []CTEntry
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		entry, ok := parseCTLine(zone, line)
		if ok {
			entries = append(entries, entry)
		}
	}
	return entries
}

// parseCTLine parses a single conntrack dump line.
// Example lines:
//
//	tcp,orig=(src=10.16.0.5,dst=10.16.0.42,sport=54321,dport=8080),reply=...
//	udp,orig=(src=10.16.0.5,dst=10.16.0.42,sport=1234,dport=53),reply=...
//	icmp,orig=(src=10.16.0.5,dst=10.16.0.42,id=1,type=8,code=0),reply=...
func parseCTLine(zone int, line string) (CTEntry, bool) {
	e := CTEntry{Zone: zone, ICMPType: -1, ICMPCode: -1, ICMPID: -1}

	// Extract protocol name before the first comma
	protoEnd := strings.IndexByte(line, ',')
	if protoEnd < 0 {
		return e, false
	}
	protoName := line[:protoEnd]
	e.Proto = CTProtoNumber(protoName)
	if e.Proto == 0 && protoName != "icmp" && protoName != "icmpv6" {
		return e, false
	}

	// Find orig=(...) tuple
	origStart := strings.Index(line, "orig=(")
	if origStart < 0 {
		return e, false
	}
	origEnd := strings.Index(line[origStart:], ")")
	if origEnd < 0 {
		return e, false
	}
	orig := line[origStart+len("orig=(") : origStart+origEnd]

	for field := range strings.SplitSeq(orig, ",") {
		kv := strings.SplitN(field, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "src":
			e.SrcIP = net.ParseIP(kv[1])
		case "dst":
			e.DstIP = net.ParseIP(kv[1])
		case "sport":
			e.SrcPort, _ = strconv.Atoi(kv[1])
		case "dport":
			e.DstPort, _ = strconv.Atoi(kv[1])
		case "type":
			e.ICMPType, _ = strconv.Atoi(kv[1])
		case "code":
			e.ICMPCode, _ = strconv.Atoi(kv[1])
		case "id":
			e.ICMPID, _ = strconv.Atoi(kv[1])
		}
	}

	if e.SrcIP == nil || e.DstIP == nil {
		return e, false
	}
	return e, true
}

// CTProtoNumber returns the IANA protocol number for a protocol name as used
// in dpctl output ("tcp", "udp", "icmp", "icmpv6", "sctp").
func CTProtoNumber(proto string) int {
	switch strings.ToLower(proto) {
	case "tcp":
		return 6
	case "udp":
		return 17
	case "icmp":
		return 1
	case "icmpv6":
		return 58
	case "sctp":
		return 132
	default:
		return 0
	}
}
