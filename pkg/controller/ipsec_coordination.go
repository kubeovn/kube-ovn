package controller

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	certv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/component-helpers/scheduling/corev1/nodeaffinity"

	"github.com/kubeovn/kube-ovn/pkg/ipsec"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func ipsecPublicHash(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func freezeIPsecTargets(ds *appsv1.DaemonSet, nodes []corev1.Node, trust []byte, enabled bool) (*ipsec.Coordination, error) {
	if ds.UID == "" {
		return nil, errors.New("CNI DaemonSet UID is unavailable")
	}
	if !enabled && !slices.ContainsFunc(ds.Spec.Template.Spec.Containers, func(container corev1.Container) bool { return container.Name == "ipsec" }) {
		return nil, errors.New("CNI DaemonSet has not prepared the IPsec container")
	}
	data, err := json.Marshal(ds.Spec.Template, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	targets, err := matchingIPsecTargets(ds, nodes)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, errors.New("IPsec cannot enable an empty node cohort")
	}
	state := &ipsec.Coordination{Version: 1, Generation: rand.Text(), Epoch: rand.Text(), Phase: ipsec.PreparePhase, DaemonSetUID: string(ds.UID), TemplateHash: ipsecPublicHash(data), TrustHash: ipsecPublicHash(trust), Targets: targets}
	if enabled {
		// Preserve a previously enabled legacy cluster during rolling takeover.
		// This records the cohort, not proof that legacy nodes are protected.
		state.Phase = ipsec.EnabledPhase
	}
	return state, nil
}

func matchingIPsecTargets(ds *appsv1.DaemonSet, nodes []corev1.Node) (map[string]string, error) {
	targets := make(map[string]string)
	affinity := nodeaffinity.GetRequiredNodeAffinity(&corev1.Pod{Spec: ds.Spec.Template.Spec})
	for _, node := range nodes {
		if node.DeletionTimestamp != nil || ds.Spec.Template.Spec.NodeName != "" && ds.Spec.Template.Spec.NodeName != node.Name {
			continue
		}
		match, err := affinity.Match(&node)
		if err != nil {
			return nil, fmt.Errorf("match IPsec target %s: %w", node.Name, err)
		}
		if match {
			if node.UID == "" {
				return nil, fmt.Errorf("IPsec target %s has no UID", node.Name)
			}
			targets[node.Name] = string(node.UID)
		}
	}
	return targets, nil
}

func encodeIPsecCoordination(cm *corev1.ConfigMap, state *ipsec.Coordination) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data["state"] = string(data)
	return nil
}

