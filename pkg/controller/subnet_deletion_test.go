package controller

import (
	"errors"
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovnlisters "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestDeletingSubnetWithInvalidVlan(t *testing.T) {
	for _, state := range []string{"missing", "conflict", "not ready"} {
		for _, occupied := range []bool{false, true} {
			name := state + "/empty"
			if occupied {
				name = state + "/occupied"
			}
			t.Run(name, func(t *testing.T) {
				subnet := &kubeovnv1.Subnet{
					Name: "underlay", DeletionTimestamp: new(metav1.Now()),
					Finalizers: []string{util.KubeOVNControllerFinalizer},
					Spec:       kubeovnv1.SubnetSpec{CIDRBlock: "10.0.0.0/24", Gateway: "10.0.0.1", Vlan: "vlan"},
				}
				options := &FakeControllerOptions{Subnets: []*kubeovnv1.Subnet{subnet}}
				if state != "missing" {
					options.Vlans = []*kubeovnv1.Vlan{{Name: "vlan", Spec: kubeovnv1.VlanSpec{ID: 341, Provider: "provider"}, Status: kubeovnv1.VlanStatus{Conflict: state == "conflict"}}}
					options.ProviderNetworks = []*kubeovnv1.ProviderNetwork{{Name: "provider"}}
				}
				if occupied {
					// Status starts at zero, so deletion must refresh usage from IP CRs.
					options.IPs = []*kubeovnv1.IP{{Name: "pod.ns", Spec: kubeovnv1.IPSpec{Subnet: subnet.Name, V4IPAddress: "10.0.0.2"}}}
				}
				fc, err := newFakeControllerWithOptions(t, options)
				require.NoError(t, err)
				c := fc.fakeController
				c.virtualIpsLister = kubeovnlisters.NewVipLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))
				require.NoError(t, c.handleAddOrUpdateSubnet(subnet.Name))
				updated, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Get(t.Context(), subnet.Name, metav1.GetOptions{})
				require.NoError(t, err)
				if occupied {
					require.Contains(t, updated.Finalizers, util.KubeOVNControllerFinalizer)
					require.True(t, updated.Status.V4UsingIPs.EqualInt64(1))
					// Releasing the last IP must allow deletion even if the VLAN
					// never recovers.
					require.NoError(t, c.config.KubeOvnClient.KubeovnV1().IPs().Delete(t.Context(), "pod.ns", metav1.DeleteOptions{}))
					require.Eventually(t, func() bool {
						ips, err := c.ipIndexer.ByIndex(IndexIPBySubnet, subnet.Name)
						return err == nil && len(ips) == 0
					}, time.Second, time.Millisecond)
					require.NoError(t, c.handleAddOrUpdateSubnet(subnet.Name))
					updated, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Get(t.Context(), subnet.Name, metav1.GetOptions{})
					require.NoError(t, err)
				}
				require.NotContains(t, updated.Finalizers, util.KubeOVNControllerFinalizer)
			})
		}
	}
}

