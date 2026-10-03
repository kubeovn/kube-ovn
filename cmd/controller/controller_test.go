package main

import (
	"os"
	"testing"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestLeaderResourceName(t *testing.T) {
	old, had := os.LookupEnv(util.EnvControllerLeaderName)
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(util.EnvControllerLeaderName, old)
		} else {
			_ = os.Unsetenv(util.EnvControllerLeaderName)
		}
	})

	if err := os.Setenv(util.EnvControllerLeaderName, " tenant-controller "); err != nil {
		t.Fatal(err)
	}
	if got := leaderResourceName(); got != "tenant-controller" {
		t.Fatalf("leaderResourceName() = %q, want %q", got, "tenant-controller")
	}

	if err := os.Unsetenv(util.EnvControllerLeaderName); err != nil {
		t.Fatal(err)
	}
	if got := leaderResourceName(); got != ovnLeaderResource {
		t.Fatalf("leaderResourceName() = %q, want %q", got, ovnLeaderResource)
	}
}
