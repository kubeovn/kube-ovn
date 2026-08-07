//go:build linux

package aclsampling

import (
	"context"
	"encoding/binary"
	"errors"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

func TestParsePSampleMessage(t *testing.T) {
	const (
		applicationID = uint32(102)
		datapathKey   = uint32(0x0abcde)
		metadata      = uint32(0x12345678)
	)
	observationDomain := applicationID<<24 | datapathKey
	cookie := make([]byte, 8)
	binary.BigEndian.PutUint64(cookie, uint64(observationDomain)<<32|uint64(metadata))

	message := psampleMessageForTest(
		psampleAttributeForTest(psampleAttrIngressIfIndex, nativeUint16ForTest(12)),
		psampleAttributeForTest(psampleAttrEgressIfIndex, nativeUint32ForTest(34)),
		psampleAttributeForTest(psampleAttrOriginalSize, nativeUint32ForTest(128)),
		psampleAttributeForTest(psampleAttrSampleGroup, nativeUint32ForTest(142)),
		psampleAttributeForTest(psampleAttrGroupSequence, nativeUint32ForTest(7)),
		psampleAttributeForTest(psampleAttrSampleRate, nativeUint32ForTest(0xffffffff)),
		psampleAttributeForTest(psampleAttrData, []byte{1, 2, 3, 4}),
		psampleAttributeForTest(psampleAttrTimestamp, nativeUint64ForTest(123456789)),
		psampleAttributeForTest(psampleAttrProtocol, nativeUint16ForTest(0x0800)),
		psampleAttributeForTest(psampleAttrUserCookie, cookie),
		psampleAttributeForTest(psampleAttrSampleProbability, nil),
		psampleAttributeForTest(100, []byte{9}),
	)

	sample, err := parsePSampleMessage(message)
	require.NoError(t, err)
	require.Equal(t, uint32(142), sample.GroupID)
	require.Equal(t, uint32(7), sample.Sequence)
	require.Equal(t, uint32(0xffffffff), sample.SampleRate)
	require.Equal(t, uint32(128), sample.OriginalSize)
	require.Equal(t, uint32(12), sample.IngressIfIndex)
	require.Equal(t, uint32(34), sample.EgressIfIndex)
	require.Equal(t, uint64(123456789), sample.Timestamp)
	require.Equal(t, uint16(0x0800), sample.Protocol)
	require.True(t, sample.RateIsProbability)
	require.Equal(t, []byte{1, 2, 3, 4}, sample.Data)
	require.Equal(t, observationDomain, *sample.Reference.ObservationDomain)
	require.Equal(t, applicationID, *sample.Reference.ApplicationID)
	require.Equal(t, datapathKey, *sample.Reference.DatapathKey)
	require.Equal(t, metadata, sample.Reference.Metadata)
}

func TestParsePSampleMessageRejectsInvalidInput(t *testing.T) {
	validCookie := make([]byte, 8)
	binary.BigEndian.PutUint64(validCookie, uint64(0x66000001)<<32|1)
	group := psampleAttributeForTest(psampleAttrSampleGroup, nativeUint32ForTest(142))
	cookie := psampleAttributeForTest(psampleAttrUserCookie, validCookie)

	tests := []struct {
		name         string
		message      []byte
		notACLSample bool
	}{
		{name: "truncated header", message: []byte{0, 1}},
		{name: "other command", message: append([]byte{1, 1, 0, 0}, group...), notACLSample: true},
		{name: "other version", message: append(append([]byte{0, 2, 0, 0}, group...), cookie...)},
		{name: "missing group", message: psampleMessageForTest(cookie)},
		{name: "missing cookie", message: psampleMessageForTest(group), notACLSample: true},
		{name: "short cookie", message: psampleMessageForTest(group, psampleAttributeForTest(psampleAttrUserCookie, []byte{1, 2, 3, 4})), notACLSample: true},
		{name: "non OVN cookie", message: psampleMessageForTest(group, psampleAttributeForTest(psampleAttrUserCookie, []byte{0, 0, 0, 100, 0, 0, 0, 200})), notACLSample: true},
		{name: "duplicate group", message: psampleMessageForTest(group, group, cookie)},
		{name: "invalid group length", message: psampleMessageForTest(psampleAttributeForTest(psampleAttrSampleGroup, []byte{1}), cookie)},
		{name: "malformed attribute", message: append([]byte{0, 1, 0, 0}, []byte{3, 0, 1, 0}...)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parsePSampleMessage(test.message)
			require.Error(t, err)
			require.Equal(t, test.notACLSample, errors.Is(err, errNotACLPSample))
		})
	}
}

