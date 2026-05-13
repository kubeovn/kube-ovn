// kube.go: kubernetes API helpers -- pod listing, labels, lease.
// No DB knowledge here.
package ovn_central_controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
)

// apiCallTimeout caps a single kube-apiserver request so a stuck apiserver
// can't freeze a tick. Wrap List/Get/Patch with context.WithTimeout(ctx, ...).
const apiCallTimeout = 10 * time.Second

// retryDecisional retries fn on transient apiserver errors with
// exponential backoff (~0.5s/1s/2s, total ≤3.5s). Use ONLY for reads
// whose result drives a recovery decision (listPeers in bringUp /
// recoverUnderLease). For diagnostic ops (publishStatus, patchLeaderLabels)
// rely on next-tick natural retry instead.
func retryDecisional(ctx context.Context, fn func() error) error {
	backoff := wait.Backoff{
		Steps:    3,
		Duration: 500 * time.Millisecond,
		Factor:   2.0,
		Jitter:   0.1,
	}
	return retry.OnError(backoff, isTransientAPIError, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return fn()
	})
}

// isTransientAPIError returns true for apiserver errors worth retrying:
// 5xx, throttling, network timeouts. NotFound/Forbidden/Unauthorized are
// considered terminal (no point retrying).
func isTransientAPIError(err error) bool {
	if err == nil {
		return false
	}
	if apierrors.IsServerTimeout(err) || apierrors.IsTooManyRequests(err) ||
		apierrors.IsServiceUnavailable(err) || apierrors.IsInternalError(err) {
		return true
	}
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		return true
	}
	return false
}

const (
	labelStatus        = "kube-ovn.io/ovn-status"
	labelNodeClusterID = "kube-ovn.io/ovn-nb-cluster-id"
	bootstrapLeaseName = "ovn-central-bootstrap"
	podLabelSelector   = "app=ovn-central"
)

// Lifecycle status published per pod. Recomputed every tick from disk +
// ovsdb runtime state -- not sticky across container restarts. Only
// statusActive (combined with Pod.Ready=true via the readiness probe)
// is consulted by peers to trigger wipe-and-rejoin; the rest are
// informational. statusRecovering is diagnostic-only.
type status string

const (
	statusJoining    status = "joining"     // stub created here, no committed data observed yet
	statusJoined     status = "joined"      // stub has received committed data via raft, but sustainedActive not yet latched
	statusStale      status = "stale"       // DB existed at startup, not yet active in this container
	statusLeaderLost status = "leader_lost" // was active in this container, no leader now
	statusActive     status = "active"      // cluster_member + known leader + committed entries
	statusRecovering status = "recovering"  // under bootstrap lease, mid destructive op
)

func newKubeClient() (kubernetes.Interface, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("in-cluster config: %w", err)
	}
	return kubernetes.NewForConfig(cfg)
}

// peerInfo summarizes the kubectl-visible state of one ovn-central pod.
//
//   - ready: Pod.Status.Conditions[PodReady] -- the readiness probe runs
//     `cluster_member + known leader + committed entries`, so a true
//     value means this peer's label is trustworthy.
//   - terminating: pod has DeletionTimestamp; about to vanish, may be
//     hung in grace. Excluded from recovery decisions.
type peerInfo struct {
	ip          string
	status      status
	running     bool
	ready       bool
	terminating bool
}

func listPeers(ctx context.Context, kc kubernetes.Interface, ns string) ([]peerInfo, error) {
	var pl *corev1.PodList
	err := retryDecisional(ctx, func() error {
		c, cancel := context.WithTimeout(ctx, apiCallTimeout)
		defer cancel()
		var lerr error
		pl, lerr = kc.CoreV1().Pods(ns).List(c, metav1.ListOptions{LabelSelector: podLabelSelector})
		return lerr
	})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	out := make([]peerInfo, 0, len(pl.Items))
	for _, p := range pl.Items {
		pi := peerInfo{
			ip:          p.Status.PodIP,
			running:     p.Status.Phase == corev1.PodRunning,
			status:      status(p.Labels[labelStatus]),
			terminating: p.DeletionTimestamp != nil,
		}
		for _, c := range p.Status.Conditions {
			if c.Type == corev1.PodReady {
				pi.ready = c.Status == corev1.ConditionTrue
				break
			}
		}
		out = append(out, pi)
	}
	return out, nil
}

// otherPeers returns peers we trust for recovery decisions: not self,
// has a Pod IP, currently Running, not Terminating.
func otherPeers(all []peerInfo, selfIP string) []peerInfo {
	out := make([]peerInfo, 0, len(all))
	for _, p := range all {
		if p.ip == "" || p.ip == selfIP || !p.running || p.terminating {
			continue
		}
		out = append(out, p)
	}
	return out
}

// anyPeerStatus reports whether any peer matches one of the given statuses.
func anyPeerStatus(peers []peerInfo, want ...status) bool {
	for _, p := range peers {
		if slices.Contains(want, p.status) {
			return true
		}
	}
	return false
}

