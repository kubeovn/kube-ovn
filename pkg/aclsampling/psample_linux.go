//go:build linux

package aclsampling

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"syscall"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

const (
	psampleFamilyName        = "psample"
	psamplePacketsGroupName  = "packets"
	psampleVersion           = 1
	psampleCommandSample     = 0
	psampleGenericHeaderSize = 4
	psampleNetlinkTypeMask   = 0x3fff
)

const (
	psampleAttrIngressIfIndex = iota
	psampleAttrEgressIfIndex
	psampleAttrOriginalSize
	psampleAttrSampleGroup
	psampleAttrGroupSequence
	psampleAttrSampleRate
	psampleAttrData
	psampleAttrGroupRefcount
	psampleAttrTunnel
	psampleAttrPad
	psampleAttrOutputTC
	psampleAttrOutputTCOccupancy
	psampleAttrLatency
	psampleAttrTimestamp
	psampleAttrProtocol
	psampleAttrUserCookie
	psampleAttrSampleProbability
)

var errNotACLPSample = errors.New("psample message is not an ACL sample")

type psampleNetlinkSocket interface {
	GetFd() int
	Receive() ([]syscall.NetlinkMessage, *unix.SockaddrNetlink, error)
	Close() error
}

type psampleNetlinkAPI interface {
	family(string) (*netlink.GenlFamily, error)
	subscribe(int) (psampleNetlinkSocket, error)
	addMembership(int, uint32) error
}

type linuxPSampleNetlink struct{}

func (linuxPSampleNetlink) family(name string) (*netlink.GenlFamily, error) {
	return netlink.GenlFamilyGet(name)
}

func (linuxPSampleNetlink) subscribe(protocol int) (psampleNetlinkSocket, error) {
	return nl.Subscribe(protocol)
}

func (linuxPSampleNetlink) addMembership(fd int, groupID uint32) error {
	return unix.SetsockoptInt(fd, unix.SOL_NETLINK, unix.NETLINK_ADD_MEMBERSHIP, int(groupID))
}

// ListenPSamples subscribes to the Linux psample packets multicast group and
// invokes handle for each ACL sample emitted to groupID. Context cancellation
// stops the blocking receive.
func ListenPSamples(ctx context.Context, groupID uint32, handle func(PacketSample) error) error {
	return listenPSamples(ctx, groupID, handle, linuxPSampleNetlink{})
}

func listenPSamples(ctx context.Context, groupID uint32, handle func(PacketSample) error, netlinkAPI psampleNetlinkAPI) (retErr error) {
	if ctx == nil {
		return errors.New("psample context must not be nil")
	}
	if handle == nil {
		return errors.New("psample handler must not be nil")
	}

	family, err := netlinkAPI.family(psampleFamilyName)
	if err != nil {
		return fmt.Errorf("get psample generic netlink family: %w", err)
	}
	if family.Version != psampleVersion {
		return fmt.Errorf("unsupported psample generic netlink version %d", family.Version)
	}
	multicastGroupID, err := psamplePacketsMulticastGroup(family)
	if err != nil {
		return err
	}

	socket, err := netlinkAPI.subscribe(unix.NETLINK_GENERIC)
	if err != nil {
		return fmt.Errorf("open psample generic netlink socket: %w", err)
	}
	if err := netlinkAPI.addMembership(socket.GetFd(), multicastGroupID); err != nil {
		subscribeErr := fmt.Errorf("subscribe to psample packets multicast group: %w", err)
		if closeErr := socket.Close(); closeErr != nil {
			return errors.Join(subscribeErr, fmt.Errorf("close psample generic netlink socket: %w", closeErr))
		}
		return subscribeErr
	}

	var closeOnce sync.Once
	var closeErr error
	closeSocket := func() {
		closeOnce.Do(func() { closeErr = socket.Close() })
	}
	done := make(chan struct{})
	defer close(done)
	defer func() {
		closeSocket()
		if closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close psample generic netlink socket: %w", closeErr))
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			closeSocket()
		case <-done:
		}
	}()

	for {
		messages, sender, err := socket.Receive()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("receive psample generic netlink message: %w", err)
		}
		if sender == nil || sender.Pid != 0 {
			continue
		}
		for _, message := range messages {
			sample, ok, err := packetSampleFromNetlinkMessage(message, family.ID, groupID)
			if err != nil {
				return err
			}
			if ok {
				if err := handle(*sample); err != nil {
					return fmt.Errorf("handle ACL psample event: %w", err)
				}
			}
		}
	}
}

func psamplePacketsMulticastGroup(family *netlink.GenlFamily) (uint32, error) {
	if family == nil {
		return 0, errors.New("psample generic netlink family is nil")
	}
	for _, group := range family.Groups {
		if group.Name == psamplePacketsGroupName {
			if group.ID == 0 {
				break
			}
			return group.ID, nil
		}
	}
	return 0, errors.New("psample packets multicast group is unavailable")
}

func packetSampleFromNetlinkMessage(message syscall.NetlinkMessage, familyID uint16, groupID uint32) (*PacketSample, bool, error) {
	switch message.Header.Type {
	case unix.NLMSG_NOOP, unix.NLMSG_DONE:
		return nil, false, nil
	case unix.NLMSG_OVERRUN:
		return nil, false, errors.New("psample netlink receive overrun: samples were lost")
	case unix.NLMSG_ERROR:
		if err := netlinkMessageError(message.Data); err != nil {
			return nil, false, fmt.Errorf("psample generic netlink error: %w", err)
		}
		return nil, false, nil
	}
	if message.Header.Type != familyID {
		return nil, false, nil
	}
	sample, err := parsePSampleMessage(message.Data)
	if errors.Is(err, errNotACLPSample) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("parse psample generic netlink message: %w", err)
	}
	if sample.GroupID != groupID {
		return nil, false, nil
	}
	return sample, true, nil
}

