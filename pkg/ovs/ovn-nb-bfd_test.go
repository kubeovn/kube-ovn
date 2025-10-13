package ovs

import (
	"testing"

	"github.com/scylladb/go-set/strset"
	"github.com/stretchr/testify/require"

	"k8s.io/apimachinery/pkg/util/uuid"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (suite *OvnClientTestSuite) testCreateBFD() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	dstIP := "192.168.124.1"
	minRx, minTx, detectMult := 101, 102, 19

	t.Run("create BFD", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-create-bfd"

		bfd, err := nbClient.CreateBFD(lrpName, dstIP, minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd)
		require.Equal(t, lrpName, bfd.LogicalPort)
		require.Equal(t, dstIP, bfd.DstIP)
		require.NotNil(t, bfd.MinRx)
		require.NotNil(t, bfd.MinTx)
		require.NotNil(t, bfd.DetectMult)
		require.Equal(t, minRx, *bfd.MinRx)
		require.Equal(t, minTx, *bfd.MinTx)
		require.Equal(t, detectMult, *bfd.DetectMult)
	})

	t.Run("create BFD with existing entry", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-create-existing-bfd"

		bfd1, err := nbClient.CreateBFD(lrpName, dstIP, minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd1)

		bfd2, err := nbClient.CreateBFD(lrpName, dstIP, minRx+1, minTx+1, detectMult+1, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd2)
		require.Equal(t, bfd1, bfd2)
		require.Equal(t, bfd1.UUID, bfd2.UUID)
		require.Equal(t, minRx, *bfd2.MinRx)
		require.Equal(t, minTx, *bfd2.MinTx)
		require.Equal(t, detectMult, *bfd2.DetectMult)
	})
}

func (suite *OvnClientTestSuite) testListBFD() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient

	lrpName := "test-list-bfd"
	dstIP1 := "192.168.124.2"
	dstIP2 := "192.168.124.3"
	minRx1, minTx1, detectMult1 := 101, 102, 19
	minRx2, minTx2, detectMult2 := 201, 202, 29

	t.Run("list BFDs", func(t *testing.T) {
		t.Parallel()

		bfd1, err := nbClient.CreateBFD(lrpName, dstIP1, minRx1, minTx1, detectMult1, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd1)

		bfd2, err := nbClient.CreateBFD(lrpName, dstIP2, minRx2, minTx2, detectMult2, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd2)

		bfdList, err := nbClient.ListBFDs(lrpName, dstIP1)
		require.NoError(t, err)
		require.Len(t, bfdList, 1)
		require.Equal(t, bfd1.UUID, bfdList[0].UUID)

		bfdList, err = nbClient.ListBFDs(lrpName, dstIP2)
		require.NoError(t, err)
		require.Len(t, bfdList, 1)
		require.Equal(t, bfd2.UUID, bfdList[0].UUID)

		bfdList, err = nbClient.ListBFDs(lrpName, "")
		require.NoError(t, err)
		require.Len(t, bfdList, 2)
		uuids := strset.NewWithSize(len(bfdList))
		for _, bfd := range bfdList {
			uuids.Add(bfd.UUID)
		}
		require.True(t, uuids.IsEqual(strset.New(bfd1.UUID, bfd2.UUID)))
	})

	t.Run("closed server list failed BFDs", func(t *testing.T) {
		t.Parallel()
		failedBFD1, err := failedNbClient.CreateBFD(lrpName, dstIP1, minRx1, minTx1, detectMult1, nil)
		require.Error(t, err)
		require.Nil(t, failedBFD1)
		// cache db should be empty
		bfdList, err := failedNbClient.ListBFDs(lrpName, dstIP1)
		require.NoError(t, err)
		require.Len(t, bfdList, 0)
	})
}

