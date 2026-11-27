package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/apimachinery/pkg/util/httpstream/spdy"
	"k8s.io/apimachinery/pkg/util/remotecommand"
	"k8s.io/client-go/kubernetes"
	corelister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestFipMigrationDuringGatewayReplay(t *testing.T) {
	enabled := vpcNatEnabled
	vpcNatEnabled = "true"
	t.Cleanup(func() { vpcNatEnabled = enabled })

	for _, phase := range []string{"before status publication", "after status publication", "same gateway waits for new pod"} {
		t.Run(phase, func(t *testing.T) {
			const namespace = "kube-system"
			const source = "gw-a"
			const destination = "gw-b"
			newer := time.Date(2026, 11, 20, 8, 10, 0, 0, time.UTC)
			fixture := newFipMigrationFixture(t, namespace, source, destination, newer)
			c, client, eip, fip := fixture.controller, fixture.client, fixture.eip, fixture.fip
			pods, eips, fips := fixture.pods, fixture.eips, fixture.fips
			destinationPod, err := c.getNatGwPod(destination)
			require.NoError(t, err)
			migrationRedo := natRuleBindingRedo(destinationPod, true)

			switch phase {
			case "before status publication":
				require.NoError(t, c.handleUpdateVpcFloatingIP(source))
				marked, err := client.KubeovnV1().IptablesFIPRules().Get(t.Context(), fip.Name, metav1.GetOptions{})
				require.NoError(t, err)
				require.Equal(t, newer.Format(natGwReplayTimeLayout), marked.Status.Redo)
				require.False(t, marked.Status.Ready)
				require.NoError(t, c.recordFipStatus(fip, eip, migrationRedo))
			case "after status publication":
				require.NoError(t, c.recordFipStatus(fip, eip, migrationRedo))
				require.NoError(t, c.handleUpdateVpcFloatingIP(source))
				staleEip := eip.DeepCopy()
				staleEip.Spec.NatGwDp = source
				require.NoError(t, eips.Update(staleEip))
				require.NoError(t, c.handleUpdateVpcEip(source))
				require.NoError(t, eips.Update(eip))
			case "same gateway waits for new pod":
				require.NoError(t, c.recordFipStatus(fip, eip, migrationRedo))
				// The destination gateway worker sees its replacement before the
				// rule worker, whose informer still selects the previous pod.
				previousPod, _, err := pods.GetByKey(namespace + "/" + destination)
				require.NoError(t, err)
				replacement := previousPod.(*corev1.Pod).DeepCopy()
				replacement.CreationTimestamp = metav1.NewTime(newer)
				require.NoError(t, pods.Update(replacement))
				current, err := client.KubeovnV1().IptablesFIPRules().Get(t.Context(), fip.Name, metav1.GetOptions{})
				require.NoError(t, err)
				current.Labels[util.VpcNatGatewayNameLabel] = destination
				require.NoError(t, fips.Update(current))
				require.NoError(t, c.handleUpdateVpcFloatingIP(destination))
				require.NoError(t, pods.Update(previousPod))
			}

			current, err := client.KubeovnV1().IptablesFIPRules().Get(t.Context(), fip.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, destination, current.Status.NatGwDp)
			require.NoError(t, fips.Update(current))
			if phase == "same gateway waits for new pod" {
				require.ErrorContains(t, c.handleUpdateIptablesFip(fip.Name), "started before redo mark")
				return
			}
			require.False(t, current.Status.Ready)
			require.Equal(t, migrationRedo, current.Status.Redo)
			gotEip, err := client.KubeovnV1().IptablesEIPs().Get(t.Context(), eip.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, eip.Status, gotEip.Status)
		})
	}
}

type fipMigrationFixture struct {
	controller       *Controller
	client           *kubeovnfake.Clientset
	eip              *kubeovnv1.IptablesEIP
	fip              *kubeovnv1.IptablesFIPRule
	pods, eips, fips cache.Indexer
}

