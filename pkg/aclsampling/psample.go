package aclsampling

// PacketSample is an ACL sample received from the Linux psample generic
// netlink family. Data contains the captured packet bytes and may be shorter
// than OriginalSize.
type PacketSample struct {
	GroupID           uint32
	Sequence          uint32
	SampleRate        uint32
	OriginalSize      uint32
	IngressIfIndex    uint32
	EgressIfIndex     uint32
	Timestamp         uint64
	Protocol          uint16
	RateIsProbability bool
	Reference         SampleReference
	Data              []byte
}
