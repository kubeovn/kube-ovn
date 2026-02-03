package ovs

import (
	"testing"

	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func newNat(natType, externalIP, logicalIP string, options ...func(nat *ovnnb.NAT)) *ovnnb.NAT {
	nat := &ovnnb.NAT{
		UUID:       ovsclient.NamedUUID(),
		Type:       natType,
		ExternalIP: externalIP,
		LogicalIP:  logicalIP,
	}

	for _, option := range options {
		option(nat)
	}

	return nat
}

func (suite *OvnClientTestSuite) testCreateNats() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-create-nats-lr"
	externalIPs := []string{"192.168.30.254", "192.168.30.253"}
	logicalIPs := []string{"10.250.0.4", "10.250.0.5"}
	nats := make([]*ovnnb.NAT, 0, 5)

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	// snat
	for _, logicalIP := range logicalIPs {
		nat, err := nbClient.newNat(lrName, "snat", externalIPs[0], logicalIP, "", "")
		require.NoError(t, err)

		nats = append(nats, nat)
	}

	// dnat_and_snat
	for _, externalIP := range externalIPs {
		nat, err := nbClient.newNat(lrName, "dnat_and_snat", externalIP, logicalIPs[0], "", "")
		require.NoError(t, err)

		nats = append(nats, nat)
	}

	err = nbClient.CreateNats(lrName, append(nats, nil)...)
	require.NoError(t, err)

	lr, err := nbClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)

	// snat
	for _, logicalIP := range logicalIPs {
		nat, err := nbClient.GetNat(lrName, "snat", externalIPs[0], logicalIP, false)
		require.NoError(t, err)

		require.Contains(t, lr.Nat, nat.UUID)
	}

	// dnat_and_snat
	for _, externalIP := range externalIPs {
		nat, err := nbClient.GetNat(lrName, "dnat_and_snat", externalIP, logicalIPs[0], false)
		require.NoError(t, err)

		require.Contains(t, lr.Nat, nat.UUID)
	}

	// invalid nat
	nilNats := make([]*ovnnb.NAT, 0)
	err = nbClient.CreateNats(lrName, nilNats...)
	require.Error(t, err)

	// no nats
	err = nbClient.CreateNats(lrName)
	require.ErrorContains(t, err, "nats is empty")

	// failed client to create nats
	failedNbClient := suite.failedOvnNBClient
	err = failedNbClient.CreateNats(lrName, nats...)
	require.Error(t, err)
}

func (suite *OvnClientTestSuite) testUpdateSnat() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient
	lrName := "test-update-snat-lr"
	externalIP := "192.168.30.254"
	logicalIP := "10.250.0.4"
	natType := ovnnb.NATTypeSNAT

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("create snat", func(t *testing.T) {
		err = nbClient.UpdateSnat(lrName, externalIP, logicalIP)
		require.NoError(t, err)

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		nat, err := nbClient.GetNat(lrName, natType, "", logicalIP, false)
		require.NoError(t, err)

		require.Contains(t, lr.Nat, nat.UUID)
	})

	t.Run("update snat", func(t *testing.T) {
		externalIP := "192.168.30.253"
		err = nbClient.UpdateSnat(lrName, externalIP, logicalIP)
		require.NoError(t, err)

		nat, err := nbClient.GetNat(lrName, natType, "", logicalIP, false)
		require.NoError(t, err)
		require.Equal(t, externalIP, nat.ExternalIP)
	})

	t.Run("failed client update snat", func(t *testing.T) {
		err = failedNbClient.UpdateSnat(lrName, externalIP, logicalIP)
		require.Error(t, err)
	})

	t.Run("update invalid dnat with empty external ip", func(t *testing.T) {
		err = nbClient.UpdateSnat(lrName, "", logicalIP)
		require.Error(t, err)
	})

	t.Run("update invalid dnat with empty logical ip", func(t *testing.T) {
		err = nbClient.UpdateSnat(lrName, externalIP, "")
		require.Error(t, err)
	})

	t.Run("update snat with empty lrName", func(t *testing.T) {
		externalIP := "192.168.30.253"
		err = nbClient.UpdateSnat("", externalIP, logicalIP)
		require.ErrorContains(t, err, "the logical router name is required")
	})
}

