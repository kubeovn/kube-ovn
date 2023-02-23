package ovs

import (
	"testing"

	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func newNat(lrName, natType, externalIP, logicalIP string, options ...func(nat *ovnnb.NAT)) *ovnnb.NAT {
	nat := &ovnnb.NAT{
		UUID:       ovsclient.NamedUUID(),
		Type:       natType,
		ExternalIP: externalIP,
		LogicalIP:  logicalIP,
		ExternalIDs: map[string]string{
			logicalRouterKey: lrName,
		},
	}

	for _, option := range options {
		option(nat)
	}

	return nat
}

func (suite *OvnClientTestSuite) testCreateNats() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-create-nats-lr"
	externalIPs := []string{"192.168.30.254", "192.168.30.253"}
	logicalIPs := []string{"10.250.0.4", "10.250.0.5"}
	nats := make([]*ovnnb.NAT, 0, 5)

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	// snat
	for _, logicalIP := range logicalIPs {
		nat, err := ovnClient.newNat(lrName, "snat", externalIPs[0], logicalIP)
		require.NoError(t, err)

		nats = append(nats, nat)
	}

	// dnat_and_snat
	for _, externalIP := range externalIPs {
		nat, err := ovnClient.newNat(lrName, "dnat_and_snat", externalIP, logicalIPs[0])
		require.NoError(t, err)

		nats = append(nats, nat)
	}

	err = ovnClient.CreateNats(lrName, append(nats, nil)...)
	require.NoError(t, err)

	lr, err := ovnClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)

	// snat
	for _, logicalIP := range logicalIPs {
		nat, err := ovnClient.GetNat(lrName, "snat", externalIPs[0], logicalIP, false)
		require.NoError(t, err)

		require.Contains(t, lr.Nat, nat.UUID)
	}

	// dnat_and_snat
	for _, externalIP := range externalIPs {
		nat, err := ovnClient.GetNat(lrName, "dnat_and_snat", externalIP, logicalIPs[0], false)
		require.NoError(t, err)

		require.Contains(t, lr.Nat, nat.UUID)
	}
}

func (suite *OvnClientTestSuite) testUpdateSnat() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-update-snat-lr"
	externalIP := "192.168.30.254"
	logicalIP := "10.250.0.4"
	natType := ovnnb.NATTypeSNAT

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("create snat", func(t *testing.T) {
		err = ovnClient.UpdateSnat(lrName, externalIP, logicalIP)
		require.NoError(t, err)

		lr, err := ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		nat, err := ovnClient.GetNat(lrName, natType, "", logicalIP, false)
		require.NoError(t, err)

		require.Contains(t, lr.Nat, nat.UUID)
	})

	t.Run("update snat", func(t *testing.T) {
		externalIP := "192.168.30.253"
		err = ovnClient.UpdateSnat(lrName, externalIP, logicalIP)
		require.NoError(t, err)

		nat, err := ovnClient.GetNat(lrName, natType, "", logicalIP, false)
		require.NoError(t, err)
		require.Equal(t, externalIP, nat.ExternalIP)
	})
}

func (suite *OvnClientTestSuite) testUpdateDnatAndSnat() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-update-dnat-and-snat-lr"
	lspName := "test-update-dnat-and-snat-lrp"
	externalIP := "192.168.30.254"
	logicalIP := "10.250.0.4"
	natType := ovnnb.NATTypeDNATAndSNAT
	externalMac := "00:00:00:08:0a:de"

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("create dnat_and_snat", func(t *testing.T) {
		t.Run("distributed gw", func(t *testing.T) {
			err = ovnClient.UpdateDnatAndSnat(lrName, externalIP, logicalIP, lspName, externalMac, kubeovnv1.GWDistributedType)
			require.NoError(t, err)

			lr, err := ovnClient.GetLogicalRouter(lrName, false)
			require.NoError(t, err)

			nat, err := ovnClient.GetNat(lrName, natType, externalIP, "", false)
			require.NoError(t, err)
			require.Equal(t, lspName, *nat.LogicalPort)
			require.Equal(t, externalMac, *nat.ExternalMAC)
			require.Equal(t, "true", nat.Options["stateless"])

			require.Contains(t, lr.Nat, nat.UUID)
		})

		t.Run("centralized gw", func(t *testing.T) {
			externalIP := "192.168.30.250"

			err = ovnClient.UpdateDnatAndSnat(lrName, externalIP, logicalIP, lspName, externalMac, kubeovnv1.GWCentralizedType)
			require.NoError(t, err)

			lr, err := ovnClient.GetLogicalRouter(lrName, false)
			require.NoError(t, err)

			nat, err := ovnClient.GetNat(lrName, natType, externalIP, "", false)
			require.NoError(t, err)
			require.Empty(t, nat.Options["stateless"])

			require.Contains(t, lr.Nat, nat.UUID)
		})
	})

	t.Run("update dnat_and_snat", func(t *testing.T) {
		t.Run("distributed gw", func(t *testing.T) {
			lspName := "test-update-dnat-and-snat-lrp-1"
			externalMac := "00:00:00:08:0a:ff"

			err = ovnClient.UpdateDnatAndSnat(lrName, externalIP, logicalIP, lspName, externalMac, kubeovnv1.GWDistributedType)
			require.NoError(t, err)

			lr, err := ovnClient.GetLogicalRouter(lrName, false)
			require.NoError(t, err)

			nat, err := ovnClient.GetNat(lrName, natType, externalIP, "", false)
			require.NoError(t, err)
			require.Equal(t, lspName, *nat.LogicalPort)
			require.Equal(t, externalMac, *nat.ExternalMAC)
			require.Equal(t, "true", nat.Options["stateless"])

			require.Contains(t, lr.Nat, nat.UUID)
		})
	})
}

