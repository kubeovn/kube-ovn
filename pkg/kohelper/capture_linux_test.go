//go:build linux

package kohelper

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestCapturePreservesWireLengthWhenTruncated(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	require.NoError(t, err)
	receiver := os.NewFile(uintptr(fds[0]), "capture-receiver")
	sender := os.NewFile(uintptr(fds[1]), "capture-sender")
	t.Cleanup(func() { require.NoError(t, receiver.Close()) })
	t.Cleanup(func() { require.NoError(t, sender.Close()) })
	raw, err := receiver.SyscallConn()
	require.NoError(t, err)
	payload := bytes.Repeat([]byte{0xab}, 512)
	_, err = sender.Write(payload)
	require.NoError(t, err)

	buffer := make([]byte, 64)
	length, err := readCapturePacket(raw, buffer)
	require.NoError(t, err)
	require.Equal(t, len(payload), length)
	require.Equal(t, payload[:len(buffer)], buffer)
}