func (suite *OvnClientTestSuite) testFindBFD() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient
	lrpName := "test-find-bfd"
	dstIP1 := "192.168.124.101"
	dstIP2 := "192.168.124.102"
	minRx1, minTx1, detectMult1 := 101, 102, 19
	minRx2, minTx2, detectMult2 := 201, 202, 29

	bfd1, err := nbClient.CreateBFD(lrpName, dstIP1, minRx1, minTx1, detectMult1, map[string]string{"k1": "v1", "k2": "v2"})
	require.NoError(t, err)

	bfd2, err := nbClient.CreateBFD(lrpName, dstIP2, minRx2, minTx2, detectMult2, map[string]string{"k2": "v2"})
	require.NoError(t, err)

	t.Run("find BFD", func(t *testing.T) {
		bfds, err := nbClient.FindBFD(map[string]string{"k1": "v1"})
		require.NoError(t, err)
		require.Len(t, bfds, 1)
		require.Equal(t, bfd1.UUID, bfds[0].UUID)
	})

	t.Run("find multiple BFDs", func(t *testing.T) {
		bfds, err := nbClient.FindBFD(map[string]string{"k2": "v2"})
		require.NoError(t, err)
		require.Len(t, bfds, 2)
		require.ElementsMatch(t, []string{bfds[0].UUID, bfds[1].UUID}, []string{bfd1.UUID, bfd2.UUID})
	})

	t.Run("find non-existent BFD", func(t *testing.T) {
		t.Parallel()

		bfds, err := nbClient.FindBFD(map[string]string{"k3": "v3"})
		require.NoError(t, err)
		require.Empty(t, bfds)
	})

	t.Run("closed server find non-existent BFD", func(t *testing.T) {
		t.Parallel()

		bfds, err := failedNbClient.FindBFD(map[string]string{"k1": "v1"})
		require.NoError(t, err)
		require.Empty(t, bfds)
	})
}

