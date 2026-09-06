package util

import (
	"testing"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestGenIPsecGwName(t *testing.T) {
	if got := GenIPsecGwName("demo"); got != "vpc-ipsec-gw-demo" {
		t.Fatalf("unexpected name %s", got)
	}
}

func TestResolveIPsecPSKSecretRef(t *testing.T) {
	ns, key := ResolveIPsecPSKSecretRef(kubeovnv1.VpcIPsecPSKSecretRef{Name: "s"}, "kube-system")
	if ns != "kube-system" || key != DefaultIPsecPSKSecretKey {
		t.Fatalf("unexpected defaults ns=%s key=%s", ns, key)
	}
	ns, key = ResolveIPsecPSKSecretRef(kubeovnv1.VpcIPsecPSKSecretRef{Name: "s", Namespace: "ns", Key: "k"}, "kube-system")
	if ns != "ns" || key != "k" {
		t.Fatalf("unexpected overrides ns=%s key=%s", ns, key)
	}
}

func TestValidateIPsecGwStatefulSetNameLength(t *testing.T) {
	if err := ValidateIPsecGwStatefulSetNameLength("ok"); err != nil {
		t.Fatal(err)
	}
	long := make([]byte, 60)
	for i := range long {
		long[i] = 'a'
	}
	if err := ValidateIPsecGwStatefulSetNameLength(string(long)); err == nil {
		t.Fatal("expected length validation error")
	}
}
