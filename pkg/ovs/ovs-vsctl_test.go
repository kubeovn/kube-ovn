package ovs

import "github.com/stretchr/testify/require"

func (suite *OvnClientTestSuite) testOvsFindBridges() {
	t := suite.T()
	t.Parallel()

	ret, err := Bridges()
	// ovs-vsctl cmd is not available in the test environment
	require.Error(t, err)
	require.Empty(t, ret)
}

func (suite *OvnClientTestSuite) testOvsBridgeExists() {
	t := suite.T()
	t.Parallel()

	ret, err := BridgeExists("bridge-name")
	// ovs-vsctl cmd is not available in the test environment
	require.Error(t, err)
	require.False(t, ret)
}

func (suite *OvnClientTestSuite) testOvsPortExists() {
	t := suite.T()
	t.Parallel()

	ret, err := PortExists("port-name")
	// ovs-vsctl cmd is not available in the test environment
	require.Error(t, err)
	require.False(t, ret)
}

func (suite *OvnClientTestSuite) testGetOvsQosList() {
	t := suite.T()
	t.Parallel()

	ret, err := GetQosList("pod-name", "pod-namespace", "iface-id")
	// ovs-vsctl cmd is not available in the test environment
	require.Error(t, err)
	require.Empty(t, ret)

	ret, err = GetQosList("pod-name", "pod-namespace", "")
	// ovs-vsctl cmd is not available in the test environment
	require.Error(t, err)
	require.Empty(t, ret)
}

func (suite *OvnClientTestSuite) testOvsClearPodBandwidth() {
	t := suite.T()
	t.Parallel()

	err := ClearPodBandwidth("pod-name", "pod-namespace", "iface-id")
	// ovs-vsctl cmd is not available in the test environment
	require.Error(t, err)
}

func (suite *OvnClientTestSuite) testOvsCleanDuplicatePort() {
	t := suite.T()
	t.Parallel()

	CleanDuplicatePort("iface-id", "port-name")
}

func (suite *OvnClientTestSuite) testValidatePortVendor() {
	t := suite.T()
	t.Parallel()

	ok, err := ValidatePortVendor("port-name")
	// ovs-vsctl cmd is not available in the test environment
	require.Error(t, err)
	require.False(t, ok)
}

func (suite *OvnClientTestSuite) testGetInterfacePodNs() {
	t := suite.T()
	t.Parallel()

	ret, err := GetInterfacePodNs("iface-id")
	// ovs-vsctl cmd is not available in the test environment
	require.Error(t, err)
	require.Empty(t, ret)
}

func (suite *OvnClientTestSuite) testConfigInterfaceMirror() {
	t := suite.T()
	t.Parallel()

	err := ConfigInterfaceMirror(true, "open", "iface-id")
	// ovs-vsctl cmd is not available in the test environment
	require.Nil(t, err)

	err = ConfigInterfaceMirror(false, "close", "iface-id")
	// ovs-vsctl cmd is not available in the test environment
	require.Error(t, err)
}

func (suite *OvnClientTestSuite) testClearPortQosBinding() {
	t := suite.T()
	t.Parallel()

	err := ClearPortQosBinding("iface-id")
	// ovs-vsctl cmd is not available in the test environment
	require.Error(t, err)
}

func (suite *OvnClientTestSuite) testOvsListExternalIDs() {
	t := suite.T()
	t.Parallel()

	ret, err := ListExternalIDs("port")
	// ovs-vsctl cmd is not available in the test environment
	require.Error(t, err)
	require.Empty(t, ret)
}

func (suite *OvnClientTestSuite) testListQosQueueIDs() {
	t := suite.T()
	t.Parallel()

	ret, err := ListQosQueueIDs()
	// ovs-vsctl cmd is not available in the test environment
	require.Error(t, err)
	require.Empty(t, ret)
}
