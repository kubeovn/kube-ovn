package ipsec

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"
	"uuid"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	certv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kubeovn/kube-ovn/pkg/fileutil"
)

const (
	CleanupSignerName        = "kubeovn.io/ipsec-cleanup"
	CleanupReceiptAnnotation = "kube-ovn.io/ipsec-cleanup-receipt"
	ReleaseReceiptAnnotation = "kube-ovn.io/ipsec-release-receipt"
)

// CleanupReceipt proves a guarded, runtime-free drain observation, not a
// completed disable. The CSR supplies API-authenticated current Pod identity;
// no CA signature, encryption certificate or runtime acknowledgement is used.
type CleanupReceipt struct {
	Generation      string    `json:"generation"`
	Epoch           string    `json:"epoch"`
	Phase           string    `json:"phase"`
	DaemonSetUID    string    `json:"daemonSetUID"`
	TemplateHash    string    `json:"templateHash"`
	NBGlobalUUID    string    `json:"nbGlobalUUID"`
	SBGlobalUUID    string    `json:"sbGlobalUUID"`
	NodeName        string    `json:"nodeName"`
	NodeUID         string    `json:"nodeUID"`
	PodUID          string    `json:"podUID"`
	CSRName         string    `json:"csrName"`
	CSRUID          string    `json:"csrUID"`
	Chassis         string    `json:"chassis"`
	Lease           string    `json:"lease"`
	OVSUUID         string    `json:"ovsUUID"`
	BootID          string    `json:"bootID"`
	Mark            uint32    `json:"mark"`
	Reqid           uint32    `json:"reqid"`
	IdentityCleared bool      `json:"identityCleared"`
	Released        bool      `json:"released"`
	Observed        time.Time `json:"observed"`
}

func cleanupReceiptHash(payload []byte) [32]byte {
	return sha256.Sum256(append([]byte("kube-ovn IPsec guarded cleanup receipt v1\x00"), payload...))
}

func signCleanupReceipt(receipt CleanupReceipt, key *rsa.PrivateKey) ([]byte, error) {
	payload, err := json.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	hash := cleanupReceiptHash(payload)
	signature, err := rsa.SignPSS(rand.Reader, key, crypto.SHA256, hash[:], &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash})
	if err != nil {
		return nil, err
	}
	return json.Marshal(SignedReceipt{Payload: payload, Signature: signature})
}

// VerifyCleanupReceipt additionally requires the caller to authenticate the
// referenced CSR and compare its live bound Pod with the frozen cleanup init
// template. Possession of this key or a mutable annotation alone is insufficient.
func VerifyCleanupReceipt(data []byte, key *rsa.PublicKey, state *Coordination, nodeName, nodeUID string, now time.Time) (*CleanupReceipt, error) {
	if len(data) > 16<<10 || key == nil || state == nil || state.Phase != CleanupPhase && state.Phase != ReleasePhase {
		return nil, errors.New("invalid IPsec guarded cleanup envelope or phase")
	}
	var signed SignedReceipt
	if err := json.Unmarshal(data, &signed); err != nil {
		return nil, err
	}
	hash := cleanupReceiptHash(signed.Payload)
	if err := rsa.VerifyPSS(key, crypto.SHA256, hash[:], signed.Signature, &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash}); err != nil {
		return nil, err
	}
	var receipt CleanupReceipt
	if err := json.Unmarshal(signed.Payload, &receipt); err != nil {
		return nil, err
	}
	if receipt.Generation != state.Generation || receipt.Epoch != state.Epoch || receipt.Phase != state.Phase || receipt.DaemonSetUID != state.DaemonSetUID || receipt.TemplateHash != state.TemplateHash || receipt.NBGlobalUUID != state.NBGlobalUUID || receipt.SBGlobalUUID != state.SBGlobalUUID || receipt.NodeName != nodeName || receipt.NodeUID != nodeUID || nodeUID == "" || state.Targets[nodeName] != nodeUID {
		return nil, errors.New("IPsec cleanup receipt does not bind the current barrier and node")
	}
	if !receipt.IdentityCleared || state.Phase == ReleasePhase && !receipt.Released || receipt.PodUID == "" || receipt.CSRName == "" || receipt.CSRUID == "" || receipt.Chassis == "" || receipt.Lease == "" || receipt.Mark == 0 || receipt.Reqid == 0 || receipt.Reqid > 1<<31-1 || !ovsdb.IsValidUUID(receipt.OVSUUID) {
		return nil, errors.New("IPsec cleanup receipt lacks a live protection binding")
	}
	boot, err := uuid.Parse(receipt.BootID)
	if err != nil || boot == uuid.Nil() || boot.String() != receipt.BootID {
		return nil, errors.New("IPsec cleanup receipt has an invalid kernel boot identity")
	}
	if receipt.Observed.After(now.Add(5*time.Second)) || !receipt.Observed.After(now.Add(-2*time.Minute)) {
		return nil, errors.New("IPsec cleanup receipt is stale or from the future")
	}
	return &receipt, nil
}