// anyPeerActiveAndReady is the trustworthy "there's a healthy cluster"
// signal: status=active AND kubelet's readiness probe passing. Stale
// labels from hung or just-degraded pods would have ready=false.
func anyPeerActiveAndReady(peers []peerInfo) bool {
	for _, p := range peers {
		if p.ready && p.status == statusActive {
			return true
		}
	}
	return false
}

// anyNorthdActive reports whether any pod is the active ovn-northd
// (a Ready endpoint of the ovn-northd Service). API error fails safe
// (returns true) so we don't stealLock and bump out a working northd.
func anyNorthdActive(ctx context.Context, kc kubernetes.Interface, ns string) bool {
	c, cancel := context.WithTimeout(ctx, apiCallTimeout)
	defer cancel()
	esl, err := kc.DiscoveryV1().EndpointSlices(ns).List(c, metav1.ListOptions{
		LabelSelector: "kubernetes.io/service-name=ovn-northd",
	})
	if err != nil {
		klog.Warningf("list ovn-northd endpointslices: %v", err)
		return true
	}
	for _, es := range esl.Items {
		for _, ep := range es.Endpoints {
			if len(ep.Addresses) == 0 {
				continue
			}
			if ep.Conditions.Ready == nil || *ep.Conditions.Ready {
				return true
			}
		}
	}
	return false
}

// publishStatus patches our pod's lifecycle status label. Empty
// status is sent as null so kubectl removes the label.
func publishStatus(ctx context.Context, kc kubernetes.Interface, ns, name string, st status) error {
	var val any
	if st != "" {
		val = string(st)
	}
	body, err := json.Marshal(map[string]any{
		"metadata": map[string]any{
			"labels": map[string]any{labelStatus: val},
		},
	})
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, apiCallTimeout)
	defer cancel()
	_, err = kc.CoreV1().Pods(ns).Patch(c, name, types.MergePatchType, body, metav1.PatchOptions{})
	return err
}

// withBootstrapLease runs fn while holding the cluster-wide bootstrap
// lease. Only one pod may be inside fn at a time -- this is the
// invariant that keeps concurrent reconvert/create-cluster from forming
// split-brain. Lease durations come from cfg: must comfortably exceed
// the worst-case destructive op (reconvert on a multi-GB DB can take
// minutes) so an apiserver hiccup doesn't release the lease mid-op.
func withBootstrapLease(ctx context.Context, kc kubernetes.Interface, cfg *Config,
	fn func(context.Context) error,
) error {
	lock := &resourcelock.LeaseLock{
		LeaseMeta:  metav1.ObjectMeta{Name: bootstrapLeaseName, Namespace: cfg.PodNamespace},
		Client:     kc.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{Identity: cfg.PodName},
	}
	leCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu    sync.Mutex
		fnErr error
	)
	leaderelection.RunOrDie(leCtx, leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   cfg.BootstrapLeaseDuration,
		RenewDeadline:   cfg.BootstrapRenewDeadline,
		RetryPeriod:     5 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(c context.Context) {
				klog.Infof("acquired bootstrap lease, running action")
				err := fn(c)
				mu.Lock()
				fnErr = err
				mu.Unlock()
				cancel()
			},
			OnStoppedLeading: func() { klog.Infof("released bootstrap lease") },
		},
	})
	mu.Lock()
	defer mu.Unlock()
	return fnErr
}

// patchNodeClusterID stamps the NB raft cluster UUID on the node so the
// cluster membership history survives pod restarts. Only written; never cleared.
func patchNodeClusterID(ctx context.Context, kc kubernetes.Interface, nodeName, clusterID string) error {
	body, err := json.Marshal(map[string]any{
		"metadata": map[string]any{
			"labels": map[string]any{labelNodeClusterID: clusterID},
		},
	})
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, apiCallTimeout)
	defer cancel()
	_, err = kc.CoreV1().Nodes().Patch(c, nodeName, types.MergePatchType, body, metav1.PatchOptions{})
	return err
}

// patchLeaderLabels stamps ovn-nb-leader / ovn-sb-leader / ovn-northd-leader
// on this pod so the kube-ovn Service selectors route to the actual leaders.
func patchLeaderLabels(ctx context.Context, kc kubernetes.Interface, ns, name string, nb, sb, northd bool) error {
	body, err := json.Marshal(map[string]any{
		"metadata": map[string]any{
			"labels": map[string]any{
				"ovn-nb-leader":     strconv.FormatBool(nb),
				"ovn-sb-leader":     strconv.FormatBool(sb),
				"ovn-northd-leader": strconv.FormatBool(northd),
			},
		},
	})
	if err != nil {
		return err
	}
	c, cancel := context.WithTimeout(ctx, apiCallTimeout)
	defer cancel()
	_, err = kc.CoreV1().Pods(ns).Patch(c, name, types.MergePatchType, body, metav1.PatchOptions{})
	return err
}
