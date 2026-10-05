package ipsec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubeovn/kube-ovn/pkg/fileutil"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
)

type agentProtection struct {
	owner   *protectionOwner
	ovs     *ovs.VswitchClient
	chassis string
}

func checkOVNProtection(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "/usr/bin/ovn-controller", "--version").Output()
	if err != nil {
		return fmt.Errorf("check OVN output protection support: %w", err)
	}
	if !bytes.Contains(output, []byte("\nIPsec output protection version 1\n")) {
		return errors.New("ovn-controller does not support IPsec output protection version 1")
	}
	return nil
}

func (p *agentProtection) publicLease() ovs.IPsecProtection {
	r := p.owner.reservation
	return ovs.IPsecProtection{NodeUID: r.NodeUID, Lease: r.Lease, Chassis: p.chassis, Mark: r.Mark, Reqid: r.Reqid}
}

func (a *Agent) bootstrapProtection(ctx context.Context, online bool) error {
	var err error
	if a.ovs == nil {
		a.ovs, err = ovs.NewCNIVswitchClient("unix:" + a.config.OVSSocket)
		if err != nil {
			return err
		}
	}
	row, err := a.ovs.IPsecDatapathConfiguration()
	if err != nil {
		return err
	}
	chassis := row.ExternalIDs["system-id"]
	if chassis == "" {
		return errors.New("OVS system-id is not available for protection bootstrap")
	}
	var nodeUID string
	if !online {
		g, err := a.store.load("current")
		if err != nil {
			return err
		}
		if g != nil {
			if g.NodeName != a.config.NodeName || g.Namespace != a.config.Namespace || g.Chassis != chassis || g.NodeUID == "" {
				return errors.New("current identity does not bind this protection bootstrap")
			}
			nodeUID = g.NodeUID
		}
	}
	if nodeUID == "" {
		node, err := a.config.Kube.CoreV1().Nodes().Get(ctx, a.config.NodeName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		nodeUID = string(node.UID)
	}
	if online {
		preparing, err := a.initialPreparation(ctx, nodeUID, row.ExternalIDs)
		if err != nil || preparing {
			return err
		}
	} else if required, err := a.protectionRequired(nodeUID, row.ExternalIDs); err != nil || !required {
		return errors.Join(err, errors.New("initial IPsec preparation needs the controller"))
	}
	return a.ensureProtection(nodeUID, chassis)
}

// ensureProtection requires the node owner lock held by Run. Guards are read
// back before a mark is published and before a certificate/runtime is enabled.
// On any failure the durable intent and existing guards remain installed.
func (a *Agent) ensureProtection(nodeUID, chassis string) error {
	a.protectionMu.Lock()
	defer a.protectionMu.Unlock()
	if err := a.prepareProtectionDirectory(); err != nil {
		return err
	}
	if err := fileutil.AtomicWriteFile(filepath.Join(a.config.ProtectionDir, "required"), []byte("IPsec output protection version 1\n"), 0o640); err != nil {
		return err
	}
	if a.protection == nil {
		kernel, err := netlink.NewHandle(unix.NETLINK_XFRM)
		if err != nil {
			return err
		}
		if err := kernel.SetSocketTimeout(3 * time.Second); err != nil {
			kernel.Close()
			return err
		}
		owner, err := prepareProtection(a.store, nodeUID, kernel)
		if err != nil {
			kernel.Close()
			return err
		}
		a.protection = &agentProtection{owner: owner, ovs: a.ovs, chassis: chassis}
	}
	p := a.protection
	if p.owner.reservation.NodeUID != nodeUID || p.chassis != chassis {
		return errors.New("IPsec protection cannot adopt a changed Node UID or chassis")
	}
	if err := p.owner.arm(); err != nil {
		return err
	}
	if err := p.ovs.PublishIPsecProtection(p.publicLease()); err != nil {
		return err
	}
	return p.verify()
}

func (p *agentProtection) verify() error {
	if err := p.owner.verify(); err != nil {
		return err
	}
	return p.ovs.VerifyIPsecProtection(p.publicLease())
}

func (a *Agent) protectionArmed(nodeUID, chassis string) bool {
	a.protectionMu.Lock()
	defer a.protectionMu.Unlock()
	p := a.protection
	return p != nil && p.owner.reservation.NodeUID == nodeUID && p.chassis == chassis && p.verify() == nil
}

func (a *Agent) closeProtection() {
	a.protectionMu.Lock()
	defer a.protectionMu.Unlock()
	if a.protection != nil {
		a.protection.owner.kernel.Close()
		a.protection = nil
	}
}
