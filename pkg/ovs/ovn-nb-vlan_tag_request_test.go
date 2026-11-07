package ovs

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *OvnClientTestSuite) testLocalnetVlanTagRequest() {
	t := suite.T()
	t.Parallel()
	nbClient := suite.ovnNBClient
	lsName := "test-localnet-vlan-request-ls"
	require.NoError(t, nbClient.CreateBareLogicalSwitch(lsName))

	for _, operation := range []struct {
		name  string
		apply func(string, int) error
	}{
		{"create", func(name string, id int) error {
			return nbClient.CreateLocalnetLogicalSwitchPort(lsName, name, "provider", "192.168.5.0/24", id)
		}},
		{"set", nbClient.SetLogicalSwitchPortVlanTag},
	} {
		t.Run(operation.name, func(t *testing.T) {
			for index, tc := range []struct {
				name         string
				tag, request *int
				want         int
			}{
				{"repair legacy request", new(200), nil, 200},
				{"repair cleared derived tag", nil, new(200), 200},
				{"repair different request", new(200), new(100), 200},
				{"change vlan", new(100), new(100), 200},
				{"clear tagged vlan", new(200), new(200), 0},
				{"clear outstanding request", nil, new(200), 0},
				{"settled untagged", nil, nil, 0},
				{"settled tagged", new(200), new(200), 200},
			} {
				t.Run(tc.name, func(t *testing.T) {
					name := fmt.Sprintf("%s-%s-%d", lsName, operation.name, index)
					require.NoError(t, nbClient.CreateLocalnetLogicalSwitchPort(lsName, name, "provider", "192.168.5.0/24", 0))
					port, err := nbClient.GetLogicalSwitchPort(name, false)
					require.NoError(t, err)
					port.Tag, port.TagRequest = tc.tag, tc.request
					require.NoError(t, nbClient.UpdateLogicalSwitchPort(port, &port.Tag, &port.TagRequest))

					require.NoError(t, operation.apply(name, tc.want))
					port, err = nbClient.GetLogicalSwitchPort(name, false)
					require.NoError(t, err)
					var want *int
					if tc.want != 0 {
						want = new(tc.want)
					}
					// northd derives tag from tag_request; both must converge together.
					require.Equal(t, want, port.TagRequest)
					require.Equal(t, want, port.Tag)

					require.NoError(t, operation.apply(name, tc.want))
					again, err := nbClient.GetLogicalSwitchPort(name, false)
					require.NoError(t, err)
					require.Equal(t, port, again)
				})
			}
		})
	}
}
