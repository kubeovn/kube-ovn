package ipsec

import (
	"context"
	"encoding/json/v2"
	"errors"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// publishReceipt proves possession of the node key using an API-authenticated
// current-Pod CSR. Imported/reused certificates need this binding too; a CN or
// an annotation alone cannot establish the current Pod/Node identity.
func (a *Agent) publishReceipt(ctx context.Context) error {
	cm, err := a.config.Kube.CoreV1().ConfigMaps(a.config.Namespace).Get(ctx, CoordinationConfigMap, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return nil // Compatibility while the controller is upgraded first.
	}
	if err != nil {
		return err
	}
	state, err := DecodeCoordination([]byte(cm.Data["state"]))
	if err != nil {
		return err
	}
	if state.Phase == DisablingPhase {
		return nil // Protection remains until coordinated cleanup is complete.
	}
	if state.Phase == CleanupPhase {
		a.protectionMu.Lock()
		defer a.protectionMu.Unlock()
		p := a.protection
		if p == nil || state.Targets[a.config.NodeName] != p.owner.reservation.NodeUID {
			return errors.New("IPsec cleanup target does not bind the local protection owner")
		}
		if err := p.verify(); err != nil {
			return err
		}
		// No cleanup receipt or protection withdrawal is issued here. The
		// local switch is a separate precondition from runtime/kernel drain.
		return p.ovs.VerifyIPsecTunnelQuiescence(p.publicLease())
	}
	g, err := a.store.load("current")
	if err != nil || g == nil {
		return err
	}
	if state.Targets[a.config.NodeName] != g.NodeUID {
		return nil // New nodes still pass the independent local startup gate.
	}
	keyPEM, err := a.store.read(g, "private-key")
	if err != nil {
		return err
	}
	key, err := privateKey(keyPEM)
	if err != nil {
		return err
	}
	csr, err := newCSR(keyPEM, g.Chassis)
	if err != nil {
		return err
	}
	i := issuer{kube: a.config.Kube, node: a.config.NodeName, nodeUID: g.NodeUID, podUID: a.config.PodUID, namespace: a.config.Namespace, trustHash: state.TrustHash, duration: a.config.Duration}
	requestCtx, cancel := context.WithTimeout(ctx, a.config.RequestTimeout)
	defer cancel()
	cert, err := i.sign(requestCtx, csr)
	if err != nil {
		return err
	}
	trust, err := a.store.read(g, "ca-bundle")
	if err != nil {
		return err
	}
	if _, err := ValidateCertificate(cert, trust, &key.PublicKey, g.Chassis, time.Now()); err != nil {
		return err
	}
	req, err := a.config.Kube.CertificatesV1().CertificateSigningRequests().Get(ctx, i.name(csr), metav1.GetOptions{})
	if err != nil {
		return err
	}
	status := a.Status()
	if !identityPrepared(status, time.Now()) || state.Phase != PreparePhase && (!status.ConfigurationApplied || !status.RuntimeHealthy || !status.ProtectionArmed) || status.NodeUID != g.NodeUID || status.Generation != g.ID || status.TrustHash != state.TrustHash {
		return errors.New("current IPsec configuration has not acknowledged the coordination barrier")
	}
	data, err := signReceipt(Receipt{Generation: state.Generation, Epoch: state.Epoch, Phase: state.Phase, NodeName: a.config.NodeName, PodUID: a.config.PodUID, CSRName: req.Name, CSRUID: string(req.UID), Observed: time.Now(), Status: status}, key)
	if err != nil {
		return err
	}
	node, err := a.config.Kube.CoreV1().Nodes().Get(ctx, a.config.NodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if string(node.UID) != g.NodeUID {
		return errors.New("node UID changed before publishing IPsec receipt")
	}
	// resourceVersion prevents a replaced node or concurrent annotation writer
	// from being silently overwritten. Only this annotation is merged.
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"uid": node.UID, "resourceVersion": node.ResourceVersion, "annotations": map[string]string{ReceiptAnnotation: string(data)}}})
	if err != nil {
		return err
	}
	_, err = a.config.Kube.CoreV1().Nodes().Patch(ctx, node.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}