func parsePSampleMessage(message []byte) (*PacketSample, error) {
	if len(message) < psampleGenericHeaderSize {
		return nil, errors.New("psample generic netlink header is truncated")
	}
	if message[0] != psampleCommandSample {
		return nil, errNotACLPSample
	}
	if message[1] != psampleVersion {
		return nil, fmt.Errorf("unsupported psample message version %d", message[1])
	}
	attrData := message[psampleGenericHeaderSize:]
	if err := validatePSampleAttributeStream(attrData); err != nil {
		return nil, fmt.Errorf("validate psample attributes: %w", err)
	}
	attrs, err := nl.ParseRouteAttr(attrData)
	if err != nil {
		return nil, fmt.Errorf("parse psample attributes: %w", err)
	}

	sample := &PacketSample{}
	seen := make(map[uint16]struct{}, len(attrs))
	for _, attr := range attrs {
		attrType := attr.Attr.Type & psampleNetlinkTypeMask
		known, attrErr := parsePSampleAttribute(sample, attrType, attr.Value)
		if !known {
			continue
		}
		if _, ok := seen[attrType]; ok {
			return nil, fmt.Errorf("psample attribute %d is duplicated", attrType)
		}
		seen[attrType] = struct{}{}
		if attrErr != nil {
			return nil, fmt.Errorf("parse psample attribute %d: %w", attrType, attrErr)
		}
	}
	if _, ok := seen[psampleAttrSampleGroup]; !ok {
		return nil, errors.New("psample group ID is missing")
	}
	if _, ok := seen[psampleAttrUserCookie]; !ok {
		return nil, errNotACLPSample
	}
	return sample, nil
}

func validatePSampleAttributeStream(data []byte) error {
	for len(data) != 0 {
		if len(data) < unix.SizeofRtAttr {
			return fmt.Errorf("%d trailing bytes do not form an attribute header", len(data))
		}
		length := int(nl.NativeEndian().Uint16(data[:2]))
		if length < unix.SizeofRtAttr {
			return fmt.Errorf("attribute length %d is shorter than its header", length)
		}
		alignedLength := (length + unix.SizeofRtAttr - 1) &^ (unix.SizeofRtAttr - 1)
		if alignedLength > len(data) {
			return fmt.Errorf("aligned attribute length %d exceeds %d remaining bytes", alignedLength, len(data))
		}
		data = data[alignedLength:]
	}
	return nil
}

func parsePSampleAttribute(sample *PacketSample, attrType uint16, value []byte) (bool, error) {
	var err error
	switch attrType {
	case psampleAttrIngressIfIndex:
		sample.IngressIfIndex, err = psampleIfIndex(value)
	case psampleAttrEgressIfIndex:
		sample.EgressIfIndex, err = psampleIfIndex(value)
	case psampleAttrOriginalSize:
		sample.OriginalSize, err = psampleUint32(value)
	case psampleAttrSampleGroup:
		sample.GroupID, err = psampleUint32(value)
	case psampleAttrGroupSequence:
		sample.Sequence, err = psampleUint32(value)
	case psampleAttrSampleRate:
		sample.SampleRate, err = psampleUint32(value)
	case psampleAttrData:
		sample.Data = append([]byte(nil), value...)
	case psampleAttrTimestamp:
		sample.Timestamp, err = psampleUint64(value)
	case psampleAttrProtocol:
		sample.Protocol, err = psampleUint16(value)
	case psampleAttrUserCookie:
		err = parsePSampleCookie(sample, value)
	case psampleAttrSampleProbability:
		if len(value) != 0 {
			err = fmt.Errorf("expected flag with no value, got %d bytes", len(value))
		} else {
			sample.RateIsProbability = true
		}
	default:
		return false, nil
	}
	return true, err
}

func parsePSampleCookie(sample *PacketSample, value []byte) error {
	if len(value) != 8 {
		return errNotACLPSample
	}
	reference, err := sampleReferenceFromUint64(binary.BigEndian.Uint64(value))
	if err != nil || reference.ApplicationID == nil {
		return errNotACLPSample
	}
	sample.Reference = reference
	return nil
}

func psampleIfIndex(value []byte) (uint32, error) {
	switch len(value) {
	case 2:
		return uint32(nl.NativeEndian().Uint16(value)), nil
	case 4:
		return nl.NativeEndian().Uint32(value), nil
	default:
		return 0, fmt.Errorf("expected a 2-byte or 4-byte interface index, got %d bytes", len(value))
	}
}

func psampleUint16(value []byte) (uint16, error) {
	if len(value) != 2 {
		return 0, fmt.Errorf("expected 2 bytes, got %d", len(value))
	}
	return nl.NativeEndian().Uint16(value), nil
}

func psampleUint32(value []byte) (uint32, error) {
	if len(value) != 4 {
		return 0, fmt.Errorf("expected 4 bytes, got %d", len(value))
	}
	return nl.NativeEndian().Uint32(value), nil
}

func psampleUint64(value []byte) (uint64, error) {
	if len(value) != 8 {
		return 0, fmt.Errorf("expected 8 bytes, got %d", len(value))
	}
	return nl.NativeEndian().Uint64(value), nil
}

func netlinkMessageError(data []byte) error {
	if len(data) < 4 {
		return errors.New("netlink error payload is truncated")
	}
	// #nosec G115 -- NLMSG_ERROR stores a signed errno in two's-complement form.
	code := int32(nl.NativeEndian().Uint32(data[:4]))
	if code == 0 {
		return nil
	}
	if code > 0 {
		return fmt.Errorf("invalid positive netlink error code %d", code)
	}
	return syscall.Errno(-code)
}