func TestDeletingSubnetMulticastQuerier(t *testing.T) {
	for _, tc := range []struct {
		name           string
		enabled        bool
		occupied       bool
		deleteFailure  bool
		cleanupFailure string
	}{
		{name: "disabled-empty"},
		{name: "disabled-occupied", occupied: true},
		{name: "enabled", enabled: true},
		{name: "delete-retry", deleteFailure: true},
		{name: "list-retry", occupied: true, cleanupFailure: "list"},
		{name: "config-retry", occupied: true, cleanupFailure: "config"},
		{name: "port-retry", occupied: true, cleanupFailure: "port"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			subnet := &kubeovnv1.Subnet{
				Name: "underlay", DeletionTimestamp: new(metav1.Now()),
				Finalizers: []string{util.KubeOVNControllerFinalizer},
				Spec: kubeovnv1.SubnetSpec{
					CIDRBlock: "10.0.0.0/24", Gateway: "10.0.0.1", Vlan: "missing",
					EnableMulticastSnoop: tc.enabled,
				},
				Status: kubeovnv1.SubnetStatus{McastQuerierIP: "10.0.0.2", McastQuerierMAC: "02:00:00:00:00:02"},
			}
			querierName := fmt.Sprintf(util.McastQuerierName, subnet.Name)
			opts := &FakeControllerOptions{
				Subnets: []*kubeovnv1.Subnet{subnet},
				IPs:     []*kubeovnv1.IP{{Name: querierName, Spec: kubeovnv1.IPSpec{Subnet: subnet.Name, V4IPAddress: "10.0.0.2"}}},
			}
			if tc.occupied {
				opts.IPs = append(opts.IPs, &kubeovnv1.IP{Name: "pod.ns", Spec: kubeovnv1.IPSpec{Subnet: subnet.Name, V4IPAddress: "10.0.0.3"}})
			}
			fc, err := newFakeControllerWithOptions(t, opts)
			require.NoError(t, err)
			c := fc.fakeController
			c.virtualIpsLister = kubeovnlisters.NewVipLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{}))
			t.Cleanup(c.addOrUpdateSubnetQueue.ShutDown)
			_, err = fc.fakeInformers.ipInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{DeleteFunc: c.enqueueDelIP})
			require.NoError(t, err)
			client := c.config.KubeOvnClient.(*kubeovnfake.Clientset)
			deleteErr := errors.New("querier deletion failed")
			failDeletion := tc.deleteFailure
			ovn := mockDeletingSubnetMulticastOVN(t, fc, subnet, tc.cleanupFailure)
			client.PrependReactor("delete", "ips", func(k8stesting.Action) (bool, runtime.Object, error) {
				require.Empty(t, ovn.config, "disable OVN multicast before releasing its IP")
				require.False(t, ovn.portExists, "delete the querier port before releasing its IP")
				if failDeletion {
					return true, nil, deleteErr
				}
				return false, nil, nil
			})
			err = c.handleAddOrUpdateSubnet(subnet.Name)
			if tc.deleteFailure || tc.cleanupFailure != "" {
				expectedErr := deleteErr
				if tc.cleanupFailure != "" {
					expectedErr = ovn.err
				}
				require.ErrorIs(t, err, expectedErr)
				updated, getErr := client.KubeovnV1().Subnets().Get(t.Context(), subnet.Name, metav1.GetOptions{})
				require.NoError(t, getErr)
				require.Contains(t, updated.Finalizers, util.KubeOVNControllerFinalizer)
				_, err = client.KubeovnV1().IPs().Get(t.Context(), querierName, metav1.GetOptions{})
				require.NoError(t, err)
				failDeletion = false
				ovn.failAt = ""
				err = c.handleAddOrUpdateSubnet(subnet.Name)
			}
			require.NoError(t, err)
			_, err = client.KubeovnV1().IPs().Get(t.Context(), querierName, metav1.GetOptions{})
			if tc.enabled {
				require.NoError(t, err)
			} else {
				require.True(t, k8serrors.IsNotFound(err), "disabled querier IP must be released during deletion")
			}
			remaining := 0
			if tc.occupied || tc.enabled {
				remaining = 1
			}
			if !tc.enabled {
				// Consume the real deletion event: cleanup must wake the subnet
				// after the IP leaves the informer cache, without a manual retry.
				require.Eventually(t, func() bool { return c.addOrUpdateSubnetQueue.Len() == 1 }, time.Second, time.Millisecond)
				key, shutdown := c.addOrUpdateSubnetQueue.Get()
				require.False(t, shutdown)
				require.Equal(t, subnet.Name, key)
				require.NoError(t, c.handleAddOrUpdateSubnet(key))
				c.addOrUpdateSubnetQueue.Done(key)
			} else {
				require.Zero(t, c.addOrUpdateSubnetQueue.Len())
			}
			updated, err := client.KubeovnV1().Subnets().Get(t.Context(), subnet.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.True(t, updated.Status.V4UsingIPs.EqualInt64(int64(remaining)))
			if remaining != 0 {
				require.Contains(t, updated.Finalizers, util.KubeOVNControllerFinalizer)
			} else {
				require.NotContains(t, updated.Finalizers, util.KubeOVNControllerFinalizer)
			}
		})
	}
}

// Track OVN state independently of IP usage so deletion tests catch an active
// querier being left behind after its IP has been released.
type deletingSubnetMulticastOVN struct {
	config     map[string]string
	portExists bool
	failAt     string
	err        error
}

func mockDeletingSubnetMulticastOVN(t *testing.T, fc *fakeController, subnet *kubeovnv1.Subnet, failAt string) *deletingSubnetMulticastOVN {
	t.Helper()
	state := &deletingSubnetMulticastOVN{
		config: map[string]string{
			"mcast_snoop": "true", "mcast_querier": "true",
			"mcast_ip4_src": subnet.Status.McastQuerierIP, "mcast_eth_src": subnet.Status.McastQuerierMAC,
		},
		portExists: true, failAt: failAt, err: errors.New("OVN multicast cleanup failed"),
	}
	if subnet.Spec.EnableMulticastSnoop {
		return state
	}
	fc.mockOvnClient.EXPECT().ListLogicalSwitch(false, gomock.Any()).DoAndReturn(
		func(_ bool, filter func(*ovnnb.LogicalSwitch) bool) ([]ovnnb.LogicalSwitch, error) {
			if state.failAt == "list" {
				return nil, state.err
			}
			ls := ovnnb.LogicalSwitch{Name: subnet.Name, OtherConfig: maps.Clone(state.config)}
			require.True(t, filter(&ls))
			return []ovnnb.LogicalSwitch{ls}, nil
		},
	).AnyTimes()
	fc.mockOvnClient.EXPECT().LogicalSwitchUpdateOtherConfig(subnet.Name, ovsdb.MutateOperationDelete, gomock.Any()).DoAndReturn(
		func(_ string, _ ovsdb.Mutator, config map[string]string) error {
			if state.failAt == "config" {
				return state.err
			}
			for key, value := range config {
				require.Equal(t, state.config[key], value)
				delete(state.config, key)
			}
			return nil
		},
	).AnyTimes()
	fc.mockOvnClient.EXPECT().DeleteLogicalSwitchPort(fmt.Sprintf(util.McastQuerierName, subnet.Name)).DoAndReturn(
		func(string) error {
			require.Empty(t, state.config)
			if state.failAt == "port" {
				return state.err
			}
			state.portExists = false
			return nil
		},
	).AnyTimes()
	return state
}