func (suite *OvnClientTestSuite) testUpdateDnatAndSnat() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient
	lrName := "test-update-dnat-and-snat-lr"
	lspName := "test-update-dnat-and-snat-lrp"
	externalIP := "192.168.30.214"
	logicalIP := "10.250.0.14"
	natType := ovnnb.NATTypeDNATAndSNAT
	externalMac := "00:00:00:08:0a:de"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("create dnat_and_snat", func(t *testing.T) {
		t.Run("distributed gw", func(t *testing.T) {
			err = nbClient.UpdateDnatAndSnat(lrName, externalIP, logicalIP, lspName, externalMac, kubeovnv1.GWDistributedType)
			require.NoError(t, err)

			lr, err := nbClient.GetLogicalRouter(lrName, false)
			require.NoError(t, err)

			nat, err := nbClient.GetNat(lrName, natType, externalIP, "", false)
			require.NoError(t, err)
			require.Equal(t, lspName, *nat.LogicalPort)
			require.Equal(t, externalMac, *nat.ExternalMAC)
			require.Equal(t, "true", nat.Options["stateless"])

			require.Contains(t, lr.Nat, nat.UUID)
		})

		t.Run("centralized gw", func(t *testing.T) {
			externalIP := "192.168.30.250"

			err = nbClient.UpdateDnatAndSnat(lrName, externalIP, logicalIP, lspName, externalMac, kubeovnv1.GWCentralizedType)
			require.NoError(t, err)

			lr, err := nbClient.GetLogicalRouter(lrName, false)
			require.NoError(t, err)

			nat, err := nbClient.GetNat(lrName, natType, externalIP, "", false)
			require.NoError(t, err)
			require.Empty(t, nat.Options["stateless"])

			require.Contains(t, lr.Nat, nat.UUID)
		})

		t.Run("update existing dnat and snat with centralized gw", func(t *testing.T) {
			externalIP := "192.168.30.250"

			err = nbClient.UpdateDnatAndSnat(lrName, externalIP, logicalIP, lspName, externalMac, kubeovnv1.GWCentralizedType)
			require.NoError(t, err)
		})
	})

	t.Run("update dnat_and_snat in distributed gw case", func(t *testing.T) {
		lspName := "test-update-dnat-and-snat-lrp-1"
		externalMac := "00:00:00:08:0a:ff"

		err = nbClient.UpdateDnatAndSnat(lrName, externalIP, logicalIP, lspName, externalMac, kubeovnv1.GWDistributedType)
		require.NoError(t, err)

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		nat, err := nbClient.GetNat(lrName, natType, externalIP, "", false)
		require.NoError(t, err)
		require.Equal(t, lspName, *nat.LogicalPort)
		require.Equal(t, externalMac, *nat.ExternalMAC)
		require.Equal(t, "true", nat.Options["stateless"])

		require.Contains(t, lr.Nat, nat.UUID)
	})

	t.Run("fail dnat_and_snat", func(t *testing.T) {
		lspName := "test-update-dnat-and-snat-lrp-1"
		externalMac := "00:00:00:08:0a:ff"
		err = failedNbClient.UpdateDnatAndSnat(lrName, externalIP, logicalIP, lspName, externalMac, kubeovnv1.GWDistributedType)
		require.Error(t, err)

		err = nbClient.UpdateDnatAndSnat(lrName, "", logicalIP, lspName, externalMac, kubeovnv1.GWDistributedType)
		require.Error(t, err)

		err = nbClient.UpdateDnatAndSnat(lrName, externalIP, "", lspName, externalMac, kubeovnv1.GWDistributedType)
		require.Error(t, err)
	})
}

