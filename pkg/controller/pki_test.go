package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestEnsureOVNCASecretCreatesMissingSecret(t *testing.T) {
	client := fake.NewSimpleClientset()
	c := &Controller{
		config: &Configuration{
			KubeClient: client,
		},
	}

	err := c.ensureOVNCASecret("kube-system", util.DefaultOVNDBTLSCA, []byte("ca"), []byte("key"))
	if err != nil {
		t.Fatalf("ensureOVNCASecret returned error: %v", err)
	}

	secret, err := client.CoreV1().Secrets("kube-system").Get(context.Background(), util.DefaultOVNDBTLSCA, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get created secret: %v", err)
	}
	if string(secret.Data["cacert"]) != "ca" {
		t.Fatalf("cacert = %q, want %q", string(secret.Data["cacert"]), "ca")
	}
	if string(secret.Data["cakey"]) != "key" {
		t.Fatalf("cakey = %q, want %q", string(secret.Data["cakey"]), "key")
	}
}

func TestEnsureOVNCASecretKeepsExistingSecret(t *testing.T) {
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.DefaultOVNDBTLSCA,
			Namespace: "kube-system",
		},
		Data: map[string][]byte{
			"cacert": []byte("old-ca"),
			"cakey":  []byte("old-key"),
		},
	}
	client := fake.NewSimpleClientset(existing)
	c := &Controller{
		config: &Configuration{
			KubeClient: client,
		},
	}

	err := c.ensureOVNCASecret("kube-system", util.DefaultOVNDBTLSCA, []byte("new-ca"), []byte("new-key"))
	if err != nil {
		t.Fatalf("ensureOVNCASecret returned error: %v", err)
	}

	secret, err := client.CoreV1().Secrets("kube-system").Get(context.Background(), util.DefaultOVNDBTLSCA, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get existing secret: %v", err)
	}
	if string(secret.Data["cacert"]) != "old-ca" {
		t.Fatalf("cacert = %q, want existing %q", string(secret.Data["cacert"]), "old-ca")
	}
	if string(secret.Data["cakey"]) != "old-key" {
		t.Fatalf("cakey = %q, want existing %q", string(secret.Data["cakey"]), "old-key")
	}
}
