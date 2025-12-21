package util

// IPTableRule wraps iptables rule
type IPTableRule struct {
	Table string
	Chain string
	Pos   string
	Rule  []string
}

type GwIPtableCounters struct {
	Packets     int
	PacketBytes int
}