func (suite *OvnClientTestSuite) testUpdateNat() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient

	lrName := "test-update-nat-lr"
	externalIP := "192.168.30.254"
	logicalIP := "10.250.0.4"
	natType := "snat"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)
	err = nbClient.UpdateSnat(lrName, externalIP, logicalIP)
	require.NoError(t, err)
	nat, err := nbClient.GetNat(lrName, natType, "", logicalIP, false)
	require.NoError(t, err)

	t.Run("update nat", func(t *testing.T) {
		externalMac := "00:00:00:08:0a:de"
		// field update
		nat.ExternalMAC = &externalMac
		err = nbClient.UpdateNat(nat, &nat.ExternalMAC)
		require.Nil(t, err)
		// not support
		err = nbClient.UpdateNat(nat, &externalMac)
		require.Error(t, err)
	})

	t.Run("failed to update nil nat", func(t *testing.T) {
		lspName := "test-update-nat-lsp"
		externalMac := "00:00:00:08:0a:de"
		err = failedNbClient.UpdateNat(nil, &lspName, &externalMac)
		require.Error(t, err)
	})

	t.Run("failed to update nat", func(t *testing.T) {
		lspName := "test-update-nat-lsp"
		externalMac := "00:00:00:08:0a:de"
		err = failedNbClient.UpdateNat(nat, &lspName, &externalMac)
		require.Error(t, err)

		t.Run("empty lrName", func(t *testing.T) {
			lspName := "test-update-dnat-and-snat-lrp-1"
			externalMac := "00:00:00:08:0a:ff"

			err = nbClient.UpdateDnatAndSnat("", externalIP, logicalIP, lspName, externalMac, kubeovnv1.GWDistributedType)
			require.ErrorContains(t, err, "the logical router name is required")
		})
	})

	t.Run("update nil nat", func(t *testing.T) {
		err := nbClient.UpdateNat(nil)
		require.ErrorContains(t, err, "nat is nil")
	})

	t.Run("update nat fields", func(t *testing.T) {
		err := nbClient.AddNat(lrName, natType, externalIP, logicalIP, "", "", nil)
		require.NoError(t, err)

		nat, err := nbClient.GetNat(lrName, natType, externalIP, logicalIP, false)
		require.NoError(t, err)
		require.NotNil(t, nat)

		newExternalIP := "192.168.30.253"
		nat.ExternalIP = newExternalIP
		err = nbClient.UpdateNat(nat, &nat.ExternalIP)
		require.NoError(t, err)

		updatedNat, err := nbClient.GetNat(lrName, natType, newExternalIP, logicalIP, false)
		require.NoError(t, err)
		require.Equal(t, newExternalIP, updatedNat.ExternalIP)
	})
}

func (suite *OvnClientTestSuite) testDeleteNat() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient
	lrName := "test-del-nat-lr"
	externalIP := "192.168.30.254"
	logicalIP := "10.250.0.4"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	prepareFunc := func() {
		nats := make([]*ovnnb.NAT, 0)

		// create snat rule
		nat, err := nbClient.newNat(lrName, "snat", externalIP, logicalIP, "", "")
		require.NoError(t, err)
		nats = append(nats, nat)

		// create dnat_and_snat rule
		nat, err = nbClient.newNat(lrName, "dnat_and_snat", externalIP, logicalIP, "", "")
		require.NoError(t, err)
		nats = append(nats, nat)

		err = nbClient.CreateNats(lrName, nats...)
		require.NoError(t, err)
	}

	prepareFunc()

	t.Run("delete snat from logical router", func(t *testing.T) {
		err = nbClient.DeleteNat(lrName, "snat", externalIP, logicalIP)
		require.NoError(t, err)

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Nat, 1)

		nat := &ovnnb.NAT{UUID: lr.Nat[0]}
		err = nbClient.GetEntityInfo(nat)
		require.NoError(t, err)
		require.Equal(t, "dnat_and_snat", nat.Type)
	})

	t.Run("delete dnat_and_snat from logical router", func(t *testing.T) {
		err = nbClient.DeleteNat(lrName, "dnat_and_snat", externalIP, logicalIP)
		require.NoError(t, err)

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Empty(t, lr.Nat)
	})

	t.Run("failed client delete dnat_and_snat from logical router", func(t *testing.T) {
		err = failedNbClient.DeleteNat(lrName, "dnat_and_snat", externalIP, logicalIP)
		require.Error(t, err)
	})

	t.Run("failed client delete invalid nat from logical router", func(t *testing.T) {
		// invalid dnat
		err = failedNbClient.DeleteNat(lrName, "dnat", externalIP, logicalIP)
		require.Error(t, err)
		// empty
		err = failedNbClient.DeleteNat(lrName, "dnat_and_snat", "", logicalIP)
		require.Error(t, err)
		err = failedNbClient.DeleteNat(lrName, "dnat_and_snat", externalIP, "")
		require.Error(t, err)
		err = failedNbClient.DeleteNat("", "dnat_and_snat", externalIP, logicalIP)
		require.Error(t, err)
	})

	t.Run("delete nat with empty logical router", func(t *testing.T) {
		err := nbClient.DeleteNat("", "dnat_and_snat", "", "")
		require.ErrorContains(t, err, "the logical router name is required")
	})
}

