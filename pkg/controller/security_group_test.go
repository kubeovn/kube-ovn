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

func Test_upgradeSecurityGroups(t *testing.T) {
	t.Parallel()

	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController
	fakeinformers := fakeController.fakeInformers
	mockOvnClient := fakeController.mockOvnClient

	sg := &kubeovnv1.SecurityGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: "sg",
		},
	}

	err := fakeinformers.sgInformer.Informer().GetStore().Add(sg)
	require.NoError(t, err)

	mockOvnClient.EXPECT().DeleteAcls(gomock.Any(), portGroupKey, "", nil, util.DefaultACLTier).Return(nil).Times(2)

	err = ctrl.upgradeSecurityGroups()
	require.NoError(t, err)
}

func Test_securityGroupALLNotExist(t *testing.T) {
	t.Parallel()

	fakeController := newFakeController(t)
	ctrl := fakeController.fakeController
	mockOvnClient := fakeController.mockOvnClient

	sgName := "sg"
	pgName := ovs.GetSgPortGroupName(sgName)

	t.Run("should return false when some port group exist", func(t *testing.T) {
		mockOvnClient.EXPECT().PortGroupExists(gomock.Eq(pgName)).Return(true, nil)
		mockOvnClient.EXPECT().PortGroupExists(gomock.Not(pgName)).Return(false, nil).Times(3)

		exist, err := ctrl.securityGroupAllNotExist([]string{sgName, "sg1", "sg2", "sg3"})
		require.NoError(t, err)
		require.False(t, exist)
	})

	t.Run("should return true when all port group does't exist", func(t *testing.T) {
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