// reconcileIPsecCoordination runs only on the controller leader. It never
// waits synchronously for Pods/CSRs, so signing and chassis registration can
// progress while the initial NB switch remains off. Failed barriers retain
// their target UIDs; missing/offline/replaced nodes are not silently dropped.
func (c *Controller) reconcileIPsecCoordination(ctx context.Context) error {
	client := c.config.KubeClient.CoreV1().ConfigMaps(c.config.PodNamespace)
	cm, err := client.Get(ctx, ipsec.CoordinationConfigMap, metav1.GetOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	if k8serrors.IsNotFound(err) {
		cm = nil
	}
	if !c.config.EnableOVNIPSec {
		return c.reconcileIPsecDisable(ctx, cm)
	}
	ds, err := c.config.KubeClient.AppsV1().DaemonSets(c.config.PodNamespace).Get(ctx, "kube-ovn-cni", metav1.GetOptions{})
	if err != nil {
		return err
	}
	secret, err := c.config.KubeClient.CoreV1().Secrets(c.config.PodNamespace).Get(ctx, util.DefaultOVNIPSecCA, metav1.GetOptions{})
	if err != nil {
		return err
	}
	trust := secret.Data["cacert"]
	if _, err := ipsec.Certificates(trust); err != nil {
		return err
	}
	nb, err := c.OVNNbClient.GetNbGlobal()
	if err != nil {
		return err
	}
	if cm == nil {
		nodes, err := c.config.KubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		state, err := freezeIPsecTargets(ds, nodes.Items, trust, nb.Ipsec)
		if err != nil {
			return err
		}
		cm = &corev1.ConfigMap{Name: ipsec.CoordinationConfigMap}
		if err := encodeIPsecCoordination(cm, state); err != nil {
			return err
		}
		_, err = client.Create(ctx, cm, metav1.CreateOptions{})
		return err
	}
	state, err := ipsec.DecodeCoordination([]byte(cm.Data["state"]))
	if err != nil {
		return err
	}
	if state.Phase == ipsec.DisablingPhase || state.Phase == ipsec.CleanupPhase || state.Phase == ipsec.ReleasePhase {
		return errors.New("IPsec disable cleanup must finish before a new enable generation")
	}
	if state.Phase == ipsec.DisabledPhase {
		nodes, err := c.config.KubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		next, err := freezeIPsecTargets(ds, nodes.Items, trust, false)
		if err != nil {
			return err
		}
		if err := encodeIPsecCoordination(cm, next); err != nil {
			return err
		}
		_, err = client.Update(ctx, cm, metav1.UpdateOptions{})
		return err
	}
	data, err := json.Marshal(ds.Spec.Template, json.Deterministic(true))
	if err != nil {
		return err
	}
	if state.Phase == ipsec.EnabledPhase && nb.Ipsec {
		nodes, err := c.config.KubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		matching, err := matchingIPsecTargets(ds, nodes.Items)
		if err != nil {
			return err
		}
		targets := maps.Clone(state.Targets)
		for name, uid := range matching {
			if frozen := targets[name]; frozen != "" && frozen != uid {
				return fmt.Errorf("IPsec frozen target %s was replaced; explicit retirement is required", name)
			}
			targets[name] = uid
		}
		// Refresh the acknowledgement challenge on rollout/trust changes, but
		// retain frozen Node UIDs and add new members without toggling NB off.
		// New members must already pass the independent local startup gate.
		// This does not authorize removing old roots or dropping offline nodes.
		if maps.Equal(targets, state.Targets) && state.DaemonSetUID == string(ds.UID) && state.TemplateHash == ipsecPublicHash(data) && state.TrustHash == ipsecPublicHash(trust) {
			return nil
		}
		state.Targets = targets
		state.DaemonSetUID, state.TemplateHash, state.TrustHash = string(ds.UID), ipsecPublicHash(data), ipsecPublicHash(trust)
		state.Generation, state.Epoch = rand.Text(), rand.Text()
		if err := encodeIPsecCoordination(cm, state); err != nil {
			return err
		}
		_, err = client.Update(ctx, cm, metav1.UpdateOptions{})
		return err
	}
	if state.DaemonSetUID != string(ds.UID) || state.TemplateHash != ipsecPublicHash(data) || state.TrustHash != ipsecPublicHash(trust) {
		return c.restartIPsecBarrier(ctx, cm, state, ds, data, trust)
	}
	nodes, err := c.config.KubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	pods, err := c.config.KubeClient.CoreV1().Pods(c.config.PodNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	requests, err := c.config.KubeClient.CertificatesV1().CertificateSigningRequests().List(ctx, metav1.ListOptions{FieldSelector: "spec.signerName=" + util.SignerName})
	if err != nil {
		return err
	}
	nodeByName := make(map[string]*corev1.Node, len(nodes.Items))
	for i := range nodes.Items {
		nodeByName[nodes.Items[i].Name] = &nodes.Items[i]
	}
	podByName := make(map[string]*corev1.Pod, len(pods.Items))
	for i := range pods.Items {
		podByName[pods.Items[i].Name] = &pods.Items[i]
	}
	requestByName := make(map[string]*certv1.CertificateSigningRequest, len(requests.Items))
	for i := range requests.Items {
		requestByName[requests.Items[i].Name] = &requests.Items[i]
	}
	for name, uid := range state.Targets {
		node := nodeByName[name]
		if node == nil {
			return fmt.Errorf("IPsec frozen target %s is missing", name)
		}
		if string(node.UID) != uid || node.DeletionTimestamp != nil {
			return fmt.Errorf("IPsec frozen target %s was removed or replaced", name)
		}
		if err := verifyIPsecReceipt(node, ds, state, trust, podByName, requestByName); err != nil {
			return fmt.Errorf("IPsec frozen target %s: %w", name, err)
		}
	}
	switch state.Phase {
	case ipsec.PreparePhase:
		state.Phase, state.Epoch = ipsec.ArmPhase, rand.Text()
	case ipsec.ArmPhase, ipsec.EnabledPhase:
		// The durable Arm barrier and its signed target receipts precede NB.
		// A crash after NB commit repeats validation, without first disabling it.
		if err := c.OVNNbClient.SetOVNIPSec(true); err != nil {
			return err
		}
		state.Phase, state.Epoch = ipsec.EnabledPhase, rand.Text()
	}
	if err := encodeIPsecCoordination(cm, state); err != nil {
		return err
	}
	_, err = client.Update(ctx, cm, metav1.UpdateOptions{})
	return err
}

func (c *Controller) restartIPsecBarrier(ctx context.Context, cm *corev1.ConfigMap, state *ipsec.Coordination, ds *appsv1.DaemonSet, template, trust []byte) error {
	nb, err := c.OVNNbClient.GetIPsecGlobal(ctx)
	if err != nil {
		return err
	}
	// Changing configuration invalidates every previous receipt. Restart the
	// challenge without shrinking the original Node UID cohort. In particular,
	// a selector change cannot silently exclude an offline or replaced target.
	state.DaemonSetUID, state.TemplateHash, state.TrustHash = string(ds.UID), ipsecPublicHash(template), ipsecPublicHash(trust)
	state.Generation, state.Epoch = rand.Text(), rand.Text()
	state.Phase = ipsec.PreparePhase
	if nb.Enabled {
		// NB may have committed immediately before a leader crashed at Arm.
		// Preserve encryption during the subsequent rollout, just as for a
		// recorded Enabled generation; never introduce an automatic off/on.
		state.Phase = ipsec.EnabledPhase
	}
	if err := encodeIPsecCoordination(cm, state); err != nil {
		return err
	}
	_, err = c.config.KubeClient.CoreV1().ConfigMaps(c.config.PodNamespace).Update(ctx, cm, metav1.UpdateOptions{})
	return err
}

func (c *Controller) reconcileIPsecDisable(ctx context.Context, cm *corev1.ConfigMap) error {
	if cm == nil {
		return c.OVNNbClient.SetOVNIPSec(false)
	}
	state, err := ipsec.DecodeCoordination([]byte(cm.Data["state"]))
	if err != nil {
		return err
	}
	update := func(phase string) error {
		state.Phase, state.Epoch = phase, rand.Text()
		if phase != ipsec.CleanupPhase && phase != ipsec.ReleasePhase {
			state.NBGlobalUUID, state.SBGlobalUUID = "", ""
		}
		if err := encodeIPsecCoordination(cm, state); err != nil {
			return err
		}
		_, err := c.config.KubeClient.CoreV1().ConfigMaps(c.config.PodNamespace).Update(ctx, cm, metav1.UpdateOptions{})
		return err
	}
	if state.Phase == ipsec.DisabledPhase {
		return c.OVNNbClient.SetOVNIPSec(false)
	}
	if state.Phase != ipsec.DisablingPhase && state.Phase != ipsec.CleanupPhase && state.Phase != ipsec.ReleasePhase {
		// Persist the new challenge before changing NB. A crash must not leave
		// an old enable receipt that can be mistaken for cleanup authorization.
		if err := update(ipsec.DisablingPhase); err != nil {
			return err
		}
		return c.OVNNbClient.SetOVNIPSec(false)
	}
	if err := c.OVNNbClient.SetOVNIPSec(false); err != nil {
		return err
	}
	nb, err := c.OVNNbClient.GetIPsecGlobal(ctx)
	if err != nil {
		return err
	}
	sb, err := c.OVNSbClient.GetIPsecGlobal(ctx)
	if err != nil {
		return err
	}
	if nb.Enabled || sb.Enabled {
		if state.Phase == ipsec.CleanupPhase {
			if err := update(ipsec.DisablingPhase); err != nil {
				return err
			}
		}
		return errors.New("IPsec disable is waiting for live NB/SB switch convergence")
	}
	ds, err := c.config.KubeClient.AppsV1().DaemonSets(c.config.PodNamespace).Get(ctx, "kube-ovn-cni", metav1.GetOptions{})
	if err != nil {
		return err
	}
	if !hasIPsecCleanupTemplate(ds) {
		return errors.New("IPsec disable is waiting for the native cleanup init template")
	}
	template, err := json.Marshal(ds.Spec.Template, json.Deterministic(true))
	if err != nil {
		return err
	}
	hash := ipsecPublicHash(template)
	if state.Phase == ipsec.CleanupPhase && state.NBGlobalUUID == nb.UUID && state.SBGlobalUUID == sb.UUID && state.DaemonSetUID == string(ds.UID) && state.TemplateHash == hash {
		// Each cleanup init performs its own guarded drain before release.
		// A cohort-wide drain barrier would deadlock maxUnavailable=1: the
		// first init could not exit until the other Pods had been replaced.
		return update(ipsec.ReleasePhase)
	}
	if state.Phase == ipsec.ReleasePhase && state.NBGlobalUUID == nb.UUID && state.SBGlobalUUID == sb.UUID && state.DaemonSetUID == string(ds.UID) && state.TemplateHash == hash {
		if err := c.verifyIPsecReleaseBarrier(ctx, ds, state); err != nil {
			return err
		}
		return update(ipsec.DisabledPhase)
	}
	// This phase confirms only the global switch. Each node must additionally
	// prove local tunnel convergence and exact connection/SA ownership while
	// guards remain installed. It is not a completed Disabled receipt.
	state.NBGlobalUUID, state.SBGlobalUUID = nb.UUID, sb.UUID
	state.DaemonSetUID, state.TemplateHash = string(ds.UID), hash
	return update(ipsec.CleanupPhase)
}

func verifyIPsecReceipt(node *corev1.Node, ds *appsv1.DaemonSet, state *ipsec.Coordination, trust []byte, pods map[string]*corev1.Pod, requests map[string]*certv1.CertificateSigningRequest) error {
	data := []byte(node.Annotations[ipsec.ReceiptAnnotation])
	if len(data) == 0 || len(data) > 16<<10 {
		return errors.New("missing or oversized IPsec receipt")
	}
	// Decode only to locate the CSR. No claim is trusted until the signature,
	// certificate, and API-populated requester fields have all been verified.
	var signed ipsec.SignedReceipt
	if err := json.Unmarshal(data, &signed); err != nil {
		return err
	}
	var claim ipsec.Receipt
	if err := json.Unmarshal(signed.Payload, &claim); err != nil {
		return err
	}
	csr := requests[claim.CSRName]
	if csr == nil {
		return errors.New("IPsec receipt CSR is missing")
	}
	if string(csr.UID) != claim.CSRUID || csr.Spec.SignerName != util.SignerName || csr.Annotations[ipsec.NodeNameAnnotation] != node.Name || csr.Annotations[ipsec.NodeUIDAnnotation] != string(node.UID) || !slices.Equal(csr.Spec.Extra["authentication.kubernetes.io/pod-uid"], []string{claim.PodUID}) {
		return errors.New("IPsec receipt CSR does not bind the target identity")
	}
	for _, condition := range csr.Status.Conditions {
		if condition.Status == corev1.ConditionTrue && (condition.Type == certv1.CertificateDenied || condition.Type == certv1.CertificateFailed) {
			return errors.New("IPsec receipt CSR is denied or failed")
		}
	}
	req, err := decodeCertificateRequest(csr.Spec.Request)
	if err != nil {
		return err
	}
	podNames := csr.Spec.Extra["authentication.kubernetes.io/pod-name"]
	if len(podNames) != 1 {
		return errors.New("IPsec receipt CSR has no bound Pod")
	}
	pod := pods[podNames[0]]
	if err := validateIPsecBoundObjects(ds.Namespace, csr, req, pod, node, ds); err != nil {
		return err
	}
	key, ok := req.PublicKey.(*rsa.PublicKey)
	if !ok {
		return errors.New("IPsec receipt key is not RSA")
	}
	if _, err := ipsec.ValidateCertificate(csr.Status.Certificate, trust, key, node.Annotations[util.ChassisAnnotation], time.Now()); err != nil {
		return err
	}
	receipt, err := ipsec.VerifyReceipt(data, key, state, node.Name, string(node.UID), time.Now())
	if err != nil {
		return err
	}
	if receipt.Status.Chassis != node.Annotations[util.ChassisAnnotation] {
		return errors.New("IPsec receipt chassis changed")
	}
	if pod.Spec.NodeName != node.Name || string(pod.UID) != receipt.PodUID {
		return errors.New("IPsec receipt is bound to another Pod or node")
	}
	owner := metav1.GetControllerOf(pod)
	if owner == nil || owner.UID != ds.UID || pod.DeletionTimestamp != nil {
		return errors.New("IPsec receipt Pod does not match the frozen owner or security context")
	}
	if !slices.ContainsFunc(ds.Spec.Template.Spec.Containers, func(container corev1.Container) bool { return container.Name == "ipsec" }) {
		return errors.New("frozen CNI template has no IPsec container")
	}
	return verifyIPsecPodTemplate(pod, ds)
}