func (suite *OvnClientTestSuite) testDeleteNats() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient
	lrName := "test-del-nats-lr"
	externalIPs := []string{"192.168.30.254", "192.168.30.253"}
	logicalIPs := []string{"10.250.0.4", "10.250.0.5"}

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	prepareFunc := func() {
		nats := make([]*ovnnb.NAT, 0)
		// create two snat rule
		for _, logicalIP := range logicalIPs {
			nat, err := nbClient.newNat(lrName, "snat", externalIPs[0], logicalIP, "", "")
			require.NoError(t, err)
			nats = append(nats, nat)
		}

		// create two dnat_and_snat rule
		for _, externalIP := range externalIPs {
			nat, err := nbClient.newNat(lrName, "dnat_and_snat", externalIP, logicalIPs[0], "", "")
			require.NoError(t, err)
			nats = append(nats, nat)
		}

		err = nbClient.CreateNats(lrName, nats...)
		require.NoError(t, err)
	}

	t.Run("delete nats from logical router", func(t *testing.T) {
		prepareFunc()

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Nat, 4)

		err = nbClient.DeleteNats(lrName, "", "")
		require.NoError(t, err)

		lr, err = nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Empty(t, lr.Nat)
	})

	t.Run("delete snat or dnat_and_snat from logical router", func(t *testing.T) {
		prepareFunc()

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Nat, 4)

		err = nbClient.DeleteNats(lrName, "snat", "")
		require.NoError(t, err)

		lr, err = nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Nat, 2)

		err = nbClient.DeleteNats(lrName, "dnat_and_snat", "")
		require.NoError(t, err)

		lr, err = nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Empty(t, lr.Nat)
	})

	t.Run("delete nat with same logical ip from logical router", func(t *testing.T) {
		prepareFunc()

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Nat, 4)

		err = nbClient.DeleteNats(lrName, "", logicalIPs[0])
		require.NoError(t, err)

		lr, err = nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Nat, 1)

		// clear
		err = nbClient.DeleteNats(lrName, "", "")
		require.NoError(t, err)

		lr, err = nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Empty(t, lr.Nat)
	})

	t.Run("delete snat with same logical ip from logical router", func(t *testing.T) {
		prepareFunc()

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Nat, 4)

		err = nbClient.DeleteNats(lrName, "snat", logicalIPs[0])
		require.NoError(t, err)

		lr, err = nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Nat, 3)

		// clear
		err = nbClient.DeleteNats(lrName, "", "")
		require.NoError(t, err)

		lr, err = nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Empty(t, lr.Nat)
	})

	t.Run("delete dnat_and_snat with same logical ip from logical router", func(t *testing.T) {
		prepareFunc()

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Nat, 4)

		err = nbClient.DeleteNats(lrName, "dnat_and_snat", logicalIPs[0])
		require.NoError(t, err)

		lr, err = nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Nat, 2)
	})

	t.Run("failed client delete snat or dnat_and_snat from logical router", func(t *testing.T) {
		err = failedNbClient.DeleteNats(lrName, "snat", "")
		require.Error(t, err)
	})

	t.Run("failed client delete invalid dnat from logical router", func(t *testing.T) {
		err = failedNbClient.DeleteNats(lrName, "dnat", "")
		require.Error(t, err)
	})

	t.Run("delete nat with non-exist logical router", func(t *testing.T) {
		err := nbClient.DeleteNats("non-exist-lrName", "dnat_and_snat", logicalIPs[0])
		require.ErrorContains(t, err, "not found logical router")
	})
}

