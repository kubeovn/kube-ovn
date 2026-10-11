package ipsec

import (
	"context"
	"crypto"
	"crypto/rsa"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
)

// runCleanup is the finite cleanup init container used when the feature is off.
// It never starts IKE, imports an identity, or issues a certificate. OVS
// runs in a separate Pod and can use its live protection
// endpoint while CNI waits for this node to finish its own cleanup.
func (a *Agent) runCleanup(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer a.closeProtection()
	defer func() {
		if a.ovs != nil {
			a.ovs.Close()
		}
	}()
	a.beat.Store(time.Now().UnixNano())
	if err := a.serveStatus(ctx); err != nil {
		return err
	}
	serving := false
	for ctx.Err() == nil {
		a.beat.Store(time.Now().UnixNano())
		needed, err := a.restoreCleanupProtection(ctx)
		if err == nil && needed && !serving {
			err = a.serveProtection(ctx)
			serving = err == nil
		}
		if err == nil && needed {
			state, stateErr := a.cleanupState(ctx)
			switch {
			case stateErr != nil:
				err = stateErr
			case state.Phase == CleanupPhase:
				err = a.cleanupPreflight(ctx)
			case state.Phase == ReleasePhase:
				a.protectionMu.Lock()
				required := a.protection != nil && a.protection.owner.reservation.Required
				a.protectionMu.Unlock()
				if required {
					err = a.cleanupPreflight(ctx)
				}
				if err == nil {
					err = a.releaseCleanup(ctx, state)
				}
			}
		}
		status := Status{Phase: "CleanupIdle"}
		if needed {
			status.Phase = "CleanupDrained"
			a.protectionMu.Lock()
			if a.protection != nil {
				status.NodeUID, status.Chassis = a.protection.owner.reservation.NodeUID, a.protection.chassis
			}
			a.protectionMu.Unlock()
		}
		if a.Status().Phase == "Disabled" {
			status.Phase = "Disabled"
		}
		if err != nil {
			status.Phase, status.Reason = "CleanupBlocked", err.Error()
			klog.ErrorS(err, "IPsec cleanup retains protection")
		}
		a.setStatus(status)
		if err == nil && (!needed || status.Phase == "Disabled") {
			return nil
		}
		a.beat.Store(time.Now().UnixNano())
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
	return ctx.Err()
}

// A fresh disabled installation must not create durable required intent or a
// lease. Lost private evidence, a replaced Node UID and foreign OVS leases are
// failures, not evidence that encryption was never active.
func (a *Agent) restoreCleanupProtection(ctx context.Context) (bool, error) {
	if a.ovs == nil {
		var err error
		a.ovs, err = ovs.NewCNIVswitchClient("unix:" + a.config.OVSSocket)
		if err != nil {
			return false, err
		}
	}
	row, err := a.ovs.IPsecDatapathConfiguration()
	if err != nil {
		return false, err
	}
	node, err := a.config.Kube.CoreV1().Nodes().Get(ctx, a.config.NodeName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	if node.UID == "" || node.DeletionTimestamp != nil {
		return false, errors.New("IPsec cleanup needs a live Node UID")
	}
	reservation, err := a.store.loadProtection(string(node.UID))
	if err != nil {
		return false, err
	}
	if reservation == nil {
		for _, key := range []string{"certificate", "private_key", "ca_cert"} {
			if row.OtherConfig[key] != "" {
				return false, errors.New("IPsec identity paths exist without their private ownership reservation")
			}
		}
		if err := a.noCleanupEvidence(row.ExternalIDs); err != nil {
			return false, err
		}
		// A frozen node may be disabled before Prepare writes any identity.
		// It still owes a bound release receipt, so use the same guarded
		// transition rather than exit and strand the global Release barrier.
		state, err := a.cleanupState(ctx)
		if k8serrors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if state.Targets[a.config.NodeName] != string(node.UID) || state.Phase == DisabledPhase {
			return false, nil
		}
		if state.Phase != DisablingPhase && state.Phase != CleanupPhase && state.Phase != ReleasePhase {
			return false, errors.New("IPsec cleanup is waiting for disable authorization")
		}
		return true, a.ensureProtection(string(node.UID), row.ExternalIDs["system-id"])
	}
	if !reservation.Required && reservation.Released {
		// A crash after the guarded release but before the API receipt patch
		// leaves a durable, non-required reservation. Recreate only the local
		// owner in Release so the signed receipt can be retried; never re-arm
		// kernel protection on this recovery path.
		cm, stateErr := a.config.Kube.CoreV1().ConfigMaps(a.config.Namespace).Get(ctx, CoordinationConfigMap, metav1.GetOptions{})
		if stateErr != nil {
			return false, stateErr
		}
		state, stateErr := DecodeCoordination([]byte(cm.Data["state"]))
		if stateErr != nil {
			return false, stateErr
		}
		if state.Phase == DisabledPhase {
			a.setStatus(Status{Phase: "Disabled", NodeUID: string(node.UID)})
			return false, nil
		}
		if state.Phase != ReleasePhase {
			return false, errors.New("inactive IPsec reservation is waiting for Release or Disabled")
		}
		a.protectionMu.Lock()
		defer a.protectionMu.Unlock()
		if a.protection == nil {
			kernel, kernelErr := netlink.NewHandle(unix.NETLINK_XFRM)
			if kernelErr != nil {
				return false, kernelErr
			}
			if kernelErr = kernel.SetSocketTimeout(3 * time.Second); kernelErr != nil {
				kernel.Close()
				return false, kernelErr
			}
			a.protection = &agentProtection{owner: &protectionOwner{store: a.store, reservation: *reservation, kernel: kernel}, ovs: a.ovs, chassis: row.ExternalIDs["system-id"]}
		}
		return true, nil
	}
	// Do not kill/adopt another daemon, even while the feature is disabled.
	if err := checkLegacyMonitor(a.config.OVSSocket); err != nil {
		return true, err
	}
	if err := checkIKEPorts(); err != nil {
		return true, err
	}
	if err := checkOVNProtection(ctx); err != nil {
		return true, err
	}
	return true, a.ensureProtection(string(node.UID), row.ExternalIDs["system-id"])
}

func (a *Agent) noCleanupEvidence(externalIDs map[string]string) error {
	for _, key := range []string{"ovn-ipsec-protection-node-uid", "ovn-ipsec-protection-lease", "ovn-ipsec-protection-mark", "ovn-ipsec-protection-reqid"} {
		if externalIDs[key] != "" {
			return errors.New("OVS protection exists without its private ownership reservation")
		}
	}
	for _, path := range []string{filepath.Join(a.config.ProtectionDir, "required"), filepath.Join(a.store.dir, "current.json"), filepath.Join(a.store.dir, "pending.json"), filepath.Join(a.store.dir, "connections")} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			if err != nil {
				return err
			}
			return errors.New("IPsec cleanup evidence exists without its private ownership reservation")
		}
	}
	return nil
}