func (suite *OvnClientTestSuite) testDeleteBFD() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient
	lrpName := "test-del-bfd"
	dstIP1 := "192.168.124.103"
	dstIP2 := "192.168.124.104"
	minRx1, minTx1, detectMult1 := 101, 102, 19
	minRx2, minTx2, detectMult2 := 201, 202, 29

	bfd1, err := nbClient.CreateBFD(lrpName, dstIP1, minRx1, minTx1, detectMult1, nil)
	require.NoError(t, err)

	bfd2, err := nbClient.CreateBFD(lrpName, dstIP2, minRx2, minTx2, detectMult2, nil)
	require.NoError(t, err)

	t.Run("delete BFD", func(t *testing.T) {
		err = nbClient.DeleteBFD(bfd1.UUID)
		require.NoError(t, err)

		bfdList, err := nbClient.ListBFDs(lrpName, dstIP1)
		require.NoError(t, err)
		require.Len(t, bfdList, 0)

		bfdList, err = nbClient.ListBFDs(lrpName, dstIP2)
		require.NoError(t, err)
		require.Len(t, bfdList, 1)
		require.Equal(t, bfd2.UUID, bfdList[0].UUID)
	})

	t.Run("delete non-existent BFD", func(t *testing.T) {
		t.Parallel()

		err := nbClient.DeleteBFD(string(uuid.NewUUID()))
		require.NoError(t, err)
	})

	t.Run("closed server delete non-existent BFD", func(t *testing.T) {
		t.Parallel()

		_, err := failedNbClient.CreateBFD(lrpName, dstIP1, minRx1, minTx1, detectMult1, nil)
		require.Error(t, err)
		err = failedNbClient.DeleteBFD(string(uuid.NewUUID()))
		// cache db should be empty
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testDeleteBFDByDstIP() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient
	lrpName := "test-del-bfd-by-dst-ip"
	dstIP1 := "192.168.124.4"
	dstIP2 := "192.168.124.5"
	minRx1, minTx1, detectMult1 := 101, 102, 19
	minRx2, minTx2, detectMult2 := 201, 202, 29

	_, err := nbClient.CreateBFD(lrpName, dstIP1, minRx1, minTx1, detectMult1, nil)
	require.NoError(t, err)

	bfd2, err := nbClient.CreateBFD(lrpName, dstIP2, minRx2, minTx2, detectMult2, nil)
	require.NoError(t, err)

	t.Run("delete BFD by dst ip", func(t *testing.T) {
		err = nbClient.DeleteBFDByDstIP(lrpName, dstIP1)
		require.NoError(t, err)

		bfdList, err := nbClient.ListBFDs(lrpName, dstIP1)
		require.NoError(t, err)
		require.Len(t, bfdList, 0)

		bfdList, err = nbClient.ListBFDs(lrpName, dstIP2)
		require.NoError(t, err)
		require.Len(t, bfdList, 1)
		require.Equal(t, bfd2.UUID, bfdList[0].UUID)
	})

	t.Run("delete multiple BFDs by dst ip", func(t *testing.T) {
		err = nbClient.DeleteBFDByDstIP(lrpName, "")
		require.NoError(t, err)

		bfdList, err := nbClient.ListBFDs(lrpName, "")
		require.NoError(t, err)
		require.Len(t, bfdList, 0)
	})

	t.Run("delete non-existent BFD by dst ip", func(t *testing.T) {
		t.Parallel()

		err := nbClient.DeleteBFDByDstIP(lrpName, "192.168.124.17")
		require.NoError(t, err)
	})

	t.Run("closed server delete non-existent BFD by dst ip", func(t *testing.T) {
		t.Parallel()

		_, err := failedNbClient.CreateBFD(lrpName, dstIP1, minRx1, minTx1, detectMult1, nil)
		require.Error(t, err)
		err = failedNbClient.DeleteBFDByDstIP(lrpName, "192.168.124.17")
		// cache db should be empty
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testListDownBFDs() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient
	lrpName := "test-list-down-bfd"
	dstIP1 := "192.168.124.6"
	dstIP2 := "192.168.124.7"
	dstIP3 := "192.168.124.8"
	minRx, minTx, detectMult := 101, 102, 19

	t.Run("list down BFDs", func(t *testing.T) {
		t.Parallel()

		bfd1, err := nbClient.CreateBFD(lrpName, dstIP1, minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd1)
		// closed server create failed BFD
		failedBFD1, err := failedNbClient.CreateBFD(lrpName, dstIP1, minRx, minTx, detectMult, nil)
		require.Error(t, err)
		require.Nil(t, failedBFD1)

		bfd2, err := nbClient.CreateBFD(lrpName, dstIP2, minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd2)

		bfd3, err := nbClient.CreateBFD(lrpName, dstIP3, minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd3)

		// Set BFD statuses
		downStatus := ovnnb.BFDStatusDown
		adminDownStatus := ovnnb.BFDStatusAdminDown
		upStatus := ovnnb.BFDStatusUp

		bfd1.Status = &downStatus
		bfd2.Status = &adminDownStatus
		bfd3.Status = &upStatus

		err = nbClient.UpdateBFD(bfd1)
		require.NoError(t, err)
		// closed server update failed BFD
		err = failedNbClient.UpdateBFD(bfd1)
		require.NoError(t, err)
		err = nbClient.UpdateBFD(bfd2)
		require.NoError(t, err)
		err = nbClient.UpdateBFD(bfd3)
		require.NoError(t, err)
		// update not exist bfd
		err = nbClient.UpdateBFD(&ovnnb.BFD{UUID: "not-exist"})
		require.Error(t, err)

		// Test listing down BFDs for specific IP
		downBFDs, err := nbClient.ListDownBFDs(dstIP1)
		require.NoError(t, err)
		require.Len(t, downBFDs, 1)
		require.Equal(t, bfd1.UUID, downBFDs[0].UUID)

		downBFDs, err = nbClient.ListDownBFDs(dstIP2)
		require.NoError(t, err)
		require.Len(t, downBFDs, 1)

		downBFDs, err = nbClient.ListDownBFDs(dstIP3)
		require.NoError(t, err)
		require.Len(t, downBFDs, 0)

		// Test listing down BFDs for non-existent IP
		nonExistentBFDs, err := nbClient.ListDownBFDs("192.168.124.9")
		require.NoError(t, err)
		require.Len(t, nonExistentBFDs, 0)
	})

	t.Run("list down BFDs with no down BFDs", func(t *testing.T) {
		t.Parallel()

		// Create a BFD with UP status
		bfd, err := nbClient.CreateBFD(lrpName, "192.168.124.10", minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd)

		upStatus := ovnnb.BFDStatusUp
		bfd.Status = &upStatus
		err = nbClient.UpdateBFD(bfd)
		require.NoError(t, err)

		// Try to list down BFDs
		downBFDs, err := nbClient.ListDownBFDs("192.168.124.10")
		require.NoError(t, err)
		require.Len(t, downBFDs, 0)
	})
}

func (suite *OvnClientTestSuite) testListUpBFDs() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrpName := "test-list-up-bfd"
	dstIP1 := "192.168.124.11"
	dstIP2 := "192.168.124.12"
	dstIP3 := "192.168.124.13"
	minRx, minTx, detectMult := 101, 102, 19

	t.Run("list up BFDs", func(t *testing.T) {
		t.Parallel()

		bfd1, err := nbClient.CreateBFD(lrpName, dstIP1, minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd1)

		bfd2, err := nbClient.CreateBFD(lrpName, dstIP2, minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd2)

		bfd3, err := nbClient.CreateBFD(lrpName, dstIP3, minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd3)

		upStatus := ovnnb.BFDStatusUp
		downStatus := ovnnb.BFDStatusDown
		adminDownStatus := ovnnb.BFDStatusAdminDown

		bfd1.Status = &upStatus
		bfd2.Status = &downStatus
		bfd3.Status = &adminDownStatus

		err = nbClient.UpdateBFD(bfd1)
		require.NoError(t, err)
		err = nbClient.UpdateBFD(bfd2)
		require.NoError(t, err)
		err = nbClient.UpdateBFD(bfd3)
		require.NoError(t, err)

		upBFDs, err := nbClient.ListUpBFDs(dstIP1)
		require.NoError(t, err)
		require.Len(t, upBFDs, 1)
		require.Equal(t, bfd1.UUID, upBFDs[0].UUID)

		upBFDs, err = nbClient.ListUpBFDs(dstIP2)
		require.NoError(t, err)
		require.Len(t, upBFDs, 0)

		upBFDs, err = nbClient.ListUpBFDs(dstIP3)
		require.NoError(t, err)
		require.Len(t, upBFDs, 0)
	})

	t.Run("list up BFDs with non-existent IP", func(t *testing.T) {
		t.Parallel()

		upBFDs, err := nbClient.ListUpBFDs("192.168.124.14")
		require.NoError(t, err)
		require.Len(t, upBFDs, 0)
	})
}

func (suite *OvnClientTestSuite) testIsLrpBfdUp() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	dstIP := "192.168.124.15"
	minRx, minTx, detectMult := 101, 102, 19

	t.Run("BFD is up", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-is-lrp-bfd-up"
		bfd, err := nbClient.CreateBFD(lrpName, dstIP, minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd)

		upStatus := ovnnb.BFDStatusUp
		bfd.Status = &upStatus
		err = nbClient.UpdateBFD(bfd)
		require.NoError(t, err)

		isUp, err := nbClient.isLrpBfdUp(lrpName, dstIP)
		require.NoError(t, err)
		require.True(t, isUp)
	})

	t.Run("BFD is down", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-is-lrp-bfd-down"
		bfd, err := nbClient.CreateBFD(lrpName, dstIP, minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd)

		downStatus := ovnnb.BFDStatusDown
		bfd.Status = &downStatus
		err = nbClient.UpdateBFD(bfd)
		require.NoError(t, err)

		isUp, err := nbClient.isLrpBfdUp(lrpName, dstIP)
		require.Error(t, err)
		require.False(t, isUp)
		require.Contains(t, err.Error(), "status is down")
	})

	t.Run("BFD status is nil", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-is-lrp-bfd-status-nil"
		bfd, err := nbClient.CreateBFD(lrpName, dstIP, minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd)

		bfd.Status = nil
		err = nbClient.UpdateBFD(bfd)
		require.NoError(t, err)

		isUp, err := nbClient.isLrpBfdUp(lrpName, dstIP)
		require.Error(t, err)
		require.False(t, isUp)
		require.Contains(t, err.Error(), "status is nil")
	})

	t.Run("No BFD exists", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-is-lrp-bfd-none"
		isUp, err := nbClient.isLrpBfdUp(lrpName, "192.168.124.16")
		require.NoError(t, err)
		require.True(t, isUp)
	})
}

