package ovndbtls

import (
	"bytes"
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestReconcileCreatesMissingSecrets(t *testing.T) {
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	client := fake.NewSimpleClientset()
	manager := NewManager(Config{
		Client:       client,
		Namespace:    "kube-system",
		Now:          func() time.Time { return now },
		CADuration:   24 * time.Hour,
		LeafDuration: time.Hour,
	})

	if err := manager.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	for _, name := range []string{CASecretName, ServerSecretName, ClientSecretName} {
		if _, err := client.CoreV1().Secrets("kube-system").Get(context.Background(), name, metav1.GetOptions{}); err != nil {
			t.Fatalf("failed to get created secret %s: %v", name, err)
		}
	}

	server, err := client.CoreV1().Secrets("kube-system").Get(context.Background(), ServerSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get server secret: %v", err)
	}
	if _, ok := server.Data[KeyServerCert]; !ok {
		t.Fatalf("server secret missing %s", KeyServerCert)
	}
	if _, ok := server.Data[KeyTrustBundle]; !ok {
		t.Fatalf("server secret missing %s", KeyTrustBundle)
	}
}

func TestReconcileRenewsLeafAfterHalfLife(t *testing.T) {
	current := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	client := fake.NewSimpleClientset()
	manager := NewManager(Config{
		Client:       client,
		Namespace:    "kube-system",
		Now:          func() time.Time { return current },
		CADuration:   24 * time.Hour,
		LeafDuration: 2 * time.Hour,
	})
	if err := manager.Reconcile(context.Background()); err != nil {
		t.Fatalf("first Reconcile returned error: %v", err)
	}
	original, err := client.CoreV1().Secrets("kube-system").Get(context.Background(), ClientSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get client secret: %v", err)
	}
	originalCert := append([]byte(nil), original.Data[KeyClientCert]...)

	current = current.Add(30 * time.Minute)
	if err := manager.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile returned error: %v", err)
	}
	unchanged, err := client.CoreV1().Secrets("kube-system").Get(context.Background(), ClientSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get client secret: %v", err)
	}
	if !bytes.Equal(originalCert, unchanged.Data[KeyClientCert]) {
		t.Fatal("client cert changed before half-life")
	}

	current = current.Add(31 * time.Minute)
	if err := manager.Reconcile(context.Background()); err != nil {
		t.Fatalf("third Reconcile returned error: %v", err)
	}
	renewed, err := client.CoreV1().Secrets("kube-system").Get(context.Background(), ClientSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get client secret: %v", err)
	}
	if bytes.Equal(originalCert, renewed.Data[KeyClientCert]) {
		t.Fatal("client cert did not change after half-life")
	}
}

func TestCARotationStagesKeepOldAndNewTrustBeforeLeafReissue(t *testing.T) {
	current := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	client := fake.NewSimpleClientset()
	manager := NewManager(Config{
		Client:          client,
		Namespace:       "kube-system",
		Now:             func() time.Time { return current },
		CADuration:      2 * time.Hour,
		LeafDuration:    time.Hour,
		TransitionDelay: 10 * time.Minute,
	})
	if err := manager.Reconcile(context.Background()); err != nil {
		t.Fatalf("first Reconcile returned error: %v", err)
	}
	caSecret, err := client.CoreV1().Secrets("kube-system").Get(context.Background(), CASecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get ca secret: %v", err)
	}
	oldCA := append([]byte(nil), caSecret.Data[KeyCACert]...)

	current = current.Add(time.Hour)
	if err := manager.Reconcile(context.Background()); err != nil {
		t.Fatalf("rotation Reconcile returned error: %v", err)
	}
	caSecret, err = client.CoreV1().Secrets("kube-system").Get(context.Background(), CASecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get ca secret: %v", err)
	}
	if got := rotationStage(caSecret); got != StageTrustExpanding {
		t.Fatalf("stage = %s, want %s", got, StageTrustExpanding)
	}
	if !bytes.Contains(caSecret.Data[KeyTrustBundle], oldCA) {
		t.Fatal("trust bundle does not contain old CA")
	}
	if !bytes.Contains(caSecret.Data[KeyTrustBundle], caSecret.Data[keyNextCACert]) {
		t.Fatal("trust bundle does not contain next CA")
	}

	current = current.Add(11 * time.Minute)
	if err := manager.Reconcile(context.Background()); err != nil {
		t.Fatalf("leaf reissue Reconcile returned error: %v", err)
	}
	caSecret, err = client.CoreV1().Secrets("kube-system").Get(context.Background(), CASecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get ca secret: %v", err)
	}
	if got := rotationStage(caSecret); got != StageLeafReissuing {
		t.Fatalf("stage = %s, want %s", got, StageLeafReissuing)
	}
	clientSecret, err := client.CoreV1().Secrets("kube-system").Get(context.Background(), ClientSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get client secret: %v", err)
	}
	if !bytes.Equal(clientSecret.Data[KeyTrustBundle], caSecret.Data[KeyTrustBundle]) {
		t.Fatal("client trust bundle was not expanded before new leaf use")
	}
}