func (suite *OvnClientTestSuite) testDeleteNat() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-del-nat-lr"
	externalIP := "192.168.30.254"
	logicalIP := "10.250.0.4"

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	prepareFunc := func() {
		nats := make([]*ovnnb.NAT, 0)

		// create snat rule
		nat, err := ovnClient.newNat(lrName, "snat", externalIP, logicalIP)
		require.NoError(t, err)
		nats = append(nats, nat)

		// create dnat_and_snat rule
		nat, err = ovnClient.newNat(lrName, "dnat_and_snat", externalIP, logicalIP)
		require.NoError(t, err)
		nats = append(nats, nat)

		err = ovnClient.CreateNats(lrName, nats...)
		require.NoError(t, err)
	}

	prepareFunc()

	t.Run("delete snat from logical router", func(t *testing.T) {
		err = ovnClient.DeleteNat(lrName, "snat", externalIP, logicalIP)
		require.NoError(t, err)

		lr, err := ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Nat, 1)

		nat := &ovnnb.NAT{UUID: lr.Nat[0]}
		err = ovnClient.GetEntityInfo(nat)
		require.NoError(t, err)
		require.Equal(t, "dnat_and_snat", nat.Type)
	})

	t.Run("delete dnat_and_snat from logical router", func(t *testing.T) {
		err = ovnClient.DeleteNat(lrName, "dnat_and_snat", externalIP, logicalIP)
		require.NoError(t, err)

		lr, err := ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Empty(t, lr.Nat)
	})
}

func (suite *OvnClientTestSuite) testDeleteNats() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-del-nats-lr"
	externalIPs := []string{"192.168.30.254", "192.168.30.253"}
	logicalIPs := []string{"10.250.0.4", "10.250.0.5"}

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	prepareFunc := func() {
		nats := make([]*ovnnb.NAT, 0)
		// create two snat rule
		for _, logicalIP := range logicalIPs {
			nat, err := ovnClient.newNat(lrName, "snat", externalIPs[0], logicalIP)
			require.NoError(t, err)
			nats = append(nats, nat)
		}

		// create two dnat_and_snat rule
		for _, externalIP := range externalIPs {
			nat, err := ovnClient.newNat(lrName, "dnat_and_snat", externalIP, logicalIPs[0])
			require.NoError(t, err)
			nats = append(nats, nat)
		}

		err = ovnClient.CreateNats(lrName, nats...)
		require.NoError(t, err)
	}

	t.Run("delete nats from logical router", func(t *testing.T) {
		prepareFunc()

		lr, err := ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Nat, 4)

		err = ovnClient.DeleteNats(lrName, "", "")
		require.NoError(t, err)

		lr, err = ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Empty(t, lr.Nat)
	})

	t.Run("delete snat or dnat_and_snat from logical router", func(t *testing.T) {
		prepareFunc()

		lr, err := ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Nat, 4)

		err = ovnClient.DeleteNats(lrName, "snat", "")
		require.NoError(t, err)

		lr, err = ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Nat, 2)

		err = ovnClient.DeleteNats(lrName, "dnat_and_snat", "")
		require.NoError(t, err)

		lr, err = ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Empty(t, lr.Nat)
	})

	t.Run("delete nat with same logical ip from logical router", func(t *testing.T) {
		prepareFunc()

		lr, err := ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Nat, 4)

		err = ovnClient.DeleteNats(lrName, "", logicalIPs[0])
		require.NoError(t, err)

		lr, err = ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Nat, 1)

		// clear
		err = ovnClient.DeleteNats(lrName, "", "")
		require.NoError(t, err)

		lr, err = ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Empty(t, lr.Nat)
	})

	t.Run("delete snat with same logical ip from logical router", func(t *testing.T) {
		prepareFunc()

		lr, err := ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Nat, 4)

		err = ovnClient.DeleteNats(lrName, "snat", logicalIPs[0])
		require.NoError(t, err)

		lr, err = ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Nat, 3)

		// clear
		err = ovnClient.DeleteNats(lrName, "", "")
		require.NoError(t, err)

		lr, err = ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Empty(t, lr.Nat)
	})

	t.Run("delete dnat_and_snat with same logical ip from logical router", func(t *testing.T) {
		prepareFunc()

		lr, err := ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Nat, 4)

		err = ovnClient.DeleteNats(lrName, "dnat_and_snat", logicalIPs[0])
		require.NoError(t, err)

		lr, err = ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Nat, 2)
	})
}

