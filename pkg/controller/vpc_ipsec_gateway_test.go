package controller

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestRenderSwanctlConf(t *testing.T) {
	gw := &kubeovnv1.VpcIPsecGateway{
		ObjectMeta: metav1.ObjectMeta{Name: "demo"},
		Spec: kubeovnv1.VpcIPsecGatewaySpec{
			RemoteEndpoint: "203.0.113.10",
			RemoteCIDRs:    []string{"10.200.0.0/16", "10.201.0.0/16"},
		},
	}
	conf, err := renderSwanctlConf(gw, []string{"10.16.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"vpc-ipsec-demo",
		"203.0.113.10",
		"10.16.0.0/16",
		"10.200.0.0/16,10.201.0.0/16",
	} {
		if !strings.Contains(conf, want) {
			t.Fatalf("expected conf to contain %q, got:\n%s", want, conf)
		}
	}
	if strings.Contains(conf, "secrets") {
		t.Fatal("connection conf must not embed psk secrets")
	}
}