func newFipMigrationFixture(t *testing.T, namespace, source, destination string, newer time.Time) fipMigrationFixture {
	t.Helper()
	older := newer.Add(-10 * time.Minute)
	pods := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	for _, gw := range []struct {
		name string
		when time.Time
	}{
		{name: source, when: newer},
		{name: destination, when: older},
	} {
		require.NoError(t, pods.Add(&corev1.Pod{
			Name: gw.name, Namespace: namespace, UID: types.UID(gw.name),
			Labels:            map[string]string{"app": util.GenNatGwName(gw.name), util.VpcNatGatewayLabel: "true"},
			CreationTimestamp: metav1.NewTime(gw.when),
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{
					State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(gw.when)}},
				}},
			},
		}))
	}

	eip := &kubeovnv1.IptablesEIP{
		Name:   "eip-b",
		Labels: map[string]string{util.VpcNatGatewayNameLabel: destination},
		Spec:   kubeovnv1.IptablesEIPSpec{NatGwDp: destination, V4ip: "192.0.2.20"},
		Status: kubeovnv1.IptablesEIPStatus{Ready: true, IP: "192.0.2.20", Redo: older.Format(natGwReplayTimeLayout)},
	}
	eips := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, eips.Add(eip))

	fip := &kubeovnv1.IptablesFIPRule{
		Name:       "fip",
		Labels:     map[string]string{util.VpcNatGatewayNameLabel: source},
		Finalizers: []string{util.KubeOVNControllerFinalizer},
		Spec:       kubeovnv1.IptablesFIPRuleSpec{EIP: eip.Name, InternalIP: "10.0.0.10"},
		Status:     kubeovnv1.IptablesFIPRuleStatus{Ready: true, NatGwDp: source, V4ip: "192.0.2.10", InternalIP: "10.0.0.10"},
	}
	fips := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, fips.Add(fip))

	client := kubeovnfake.NewSimpleClientset()
	_, err := client.KubeovnV1().IptablesEIPs().Create(t.Context(), eip, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = client.KubeovnV1().IptablesFIPRules().Create(t.Context(), fip, metav1.CreateOptions{})
	require.NoError(t, err)
	c := &Controller{
		config:                  &Configuration{PodNamespace: namespace, KubeOvnClient: client},
		podsLister:              corelister.NewPodLister(pods),
		iptablesFipsLister:      kubeovnlister.NewIptablesFIPRuleLister(fips),
		iptablesEipsLister:      kubeovnlister.NewIptablesEIPLister(eips),
		vpcNatGwKeyMutex:        keymutex.NewHashed(0),
		vpcNatGwExecKeyMutex:    keymutex.NewHashed(0),
		vpcNatGatewayLister:     kubeovnlister.NewVpcNatGatewayLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})),
		iptablesDnatRulesLister: kubeovnlister.NewIptablesDnatRuleLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})),
		iptablesSnatRulesLister: kubeovnlister.NewIptablesSnatRuleLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})),
	}
	return fipMigrationFixture{controller: c, client: client, eip: eip, fip: fip, pods: pods, eips: eips, fips: fips}
}

func TestFipMigrationReplaysAfterDestinationPodReplacement(t *testing.T) {
	testFipReplaysAfterDestinationPodReplacement(t, false)
}

func TestFipFirstInstallReplaysAfterDestinationPodReplacement(t *testing.T) {
	testFipReplaysAfterDestinationPodReplacement(t, true)
}