func (suite *OvnClientTestSuite) testGetNat() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient

	lrName := "test_get_nat_lr"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("snat", func(t *testing.T) {
		t.Parallel()
		natType := "snat"
		externalIP := "192.168.30.254"
		logicalIP := "10.250.0.4"

		err := nbClient.AddNat(lrName, natType, externalIP, logicalIP, "", "", nil)
		require.NoError(t, err)

		t.Run("found nat", func(t *testing.T) {
			_, err := nbClient.GetNat(lrName, natType, externalIP, logicalIP, false)
			require.NoError(t, err)
		})

		t.Run("logical ip is different", func(t *testing.T) {
			_, err := nbClient.GetNat(lrName, natType, externalIP, "10.250.0.171", false)
			require.ErrorContains(t, err, "not found")
		})

		t.Run("logical router name is different", func(t *testing.T) {
			_, err := nbClient.GetNat(lrName+"x", natType, externalIP, logicalIP, false)
			require.ErrorContains(t, err, "not found")
		})
	})

	t.Run("dnat_and_snat", func(t *testing.T) {
		t.Parallel()
		natType := "dnat_and_snat"
		externalIP := "192.168.30.254"
		logicalIP := "10.250.0.4"

		err := nbClient.AddNat(lrName, natType, externalIP, logicalIP, "", "", nil)
		require.NoError(t, err)

		t.Run("found nat", func(t *testing.T) {
			_, err := nbClient.GetNat(lrName, natType, externalIP, logicalIP, false)
			require.NoError(t, err)
		})

		t.Run("external ip is different", func(t *testing.T) {
			_, err := nbClient.GetNat(lrName, natType, "192.168.30.255", logicalIP, false)
			require.ErrorContains(t, err, "not found")
		})
	})

	t.Run("failed to add invalid dnat", func(t *testing.T) {
		t.Parallel()
		natType := "dnat"
		externalIP := "192.168.30.254"
		logicalIP := "10.250.0.4"
		err := nbClient.AddNat(lrName, natType, externalIP, logicalIP, "", "", nil)
		require.Error(t, err)
	})

	t.Run("add snat with options", func(t *testing.T) {
		t.Parallel()
		natType := "snat"
		externalIP := "192.168.30.250"
		logicalIP := "10.250.0.10"
		options := map[string]string{"k1": "v1"}
		err := nbClient.AddNat(lrName, natType, externalIP, logicalIP, "", "", options)
		require.Nil(t, err)
	})

	t.Run("failed client get snat", func(t *testing.T) {
		t.Parallel()
		natType := "snat"
		externalIP := "192.168.30.254"
		logicalIP := "10.250.0.4"

		err := failedNbClient.AddNat(lrName, natType, externalIP, logicalIP, "", "", nil)
		require.Error(t, err)

		_, err = failedNbClient.GetNat(lrName, natType, externalIP, logicalIP, false)
		require.Error(t, err)
	})
}