func (suite *OvnClientTestSuite) testGetNat() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test_get_nat_lr"

	t.Run("snat", func(t *testing.T) {
		t.Parallel()
		natType := "snat"
		externalIP := "192.168.30.254"
		logicalIP := "10.250.0.4"

		err := ovnClient.CreateBareNat(lrName, natType, externalIP, logicalIP)
		require.NoError(t, err)

		t.Run("found nat", func(t *testing.T) {
			_, err := ovnClient.GetNat(lrName, natType, externalIP, logicalIP, false)
			require.NoError(t, err)
		})

		t.Run("logical ip is different", func(t *testing.T) {
			_, err := ovnClient.GetNat(lrName, natType, externalIP, "10.250.0.10", false)
			require.ErrorContains(t, err, "not found")
		})

		t.Run("logical router name is different", func(t *testing.T) {
			_, err := ovnClient.GetNat(lrName+"x", natType, externalIP, logicalIP, false)
			require.ErrorContains(t, err, "not found")
		})
	})

	t.Run("dnat_and_snat", func(t *testing.T) {
		t.Parallel()
		natType := "dnat_and_snat"
		externalIP := "192.168.30.254"
		logicalIP := "10.250.0.4"

		err := ovnClient.CreateBareNat(lrName, natType, externalIP, logicalIP)
		require.NoError(t, err)

		t.Run("found nat", func(t *testing.T) {
			_, err := ovnClient.GetNat(lrName, natType, externalIP, logicalIP, false)
			require.NoError(t, err)
		})

		t.Run("external ip is different", func(t *testing.T) {
			_, err := ovnClient.GetNat(lrName, natType, "192.168.30.255", logicalIP, false)
			require.ErrorContains(t, err, "not found")
		})
	})
}

func (suite *OvnClientTestSuite) test_newNat() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-new-nat-lr"
	natType := "snat"
	externalIP := "192.168.30.254"
	logicalIP := "10.250.0.4"

	t.Run("new snat rule", func(t *testing.T) {
		t.Parallel()

		expect := &ovnnb.NAT{
			Type:       natType,
			ExternalIP: externalIP,
			LogicalIP:  logicalIP,
			ExternalIDs: map[string]string{
				logicalRouterKey: lrName,
			},
		}

		nat, err := ovnClient.newNat(lrName, natType, externalIP, logicalIP)
		require.NoError(t, err)
		expect.UUID = nat.UUID
		require.Equal(t, expect, nat)
	})

	t.Run("new stateless dnat_and_snat rule", func(t *testing.T) {
		t.Parallel()

		lspName := "test-new-nat-lsp"
		externalMac := "00:00:00:f7:82:60"
		natType := "dnat_and_snat"

		expect := &ovnnb.NAT{
			Type:       natType,
			ExternalIP: externalIP,
			LogicalIP:  logicalIP,
			ExternalIDs: map[string]string{
				logicalRouterKey: lrName,
			},
			LogicalPort: &lspName,
			ExternalMAC: &externalMac,
		}

		options := func(nat *ovnnb.NAT) {
			nat.LogicalPort = &lspName
			nat.ExternalMAC = &externalMac
		}

		nat, err := ovnClient.newNat(lrName, natType, externalIP, logicalIP, options)
		require.NoError(t, err)
		expect.UUID = nat.UUID
		require.Equal(t, expect, nat)
	})
}