func testFipReplaysAfterDestinationPodReplacement(t *testing.T, firstInstall bool) {
	t.Helper()
	enabled := vpcNatEnabled
	vpcNatEnabled = "true"
	t.Cleanup(func() { vpcNatEnabled = enabled })

	const namespace, source, destination = "kube-system", "gw-a", "gw-b"
	newer := time.Date(2026, 11, 20, 8, 10, 0, 0, time.UTC)
	fixture := newFipMigrationFixture(t, namespace, source, destination, newer)
	c := fixture.controller
	previous, err := c.getNatGwPod(destination)
	require.NoError(t, err)
	replacement := previous.DeepCopy()
	replacement.UID = "replacement"
	replacement.CreationTimestamp = metav1.NewTime(newer)
	replacement.Status.ContainerStatuses[0].State.Running.StartedAt = metav1.NewTime(newer)

	client := fixture.client.KubeovnV1().IptablesFIPRules()
	reconcile := c.handleUpdateIptablesFip
	if firstInstall {
		rule := fixture.fip.DeepCopy()
		rule.Status = kubeovnv1.IptablesFIPRuleStatus{}
		rule.Labels = nil
		_, err := client.Update(t.Context(), rule, metav1.UpdateOptions{})
		require.NoError(t, err)
		require.NoError(t, fixture.fips.Update(rule))
		reconcile = c.handleAddIptablesFip
	}

	var applies atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(replacement); err != nil {
				t.Error(err)
			}
			return
		}
		if req.URL.Path != "/api/v1/namespaces/"+namespace+"/pods/"+destination+"/exec" {
			t.Errorf("unexpected pod exec path %q", req.URL.Path)
			http.NotFound(w, req)
			return
		}
		applies.Add(1)
		serveSuccessfulNatExec(t, w, req)
	}))
	t.Cleanup(server.Close)
	c.config.KubeRestConfig = &rest.Config{Host: server.URL}
	c.config.KubeClient, err = kubernetes.NewForConfig(c.config.KubeRestConfig)
	require.NoError(t, err)

	replaced := false
	replayDone := make(chan error, 1)
	fixture.client.PrependReactor("patch", "iptables-fip-rules", func(ktesting.Action) (bool, runtime.Object, error) {
		if !replaced {
			require.NoError(t, fixture.pods.Update(replacement))
			// The replacement's gateway replay runs before the new binding is visible.
			go func() {
				for applies.Load() == 0 {
					time.Sleep(time.Millisecond)
				}
				replayDone <- c.handleUpdateVpcFloatingIP(destination)
			}()
			replaced = true
		}
		return false, nil, nil
	})

	require.NoError(t, reconcile(fixture.fip.Name))
	require.NoError(t, <-replayDone)
	pending, err := client.Get(t.Context(), fixture.fip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, destination, pending.Status.NatGwDp)
	require.Equal(t, destination, pending.Labels[util.VpcNatGatewayNameLabel])
	require.False(t, pending.Status.Ready)
	wantRedo := replacement.CreationTimestamp.UTC().Format(natGwReplayTimeLayout)
	if firstInstall {
		wantRedo = previous.CreationTimestamp.UTC().Format(natGwReplayTimeLayout)
	}
	require.Equal(t, wantRedo, pending.Status.Redo)

	require.NoError(t, fixture.fips.Update(pending))
	require.NoError(t, c.handleUpdateIptablesFip(fixture.fip.Name))
	done, err := client.Get(t.Context(), fixture.fip.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, done.Status.Ready)
	require.EqualValues(t, 2, applies.Load())
}

func serveSuccessfulNatExec(t *testing.T, w http.ResponseWriter, req *http.Request) {
	t.Helper()
	if _, err := httpstream.Handshake(req, w, []string{remotecommand.StreamProtocolV4Name}); err != nil {
		t.Error(err)
		return
	}
	type execStream struct {
		httpstream.Stream
		replied <-chan struct{}
	}
	streams := make(chan execStream, 3)
	conn := spdy.NewResponseUpgrader().UpgradeResponse(w, req, func(stream httpstream.Stream, replied <-chan struct{}) error {
		streams <- execStream{Stream: stream, replied: replied}
		return nil
	})
	if conn == nil {
		t.Error("failed to upgrade pod exec")
		return
	}
	defer conn.Close()
	var statusStream httpstream.Stream
	for range 3 {
		select {
		case stream := <-streams:
			<-stream.replied
			if stream.Headers().Get(corev1.StreamType) == corev1.StreamTypeError {
				statusStream = stream.Stream
			}
		case <-time.After(5 * time.Second):
			t.Error("timed out waiting for pod exec streams")
			return
		}
	}
	if statusStream == nil {
		t.Error("missing pod exec status stream")
		return
	}
	if err := json.NewEncoder(statusStream).Encode(metav1.Status{Status: metav1.StatusSuccess}); err != nil {
		t.Error(err)
	}
}
