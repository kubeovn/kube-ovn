package ipsec

import (
	"bytes"
	"crypto/rand"
	"encoding/json/v2"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestCandidateDrainInventory(t *testing.T) {
	if os.Getenv("KUBE_OVN_IPSEC_RUNTIME_TEST") != "true" {
		t.Skip("requires the isolated candidate-image runtime harness")
	}
	storage := store{dir: t.TempDir()}
	lock, err := storage.lock()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Close()) })
	kernel, err := netlink.NewHandle(unix.NETLINK_XFRM)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, kernel.Close()) })
	require.NoError(t, kernel.SetSocketTimeout(3*time.Second))
	owner, err := prepareProtection(storage, "drain-node-uid", kernel)
	require.NoError(t, err)
	require.NoError(t, owner.arm())
	r := &runtimeManager{store: storage, expected: runtimeConfiguration{NodeUID: "drain-node-uid"}}
	session, err := r.prepareConnectionSession()
	require.NoError(t, err)
	// A disjoint foreign SA survives every successful and failed observation.
	key := make([]byte, 20)
	_, err = rand.Read(key)
	require.NoError(t, err)
	foreign := &netlink.XfrmState{
		Src: net.ParseIP("198.51.100.10"), Dst: net.ParseIP("198.51.100.20"),
		Proto: netlink.XFRM_PROTO_ESP, Mode: netlink.XFRM_MODE_TRANSPORT, Spi: 0x759831, Reqid: 99,
		Aead: &netlink.XfrmStateAlgo{Name: "rfc4106(gcm(aes))", Key: key, ICVLen: 128},
	}
	require.NoError(t, kernel.XfrmStateAdd(foreign))
	require.NoError(t, r.recordDrain(*session, kernel))
	data, err := readRegularFile(filepath.Join(filepath.Dir(session.intentPath(storage)), "drain.json"))
	require.NoError(t, err)
	var observation drainObservation
	require.NoError(t, json.Unmarshal(data, &observation))
	require.Equal(t, *session, observation.connectionSession)
	require.False(t, observation.Observed.IsZero())
	// No ledger exists for this new session. An unobserved installed SA must
	// still block completion; a mark/reqid match is never deletion permission.
	unobserved, err := r.prepareConnectionSession()
	require.NoError(t, err)
	remaining := *foreign
	remaining.Spi, remaining.Reqid = 0x759832, int(owner.reservation.Reqid)
	require.NoError(t, kernel.XfrmStateAdd(&remaining))
	require.ErrorContains(t, r.recordDrain(*unobserved, kernel), "remaining or ambiguous kernel SA")
	_, err = os.Stat(filepath.Join(filepath.Dir(unobserved.intentPath(storage)), "drain.json"))
	require.True(t, os.IsNotExist(err), "failed drain must not create a completion observation")
	actual, err := kernel.XfrmStateGet(&remaining)
	require.NoError(t, err, "an unobserved SA must be preserved for recovery")
	require.Equal(t, remaining.Reqid, actual.Reqid)
	require.NoError(t, kernel.XfrmStateDel(&remaining)) // Remove only this test's synthetic fixture.
	policy := &netlink.XfrmPolicy{
		Src: &net.IPNet{IP: net.ParseIP("198.51.100.10"), Mask: net.CIDRMask(32, 32)},
		Dst: &net.IPNet{IP: net.ParseIP("198.51.100.20"), Mask: net.CIDRMask(32, 32)},
		Dir: netlink.XFRM_DIR_IN, Action: netlink.XFRM_POLICY_BLOCK, Priority: 10,
		Mark: &netlink.XfrmMark{Value: owner.reservation.Mark, Mask: ^uint32(0)},
	}
	require.NoError(t, kernel.XfrmPolicyAdd(policy))
	require.ErrorContains(t, r.recordDrain(*unobserved, kernel), "remaining or ambiguous marked policy")
	require.NoError(t, kernel.XfrmPolicyDel(policy)) // Remove only this test's synthetic fixture.
	require.NoError(t, r.recordDrain(*unobserved, kernel))
	require.NoError(t, owner.verify(), "every drain attempt must preserve both guards")
	actual, err = kernel.XfrmStateGet(foreign)
	require.NoError(t, err)
	require.Equal(t, foreign.Reqid, actual.Reqid)
	require.NotNil(t, actual.Aead)
	require.True(t, bytes.Equal(key, actual.Aead.Key), "the foreign SA must retain its synthetic key")
}