func (suite *OvnClientTestSuite) testNewNat() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient

	lrName := "test-new-nat-lr"
	natType := "snat"
	externalIP := "192.168.30.254"
	logicalIP := "10.250.0.4"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("new snat rule", func(t *testing.T) {
		t.Parallel()

		expect := &ovnnb.NAT{
			Type:       natType,
			ExternalIP: externalIP,
			LogicalIP:  logicalIP,
		}

		nat, err := nbClient.newNat(lrName, natType, externalIP, logicalIP, "", "")
		require.NoError(t, err)
		expect.UUID = nat.UUID
		require.Equal(t, expect, nat)

		// newNAT for existing nat
		err = nbClient.CreateNats(lrName, nat)
		require.NoError(t, err)
		nat, err = nbClient.newNat(lrName, natType, externalIP, logicalIP, "", "")
		require.NoError(t, err)
		require.Nil(t, nat)
	})

	t.Run("fail to new snat rule", func(t *testing.T) {
		t.Parallel()

		// failed client new nat
		_, err := failedNbClient.newNat(lrName, natType, externalIP, logicalIP, "", "")
		require.Error(t, err)
		// invalid nat type
		natType := "dnat"
		_, err = failedNbClient.newNat(lrName, natType, externalIP, logicalIP, "", "")
		require.Error(t, err)
		// empty
		_, err = failedNbClient.newNat("", natType, externalIP, logicalIP, "", "")
		require.Error(t, err)
		_, err = failedNbClient.newNat(lrName, "", externalIP, logicalIP, "", "")
		require.Error(t, err)
		_, err = failedNbClient.newNat(lrName, natType, "", logicalIP, "", "")
		require.Error(t, err)
		_, err = failedNbClient.newNat(lrName, natType, externalIP, "", "", "")
		require.Error(t, err)
	})

	t.Run("fail to new snat rule", func(t *testing.T) {
		t.Parallel()

		// failed client new nat
		_, err := failedNbClient.newNat(lrName, natType, externalIP, logicalIP, "", "")
		require.Error(t, err)
		// invalid nat type
		natType := "dnat"
		_, err = failedNbClient.newNat(lrName, natType, externalIP, logicalIP, "", "")
		require.Error(t, err)
		// empty
		_, err = failedNbClient.newNat("", natType, externalIP, logicalIP, "", "")
		require.Error(t, err)
		_, err = failedNbClient.newNat(lrName, "", externalIP, logicalIP, "", "")
		require.Error(t, err)
		_, err = failedNbClient.newNat(lrName, natType, "", logicalIP, "", "")
		require.Error(t, err)
		_, err = failedNbClient.newNat(lrName, natType, externalIP, "", "", "")
		require.Error(t, err)
	})

	t.Run("new stateless dnat_and_snat rule", func(t *testing.T) {
		t.Parallel()

		lspName := "test-new-nat-lsp"
		externalMac := "00:00:00:f7:82:60"
		natType := "dnat_and_snat"

		expect := &ovnnb.NAT{
			Type:        natType,
			ExternalIP:  externalIP,
			LogicalIP:   logicalIP,
			LogicalPort: &lspName,
			ExternalMAC: &externalMac,
		}

		options := func(nat *ovnnb.NAT) {
			nat.LogicalPort = &lspName
			nat.ExternalMAC = &externalMac
		}

		nat, err := nbClient.newNat(lrName, natType, externalIP, logicalIP, "", "", options)
		require.NoError(t, err)
		expect.UUID = nat.UUID
		require.Equal(t, expect, nat)
	})

	t.Run("natType ovnnb.NATTypeDNAT", func(t *testing.T) {
		t.Parallel()
		nat, err := nbClient.newNat(lrName, ovnnb.NATTypeDNAT, externalIP, logicalIP, "", "")
		require.ErrorContains(t, err, "does not support dnat for now")
		require.Nil(t, nat)
	})

	t.Run("natType empty", func(t *testing.T) {
		t.Parallel()
		nat, err := nbClient.newNat(lrName, "", externalIP, logicalIP, "", "")
		require.ErrorContains(t, err, "nat type must be one of [ snat, dnat_and_snat ]")
		require.Nil(t, nat)
	})

	t.Run("natType ovnnb.NATTypeSNAT with empty logicalIP", func(t *testing.T) {
		t.Parallel()
		nat, err := nbClient.newNat(lrName, ovnnb.NATTypeSNAT, externalIP, "", "", "")
		require.ErrorContains(t, err, "logical ip is required")
		require.Nil(t, nat)
	})

	t.Run("natType ovnnb.NATTypeDNATAndSNAT with empty externalIP", func(t *testing.T) {
		t.Parallel()
		nat, err := nbClient.newNat(lrName, ovnnb.NATTypeDNATAndSNAT, "", logicalIP, "", "")
		require.ErrorContains(t, err, "external ip is required")
		require.Nil(t, nat)
	})
}

