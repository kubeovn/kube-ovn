package acl_sample

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/aclsampling"
)

type fakeResolver struct {
	reference aclsampling.SampleReference
	event     *aclsampling.Event
	closed    bool
}

func (r *fakeResolver) ResolveNetworkPolicyACLSample(reference aclsampling.SampleReference) (*aclsampling.Event, error) {
	r.reference = reference
	return r.event, nil
}

func (r *fakeResolver) Close() {
	r.closed = true
}

func TestRunDecode(t *testing.T) {
	resolver := &fakeResolver{event: &aclsampling.Event{
		SchemaVersion: aclsampling.SchemaVersionV1,
		Feature:       "network-policy",
		Verdict:       aclsampling.VerdictAllow,
		OVN:           aclsampling.OVNACLReference{UUID: "acl-uuid"},
		Sample:        aclsampling.SampleDetails{Metadata: 200},
	}}
	var stdout, stderr strings.Builder
	deps := dependencies{
		newResolver: func(address string) (sampleResolver, error) {
			require.Equal(t, "unix:/var/run/ovn/ovnnb_db.sock", address)
			return resolver, nil
		},
	}

	err := run(context.Background(), []string{
		"decode",
		"--ovn-nb-addr=unix:/var/run/ovn/ovnnb_db.sock",
		"0x640abcde000000c8",
	}, &stdout, &stderr, deps)
	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.True(t, resolver.closed)
	require.Equal(t, uint32(200), resolver.reference.Metadata)
	require.Equal(t, uint32(100), *resolver.reference.ApplicationID)
	require.Contains(t, stdout.String(), "schemaVersion: v1")
	require.Contains(t, stdout.String(), "aclUUID: acl-uuid")
}

func TestRunListen(t *testing.T) {
	reference, err := aclsampling.ParseSampleReference("0x640abcde000000c8")
	require.NoError(t, err)

	var stdout, stderr strings.Builder
	deps := dependencies{
		listen: func(_ context.Context, groupID uint32, handle func(aclsampling.PacketSample) error) error {
			require.Equal(t, uint32(5000), groupID)
			return handle(aclsampling.PacketSample{Reference: reference})
		},
	}

	err = run(context.Background(), []string{"listen", "--group-id=5000"}, &stdout, &stderr, deps)
	require.NoError(t, err)
	require.Empty(t, stderr.String())
	require.Equal(t, "0x640abcde000000c8\n", stdout.String())
}

func TestRunValidatesArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing subcommand", want: "subcommand is required"},
		{name: "unknown subcommand", args: []string{"unknown"}, want: "unknown ACL sample subcommand"},
		{name: "missing address", args: []string{"decode", "200"}, want: "--ovn-nb-addr is required"},
		{name: "missing value", args: []string{"decode", "--ovn-nb-addr=unix:/db.sock"}, want: "exactly one"},
		{name: "listen positional argument", args: []string{"listen", "unexpected"}, want: "does not accept positional"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			err := run(context.Background(), test.args, &stdout, &stderr, dependencies{
				newResolver: func(string) (sampleResolver, error) {
					return nil, errors.New("must not be called")
				},
			})
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestPrintUsageReturnsWriteError(t *testing.T) {
	wantErr := errors.New("write failed")
	err := printUsage(errorWriter{err: wantErr})
	require.ErrorIs(t, err, wantErr)
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}
