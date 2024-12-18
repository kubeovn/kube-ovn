package ovs

import (
	"fmt"

	"github.com/stretchr/testify/require"
)

func (suite *OvnClientTestSuite) testUpdateOVSVsctlLimiter() {
	t := suite.T()
	t.Parallel()

	UpdateOVSVsctlLimiter(int32(10))
}

func (suite *OvnClientTestSuite) testOvsExec() {
	t := suite.T()
	t.Parallel()

	ret, err := Exec(suite.ovsSocket, "show")
	// ovs-vsctl cmd is not available in the test environment
	require.Error(t, err)
	require.Empty(t, ret)
}

func (suite *OvnClientTestSuite) testOvsCreate() {
	t := suite.T()
	t.Parallel()

	var qosCommandValues []string
	qosCommandValues = append(qosCommandValues, fmt.Sprintf("other_config:latency=%d", 10))
	qosCommandValues = append(qosCommandValues, fmt.Sprintf("other_config:jitter=%d", 10))
	qosCommandValues = append(qosCommandValues, fmt.Sprintf("other_config:limit=%d", 10))
	qosCommandValues = append(qosCommandValues, fmt.Sprintf("other_config:loss=%v", 10))
	ret, err := ovsCreate("qos", qosCommandValues...)
	// ovs-vsctl cmd is not available in the test environment
	require.Error(t, err)
	require.Empty(t, ret)
}

func (suite *OvnClientTestSuite) testOvsDestroy() {
	t := suite.T()
	t.Parallel()

	err := ovsDestroy("qos", "qos-uuid")
	// ovs-vsctl cmd is not available in the test environment
	require.Error(t, err)
}

func (suite *OvnClientTestSuite) testOvsSet() {
	t := suite.T()
	t.Parallel()

	err := ovsSet("port", "port-name", "qos=qos-uuid")
	// ovs-vsctl cmd is not available in the test environment
	require.Error(t, err)
}

func (suite *OvnClientTestSuite) testOvsAdd() {
	t := suite.T()
	t.Parallel()

	err := ovsAdd("port", "port-name", "qos=qos-uuid")
	// ovs-vsctl cmd is not available in the test environment
	require.Error(t, err)
}

func (suite *OvnClientTestSuite) testOvsFind() {
	t := suite.T()
	t.Parallel()

	ret, err := ovsFind("port", "name", "qos=qos-uuid")
	// ovs-vsctl cmd is not available in the test environment
	require.Error(t, err)
	require.Empty(t, ret)
}

func (suite *OvnClientTestSuite) testParseOvsFindOutput() {
	t := suite.T()
	t.Parallel()
	input := `br-int

br-businessnet
`
	ret := parseOvsFindOutput(input)
	require.Len(t, ret, 2)
}

func (suite *OvnClientTestSuite) testOvsClear() {
	t := suite.T()
	t.Parallel()

	err := ovsClear("port", "port-name", "qos")
	// ovs-vsctl cmd is not available in the test environment
	require.Error(t, err)
}

func (suite *OvnClientTestSuite) testOvsGet() {
	t := suite.T()
	t.Parallel()

	ret, err := ovsGet("port", "port-name", "qos", "qos-uuid")
	// ovs-vsctl cmd is not available in the test environment
	require.Error(t, err)
	require.Empty(t, ret)
	ret, err = ovsGet("port", "port-name", "qos", "")
	// ovs-vsctl cmd is not available in the test environment
	require.Error(t, err)
	require.Empty(t, ret)
}

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

func (suite *OvnClientTestSuite) testOvsCleanLostInterface() {
	t := suite.T()
	t.Parallel()

	CleanLostInterface()
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

func (suite *OvnClientTestSuite) testGetResidualInternalPorts() {
	t := suite.T()
	t.Parallel()

	ret := GetResidualInternalPorts()
	// ovs-vsctl cmd is not available in the test environment
	require.Empty(t, ret)
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
