package ipam

type IPPool struct {
	V4IPs       *IPRangeList
	V4Free      *IPRangeList
	V4Available *IPRangeList
	V4Reserved  *IPRangeList
	V4Released  *IPRangeList
	V4Using     *IPRangeList
	V6IPs       *IPRangeList
	V6Free      *IPRangeList
	V6Available *IPRangeList
	V6Reserved  *IPRangeList
	V6Released  *IPRangeList
	V6Using     *IPRangeList
}