func (suite *OvnClientTestSuite) testBfdAddL3HAHandler() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	t.Run("BFD status is nil", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-bfd-add-l3ha-handler-nil"
		dstIP := "192.168.124.19"
		minRx, minTx, detectMult := 101, 102, 19

		bfd, err := nbClient.CreateBFD(lrpName, dstIP, minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd)

		bfd.Status = nil
		err = nbClient.UpdateBFD(bfd)
		require.NoError(t, err)

		nbClient.bfdAddL3HAHandler(ovnnb.BFDTable, bfd)

		updatedBfd, err := nbClient.ListBFDs(lrpName, dstIP)
		require.NoError(t, err)
		require.Nil(t, updatedBfd[0].Status)
	})

	t.Run("BFD status is down", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-bfd-add-l3ha-handler-down"
		dstIP := "192.168.124.20"
		minRx, minTx, detectMult := 101, 102, 19

		bfd, err := nbClient.CreateBFD(lrpName, dstIP, minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd)

		downStatus := ovnnb.BFDStatusDown
		bfd.Status = &downStatus
		err = nbClient.UpdateBFD(bfd)
		require.NoError(t, err)

		nbClient.bfdAddL3HAHandler(ovnnb.BFDTable, bfd)

		updatedBfd, err := nbClient.ListBFDs(lrpName, dstIP)
		require.NoError(t, err)
		require.NotNil(t, updatedBfd[0].Status)
		require.Equal(t, ovnnb.BFDStatusDown, *updatedBfd[0].Status)
	})

	t.Run("BFD status is already up", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-bfd-add-l3ha-handler-up"
		dstIP := "192.168.124.21"
		minRx, minTx, detectMult := 101, 102, 19

		bfd, err := nbClient.CreateBFD(lrpName, dstIP, minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd)

		upStatus := ovnnb.BFDStatusUp
		bfd.Status = &upStatus
		err = nbClient.UpdateBFD(bfd)
		require.NoError(t, err)

		nbClient.bfdAddL3HAHandler(ovnnb.BFDTable, bfd)

		updatedBfd, err := nbClient.ListBFDs(lrpName, dstIP)
		require.NoError(t, err)
		require.NotNil(t, updatedBfd[0].Status)
		require.Equal(t, ovnnb.BFDStatusUp, *updatedBfd[0].Status)
	})

	t.Run("Wrong table", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-bfd-add-l3ha-handler-up-wrong-table"
		dstIP := "192.168.124.22"
		minRx, minTx, detectMult := 101, 102, 19

		bfd, err := nbClient.CreateBFD(lrpName, dstIP, minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd)

		nbClient.bfdAddL3HAHandler("WrongTable", bfd)

		updatedBfd, err := nbClient.ListBFDs(lrpName, dstIP)
		require.NoError(t, err)
		require.Nil(t, updatedBfd[0].Status)
	})
}

