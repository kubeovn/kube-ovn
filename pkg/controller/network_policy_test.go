package controller

import (
	"testing"

	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestParsePolicyFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		annotation     *string
		wantProviders  map[string]struct{}
		wantIncludeSvc bool
		wantErr        bool
	}{
		{
			name:           "annotation omitted",
			annotation:     nil,
			wantProviders:  nil,
			wantIncludeSvc: true,
			wantErr:        false,
		},
		{
			name:       "primary only",
			annotation: ptrString("primary"),
			wantProviders: map[string]struct{}{
				util.OvnProvider: {},
			},
			wantIncludeSvc: true,
			wantErr:        false,
		},
		{
			name:       "secondary only",
			annotation: ptrString("ns1/net1"),
			wantProviders: map[string]struct{}{
				"net1.ns1." + util.OvnProvider: {},
			},
			wantIncludeSvc: false,
			wantErr:        false,
		},
		{
			name:       "primary and secondary",
			annotation: ptrString(" primary , ns1/net1 "),
			wantProviders: map[string]struct{}{
				util.OvnProvider:               {},
				"net1.ns1." + util.OvnProvider: {},
			},
			wantIncludeSvc: true,
			wantErr:        false,
		},
		{
			name:       "invalid all",
			annotation: ptrString("all"),
			wantErr:    true,
		},
		{
			name:       "invalid default",
			annotation: ptrString("default"),
			wantErr:    true,
		},
		{
			name:       "invalid no entries",
			annotation: ptrString(","),
			wantErr:    true,
		},
		{
			name:       "invalid token",
			annotation: ptrString("foo"),
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			np := &netv1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "np",
					Namespace: "default",
				},
			}
			if tt.annotation != nil {
				np.Annotations = map[string]string{
					policyForAnnotation: *tt.annotation,
				}
			}

			providers, includeSvc, err := parsePolicyFor(np)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantIncludeSvc, includeSvc)
			if tt.wantProviders == nil {
				require.Nil(t, providers)
				return
			}
			require.Equal(t, tt.wantProviders, providers)
		})
	}
}

func ptrString(s string) *string {
	return &s
}