func (suite *OvnClientTestSuite) testNatFilter() {
	t := suite.T()
	t.Parallel()

	externalIPs := []string{"192.168.30.254", "192.168.30.253"}
	logicalIPs := []string{"10.250.0.4", "10.250.0.5"}

	nats := make([]*ovnnb.NAT, 0)
	// create two snat rule
	for _, logicalIP := range logicalIPs {
		nat := newNat("snat", externalIPs[0], logicalIP)
		nat.ExternalIDs = map[string]string{"k1": "v1"}
		nats = append(nats, nat)
	}

	// create two dnat_and_snat rule
	for _, externalIP := range externalIPs {
		nat := newNat("dnat_and_snat", externalIP, logicalIPs[0])
		nat.ExternalIDs = map[string]string{"k1": "v1"}
		nats = append(nats, nat)
	}

	// create three snat rule with different external-ids
	for range 3 {
		nat := newNat("snat", externalIPs[0], logicalIPs[0])
		nat.ExternalIDs = map[string]string{"k1": "v2"}
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
		filterFunc := natFilter("", "", map[string]string{"k1": "v1"})
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
		filterFunc := natFilter("snat", "", map[string]string{"k1": "v1"})
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
		filterFunc := natFilter("dnat_and_snat", "", map[string]string{"k1": "v1"})
		count := 0
		for _, nat := range nats {
			if filterFunc(nat) {
				count++
			}
		}
		require.Equal(t, count, 2)
	})

	t.Run("include all nat with same logical ip", func(t *testing.T) {
		filterFunc := natFilter("", logicalIPs[0], map[string]string{"k1": "v1"})
		count := 0
		for _, nat := range nats {
			if filterFunc(nat) {
				count++
			}
		}
		require.Equal(t, count, 3)
	})

	t.Run("include snat with same logical ip", func(t *testing.T) {
		filterFunc := natFilter("snat", logicalIPs[0], map[string]string{"k1": "v1"})
		count := 0
		for _, nat := range nats {
			if filterFunc(nat) {
				count++
			}
		}
		require.Equal(t, count, 1)
	})

	t.Run("include dnat_and_snat with same logical ip", func(t *testing.T) {
		filterFunc := natFilter("dnat_and_snat", logicalIPs[0], map[string]string{"k1": "v1"})
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

		nat := newNat("snat", externalIPs[0], logicalIPs[0])
		filterFunc := natFilter("", "", map[string]string{
			"k1":  "v1",
			"key": "value",
		})

		require.False(t, filterFunc(nat))
	})

	t.Run("exclude nat with empty external id value", func(t *testing.T) {
		filterFunc := natFilter("", "", map[string]string{"k1": ""})
		count := 0
		for _, nat := range nats {
			if filterFunc(nat) {
				count++
			}
		}
		require.Equal(t, count, 7)
	})
}

func (suite *OvnClientTestSuite) testAddNat() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-add-nat-lport-mac-lr"
	natType := "dnat_and_snat"
	externalIP := "192.168.30.254"
	logicalIP := "10.250.0.4"
	logicalPort := "test-logical-port"
	externalMac := "00:00:00:f7:82:60"
	options := map[string]string{"staleless": "true"}

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = nbClient.CreateLogicalRouter(lrName + "1")
	require.NoError(t, err)

	t.Run("add nat with logical port and mac", func(t *testing.T) {
		err := nbClient.AddNat(lrName, natType, externalIP, logicalIP, externalMac, logicalPort, nil)
		require.NoError(t, err)

		nat, err := nbClient.GetNat(lrName, natType, externalIP, logicalIP, false)
		require.NoError(t, err)
		require.Equal(t, logicalPort, *nat.LogicalPort)
		require.Equal(t, externalMac, *nat.ExternalMAC)
	})

	t.Run("add nat with options", func(t *testing.T) {
		err := nbClient.AddNat(lrName+"1", natType, externalIP, logicalIP, externalMac, logicalPort, options)
		require.NoError(t, err)

		nat, err := nbClient.GetNat(lrName+"1", natType, externalIP, logicalIP, false)
		require.NoError(t, err)
		require.Equal(t, options, nat.Options)
	})

	t.Run("add nat with empty lrName", func(t *testing.T) {
		err := nbClient.AddNat("", natType, externalIP, logicalIP, externalMac, logicalPort, options)
		require.ErrorContains(t, err, "the logical router name is required")
	})
}

