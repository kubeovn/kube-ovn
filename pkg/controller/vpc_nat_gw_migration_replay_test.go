package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corelister "k8s.io/client-go/listers/core/v1"
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

			switch phase {
			case "before status publication":
				require.NoError(t, c.handleUpdateVpcFloatingIP(source))
				marked, err := client.KubeovnV1().IptablesFIPRules().Get(t.Context(), fip.Name, metav1.GetOptions{})
				require.NoError(t, err)
				require.Equal(t, newer.Format(natGwReplayTimeLayout), marked.Status.Redo)
				require.False(t, marked.Status.Ready)
				require.NoError(t, c.recordFipStatus(fip, eip))
			case "after status publication":
				require.NoError(t, c.recordFipStatus(fip, eip))
				require.NoError(t, c.handleUpdateVpcFloatingIP(source))
				staleEip := eip.DeepCopy()
				staleEip.Spec.NatGwDp = source
				require.NoError(t, eips.Update(staleEip))
				require.NoError(t, c.handleUpdateVpcEip(source))
				require.NoError(t, eips.Update(eip))
			case "same gateway waits for new pod":
				require.NoError(t, c.recordFipStatus(fip, eip))
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
			err = c.handleUpdateIptablesFip(fip.Name)
			if phase == "same gateway waits for new pod" {
				require.ErrorContains(t, err, "started before redo mark")
				return
			}
			require.NoError(t, err)
			require.True(t, current.Status.Ready)
			require.Empty(t, current.Status.Redo)
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
			Name: gw.name, Namespace: namespace,
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
		config:             &Configuration{PodNamespace: namespace, KubeOvnClient: client},
		podsLister:         corelister.NewPodLister(pods),
		iptablesFipsLister: kubeovnlister.NewIptablesFIPRuleLister(fips),
		iptablesEipsLister: kubeovnlister.NewIptablesEIPLister(eips),
		vpcNatGwKeyMutex:   keymutex.NewHashed(0),
	}
	return fipMigrationFixture{controller: c, client: client, eip: eip, fip: fip, pods: pods, eips: eips, fips: fips}
}