func (suite *OvnClientTestSuite) test_natFilter() {
	t := suite.T()
	t.Parallel()

	lrName := "test-filter-nat-lr"
	externalIPs := []string{"192.168.30.254", "192.168.30.253"}
	logicalIPs := []string{"10.250.0.4", "10.250.0.5"}

	nats := make([]*ovnnb.NAT, 0)
	// create two snat rule
	for _, logicalIP := range logicalIPs {
		nat := newNat(lrName, "snat", externalIPs[0], logicalIP)
		nats = append(nats, nat)
	}

	// create two dnat_and_snat rule
	for _, externalIP := range externalIPs {
		nat := newNat(lrName, "dnat_and_snat", externalIP, logicalIPs[0])
		nats = append(nats, nat)
	}

	// create three snat rule with other acl parent key
	for i := 0; i < 3; i++ {
		nat := newNat(lrName, "snat", externalIPs[0], logicalIPs[0])
		nat.ExternalIDs[logicalRouterKey] = lrName + "-test"
		nats = append(nats, nat)

	}

	t.Run("include all nat", func(t *testing.T) {
		filterFunc := natFilter("", "", nil)
		count := 0
		for _, nat := range nats {
			if filterFunc(nat) {
				count++
			}
		}
		require.Equal(t, count, 7)
	})

	t.Run("include all nat with external ids", func(t *testing.T) {
		filterFunc := natFilter("", "", map[string]string{logicalRouterKey: lrName})
		count := 0
		for _, nat := range nats {
			if filterFunc(nat) {
				count++
			}
		}
		require.Equal(t, count, 4)
	})

	t.Run("include snat", func(t *testing.T) {
		filterFunc := natFilter("snat", "", nil)
		count := 0
		for _, nat := range nats {
			if filterFunc(nat) {
				count++
			}
		}
		require.Equal(t, count, 5)
	})

	t.Run("include snat with external ids", func(t *testing.T) {
		filterFunc := natFilter("snat", "", map[string]string{logicalRouterKey: lrName})
		count := 0
		for _, nat := range nats {
			if filterFunc(nat) {
				count++
			}
		}
		require.Equal(t, count, 2)
	})

	t.Run("include dnat_and_snat", func(t *testing.T) {
		filterFunc := natFilter("dnat_and_snat", "", nil)
		count := 0
		for _, nat := range nats {
			if filterFunc(nat) {
				count++
			}
		}
		require.Equal(t, count, 2)
	})

	t.Run("include dnat_and_snat with external ids", func(t *testing.T) {
		filterFunc := natFilter("dnat_and_snat", "", map[string]string{logicalRouterKey: lrName})
		count := 0
		for _, nat := range nats {
			if filterFunc(nat) {
				count++
			}
		}
		require.Equal(t, count, 2)
	})

	t.Run("include all nat with same logical ip", func(t *testing.T) {
		filterFunc := natFilter("", logicalIPs[0], map[string]string{logicalRouterKey: lrName})
		count := 0
		for _, nat := range nats {
			if filterFunc(nat) {
				count++
			}
		}
		require.Equal(t, count, 3)
	})

	t.Run("include snat with same logical ip", func(t *testing.T) {
		filterFunc := natFilter("snat", logicalIPs[0], map[string]string{logicalRouterKey: lrName})
		count := 0
		for _, nat := range nats {
			if filterFunc(nat) {
				count++
			}
		}
		require.Equal(t, count, 1)
	})

	t.Run("include dnat_and_snat with same logical ip", func(t *testing.T) {
		filterFunc := natFilter("dnat_and_snat", logicalIPs[0], map[string]string{logicalRouterKey: lrName})
		count := 0
		for _, nat := range nats {
			if filterFunc(nat) {
				count++
			}
		}
		require.Equal(t, count, 2)
	})

	t.Run("result should exclude nat when externalIDs's length is not equal", func(t *testing.T) {
		t.Parallel()

		nat := newNat(lrName, "snat", externalIPs[0], logicalIPs[0])
		filterFunc := natFilter("", "", map[string]string{
			logicalRouterKey: lrName,
			"key":            "value",
		})

		require.False(t, filterFunc(nat))
	})
}
