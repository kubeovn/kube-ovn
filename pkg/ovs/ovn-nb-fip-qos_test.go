package ovs

import (
	"fmt"
	"testing"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/stretchr/testify/require"
)

//func newQoS(lsName, direction, match, priority, rateLimit string, options ...func(qos *ovnnb.QoS)) *ovnnb.QoS {
//	intPriority, _ := strconv.Atoi(priority)
//	intRateLimit, _ := strconv.Atoi(rateLimit)
//
//	qos := &ovnnb.QoS{
//		UUID:      ovsclient.NamedUUID(),
//		Direction: direction,
//		Match:     match,
//		Priority:  intPriority,
//		Bandwidth: ptr.To(intRateLimit),
//		ExternalIDs: map[string]string{
//			"logical_switch": lsName,
//		},
//	}
//
//	for _, option := range options {
//		option(qos)
//	}
//
//	return qos
//}

func (suite *OvnClientTestSuite) TestCreateQoS() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	vpcName := "test_vpc"
	externalSubnet := "test_external_subnet"
	subnetName := "test_subnet"
	v4Eip := "192.168.1.100"
	burstMax := 1000
	rateMax := 1000
	direction := ovnnb.QoSDirectionFromLport
	//priority := "2003"

	t.Run("create logical router", func(t *testing.T) {
		t.Parallel()
		err := nbClient.CreateLogicalRouter(vpcName)
		require.NoError(t, err)
		lr, err := nbClient.GetLogicalRouter(vpcName, false)
		fmt.Println(lr.Ports)
		require.NoError(t, err)
		require.NotEmpty(t, lr)
	})

	t.Run("create logical switch", func(t *testing.T) {
		t.Parallel()

		err := nbClient.CreateLogicalSwitch(subnetName, vpcName, "10.18.0.0/16", "10.18.0.1", "", false, false)
		require.NoError(t, err)

		ls, err := nbClient.GetLogicalSwitch(externalSubnet, false)
		fmt.Println(ls)
		require.NoError(t, err)
		require.NotNil(t, ls)
	})

	t.Run("create external subnet", func(t *testing.T) {
		t.Parallel()

		err := nbClient.CreateBareLogicalSwitch(externalSubnet)
		require.NoError(t, err)
		ls, err := nbClient.GetLogicalSwitch(externalSubnet, false)
		fmt.Println("external subnet:", ls)
	})

	t.Run("create QoS rule", func(t *testing.T) {
		t.Parallel()

		//qos := a(lsName, direction, match, priority, rateLimit)
		err := nbClient.AddQos(vpcName, externalSubnet, v4Eip, burstMax, rateMax, direction)
		require.NoError(t, err)

		ls, err := nbClient.GetLogicalSwitch(externalSubnet, false)
		require.NoError(t, err)
		require.Len(t, ls.QOSRules, 1)
	})
}

//func (suite *OvnClientTestSuite) testDeleteQoS() {
//	t := suite.T()
//	t.Parallel()
//
//	nbClient := suite.ovnNBClient
//	lsName := "test_delete_qos_ls"
//	match := "ip4.src == 192.168.1.0/24"
//	direction := ovnnb.QoSDirectionFromLport
//	priority := "1000"
//	rateLimit := "1000"
//
//	t.Run("delete QoS rule", func(t *testing.T) {
//		t.Parallel()
//
//		qos := newQoS(lsName, direction, match, priority, rateLimit)
//		err := nbClient.CreateQoS(lsName, qos)
//		require.NoError(t, err)
//
//		err = nbClient.DeleteQoS(lsName, qos.UUID)
//		require.NoError(t, err)
//
//		ls, err := nbClient.GetLogicalSwitch(lsName, false)
//		require.NoError(t, err)
//		require.Empty(t, ls.QOSRules)
//	})
//}
//
//func (suite *OvnClientTestSuite) testUpdateQoS() {
//	t := suite.T()
//	t.Parallel()
//
//	nbClient := suite.ovnNBClient
//	lsName := "test_update_qos_ls"
//	match := "ip4.src == 192.168.1.0/24"
//	direction := ovnnb.QoSDirectionFromLport
//	priority := "1000"
//	rateLimit := "1000"
//
//	t.Run("update QoS rule", func(t *testing.T) {
//		t.Parallel()
//
//		qos := newQoS(lsName, direction, match, priority, rateLimit)
//		err := nbClient.CreateQoS(lsName, qos)
//		require.NoError(t, err)
//
//		qos.RateLimit = ptr.To(2000)
//		err = nbClient.UpdateQoS(lsName, qos)
//		require.NoError(t, err)
//
//		ls, err := nbClient.GetLogicalSwitch(lsName, false)
//		require.NoError(t, err)
//		require.Equal(t, 2000, *ls.QOSRules[0].RateLimit)
//	})
//}
