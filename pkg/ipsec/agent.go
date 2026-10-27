package ipsec

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/json/v2"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/fileutil"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// Configuration describes the node IPsec module, independently of CNI setup.
type Configuration struct {
	NodeName, Namespace, OVSSocket, KeyDir, RuntimeDir string
	PodUID                                             string
	ProtectionDir                                      string
	Duration, RequestTimeout                           time.Duration
	Priority                                           int
	CleanupOnly                                        bool
	Kube                                               kubernetes.Interface
}

// Agent owns certificate reconciliation and the IKE/OVS monitor process pair.
type Agent struct {
	config  Configuration
	store   store
	runtime *runtimeManager
	mu      sync.RWMutex
	status  Status
	caHash  string
	beat    atomic.Int64
	ovs     *ovs.VswitchClient

	protectionMu sync.Mutex
	protection   *agentProtection
}

// Status deliberately excludes certificate contents, private keys and SA keys.
type Status struct {
	Phase                string    `json:"phase"`
	Reason               string    `json:"reason,omitempty"`
	NodeUID              string    `json:"nodeUID,omitempty"`
	Chassis              string    `json:"chassis,omitempty"`
	Generation           string    `json:"generation,omitempty"`
	CertificateHash      string    `json:"certificateHash,omitempty"`
	TrustHash            string    `json:"trustHash,omitempty"`
	ConfigurationApplied bool      `json:"configurationApplied"`
	RuntimeHealthy       bool      `json:"runtimeHealthy"`
	ProtectionArmed      bool      `json:"protectionArmed"`
	Expires              time.Time `json:"expires,omitzero"`
}

func New(config Configuration) (*Agent, error) {
	if config.ProtectionDir == "" {
		config.ProtectionDir = "/run/kube-ovn-ipsec-protection"
	}
	if config.NodeName == "" || config.Namespace == "" || config.PodUID == "" || config.Kube == nil {
		return nil, errors.New("IPsec node, namespace, Pod UID and Kubernetes client are required")
	}
	if config.Duration < 10*time.Minute || config.Duration/time.Second > 1<<31-1 || config.RequestTimeout <= 0 {
		return nil, errors.New("invalid IPsec certificate duration or request timeout")
	}
	if config.Priority < -20 || config.Priority > 19 {
		return nil, errors.New("IPsec process priority must be between -20 and 19")
	}
	return &Agent{config: config, store: store{dir: config.KeyDir}, runtime: &runtimeManager{dir: config.RuntimeDir, store: store{dir: config.KeyDir}, ovsSocket: config.OVSSocket, priority: config.Priority}, status: Status{Phase: "WaitingTrust"}}, nil
}

func (a *Agent) Status() Status {
	a.mu.RLock()
	status := a.status
	a.mu.RUnlock()
	status.RuntimeHealthy, status.ConfigurationApplied = a.runtime.configurationApplied(status)
	status.ProtectionArmed = a.protectionArmed(status.NodeUID, status.Chassis)
	if status.Phase == "Configured" && status.ConfigurationApplied && status.ProtectionArmed {
		status.Phase = "Running"
	}
	return status
}

func (a *Agent) setStatus(status Status) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.status = status
}

