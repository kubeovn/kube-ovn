package ovs

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"k8s.io/klog/v2"
)

func (suite *OvnClientTestSuite) testFipQos() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	vpcName := "test_vpc"
	externalSubnet := "test_external_subnet"
	v4Eip := "192.168.100.10"
	klog.SetOutput(io.Discard)

	t.Run("create logical router", func(t *testing.T) {
		err := nbClient.CreateLogicalRouter(vpcName)
		require.NoError(t, err)
		lr, err := nbClient.GetLogicalRouter(vpcName, false)
		require.NoError(t, err)
		require.NotEmpty(t, lr)
	})

	t.Run(" create logical switch", func(t *testing.T) {
		err := nbClient.CreateGatewayLogicalSwitch(externalSubnet, vpcName, "ovn", "192.168.100.0/24", "", 10)
		require.NoError(t, err)
		_, err = nbClient.GetLogicalSwitch(externalSubnet, false)
		require.NoError(t, err)
	})

	t.Run("create Qos rule for egress", func(t *testing.T) {
		err := nbClient.AddQos(vpcName, externalSubnet, v4Eip, 1000, 1000, "to-lport")
		require.NoError(t, err)

		ls, err := nbClient.GetLogicalSwitch(externalSubnet, false)
		require.NoError(t, err)
		require.Len(t, ls.QOSRules, 1)
		// fmt.Println("QoS rules:", ls.QOSRules)
	})

	t.Run("create Qos rule for ingress ", func(t *testing.T) {
		err := nbClient.AddQos(vpcName, externalSubnet, v4Eip, 1000, 1000, "from-lport")
		require.NoError(t, err)

		ls, err := nbClient.GetLogicalSwitch(externalSubnet, false)
		require.NoError(t, err)
		require.Len(t, ls.QOSRules, 2)
		// fmt.Println("QoS rules:", ls.QOSRules)
	})

	t.Run("get QoS rules for logical switch", func(t *testing.T) {
		qosRules, err := nbClient.GetQos(vpcName, externalSubnet, v4Eip, "from-lport")
		require.NoError(t, err)
		require.NotEmpty(t, qosRules, "QoS rules should not be empty")
		// for _, qos := range qosRules {
		//	 fmt.Println(qos)
		// }
	})

	t.Run("update Qos rule for egress", func(t *testing.T) {
		err := nbClient.UpdateQos(vpcName, externalSubnet, v4Eip, 2000, 2000, "to-lport")
		require.NoError(t, err)
		qosRules, err := nbClient.GetQos(vpcName, externalSubnet, v4Eip, "to-lport")
		require.NoError(t, err)
		require.Len(t, qosRules, 1)
		require.Equal(t, 2000, qosRules[0].Bandwidth["rate"], "QoS rate should be updated to 2000")
	})

	t.Run("update Qos rule for ingress", func(t *testing.T) {
		err := nbClient.UpdateQos(vpcName, externalSubnet, v4Eip, 2000, 2000, "from-lport")
		require.NoError(t, err)
		qosRules, err := nbClient.GetQos(vpcName, externalSubnet, v4Eip, "from-lport")
		require.NoError(t, err)
		require.Len(t, qosRules, 1)
		require.Equal(t, 2000, qosRules[0].Bandwidth["rate"], "QoS rate should be updated to 2000")
	})

	t.Run("delete Qos rule for egress", func(t *testing.T) {
		err := nbClient.DeleteQos(vpcName, externalSubnet, v4Eip, "to-lport")
		require.NoError(t, err)
		qosRules, err := nbClient.GetQos(vpcName, externalSubnet, v4Eip, "to-lport")
		require.NoError(t, err)
		require.Empty(t, qosRules, "QoS rules for egress should be deleted")
	})

	t.Run("delete Qos rule for ingress", func(t *testing.T) {
		err := nbClient.DeleteQos(vpcName, externalSubnet, v4Eip, "from-lport")
		require.NoError(t, err)
		qosRules, err := nbClient.GetQos(vpcName, externalSubnet, v4Eip, "from-lport")
		require.NoError(t, err)
		require.Empty(t, qosRules, "QoS rules for ingress should be deleted")
	})

	t.Run("delete logical router", func(t *testing.T) {
		err := nbClient.DeleteLogicalRouter(vpcName)
		require.NoError(t, err)
		lr, err := nbClient.GetLogicalRouter(vpcName, false)
		require.Error(t, err)
		require.Nil(t, lr, "Logical router should be deleted")
	})

	t.Run("delete logical switch", func(t *testing.T) {
		err := nbClient.DeleteLogicalSwitch(externalSubnet)
		require.NoError(t, err)
		ls, err := nbClient.GetLogicalSwitch(externalSubnet, false)
		require.Error(t, err)
		require.Nil(t, ls, "Logical switch should be deleted")
	})
}
