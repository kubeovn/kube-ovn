//go:build linux

package aclsampling

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

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
		{name: "malformed attribute", message: append([]byte{0, 1, 0, 0}, rawPSampleAttributeForTest(3, 1, nil)...)},
		{name: "missing attribute padding", message: append([]byte{0, 1, 0, 0}, rawPSampleAttributeForTest(5, psampleAttrData, []byte{1})...)},
		{name: "trailing attribute bytes", message: append(psampleMessageForTest(group, cookie), 0)},
		{name: "probability flag with value", message: psampleMessageForTest(group, cookie, psampleAttributeForTest(psampleAttrSampleProbability, []byte{1}))},
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

	_, ok, err = packetSampleFromNetlinkMessage(syscall.NetlinkMessage{
		Header: syscall.NlMsghdr{Type: unix.NLMSG_OVERRUN},
	}, familyID, 142)
	require.ErrorContains(t, err, "samples were lost")
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

func TestListenPSamplesReturnsSetupErrors(t *testing.T) {
	t.Run("family discovery", func(t *testing.T) {
		familyErr := errors.New("family failed")
		netlinkAPI := newFakePSampleNetlink(nil)
		netlinkAPI.familyErr = familyErr
		err := listenPSamples(context.Background(), 142, func(PacketSample) error { return nil }, netlinkAPI)
		require.ErrorIs(t, err, familyErr)
	})

	t.Run("socket subscribe", func(t *testing.T) {
		subscribeErr := errors.New("subscribe failed")
		netlinkAPI := newFakePSampleNetlink(nil)
		netlinkAPI.subscribeErr = subscribeErr
		err := listenPSamples(context.Background(), 142, func(PacketSample) error { return nil }, netlinkAPI)
		require.ErrorIs(t, err, subscribeErr)
	})
}

func TestListenPSamplesJoinsMembershipAndCloseErrors(t *testing.T) {
	membershipErr := errors.New("membership failed")
	closeErr := errors.New("close failed")
	var closeCount atomic.Int32
	socket := &fakePSampleSocket{closeFunc: func() error {
		closeCount.Add(1)
		return closeErr
	}}
	netlinkAPI := newFakePSampleNetlink(socket)
	netlinkAPI.membershipErr = membershipErr

	err := listenPSamples(context.Background(), 142, func(PacketSample) error { return nil }, netlinkAPI)
	require.ErrorIs(t, err, membershipErr)
	require.ErrorIs(t, err, closeErr)
	require.Equal(t, int32(1), closeCount.Load())
	require.Equal(t, uint32(42), netlinkAPI.membershipGroup)
}

func TestListenPSamplesJoinsReceiveAndCloseErrors(t *testing.T) {
	closeErr := errors.New("close failed")
	var closeCount atomic.Int32
	socket := &fakePSampleSocket{
		receiveFunc: func() ([]syscall.NetlinkMessage, *unix.SockaddrNetlink, error) {
			return nil, nil, unix.EPERM
		},
		closeFunc: func() error {
			closeCount.Add(1)
			return closeErr
		},
	}

	err := listenPSamples(context.Background(), 142, func(PacketSample) error { return nil }, newFakePSampleNetlink(socket))
	require.ErrorIs(t, err, unix.EPERM)
	require.ErrorIs(t, err, closeErr)
	require.Equal(t, int32(1), closeCount.Load())
}

func TestListenPSamplesJoinsHandlerAndCloseErrors(t *testing.T) {
	handleErr := errors.New("handler failed")
	closeErr := errors.New("close failed")
	var closeCount atomic.Int32
	socket := &fakePSampleSocket{
		receiveFunc: func() ([]syscall.NetlinkMessage, *unix.SockaddrNetlink, error) {
			return []syscall.NetlinkMessage{validPSampleNetlinkMessageForTest(30, 142)}, &unix.SockaddrNetlink{}, nil
		},
		closeFunc: func() error {
			closeCount.Add(1)
			return closeErr
		},
	}

	err := listenPSamples(context.Background(), 142, func(PacketSample) error { return handleErr }, newFakePSampleNetlink(socket))
	require.ErrorIs(t, err, handleErr)
	require.ErrorIs(t, err, closeErr)
	require.Equal(t, int32(1), closeCount.Load())
}