func (a *Agent) Run(ctx context.Context) error {
	if a.config.CleanupOnly {
		return a.runCleanupLoop(ctx)
	}
	lock, err := a.store.lock()
	if err != nil {
		return err
	}
	defer func() {
		if err := lock.Close(); err != nil {
			klog.ErrorS(err, "Close IPsec owner lock")
		}
	}()
	// Reject an existing owner before issuing an identity or changing shared
	// OVSDB paths; checking only when the IKE pair starts is too late.
	if err := checkLegacyMonitor(a.config.OVSSocket); err != nil {
		return err
	}
	if err := checkIKEPorts(); err != nil {
		return err
	}
	if err := checkOVNProtection(ctx); err != nil {
		return err
	}
	defer a.closeProtection()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() {
		if a.ovs != nil {
			a.ovs.Close()
		}
	}()
	a.beat.Store(time.Now().UnixNano())
	if err := a.serveStatus(ctx); err != nil {
		return err
	}
	if err := a.serveProtection(ctx); err != nil {
		return err
	}
	runtimeDone := make(chan struct{})
	go func() { defer close(runtimeDone); a.runtime.run(ctx) }()
	defer func() { cancel(); <-runtimeDone }()
	wake := make(chan struct{}, 1)
	notify := func(any) {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	factory := informers.NewSharedInformerFactoryWithOptions(a.config.Kube, 0,
		informers.WithNamespace(a.config.Namespace), informers.WithTransform(util.TrimManagedFields),
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.FieldSelector = "metadata.name=" + util.DefaultOVNIPSecCA
			opts.AllowWatchBookmarks = true
		}))
	secrets := factory.Core().V1().Secrets()
	if _, err := secrets.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{AddFunc: notify, UpdateFunc: func(_, obj any) { notify(obj) }, DeleteFunc: notify}); err != nil {
		return err
	}
	factory.Start(ctx.Done())
	defer factory.Shutdown()
	backoff := time.Second
	for ctx.Err() == nil {
		a.beat.Store(time.Now().UnixNano())
		var err error
		online := secrets.Informer().HasSynced()
		// Restore existing protection immediately. During an initial Prepare,
		// keep the plaintext network available until every node has its identity.
		if err = a.bootstrapProtection(ctx, online); err == nil {
			if online {
				err = a.reconcile(ctx, secrets.Lister())
				if err == nil {
					err = a.publishReceipt(ctx)
				}
			} else {
				// Reuse only the last committed, locally validated generation while
				// the API is unreachable. Do not issue requests or import legacy files.
				// Once synchronized, explicit trust changes take precedence immediately.
				err = a.restoreCurrent(ctx)
			}
		}
		a.beat.Store(time.Now().UnixNano())
		delay := 30 * time.Second
		if a.Status().Phase == "Prepared" {
			delay = 2 * time.Second
		}
		if err != nil {
			klog.ErrorS(err, "Reconcile node IPsec")
			status := a.Status()
			status.Phase = "Degraded"
			status.Reason = err.Error()
			a.setStatus(status)
			delay, backoff = backoff, min(30*time.Second, backoff*2)
		} else {
			backoff = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
		case <-wake:
		case <-timer.C:
		}
		timer.Stop()
	}
	return nil
}

