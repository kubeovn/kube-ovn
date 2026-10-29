package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnderlayServiceFlowTracksPatchOfport(t *testing.T) {
	dir := t.TempDir()
	patchOfportFile := filepath.Join(dir, "patch-ofport")
	underlayOfportFile := filepath.Join(dir, "underlay-ofport")
	require.NoError(t, os.WriteFile(patchOfportFile, []byte("-1\n"), 0o600))
	require.NoError(t, os.WriteFile(underlayOfportFile, []byte("3\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ovs-vsctl"), []byte(`#!/bin/sh
case "$*" in
  *" eth1 ofport") cat "$UNDERLAY_OFPORT_FILE" ;;
  *" patch-localnet.underlay-to-br-int ofport") cat "$PATCH_OFPORT_FILE" ;;
  *) exit 1 ;;
esac
`), 0o755))
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("PATCH_OFPORT_FILE", patchOfportFile)
	t.Setenv("UNDERLAY_OFPORT_FILE", underlayOfportFile)

	c := &Controller{
		flowCache: make(map[string]map[string]LbServiceRules),
		flowChan:  make(chan struct{}, 1),
	}
	rule := LbServiceRules{
		IP: "172.19.0.100", Port: 80, Protocol: "TCP", DstMac: "00:00:00:01:00:01",
		UnderlayNic: "eth1", BridgeName: "br-provider", SubnetName: "underlay",
	}
	require.ErrorIs(t, c.AddOrUpdateUnderlaySubnetSvcLocalFlowCache(rule), errUnderlayLocalnetPatchNotReady)
	require.Empty(t, c.storeFlowCache()["br-provider"])

	require.NoError(t, os.WriteFile(patchOfportFile, []byte("5\n"), 0o600))
	require.NoError(t, c.AddOrUpdateUnderlaySubnetSvcLocalFlowCache(rule))

	checkOutput := func(want string) {
		t.Helper()
		flows := c.storeFlowCache()["br-provider"]
		require.Len(t, flows, 1)
		require.True(t, strings.HasSuffix(flows[0], "output:"+want), "flow = %q", flows[0])
	}
	checkOutput("5")

	require.NoError(t, os.WriteFile(patchOfportFile, []byte("6\n"), 0o600))
	checkOutput("6")

	require.NoError(t, os.WriteFile(patchOfportFile, []byte("-1\n"), 0o600))
	require.Empty(t, c.storeFlowCache()["br-provider"])

	require.NoError(t, os.WriteFile(patchOfportFile, []byte("missing\n"), 0o600))
	require.Empty(t, c.storeFlowCache()["br-provider"])

	require.NoError(t, os.WriteFile(patchOfportFile, []byte("7\n"), 0o600))
	checkOutput("7")

	require.NoError(t, os.WriteFile(underlayOfportFile, []byte("-1\n"), 0o600))
	require.Empty(t, c.storeFlowCache()["br-provider"])
	require.NoError(t, os.WriteFile(underlayOfportFile, []byte("3\n"), 0o600))
	checkOutput("7")
}
