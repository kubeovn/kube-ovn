package ipsec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Existing protection is never relaxed by a new Prepare, missing API data or
// an OVSDB rebuild. Only a fresh, controller-authorized enable can prepare
// certificates over the ordinary Pod network before publishing output marks.
func (a *Agent) protectionRequired(nodeUID string, externalIDs map[string]string) (bool, error) {
	reservation, err := a.store.loadProtection(nodeUID)
	if err != nil {
		return false, err
	}
	if reservation != nil && reservation.Required {
		return true, nil
	}
	for _, key := range []string{"ovn-ipsec-protection-node-uid", "ovn-ipsec-protection-lease", "ovn-ipsec-protection-mark", "ovn-ipsec-protection-reqid"} {
		if externalIDs[key] != "" {
			return true, nil
		}
	}
	_, err = os.Lstat(filepath.Join(a.config.ProtectionDir, "required"))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func (a *Agent) initialPreparation(ctx context.Context, nodeUID string, externalIDs map[string]string) (bool, error) {
	required, err := a.protectionRequired(nodeUID, externalIDs)
	if err != nil || required {
		return false, err
	}
	cm, err := a.config.Kube.CoreV1().ConfigMaps(a.config.Namespace).Get(ctx, CoordinationConfigMap, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	state, err := DecodeCoordination([]byte(cm.Data["state"]))
	if err != nil {
		return false, err
	}
	if state.Phase == PreparePhase {
		if state.Targets[a.config.NodeName] != nodeUID || nodeUID == "" {
			return false, errors.New("initial preparation does not bind the current Node UID")
		}
		return true, nil
	}
	if state.Phase != ArmPhase && state.Phase != EnabledPhase {
		return false, errors.New("IPsec is not in an enable phase")
	}
	return false, nil
}

func identityPrepared(status Status, now time.Time) bool {
	return status.NodeUID != "" && status.Chassis != "" && status.Generation != "" && status.CertificateHash != "" && status.TrustHash != "" && status.Expires.After(now)
}

// Reserve ownership before writing pending identities so an interrupted
// Prepare can use the ordinary guarded cleanup protocol. Reservation alone
// installs no policy, publishes no mark and leaves the signing network usable.
func (a *Agent) reservePreparation(nodeUID string) error {
	kernel, err := netlink.NewHandle(unix.NETLINK_XFRM)
	if err != nil {
		return err
	}
	defer kernel.Close()
	if err := kernel.SetSocketTimeout(3 * time.Second); err != nil {
		return err
	}
	owner, err := prepareProtection(a.store, nodeUID, kernel)
	if err != nil {
		return err
	}
	if owner.reservation.Required {
		return errors.New("initial preparation cannot replace required protection")
	}
	owner.reservation.Released = false
	return owner.save()
}

func (a *Agent) prepareIdentity(g *generation, trust []byte) error {
	certPEM, err := a.store.read(g, "certificate")
	if err != nil {
		return err
	}
	certs, err := Certificates(certPEM)
	if err != nil {
		return err
	}
	if err := a.store.save("current", g); err != nil {
		return err
	}
	a.setStatus(Status{
		Phase: "Prepared", NodeUID: g.NodeUID, Chassis: g.Chassis, Generation: g.ID,
		CertificateHash: digest(certPEM), TrustHash: digest(trust), Expires: certs[0].NotAfter,
	})
	return nil
}
