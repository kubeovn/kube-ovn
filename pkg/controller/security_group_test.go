package controller

import (
	"encoding/hex"
	"testing"

	"github.com/cnf/structhash"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func mockLsp() *ovnnb.LogicalSwitchPort {
	return &ovnnb.LogicalSwitchPort{
		ExternalIDs: map[string]string{
			"associated_sg_default-securitygroup": "false",
			"associated_sg_sg":                    "true",
			"security_groups":                     "sg",
		},
	}
}

func Test_getPortSg(t *testing.T) {
	t.Run("only have one sg", func(t *testing.T) {
		c := &Controller{}
		port := mockLsp()
		out, err := c.getPortSg(port)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"sg"}, out)
	})

	t.Run("have two and more sgs", func(t *testing.T) {
		c := &Controller{}
		port := mockLsp()
		port.ExternalIDs["associated_sg_default-securitygroup"] = "true"
		out, err := c.getPortSg(port)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"sg", "default-securitygroup"}, out)
	})
}

func Test_updateDenyAllSgPortsIncludesPortsWithMissingSecurityGroups(t *testing.T) {
	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient

	ports := []ovnnb.LogicalSwitchPort{
		{
			Name: "early",
			ExternalIDs: map[string]string{
				sgsKey: "later",
			},
		},
		{
			Name: "existing",
			ExternalIDs: map[string]string{
				sgsKey: "ready",
			},
		},
	}
	mockOvnClient.EXPECT().ListNormalLogicalSwitchPorts(true, map[string]string{sgsKey: ""}).Return(ports, nil)
	mockOvnClient.EXPECT().PortGroupSetPorts(ovs.GetSgPortGroupName(util.DenyAllSecurityGroup), []string{"early", "existing"}).Return(nil)

	require.NoError(t, ctrl.updateDenyAllSgPorts())
}

func TestHandleAddOrUpdateSgForceRecordsRuleHashes(t *testing.T) {
	fc := newFakeController(t)
	ctrl := fc.fakeController

	sg := &kubeovnv1.SecurityGroup{
		Name: "test-sg",
		Spec: kubeovnv1.SecurityGroupSpec{
			Tier: util.SecurityGroupAPITierMinimum,
			IngressRules: []kubeovnv1.SecurityGroupRule{{
				IPVersion:     "ipv4",
				Priority:      1,
				RemoteType:    kubeovnv1.SgRemoteTypeAddress,
				RemoteAddress: "10.0.0.0/8",
				Protocol:      kubeovnv1.SgProtocolALL,
				Policy:        kubeovnv1.SgPolicy(ovnnb.ACLActionAllow),
			}},
			EgressRules: []kubeovnv1.SecurityGroupRule{{
				IPVersion:     "ipv4",
				Priority:      1,
				RemoteType:    kubeovnv1.SgRemoteTypeAddress,
				RemoteAddress: "192.168.0.0/16",
				Protocol:      kubeovnv1.SgProtocolALL,
				Policy:        kubeovnv1.SgPolicy(ovnnb.ACLActionAllow),
			}},
		},
	}
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	require.NoError(t, indexer.Add(sg))
	ctrl.sgsLister = kubeovnlister.NewSecurityGroupLister(indexer)
	ctrl.sgKeyMutex = keymutex.NewHashed(0)
	ctrl.syncSgPortsQueue = newTypedRateLimitingQueue[string]("TestSyncSecurityGroupPorts", nil)
	ctrl.config.KubeOvnClient = kubeovnfake.NewSimpleClientset()
	_, err := ctrl.config.KubeOvnClient.KubeovnV1().SecurityGroups().Create(t.Context(), sg, metav1.CreateOptions{})
	require.NoError(t, err)

	fc.mockOvnClient.EXPECT().CreatePortGroup(ovs.GetSgPortGroupName(sg.Name), map[string]string{
		"type": "security_group",
		sgKey:  sg.Name,
	}).Return(nil)
	fc.mockOvnClient.EXPECT().CreateAddressSet(ovs.GetSgV4AssociatedName(sg.Name), map[string]string{sgKey: sg.Name}).Return(nil)
	fc.mockOvnClient.EXPECT().CreateAddressSet(ovs.GetSgV6AssociatedName(sg.Name), map[string]string{sgKey: sg.Name}).Return(nil)
	fc.mockOvnClient.EXPECT().UpdateSgACL(gomock.Any(), ovnnb.ACLDirectionToLport).Return(nil)
	fc.mockOvnClient.EXPECT().CreateSgBaseACL(sg.Name, ovnnb.ACLDirectionToLport).Return(nil)
	fc.mockOvnClient.EXPECT().UpdateSgACL(gomock.Any(), ovnnb.ACLDirectionFromLport).Return(nil)
	fc.mockOvnClient.EXPECT().CreateSgBaseACL(sg.Name, ovnnb.ACLDirectionFromLport).Return(nil)

	require.NoError(t, ctrl.handleAddOrUpdateSg(sg.Name, true))
	updated, err := ctrl.config.KubeOvnClient.KubeovnV1().SecurityGroups().Get(t.Context(), sg.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, hex.EncodeToString(structhash.Md5(sg.Spec.IngressRules, 1)), updated.Status.IngressMd5)
	require.Equal(t, hex.EncodeToString(structhash.Md5(sg.Spec.EgressRules, 1)), updated.Status.EgressMd5)
	require.True(t, updated.Status.IngressLastSyncSuccess)
	require.True(t, updated.Status.EgressLastSyncSuccess)
}