// runCleanupLoop retries the finite cleanup init while node-local
// dependencies become available. A fresh node can briefly lack the OVS socket,
// its storage directory, or the API endpoint; those transient failures must not
// discard protection or be mistaken for successful cleanup.
func (a *Agent) runCleanupLoop(ctx context.Context) error {
	for ctx.Err() == nil {
		lock, err := a.store.lock()
		if err == nil {
			err = a.runCleanup(ctx)
			if closeErr := lock.Close(); err == nil {
				err = closeErr
			}
		}
		if err == nil {
			return nil
		}
		if err != nil {
			klog.ErrorS(err, "IPsec cleanup retrying")
		}
		if ctx.Err() != nil {
			break
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	return ctx.Err()
}

func (a *Agent) reconcile(ctx context.Context, secrets listers.SecretLister) error {
	secret, err := secrets.Secrets(a.config.Namespace).Get(util.DefaultOVNIPSecCA)
	if err != nil {
		return err
	}
	trust := secret.Data["cacert"]
	if _, err := Certificates(trust); err != nil {
		return fmt.Errorf("invalid IPsec trust: %w", err)
	}
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
		return errors.New("OVS system-id is not available")
	}
	node, err := a.config.Kube.CoreV1().Nodes().Get(ctx, a.config.NodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	preparing, err := a.initialPreparation(ctx, string(node.UID), row.ExternalIDs)
	if err != nil {
		return err
	}
	if preparing {
		if err := a.reservePreparation(string(node.UID)); err != nil {
			return err
		}
	} else {
		if err := a.ensureProtection(string(node.UID), chassis); err != nil {
			return err
		}
	}
	if err := a.store.importLegacy(string(node.UID), chassis, trust, row.OtherConfig); err != nil {
		return err
	}
	g, err := a.identity(ctx, string(node.UID), chassis, trust)
	if err != nil {
		return err
	}
	source := g
	source.NodeName = a.config.NodeName
	source.Namespace = a.config.Namespace
	g, err = a.store.prepareGeneration(source, trust)
	if err != nil {
		return err
	}
	if preparing {
		return a.prepareIdentity(g, trust)
	}
	if err := a.activate(ctx, row.UUID, g, trust, "Configured"); err != nil {
		return err
	}
	pending, err := a.store.load("pending")
	if err != nil {
		return err
	}
	if pending != nil && a.store.sameIdentity(pending, source) {
		if err := os.Remove(filepath.Join(a.config.KeyDir, "pending.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	// Only reclaim files after the owned runtime has loaded the current public
	// configuration. Retain the database references even if they differ after
	// an interrupted commit or another writer's update.
	if a.Status().ConfigurationApplied {
		row, err := a.ovs.IPsecConfiguration()
		if err != nil {
			return err
		}
		if err := a.store.collectGenerations(time.Now(), row.OtherConfig); err != nil {
			return err
		}
	}
	return nil
}

func (a *Agent) activate(ctx context.Context, ovsUUID string, g *generation, trust []byte, phase string) error {
	certPEM, err := a.store.read(g, "certificate")
	if err != nil {
		return err
	}
	certs, err := Certificates(certPEM)
	if err != nil {
		return err
	}
	status := Status{
		Phase: phase, NodeUID: g.NodeUID, Chassis: g.Chassis, Generation: g.ID,
		CertificateHash: digest(certPEM), TrustHash: digest(trust), Expires: certs[0].NotAfter,
	}
	// Invalidate the old acknowledgement before changing trust or OVSDB. A
	// failed/interrupted activation must not advertise the old configuration
	// as acknowledgement of the next generation.
	a.runtime.expectConfiguration(status)
	if err := a.ensureProtection(g.NodeUID, g.Chassis); err != nil {
		return err
	}
	if err := a.store.retainPrevious(g); err != nil {
		return err
	}
	if err := a.applyTrust(ctx, trust); err != nil {
		return err
	}
	paths := map[string]string{"certificate": a.store.path(g, "certificate"), "private_key": a.store.path(g, "private-key"), "ca_cert": a.store.path(g, "ca-bundle")}
	if err := a.ovs.SetIPsecConfiguration(ovsUUID, paths); err != nil {
		return err
	}
	if err := a.store.save("current", g); err != nil {
		return err
	}
	a.setStatus(status)
	a.runtime.enabled.Store(true)
	return nil
}

func (a *Agent) restoreCurrent(ctx context.Context) error {
	g, err := a.store.load("current")
	if err != nil {
		return err
	}
	if g == nil || g.NodeName != a.config.NodeName || g.Namespace != a.config.Namespace || g.NodeUID == "" {
		return errors.New("API trust is not synchronized and no bound current generation is available")
	}
	key, err := a.store.read(g, "private-key")
	if err != nil {
		return err
	}
	cert, err := a.store.read(g, "certificate")
	if err != nil {
		return err
	}
	trust, err := a.store.read(g, "ca-bundle")
	if err != nil {
		return err
	}
	if digest(append(append(append([]byte{}, key...), cert...), trust...)) != g.ID {
		return errors.New("current IPsec generation content is inconsistent")
	}
	if _, err := validateIdentity(cert, key, trust, g.Chassis, time.Now()); err != nil {
		return err
	}
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
	if row.ExternalIDs["system-id"] != g.Chassis {
		return errors.New("current IPsec chassis does not match the local OVS identity")
	}
	return a.activate(ctx, row.UUID, g, trust, "Restored")
}

func (a *Agent) identity(ctx context.Context, nodeUID, chassis string, trust []byte) (*generation, error) {
	g, err := a.store.load("current")
	if err != nil {
		return nil, err
	}
	if g != nil && g.NodeUID == nodeUID && g.Chassis == chassis {
		cert, certErr := a.store.read(g, "certificate")
		key, keyErr := a.store.read(g, "private-key")
		if certErr == nil && keyErr == nil {
			leaf, err := validateIdentity(cert, key, trust, chassis, time.Now())
			if err == nil && time.Now().Before(renewalTime(leaf, nodeUID)) {
				return g, nil
			}
		}
	}
	previous := g
	g, key, err := a.store.pending(nodeUID, chassis)
	if err != nil {
		return nil, err
	}
	// A signed pending generation may have been committed to OVSDB just before
	// a crash. Validate and finish it instead of issuing another certificate.
	if cert, err := a.store.read(g, "certificate"); err == nil {
		if _, err := validateIdentity(cert, key, trust, chassis, time.Now()); err == nil {
			return g, nil
		}
	}
	csr, err := newCSR(key, chassis)
	if err != nil {
		return nil, err
	}
	i := issuer{kube: a.config.Kube, node: a.config.NodeName, nodeUID: nodeUID, podUID: a.config.PodUID, namespace: a.config.Namespace, duration: a.config.Duration, trustHash: digest(trust)}
	issueCtx, cancel := context.WithTimeout(ctx, a.config.RequestTimeout)
	defer cancel()
	cert, err := i.sign(issueCtx, csr)
	if err == nil {
		_, err = validateIdentity(cert, key, trust, chassis, time.Now())
	}
	if err == nil {
		err = a.store.write(g, "certificate", cert)
	}
	if err == nil {
		return g, nil
	}
	// Renewal failure must not discard an unexpired, still trusted identity.
	if previous != nil && previous.NodeUID == nodeUID && previous.Chassis == chassis {
		oldCert, certErr := a.store.read(previous, "certificate")
		oldKey, keyErr := a.store.read(previous, "private-key")
		if certErr == nil && keyErr == nil {
			if _, validErr := validateIdentity(oldCert, oldKey, trust, chassis, time.Now()); validErr == nil {
				klog.ErrorS(err, "IPsec renewal failed; keeping the valid current identity")
				return previous, nil
			}
		}
	}
	return nil, err
}

func renewalTime(cert *x509.Certificate, nodeUID string) time.Time {
	// Spread renewal between 40% and 60% of the certificate's lifetime.
	// The same identity keeps its schedule through retries and Pod restarts.
	lifetime := cert.NotAfter.Sub(cert.NotBefore)
	hash := sha256.Sum256(append([]byte(nodeUID), cert.Raw...))
	window := lifetime / 5
	offset := window / 65536 * time.Duration(binary.BigEndian.Uint16(hash[:2]))
	return cert.NotBefore.Add(lifetime/5*2 + offset)
}

func (a *Agent) applyTrust(ctx context.Context, trust []byte) error {
	hash := digest(trust)
	if a.caHash == hash {
		return nil
	}
	certs, err := Certificates(trust)
	if err != nil {
		return err
	}
	dir := "/etc/ipsec.d/cacerts"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	keep := make(map[string]bool)
	for _, cert := range certs {
		name := "kube-ovn-" + digest(cert.Raw) + ".pem"
		keep[name] = true
		if err := fileutil.AtomicWriteFile(filepath.Join(dir, name), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), 0o600); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	// Load additions before removing old owned files. A failed reread keeps
	// the old files available to the currently running identity and retry.
	if err := a.runtime.reloadTrust(ctx); err != nil {
		return err
	}
	removed := false
	for _, entry := range entries {
		if !entry.IsDir() && bytes.HasPrefix([]byte(entry.Name()), []byte("kube-ovn-")) && !keep[entry.Name()] {
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				return err
			}
			removed = true
		}
	}
	if removed {
		if err := a.runtime.reloadTrust(ctx); err != nil {
			return err
		}
	}
	a.caHash = hash
	return nil
}

func (a *Agent) serveStatus(ctx context.Context) error {
	if err := os.MkdirAll(a.config.RuntimeDir, 0o700); err != nil {
		return err
	}
	socket := filepath.Join(a.config.RuntimeDir, "status.sock")
	if err := os.Remove(socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", func(w http.ResponseWriter, _ *http.Request) {
		if time.Since(time.Unix(0, a.beat.Load())) > a.config.RequestTimeout+90*time.Second {
			http.Error(w, "IPsec reconciliation is stalled", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		status := a.Status()
		if !identityPrepared(status, time.Now()) || status.Phase != "Prepared" && (!status.ProtectionArmed || !status.ConfigurationApplied || (status.Phase != "Configured" && status.Phase != "Running" && status.Phase != "Restored")) {
			http.Error(w, "IPsec is not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.MarshalWrite(w, a.Status()); err != nil {
			klog.ErrorS(err, "Write IPsec status")
		}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		if err := server.Close(); err != nil {
			klog.ErrorS(err, "Close IPsec status server")
		}
	}()
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			klog.ErrorS(err, "Serve IPsec status")
		}
	}()
	return nil
}

// Check probes only the private local status socket; it needs no Kubernetes
// client and never invokes a command that dumps sensitive XFRM state.
func Check(ctx context.Context, runtimeDir, probe string) error {
	if probe != "livez" && probe != "readyz" {
		return errors.New("unknown IPsec probe")
	}
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(runtimeDir, "status.sock"))
	}}}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost/"+probe, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("IPsec probe returned HTTP %d", resp.StatusCode)
	}
	return nil
}
