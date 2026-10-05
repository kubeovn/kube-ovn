package ipsec

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRestoreCurrentRejectsUnboundOrChangedIdentity(t *testing.T) {
	cert, key, trust := testIdentity(t, "chassis-a")
	_, _, otherTrust := testIdentity(t, "chassis-a")
	for _, scenario := range []string{"missing-current", "missing-node-name", "different-node-name", "different-namespace", "missing-node-uid", "wrong-chassis", "untrusted-root", "changed-file"} {
		t.Run(scenario, func(t *testing.T) {
			a := &Agent{config: Configuration{NodeName: "node-a", Namespace: "kube-system"}, store: store{dir: t.TempDir()}, runtime: &runtimeManager{}}
			source := &generation{ID: digest(key), NodeName: "node-a", Namespace: "kube-system", NodeUID: "node-uid", Chassis: "chassis-a"}
			require.NoError(t, a.store.write(source, "private-key", key))
			require.NoError(t, a.store.write(source, "certificate", cert))
			bundle := trust
			if scenario == "untrusted-root" {
				bundle = otherTrust
			}
			g, err := a.store.prepareGeneration(source, bundle)
			require.NoError(t, err)
			switch scenario {
			case "missing-node-name":
				g.NodeName = ""
			case "different-node-name":
				g.NodeName = "node-b"
			case "different-namespace":
				g.Namespace = "other"
			case "missing-node-uid":
				g.NodeUID = ""
			case "wrong-chassis":
				g.Chassis = "other-chassis"
			case "changed-file":
				require.NoError(t, a.store.write(g, "ca-bundle", otherTrust))
			}
			if scenario != "missing-current" {
				require.NoError(t, a.store.save("current", g))
			}
			require.Error(t, a.restoreCurrent(t.Context()))
			require.False(t, a.runtime.enabled.Load())
			require.Nil(t, a.ovs, "invalid recovery must fail before touching OVSDB")
		})
	}
}
