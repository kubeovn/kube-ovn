package ovs

import (
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (suite *OvnClientTestSuite) Test_MeterLifecycle() {
	suite.testMeterLifecycle()
}

func (suite *OvnClientTestSuite) testMeterLifecycle() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	name := "test-meter-pktps"

	// ignoreNotFound=true should return nil when meter不存在
	meter, err := nbClient.GetMeter(name, true)
	require.NoError(t, err)
	require.Nil(t, meter)

	err = nbClient.CreateOrUpdateMeter(name, ovnnb.MeterUnitPktps, 100, 10)
	require.NoError(t, err)

	meter, err = nbClient.GetMeter(name, false)
	require.NoError(t, err)
	require.Equal(t, name, meter.Name)
	require.Len(t, meter.Bands, 1)

	exists, err := nbClient.MeterExists(name)
	require.NoError(t, err)
	require.True(t, exists)

	all, err := nbClient.ListAllMeters()
	require.NoError(t, err)
	var found bool
	for _, m := range all {
		if m != nil && m.Name == name {
			found = true
			break
		}
	}
	require.True(t, found)

	err = nbClient.DeleteMeter(name)
	require.NoError(t, err)

	meter, err = nbClient.GetMeter(name, true)
	require.NoError(t, err)
	require.Nil(t, meter)

	exists, err = nbClient.MeterExists(name)
	require.NoError(t, err)
	require.False(t, exists)
}
