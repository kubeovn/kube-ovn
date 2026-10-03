package cni

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

type fakeReleaseVfNetNS struct {
	doErr error
}

func (f *fakeReleaseVfNetNS) Do(func(ns.NetNS) error) error { return f.doErr }
func (f *fakeReleaseVfNetNS) Set() error                    { return nil }
func (f *fakeReleaseVfNetNS) Path() string                  { return "fake" }
func (f *fakeReleaseVfNetNS) Fd() uintptr                   { return 0 }
func (f *fakeReleaseVfNetNS) Close() error                  { return nil }

func TestReleaseVfSkipsMissingPodNetNS(t *testing.T) {
	handler := executionHandler{}
	for _, podNetns := range []string{"", filepath.Join(t.TempDir(), "missing")} {
		t.Run(podNetns, func(t *testing.T) {
			err := handler.releaseVf("pod", "namespace", podNetns, "net1", util.OffloadType, "0000:65:00.1")
			if err != nil {
				t.Fatalf("releaseVf() error = %v", err)
			}
		})
	}
}

func TestReleaseVfKeepsUnexpectedPodNetNSError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	err := (executionHandler{}).releaseVf("pod", "namespace", path, "net1", util.OffloadType, "0000:65:00.1")
	if err == nil {
		t.Fatal("releaseVf() error = nil, want an error for a non-namespace path")
	}
}

func TestReleaseVfReturnsNetNSOperationError(t *testing.T) {
	originalGetNS := getNetNSForReleaseVf
	originalGetCurrentNS := getCurrentNetNSForReleaseVf
	t.Cleanup(func() {
		getNetNSForReleaseVf = originalGetNS
		getCurrentNetNSForReleaseVf = originalGetCurrentNS
	})

	wantErr := errors.New("netns operation failed")
	podNS := &fakeReleaseVfNetNS{doErr: wantErr}
	hostNS := &fakeReleaseVfNetNS{}
	getNetNSForReleaseVf = func(string) (ns.NetNS, error) { return podNS, nil }
	getCurrentNetNSForReleaseVf = func() (ns.NetNS, error) { return hostNS, nil }

	err := (executionHandler{}).releaseVf("pod", "namespace", "fake", "net1", util.OffloadType, "0000:65:00.1")
	require.ErrorIs(t, err, wantErr)
}
