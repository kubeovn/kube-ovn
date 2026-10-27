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
	require.NoError(t, os.WriteFile(patchOfportFile, []byte("5\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ovs-vsctl"), []byte(`#!/bin/sh
case "$*" in
  *" eth1 ofport") printf '3\n' ;;
  *" patch-localnet.underlay-to-br-int ofport") cat "$PATCH_OFPORT_FILE" ;;
  *) exit 1 ;;
esac
`), 0o755))
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("PATCH_OFPORT_FILE", patchOfportFile)

	c := &Controller{
		flowCache: make(map[string]map[string]LbServiceRules),
		flowChan:  make(chan struct{}, 1),
	}
	require.NoError(t, c.AddOrUpdateUnderlaySubnetSvcLocalFlowCache(
		"172.19.0.100", 80, "TCP", "00:00:00:01:00:01", "eth1", "br-provider", "underlay",
	))

	checkOutput := func(want string) {
		t.Helper()
		flows := c.storeFlowCache()["br-provider"]
		require.Len(t, flows, 1)
		require.True(t, strings.HasSuffix(flows[0], "output:"+want), "flow = %q", flows[0])
	}
	checkOutput("5")

	require.NoError(t, os.WriteFile(patchOfportFile, []byte("6\n"), 0o600))
	checkOutput("6")

	require.NoError(t, os.WriteFile(patchOfportFile, []byte("missing\n"), 0o600))
	require.Empty(t, c.storeFlowCache()["br-provider"])

	require.NoError(t, os.WriteFile(patchOfportFile, []byte("7\n"), 0o600))
	checkOutput("7")
}