func TestListenPSamplesCancellationClosesSocketOnce(t *testing.T) {
	closeErr := errors.New("close failed")
	receiveStarted := make(chan struct{})
	socketClosed := make(chan struct{})
	var receiveOnce sync.Once
	var closeCount atomic.Int32
	socket := &fakePSampleSocket{
		receiveFunc: func() ([]syscall.NetlinkMessage, *unix.SockaddrNetlink, error) {
			receiveOnce.Do(func() { close(receiveStarted) })
			<-socketClosed
			return nil, nil, unix.EBADF
		},
		closeFunc: func() error {
			closeCount.Add(1)
			close(socketClosed)
			return closeErr
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- listenPSamples(ctx, 142, func(PacketSample) error { return nil }, newFakePSampleNetlink(socket))
	}()
	<-receiveStarted
	cancel()

	select {
	case err := <-result:
		require.ErrorIs(t, err, closeErr)
	case <-time.After(3 * time.Second):
		t.Fatal("ListenPSamples did not stop after context cancellation")
	}
	require.Equal(t, int32(1), closeCount.Load())
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

func rawPSampleAttributeForTest(length, attrType uint16, value []byte) []byte {
	attr := make([]byte, unix.SizeofRtAttr+len(value))
	nl.NativeEndian().PutUint16(attr[0:2], length)
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

type fakePSampleSocket struct {
	receiveFunc func() ([]syscall.NetlinkMessage, *unix.SockaddrNetlink, error)
	closeFunc   func() error
}

func (*fakePSampleSocket) GetFd() int { return 10 }

func (s *fakePSampleSocket) Receive() ([]syscall.NetlinkMessage, *unix.SockaddrNetlink, error) {
	return s.receiveFunc()
}

func (s *fakePSampleSocket) Close() error {
	return s.closeFunc()
}

type fakePSampleNetlink struct {
	socket          psampleNetlinkSocket
	familyErr       error
	subscribeErr    error
	membershipErr   error
	membershipGroup uint32
}

func newFakePSampleNetlink(socket psampleNetlinkSocket) *fakePSampleNetlink {
	return &fakePSampleNetlink{socket: socket}
}

func (f *fakePSampleNetlink) family(string) (*netlink.GenlFamily, error) {
	if f.familyErr != nil {
		return nil, f.familyErr
	}
	return &netlink.GenlFamily{
		ID:      30,
		Version: psampleVersion,
		Groups:  []netlink.GenlMulticastGroup{{ID: 42, Name: psamplePacketsGroupName}},
	}, nil
}

func (f *fakePSampleNetlink) subscribe(int) (psampleNetlinkSocket, error) {
	if f.subscribeErr != nil {
		return nil, f.subscribeErr
	}
	return f.socket, nil
}

func (f *fakePSampleNetlink) addMembership(_ int, groupID uint32) error {
	f.membershipGroup = groupID
	return f.membershipErr
}

func validPSampleNetlinkMessageForTest(familyID uint16, groupID uint32) syscall.NetlinkMessage {
	cookie := make([]byte, 8)
	binary.BigEndian.PutUint64(cookie, uint64(0x66000001)<<32|1)
	return syscall.NetlinkMessage{
		Header: syscall.NlMsghdr{Type: familyID},
		Data: psampleMessageForTest(
			psampleAttributeForTest(psampleAttrSampleGroup, nativeUint32ForTest(groupID)),
			psampleAttributeForTest(psampleAttrUserCookie, cookie),
		),
	}
}