func TestPacketSampleFromNetlinkMessageFiltersFamilyAndGroup(t *testing.T) {
	const familyID = uint16(30)
	cookie := make([]byte, 8)
	binary.BigEndian.PutUint64(cookie, uint64(0x66000001)<<32|1)
	payload := psampleMessageForTest(
		psampleAttributeForTest(psampleAttrSampleGroup, nativeUint32ForTest(142)),
		psampleAttributeForTest(psampleAttrUserCookie, cookie),
	)

	sample, ok, err := packetSampleFromNetlinkMessage(syscall.NetlinkMessage{
		Header: syscall.NlMsghdr{Type: familyID},
		Data:   payload,
	}, familyID, 142)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint32(142), sample.GroupID)

	_, ok, err = packetSampleFromNetlinkMessage(syscall.NetlinkMessage{
		Header: syscall.NlMsghdr{Type: familyID},
		Data:   payload,
	}, familyID, 143)
	require.NoError(t, err)
	require.False(t, ok)

	_, ok, err = packetSampleFromNetlinkMessage(syscall.NetlinkMessage{
		Header: syscall.NlMsghdr{Type: familyID + 1},
		Data:   payload,
	}, familyID, 142)
	require.NoError(t, err)
	require.False(t, ok)

	errorCode := -int32(unix.EPERM)
	errorPayload := nativeUint32ForTest(uint32(errorCode))
	_, ok, err = packetSampleFromNetlinkMessage(syscall.NetlinkMessage{
		Header: syscall.NlMsghdr{Type: unix.NLMSG_ERROR},
		Data:   errorPayload,
	}, familyID, 142)
	require.ErrorIs(t, err, unix.EPERM)
	require.False(t, ok)
}

func TestPSamplePacketsMulticastGroup(t *testing.T) {
	family := &netlink.GenlFamily{Groups: []netlink.GenlMulticastGroup{
		{ID: 10, Name: "config"},
		{ID: 42, Name: psamplePacketsGroupName},
	}}
	groupID, err := psamplePacketsMulticastGroup(family)
	require.NoError(t, err)
	require.Equal(t, uint32(42), groupID)

	_, err = psamplePacketsMulticastGroup(&netlink.GenlFamily{})
	require.Error(t, err)
	_, err = psamplePacketsMulticastGroup(nil)
	require.Error(t, err)
}

func TestListenPSamplesValidatesArguments(t *testing.T) {
	//nolint:staticcheck // Explicitly verify nil context validation.
	require.Error(t, ListenPSamples(nil, 0, func(PacketSample) error { return nil }))
	require.Error(t, ListenPSamples(context.Background(), 0, nil))
}

func psampleMessageForTest(attrs ...[]byte) []byte {
	message := []byte{psampleCommandSample, psampleVersion, 0, 0}
	for _, attr := range attrs {
		message = append(message, attr...)
	}
	return message
}

func psampleAttributeForTest(attrType uint16, value []byte) []byte {
	length := unix.SizeofRtAttr + len(value)
	alignedLength := (length + 3) &^ 3
	attr := make([]byte, alignedLength)
	nl.NativeEndian().PutUint16(attr[0:2], uint16(length))
	nl.NativeEndian().PutUint16(attr[2:4], attrType)
	copy(attr[unix.SizeofRtAttr:], value)
	return attr
}

func nativeUint16ForTest(value uint16) []byte {
	data := make([]byte, 2)
	nl.NativeEndian().PutUint16(data, value)
	return data
}

func nativeUint32ForTest(value uint32) []byte {
	data := make([]byte, 4)
	nl.NativeEndian().PutUint32(data, value)
	return data
}

func nativeUint64ForTest(value uint64) []byte {
	data := make([]byte, 8)
	nl.NativeEndian().PutUint64(data, value)
	return data
}