func (suite *OvnClientTestSuite) testBfdUpdateL3HAHandler() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	failedNbClient := suite.failedOvnNBClient

	t.Run("BFD status change with wrong table", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-bfd-update-l3ha-handler-wrong-table"
		dstIP := "192.168.124.26"
		minRx, minTx, detectMult := 101, 102, 19

		bfd, err := nbClient.CreateBFD(lrpName, dstIP, minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd)

		upStatus := ovnnb.BFDStatusUp
		bfd.Status = &upStatus
		err = nbClient.UpdateBFD(bfd)
		require.NoError(t, err)

		newBfd := *bfd
		downStatus := ovnnb.BFDStatusDown
		newBfd.Status = &downStatus

		nbClient.bfdUpdateL3HAHandler("WrongTable", bfd, &newBfd)
	})

	t.Run("BFD status change with nil status", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-bfd-update-l3ha-handler-nil-status"
		dstIP := "192.168.124.27"
		minRx, minTx, detectMult := 101, 102, 19

		bfd, err := nbClient.CreateBFD(lrpName, dstIP, minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd)
		bfd.Status = nil
		err = nbClient.UpdateBFD(bfd)
		require.NoError(t, err)

		newBfd := *bfd
		downStatus := ovnnb.BFDStatusDown
		newBfd.Status = &downStatus

		nbClient.bfdUpdateL3HAHandler(ovnnb.BFDTable, bfd, &newBfd)

		updatedBfd, err := nbClient.ListBFDs(lrpName, dstIP)
		require.NoError(t, err)
		require.Nil(t, updatedBfd[0].Status)
	})

	t.Run("BFD status not changed", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-bfd-update-l3ha-handler-same-status"
		dstIP := "192.168.124.28"
		minRx, minTx, detectMult := 101, 102, 19

		bfd, err := nbClient.CreateBFD(lrpName, dstIP, minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd)
		upStatus := ovnnb.BFDStatusUp
		bfd.Status = &upStatus
		err = nbClient.UpdateBFD(bfd)
		require.NoError(t, err)

		newBfd := *bfd
		newBfd.Status = &upStatus

		nbClient.bfdUpdateL3HAHandler(ovnnb.BFDTable, bfd, &newBfd)
	})

	t.Run("BFD status change from AdminDown to Down", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-bfd-update-l3ha-handler-admin-down-to-down"
		dstIP := "192.168.124.23"
		minRx, minTx, detectMult := 101, 102, 19

		bfd, err := nbClient.CreateBFD(lrpName, dstIP, minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd)

		adminDownStatus := ovnnb.BFDStatusAdminDown
		bfd.Status = &adminDownStatus
		err = nbClient.UpdateBFD(bfd)
		require.NoError(t, err)

		newBfd := *bfd
		downStatus := ovnnb.BFDStatusDown
		newBfd.Status = &downStatus

		nbClient.bfdUpdateL3HAHandler(ovnnb.BFDTable, bfd, &newBfd)
	})

	t.Run("BFD status change from Down to Up", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-bfd-update-l3ha-handler-down-to-up"
		dstIP := "192.168.124.24"
		minRx, minTx, detectMult := 101, 102, 19

		bfd, err := nbClient.CreateBFD(lrpName, dstIP, minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd)

		downStatus := ovnnb.BFDStatusDown
		bfd.Status = &downStatus
		err = nbClient.UpdateBFD(bfd)
		require.NoError(t, err)

		newBfd := *bfd
		upStatus := ovnnb.BFDStatusUp
		newBfd.Status = &upStatus

		nbClient.bfdUpdateL3HAHandler(ovnnb.BFDTable, bfd, &newBfd)
	})

	t.Run("BFD status change from Up to Down", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-bfd-update-l3ha-handler-up-to-down"
		dstIP := "192.168.124.25"
		minRx, minTx, detectMult := 101, 102, 19

		bfd, err := nbClient.CreateBFD(lrpName, dstIP, minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd)

		upStatus := ovnnb.BFDStatusUp
		bfd.Status = &upStatus
		err = nbClient.UpdateBFD(bfd)
		require.NoError(t, err)

		newBfd := *bfd
		downStatus := ovnnb.BFDStatusDown
		newBfd.Status = &downStatus

		nbClient.bfdUpdateL3HAHandler(ovnnb.BFDTable, bfd, &newBfd)
	})

	t.Run("failed client update BFD status", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-failed-client-bfd-update"
		dstIP := "192.168.124.28"
		minRx, minTx, detectMult := 101, 102, 19

		failedBFD, err := failedNbClient.CreateBFD(lrpName, dstIP, minRx, minTx, detectMult, nil)
		require.Error(t, err)
		require.Nil(t, failedBFD)
		newBfd := &ovnnb.BFD{
			LogicalPort: lrpName,
			UUID:        "test-failed-client-bfd-update",
			DstIP:       dstIP,
			MinRx:       &minRx,
			MinTx:       &minTx,
			DetectMult:  &detectMult,
		}
		err = failedNbClient.UpdateBFD(newBfd)
		require.NoError(t, err)
	})
}