// This is a live preflight only, never a Disabled receipt or authorization to
// remove output protection. No CA or cert-manager access is needed on this path.
func (a *Agent) cleanupPreflight(ctx context.Context) error {
	cm, err := a.config.Kube.CoreV1().ConfigMaps(a.config.Namespace).Get(ctx, CoordinationConfigMap, metav1.GetOptions{})
	if err != nil {
		return err
	}
	state, err := DecodeCoordination([]byte(cm.Data["state"]))
	if err != nil {
		return err
	}
	if state.Phase != CleanupPhase && state.Phase != ReleasePhase {
		return errors.New("IPsec cleanup is waiting for the controller's live NB/SB barrier")
	}
	claim, err := a.observeCleanup(state)
	if err != nil {
		return err
	}
	return a.publishCleanupReceipt(ctx, cm, state, claim)
}

func (a *Agent) cleanupState(ctx context.Context) (*Coordination, error) {
	cm, err := a.config.Kube.CoreV1().ConfigMaps(a.config.Namespace).Get(ctx, CoordinationConfigMap, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return DecodeCoordination([]byte(cm.Data["state"]))
}

func (a *Agent) releaseCleanup(ctx context.Context, state *Coordination) error {
	latest, err := a.cleanupState(ctx)
	if err != nil {
		return err
	}
	if latest.Phase != ReleasePhase || latest.Generation != state.Generation || latest.Epoch != state.Epoch || latest.NBGlobalUUID != state.NBGlobalUUID || latest.SBGlobalUUID != state.SBGlobalUUID {
		return errors.New("IPsec release challenge changed before guard withdrawal")
	}
	a.protectionMu.Lock()
	p := a.protection
	if p == nil || state.Targets[a.config.NodeName] != p.owner.reservation.NodeUID {
		a.protectionMu.Unlock()
		return errors.New("IPsec release target does not bind the local protection owner")
	}
	if p.owner.reservation.Required {
		if _, err := a.store.liveDrainInventory(p.owner.reservation, p.owner.kernel); err != nil {
			a.protectionMu.Unlock()
			return err
		}
	}
	row, err := p.ovs.IPsecDatapathConfiguration()
	if err == nil {
		lease := p.publicLease()
		lease.OVSUUID = row.UUID
		err = p.ovs.WithdrawIPsecProtection(lease)
	}
	if err == nil {
		err = p.owner.release()
	}
	a.protectionMu.Unlock()
	if err != nil {
		return err
	}
	reservation := p.owner.reservation
	if err := a.publishReleaseReceipt(ctx, state, reservation); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(a.config.ProtectionDir, "required")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Retain the inactive private reservation after release. It distinguishes
	// owned certificate/connection history from lost ownership evidence, and
	// lets a restarted init container retry its release receipt.
	a.setStatus(Status{Phase: "Disabled", NodeUID: state.Targets[a.config.NodeName]})
	return nil
}

func (a *Agent) publishReleaseReceipt(ctx context.Context, state *Coordination, reservation protectionReservation) error {
	node, err := a.config.Kube.CoreV1().Nodes().Get(ctx, a.config.NodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	var signed SignedReceipt
	if err := json.Unmarshal([]byte(node.Annotations[CleanupReceiptAnnotation]), &signed); err != nil {
		return err
	}
	var claim CleanupReceipt
	if err := json.Unmarshal(signed.Payload, &claim); err != nil {
		return err
	}
	if (claim.Phase != CleanupPhase && claim.Phase != ReleasePhase || claim.Released) || claim.NodeName != node.Name || claim.NodeUID != string(node.UID) ||
		claim.NodeUID != reservation.NodeUID || claim.Lease != reservation.Lease || claim.Mark != reservation.Mark || claim.Reqid != reservation.Reqid ||
		state.Phase != ReleasePhase || claim.Generation != state.Generation || claim.DaemonSetUID != state.DaemonSetUID || claim.TemplateHash != state.TemplateHash ||
		claim.NBGlobalUUID != state.NBGlobalUUID || claim.SBGlobalUUID != state.SBGlobalUUID || state.Targets[node.Name] != string(node.UID) {
		return errors.New("IPsec release receipt does not bind the cleanup lease and Node")
	}
	keyPEM, err := a.store.cleanupKey(reservation)
	if err != nil {
		return err
	}
	key, err := privateKey(keyPEM)
	if err != nil {
		return err
	}
	hash := cleanupReceiptHash(signed.Payload)
	if err := rsa.VerifyPSS(&key.PublicKey, crypto.SHA256, hash[:], signed.Signature, &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash}); err != nil {
		return fmt.Errorf("verify cleanup receipt before release: %w", err)
	}
	request, err := a.cleanupBinding(ctx, claim, keyPEM)
	if err != nil {
		return err
	}
	if string(request.UID) != claim.CSRUID {
		return errors.New("IPsec cleanup CSR changed before release receipt")
	}
	claim.Phase, claim.Epoch, claim.Released, claim.Observed = ReleasePhase, state.Epoch, true, time.Now()
	data, err := signCleanupReceipt(claim, key)
	if err != nil {
		return err
	}
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"uid": node.UID, "resourceVersion": node.ResourceVersion, "annotations": map[string]string{ReleaseReceiptAnnotation: string(data)}}})
	if err != nil {
		return err
	}
	_, err = a.config.Kube.CoreV1().Nodes().Patch(ctx, node.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

func (a *Agent) observeCleanup(state *Coordination) (CleanupReceipt, error) {
	if err := checkLegacyMonitor(a.config.OVSSocket); err != nil {
		return CleanupReceipt{}, err
	}
	if err := checkIKEPorts(); err != nil {
		return CleanupReceipt{}, err
	}
	a.protectionMu.Lock()
	defer a.protectionMu.Unlock()
	p := a.protection
	if p == nil || state.Targets[a.config.NodeName] != p.owner.reservation.NodeUID {
		return CleanupReceipt{}, errors.New("IPsec cleanup target does not bind the local protection owner")
	}
	if err := p.verify(); err != nil {
		return CleanupReceipt{}, err
	}
	row, err := p.ovs.IPsecDatapathConfiguration()
	if err != nil {
		return CleanupReceipt{}, err
	}
	lease := p.publicLease()
	lease.OVSUUID = row.UUID
	if err := p.ovs.VerifyIPsecTunnelQuiescence(lease); err != nil {
		return CleanupReceipt{}, err
	}
	bootID, err := a.store.liveDrainInventory(p.owner.reservation, p.owner.kernel)
	if err != nil {
		return CleanupReceipt{}, err
	}
	paths, err := a.store.cleanupIdentity(row.OtherConfig, a.config.NodeName, a.config.Namespace, lease.NodeUID, lease.Chassis)
	if err != nil {
		return CleanupReceipt{}, err
	}
	if err := p.ovs.ClearIPsecIdentity(lease, paths); err != nil {
		return CleanupReceipt{}, err
	}
	row, err = p.ovs.IPsecDatapathConfiguration()
	if err != nil {
		return CleanupReceipt{}, err
	}
	for _, key := range []string{"certificate", "private_key", "ca_cert"} {
		if row.OtherConfig[key] != "" {
			return CleanupReceipt{}, errors.New("IPsec cleanup identity removal did not converge")
		}
	}
	if err := p.ovs.VerifyIPsecTunnelQuiescence(lease); err != nil {
		return CleanupReceipt{}, err
	}
	finalBootID, err := a.store.liveDrainInventory(p.owner.reservation, p.owner.kernel)
	if err != nil {
		return CleanupReceipt{}, err
	}
	if finalBootID != bootID {
		return CleanupReceipt{}, errors.New("IPsec cleanup kernel boot changed during identity removal")
	}
	return CleanupReceipt{
		Generation: state.Generation, Epoch: state.Epoch, Phase: state.Phase, DaemonSetUID: state.DaemonSetUID, TemplateHash: state.TemplateHash,
		NBGlobalUUID: state.NBGlobalUUID, SBGlobalUUID: state.SBGlobalUUID,
		NodeName: a.config.NodeName, NodeUID: lease.NodeUID, PodUID: a.config.PodUID, Chassis: lease.Chassis,
		Lease: lease.Lease, Mark: lease.Mark, Reqid: lease.Reqid, OVSUUID: lease.OVSUUID, BootID: bootID, IdentityCleared: true,
	}, nil
}
