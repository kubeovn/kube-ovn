package ovs

import (
	"testing"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestMatchExternalIDs(t *testing.T) {
	t.Parallel()

	actual := map[string]string{"sg": "web", "vendor": "kube-ovn"}
	if !matchExternalIDs(actual, nil) {
		t.Fatal("empty wanted ids should match")
	}
	if !matchExternalIDs(actual, map[string]string{"sg": "web"}) {
		t.Fatal("exact key/value should match")
	}
	if matchExternalIDs(actual, map[string]string{"sg": "db"}) {
		t.Fatal("mismatched value should not match")
	}
	if !matchExternalIDs(actual, map[string]string{"sg": ""}) {
		t.Fatal("empty wanted value should mean key is present")
	}
	if matchExternalIDs(actual, map[string]string{"missing": ""}) {
		t.Fatal("empty wanted value should not match missing key")
	}
	if matchExternalIDs(map[string]string{}, map[string]string{"sg": "web"}) {
		t.Fatal("empty actual should not match non-empty wanted")
	}
	if !matchExternalIDsMode(map[string]string{"sg": ""}, map[string]string{"sg": ""}, false) {
		t.Fatal("empty wanted value without present-mode should match empty actual value")
	}
}

func TestHasVendor(t *testing.T) {
	t.Parallel()
	if hasVendor(nil) || hasVendor(map[string]string{}) {
		t.Fatal("empty external ids should not have vendor")
	}
	if !hasVendor(map[string]string{"vendor": util.CniTypeName}) {
		t.Fatal("kube-ovn vendor should match")
	}
	if hasVendor(map[string]string{"vendor": "other"}) {
		t.Fatal("foreign vendor should not match")
	}
}