// A separate local attestation key also covers an interrupted initial Arm that
// never obtained an encryption identity. It cannot be loaded by IKE or request
// a certificate from the encryption signer. Run holds the private owner lock.
func (s store) cleanupKey(reservation protectionReservation) ([]byte, error) {
	name := "cleanup-key-" + digest([]byte(reservation.NodeUID+":"+reservation.Lease)) + ".pem"
	path := filepath.Join(s.dir, name)
	data, err := readRegularFile(path)
	if errors.Is(err, os.ErrNotExist) {
		data, err = newPrivateKey()
		if err == nil {
			err = fileutil.AtomicWriteFile(path, data, 0o600)
		}
	}
	if err != nil {
		return nil, err
	}
	if _, err := privateKey(data); err != nil {
		return nil, err // Never replace unreadable or malformed evidence.
	}
	return data, nil
}

func (a *Agent) cleanupBinding(ctx context.Context, claim CleanupReceipt, keyPEM []byte) (*certv1.CertificateSigningRequest, error) {
	request, err := newCSR(keyPEM, claim.Chassis)
	if err != nil {
		return nil, err
	}
	name := requestName("cleanup:"+claim.NodeUID+":"+claim.PodUID+":"+claim.Generation+":"+claim.Epoch, request)
	client := a.config.Kube.CertificatesV1().CertificateSigningRequests()
	req := &certv1.CertificateSigningRequest{
		Name: name, Annotations: map[string]string{NodeNameAnnotation: claim.NodeName, NodeUIDAnnotation: claim.NodeUID},
		Spec: certv1.CertificateSigningRequestSpec{Request: request, SignerName: CleanupSignerName, Usages: []certv1.KeyUsage{certv1.UsageIPsecTunnel}},
	}
	actual, err := client.Create(ctx, req, metav1.CreateOptions{})
	if k8serrors.IsAlreadyExists(err) {
		actual, err = client.Get(ctx, name, metav1.GetOptions{})
	}
	if err != nil {
		return nil, err
	}
	if actual.UID == "" || actual.DeletionTimestamp != nil || !bytes.Equal(actual.Spec.Request, request) || actual.Spec.SignerName != CleanupSignerName || !slices.Equal(actual.Spec.Usages, req.Spec.Usages) || actual.Spec.ExpirationSeconds != nil || actual.Annotations[NodeNameAnnotation] != claim.NodeName || actual.Annotations[NodeUIDAnnotation] != claim.NodeUID {
		return nil, errors.New("IPsec cleanup CSR conflicts with the attested request")
	}
	if actual.Spec.Username != "system:serviceaccount:"+a.config.Namespace+":kube-ovn-cni" || !slices.Equal(actual.Spec.Extra["authentication.kubernetes.io/pod-uid"], []string{claim.PodUID}) || len(actual.Spec.Extra["authentication.kubernetes.io/pod-name"]) != 1 {
		return nil, errors.New("IPsec cleanup CSR is not authenticated as the current bound Pod")
	}
	for _, condition := range actual.Status.Conditions {
		if condition.Status == corev1.ConditionTrue && (condition.Type == certv1.CertificateDenied || condition.Type == certv1.CertificateFailed) {
			return nil, errors.New("IPsec cleanup binding CSR is denied or failed")
		}
	}
	if len(actual.Status.Certificate) != 0 {
		return nil, errors.New("IPsec cleanup binding must not issue an encryption certificate")
	}
	return actual, nil
}

func (a *Agent) publishCleanupReceipt(ctx context.Context, cm *corev1.ConfigMap, state *Coordination, claim CleanupReceipt) error {
	keyPEM, err := a.store.cleanupKey(protectionReservation{NodeUID: claim.NodeUID, Lease: claim.Lease})
	if err != nil {
		return err
	}
	request, err := a.cleanupBinding(ctx, claim, keyPEM)
	if err != nil {
		return err
	}
	claim.CSRName, claim.CSRUID = request.Name, string(request.UID)
	// Binding the CSR may take an API round trip. Repeat all negative kernel
	// and local tunnel checks; never sign a historical drain.json or Status.
	current, err := a.observeCleanup(state)
	if err != nil {
		return err
	}
	if current.Lease != claim.Lease || current.Mark != claim.Mark || current.Reqid != claim.Reqid || current.OVSUUID != claim.OVSUUID || current.BootID != claim.BootID || current.NodeUID != claim.NodeUID || current.Chassis != claim.Chassis {
		return errors.New("IPsec protection changed before publishing cleanup evidence")
	}
	latest, err := a.config.Kube.CoreV1().ConfigMaps(a.config.Namespace).Get(ctx, CoordinationConfigMap, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if latest.UID != cm.UID || latest.ResourceVersion != cm.ResourceVersion || latest.Data["state"] != cm.Data["state"] {
		return errors.New("IPsec cleanup challenge changed during identity binding")
	}
	node, err := a.config.Kube.CoreV1().Nodes().Get(ctx, a.config.NodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if string(node.UID) != claim.NodeUID || node.DeletionTimestamp != nil {
		return errors.New("IPsec Node UID changed before publishing cleanup evidence")
	}
	key, err := privateKey(keyPEM)
	if err != nil {
		return err
	}
	claim.Observed = time.Now()
	data, err := signCleanupReceipt(claim, key)
	if err != nil {
		return err
	}
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"uid": node.UID, "resourceVersion": node.ResourceVersion, "annotations": map[string]string{CleanupReceiptAnnotation: string(data)}}})
	if err != nil {
		return err
	}
	_, err = a.config.Kube.CoreV1().Nodes().Patch(ctx, node.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}