func (suite *OvnClientTestSuite) testGetNATByUUID() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-get-nat-by-uuid-lr"
	natType := "snat"
	externalIP := "192.168.30.254"
	logicalIP := "10.250.0.4"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("get existing nat by uuid", func(t *testing.T) {
		err := nbClient.AddNat(lrName, natType, externalIP, logicalIP, "", "", nil)
		require.NoError(t, err)

		originalNat, err := nbClient.GetNat(lrName, natType, externalIP, logicalIP, false)
		require.NoError(t, err)

		nat, err := nbClient.GetNATByUUID(originalNat.UUID)
		require.NoError(t, err)
		require.Equal(t, originalNat.UUID, nat.UUID)
		require.Equal(t, originalNat.Type, nat.Type)
		require.Equal(t, originalNat.ExternalIP, nat.ExternalIP)
		require.Equal(t, originalNat.LogicalIP, nat.LogicalIP)
	})

	t.Run("get nat with invalid uuid", func(t *testing.T) {
		nat, err := nbClient.GetNATByUUID("invalid-uuid")
		require.Error(t, err)
		require.Nil(t, nat)
	})

	t.Run("get nat with empty uuid", func(t *testing.T) {
		nat, err := nbClient.GetNATByUUID("")
		require.Error(t, err)
		require.Nil(t, nat)
	})
}

func (suite *OvnClientTestSuite) testGetNatValidations() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-get-nat-validations-lr"
	externalIP := "192.168.30.254"
	logicalIP := "10.250.0.4"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("get nat with dnat type", func(t *testing.T) {
		nat, err := nbClient.GetNat(lrName, "dnat", externalIP, logicalIP, false)
		require.ErrorContains(t, err, "does not support dnat for now")
		require.Nil(t, nat)
	})

	t.Run("get nat with invalid type", func(t *testing.T) {
		nat, err := nbClient.GetNat(lrName, "invalid-type", externalIP, logicalIP, false)
		require.ErrorContains(t, err, "nat type must be one of [ snat, dnat_and_snat ]")
		require.Nil(t, nat)
	})

	t.Run("get snat with empty logical ip", func(t *testing.T) {
		nat, err := nbClient.GetNat(lrName, "snat", externalIP, "", false)
		require.ErrorContains(t, err, "logical ip is required when nat type is snat")
		require.Nil(t, nat)
	})

	t.Run("get dnat_and_snat with empty external ip", func(t *testing.T) {
		nat, err := nbClient.GetNat(lrName, "dnat_and_snat", "", logicalIP, false)
		require.ErrorContains(t, err, "external ip is required when nat type is dnat_and_snat")
		require.Nil(t, nat)
	})

	t.Run("get nat with ignore not found flag", func(t *testing.T) {
		nat, err := nbClient.GetNat(lrName, "snat", externalIP, logicalIP, true)
		require.NoError(t, err)
		require.Nil(t, nat)
	})

	t.Run("get nat with multiple matches", func(t *testing.T) {
		nat1, err := nbClient.newNat(lrName, "snat", externalIP, logicalIP, "", "")
		require.NoError(t, err)
		nat2, err := nbClient.newNat(lrName, "snat", externalIP, logicalIP, "", "")
		require.NoError(t, err)

		err = nbClient.CreateNats(lrName, nat1, nat2)
		require.NoError(t, err)

		nat, err := nbClient.GetNat(lrName, "snat", externalIP, logicalIP, false)
		require.ErrorContains(t, err, "more than one nat")
		require.Nil(t, nat)
	})

	t.Run("get nat without type filter", func(t *testing.T) {
		err := nbClient.DeleteNats(lrName, "", "")
		require.NoError(t, err)

		nat1, err := nbClient.newNat(lrName, "snat", externalIP, logicalIP, "", "")
		require.NoError(t, err)

		err = nbClient.CreateNats(lrName, nat1)
		require.NoError(t, err)

		nat, err := nbClient.GetNat(lrName, "", externalIP, logicalIP, false)
		require.ErrorContains(t, err, "nat type must be one of [ snat, dnat_and_snat ]")
		require.Nil(t, nat)
	})
}
