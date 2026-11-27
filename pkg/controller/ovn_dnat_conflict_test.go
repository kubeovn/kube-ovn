package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnlisters "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
)

func TestPortProtocol(t *testing.T) {
	for name, tc := range map[string]struct {
		protocol string
		want     string
	}{
		"empty defaults to tcp": {want: "tcp"},
		"lowercases protocol":   {protocol: "UDP", want: "udp"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := portProtocol(tc.protocol); got != tc.want {
				t.Fatalf("portProtocol(%q) = %q, want %q", tc.protocol, got, tc.want)
			}
		})
	}
}

func TestCanonicalPort(t *testing.T) {
	for name, tc := range map[string]struct {
		port string
		want string
	}{
		"canonical port":         {port: "80", want: "80"},
		"leading zero":           {port: "080", want: "80"},
		"plus sign":              {port: "+80", want: "80"},
		"invalid port unchanged": {port: "invalid", want: "invalid"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := canonicalPort(tc.port); got != tc.want {
				t.Fatalf("canonicalPort(%q) = %q, want %q", tc.port, got, tc.want)
			}
		})
	}
}

func TestIsOvnDnatDuplicatedCanonicalizesPorts(t *testing.T) {
	for name, externalPort := range map[string]string{
		"leading zero": "080",
		"plus sign":    "+80",
	} {
		t.Run(name, func(t *testing.T) {
			existing := &kubeovnv1.OvnDnatRule{
				ObjectMeta: metav1.ObjectMeta{Name: "existing", CreationTimestamp: metav1.Now()},
				Spec: kubeovnv1.OvnDnatRuleSpec{
					OvnEip:       "eip1",
					ExternalPort: externalPort,
					Protocol:     "tcp",
				},
				Status: kubeovnv1.OvnDnatRuleStatus{V4Eip: "10.0.0.1"},
			}
			indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			require.NoError(t, indexer.Add(existing))
			ctrl := &Controller{ovnDnatRulesLister: kubeovnlisters.NewOvnDnatRuleLister(indexer)}
			current := &kubeovnv1.OvnDnatRule{
				ObjectMeta: metav1.ObjectMeta{Name: "current"},
			}

			require.Error(t, ctrl.isOvnDnatDuplicated("eip1", "80", "tcp", current))
		})
	}
}

func TestOvnDnatHoldsPort(t *testing.T) {
	older := metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	newer := metav1.NewTime(older.Add(time.Minute))
	rule := func(name string, created metav1.Time, programmed bool) *kubeovnv1.OvnDnatRule {
		dnat := &kubeovnv1.OvnDnatRule{Name: name, CreationTimestamp: created}
		if programmed {
			dnat.Status.V4Eip = "10.0.0.1"
		}
		return dnat
	}

	terminating := rule("a", older, false)
	terminating.DeletionTimestamp = &newer
	for name, tc := range map[string]struct {
		other   *kubeovnv1.OvnDnatRule
		current *kubeovnv1.OvnDnatRule
		want    bool
	}{
		"programmed rule wins":                {other: rule("a", older, true), current: rule("b", newer, false), want: true},
		"older pending rule wins":             {other: rule("a", older, false), current: rule("b", newer, false), want: true},
		"name breaks timestamp tie":           {other: rule("a", older, false), current: rule("b", older, false), want: true},
		"terminating pending rule is ignored": {other: terminating, current: rule("b", newer, false)},
		"rule does not conflict with itself":  {other: rule("a", older, true), current: rule("a", older, true)},
	} {
		t.Run(name, func(t *testing.T) {
			if got := ovnDnatHoldsPort(tc.other, tc.current); got != tc.want {
				t.Fatalf("ovnDnatHoldsPort() = %t, want %t", got, tc.want)
			}
		})
	}
}