func Test_validateSgRule(t *testing.T) {
	t.Parallel()

	ctrl := &Controller{}
	baseSG := func(rules ...kubeovnv1.SecurityGroupRule) *kubeovnv1.SecurityGroup {
		return &kubeovnv1.SecurityGroup{
			Name: "test-sg",
			Spec: kubeovnv1.SecurityGroupSpec{
				Tier:         util.SecurityGroupAPITierMinimum,
				IngressRules: rules,
			},
		}
	}

	t.Run("valid local address as CIDR", func(t *testing.T) {
		t.Parallel()

		sg := baseSG(kubeovnv1.SecurityGroupRule{
			IPVersion:     "ipv4",
			Priority:      1,
			RemoteType:    kubeovnv1.SgRemoteTypeAddress,
			RemoteAddress: "10.0.0.0/8",
			LocalAddress:  "192.168.1.0/24",
			Protocol:      "all",
			Policy:        kubeovnv1.SgPolicy(ovnnb.ACLActionAllow),
		})
		err := ctrl.validateSgRule(sg)
		require.NoError(t, err)
	})

	t.Run("valid local address as IP", func(t *testing.T) {
		t.Parallel()

		sg := baseSG(kubeovnv1.SecurityGroupRule{
			IPVersion:     "ipv4",
			Priority:      1,
			RemoteType:    kubeovnv1.SgRemoteTypeAddress,
			RemoteAddress: "10.0.0.1",
			LocalAddress:  "192.168.1.100",
			Protocol:      "all",
			Policy:        kubeovnv1.SgPolicy(ovnnb.ACLActionAllow),
		})
		err := ctrl.validateSgRule(sg)
		require.NoError(t, err)
	})

	t.Run("invalid local address CIDR", func(t *testing.T) {
		t.Parallel()

		sg := baseSG(kubeovnv1.SecurityGroupRule{
			IPVersion:     "ipv4",
			Priority:      1,
			RemoteType:    kubeovnv1.SgRemoteTypeAddress,
			RemoteAddress: "10.0.0.1",
			LocalAddress:  "999.999.999.0/24",
			Protocol:      "all",
			Policy:        kubeovnv1.SgPolicy(ovnnb.ACLActionAllow),
		})
		err := ctrl.validateSgRule(sg)
		require.ErrorContains(t, err, "invalid CIDR")
	})

	t.Run("invalid local address IP", func(t *testing.T) {
		t.Parallel()

		sg := baseSG(kubeovnv1.SecurityGroupRule{
			IPVersion:     "ipv4",
			Priority:      1,
			RemoteType:    kubeovnv1.SgRemoteTypeAddress,
			RemoteAddress: "10.0.0.1",
			LocalAddress:  "not-an-ip",
			Protocol:      "all",
			Policy:        kubeovnv1.SgPolicy(ovnnb.ACLActionAllow),
		})
		err := ctrl.validateSgRule(sg)
		require.ErrorContains(t, err, "invalid ip address")
	})

	t.Run("valid local port range with TCP and local address", func(t *testing.T) {
		t.Parallel()

		sg := baseSG(kubeovnv1.SecurityGroupRule{
			IPVersion:          "ipv4",
			Priority:           1,
			RemoteType:         kubeovnv1.SgRemoteTypeAddress,
			RemoteAddress:      "10.0.0.1",
			LocalAddress:       "192.168.1.100",
			Protocol:           "tcp",
			Policy:             kubeovnv1.SgPolicy(ovnnb.ACLActionAllow),
			PortRangeMin:       80,
			PortRangeMax:       443,
			SourcePortRangeMin: 1024,
			SourcePortRangeMax: 65535,
		})
		err := ctrl.validateSgRule(sg)
		require.NoError(t, err)
	})

	t.Run("invalid local port range out of bounds", func(t *testing.T) {
		t.Parallel()

		sg := baseSG(kubeovnv1.SecurityGroupRule{
			IPVersion:          "ipv4",
			Priority:           1,
			RemoteType:         kubeovnv1.SgRemoteTypeAddress,
			RemoteAddress:      "10.0.0.1",
			LocalAddress:       "192.168.1.100",
			Protocol:           "tcp",
			Policy:             kubeovnv1.SgPolicy(ovnnb.ACLActionAllow),
			PortRangeMin:       80,
			PortRangeMax:       443,
			SourcePortRangeMin: 0,
			SourcePortRangeMax: 65535,
		})
		err := ctrl.validateSgRule(sg)
		require.ErrorContains(t, err, "sourcePortRange is out of range")
	})

	t.Run("invalid local port range min greater than max", func(t *testing.T) {
		t.Parallel()

		sg := baseSG(kubeovnv1.SecurityGroupRule{
			IPVersion:          "ipv4",
			Priority:           1,
			RemoteType:         kubeovnv1.SgRemoteTypeAddress,
			RemoteAddress:      "10.0.0.1",
			LocalAddress:       "192.168.1.100",
			Protocol:           "udp",
			Policy:             kubeovnv1.SgPolicy(ovnnb.ACLActionAllow),
			PortRangeMin:       80,
			PortRangeMax:       443,
			SourcePortRangeMin: 9000,
			SourcePortRangeMax: 8000,
		})
		err := ctrl.validateSgRule(sg)
		require.ErrorContains(t, err, "range Minimum value greater than maximum value")
	})
}
