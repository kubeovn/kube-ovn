package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
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

func Test_securityGroupALLNotExist(t *testing.T) {
	t.Parallel()

	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient

	sgName := "sg"
	pgName := ovs.GetSgPortGroupName(sgName)

	t.Run("should return false when some port group exists", func(t *testing.T) {
		mockOvnClient.EXPECT().PortGroupExists(gomock.Eq(pgName)).Return(true, nil)
		mockOvnClient.EXPECT().PortGroupExists(gomock.Not(pgName)).Return(false, nil).Times(3)

		exist, err := ctrl.securityGroupAllNotExist([]string{sgName, "sg1", "sg2", "sg3"})
		require.NoError(t, err)
		require.False(t, exist)
	})

	t.Run("should return true when all port group don't exist", func(t *testing.T) {
		mockOvnClient.EXPECT().PortGroupExists(gomock.Any()).Return(false, nil).Times(3)

		exist, err := ctrl.securityGroupAllNotExist([]string{"sg1", "sg2", "sg3"})
		require.NoError(t, err)
		require.True(t, exist)
	})

	t.Run("should return true when sgs is empty", func(t *testing.T) {
		exist, err := ctrl.securityGroupAllNotExist([]string{})
		require.NoError(t, err)
		require.True(t, exist)
	})
}

func Test_validateSgRule(t *testing.T) {
	t.Parallel()

	ctrl := &Controller{}
	baseSG := func(rules ...kubeovnv1.SecurityGroupRule) *kubeovnv1.SecurityGroup {
		return &kubeovnv1.SecurityGroup{
			ObjectMeta: metav1.ObjectMeta{Name: "test-sg"},
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
