package main

import (
	"os"
	"testing"

	"github.com/kubeovn/kube-ovn/cmd/acl_sample"
)

func TestMainDispatchesACLSampleCommand(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() {
		os.Args = originalArgs
	})

	os.Args = []string{acl_sample.CommandName, "help"}
	main()
}
