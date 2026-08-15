package speaker

import (
	"testing"

	"github.com/osrg/gobgp/v4/api"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestCollectEIPExpectedPrefixes(t *testing.T) {
	eips := []*kubeovnv1.IptablesEIP{
		{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{util.BgpAnnotation: "true"}},
			Spec:       kubeovnv1.IptablesEIPSpec{V4ip: "192.0.2.10", V6ip: "2001:db8::10"},
			Status:     kubeovnv1.IptablesEIPStatus{Ready: true},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{util.BgpAnnotation: "true"}},
			Spec:       kubeovnv1.IptablesEIPSpec{V4ip: "192.0.2.11"},
			Status:     kubeovnv1.IptablesEIPStatus{Ready: false},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{util.BgpAnnotation: "false"}},
			Spec:       kubeovnv1.IptablesEIPSpec{V4ip: "192.0.2.12"},
			Status:     kubeovnv1.IptablesEIPStatus{Ready: true},
		},
		{
			Spec:   kubeovnv1.IptablesEIPSpec{V4ip: "192.0.2.13"},
			Status: kubeovnv1.IptablesEIPStatus{Ready: true},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{util.BgpAnnotation: "true"}},
			Spec:       kubeovnv1.IptablesEIPSpec{V4ip: "192.0.2.10"},
			Status:     kubeovnv1.IptablesEIPStatus{Ready: true},
		},
	}

	prefixes := collectEIPExpectedPrefixes(eips)
	require.ElementsMatch(t, []string{"192.0.2.10/32"}, prefixes[api.Family_AFI_IP].UnsortedList())
	require.ElementsMatch(t, []string{"2001:db8::10/128"}, prefixes[api.Family_AFI_IP6].UnsortedList())
}

func TestSyncEIPRoutesRequiresGatewayName(t *testing.T) {
	t.Setenv(util.EnvGatewayName, "")

	err := (&Controller{}).syncEIPRoutes()
	require.ErrorContains(t, err, "failed to retrieve the name of the gateway")
}