func (suite *OvnClientTestSuite) testBfdDelL3HAHandler() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient

	t.Run("BFD deletion with wrong table", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-bfd-del-l3ha-handler-wrong-table"
		dstIP := "192.168.124.30"
		minRx, minTx, detectMult := 101, 102, 19

		bfd, err := nbClient.CreateBFD(lrpName, dstIP, minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd)

		nbClient.bfdDelL3HAHandler("WrongTable", bfd)

		// Verify that the BFD is not deleted
		bfdList, err := nbClient.ListBFDs(lrpName, dstIP)
		require.NoError(t, err)
		require.Len(t, bfdList, 1)
	})

	t.Run("BFD deletion with correct table", func(t *testing.T) {
		t.Parallel()

		lrpName := "test-bfd-del-l3ha-handler-correct-table"
		dstIP := "192.168.124.31"
		minRx, minTx, detectMult := 101, 102, 19

		bfd, err := nbClient.CreateBFD(lrpName, dstIP, minRx, minTx, detectMult, nil)
		require.NoError(t, err)
		require.NotNil(t, bfd)

		nbClient.bfdDelL3HAHandler(ovnnb.BFDTable, bfd)
	})
}

func (suite *OvnClientTestSuite) testMonitorBFDs() {
	nbClient := suite.ovnNBClient
	nbClient.MonitorBFD()
}
