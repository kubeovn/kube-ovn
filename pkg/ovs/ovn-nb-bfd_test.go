package ovs

import (
	"testing"

	"github.com/scylladb/go-set/strset"
	"github.com/stretchr/testify/require"
)

func (suite *OvnClientTestSuite) testCreateBFD() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrpName := "test-create-bfd"
	dstIP := "192.168.124.1"
	minRx, minTx, detectMult := 101, 102, 19

	t.Run("create BFD", func(t *testing.T) {
		t.Parallel()

		bfd, err := ovnClient.CreateBFD(lrpName, dstIP, minRx, minTx, detectMult)
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
}

func (suite *OvnClientTestSuite) testListBFD() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrpName := "test-list-bfd"
	dstIP1 := "192.168.124.2"
	dstIP2 := "192.168.124.3"
	minRx1, minTx1, detectMult1 := 101, 102, 19
	minRx2, minTx2, detectMult2 := 201, 202, 29

	t.Run("list BFDs", func(t *testing.T) {
		t.Parallel()

		bfd1, err := ovnClient.CreateBFD(lrpName, dstIP1, minRx1, minTx1, detectMult1)
		require.NoError(t, err)
		require.NotNil(t, bfd1)

		bfd2, err := ovnClient.CreateBFD(lrpName, dstIP2, minRx2, minTx2, detectMult2)
		require.NoError(t, err)
		require.NotNil(t, bfd2)

		bfdList, err := ovnClient.ListBFDs(lrpName, dstIP1)
		require.NoError(t, err)
		require.Len(t, bfdList, 1)
		require.Equal(t, bfd1.UUID, bfdList[0].UUID)

		bfdList, err = ovnClient.ListBFDs(lrpName, dstIP2)
		require.NoError(t, err)
		require.Len(t, bfdList, 1)
		require.Equal(t, bfd2.UUID, bfdList[0].UUID)

		bfdList, err = ovnClient.ListBFDs(lrpName, "")
		require.NoError(t, err)
		require.Len(t, bfdList, 2)
		uuids := strset.NewWithSize(len(bfdList))
		for _, bfd := range bfdList {
			uuids.Add(bfd.UUID)
		}
		require.True(t, uuids.IsEqual(strset.New(bfd1.UUID, bfd2.UUID)))
	})
}

func (suite *OvnClientTestSuite) testDeleteBFD() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrpName := "test-del-bfd"
	dstIP1 := "192.168.124.4"
	dstIP2 := "192.168.124.5"
	minRx1, minTx1, detectMult1 := 101, 102, 19
	minRx2, minTx2, detectMult2 := 201, 202, 29

	_, err := ovnClient.CreateBFD(lrpName, dstIP1, minRx1, minTx1, detectMult1)
	require.NoError(t, err)

	bfd2, err := ovnClient.CreateBFD(lrpName, dstIP2, minRx2, minTx2, detectMult2)
	require.NoError(t, err)

	t.Run("delete BFD", func(t *testing.T) {
		err = ovnClient.DeleteBFD(lrpName, dstIP1)
		require.NoError(t, err)

		bfdList, err := ovnClient.ListBFDs(lrpName, dstIP1)
		require.NoError(t, err)
		require.Len(t, bfdList, 0)

		bfdList, err = ovnClient.ListBFDs(lrpName, dstIP2)
		require.NoError(t, err)
		require.Len(t, bfdList, 1)
		require.Equal(t, bfd2.UUID, bfdList[0].UUID)
	})

	t.Run("delete multiple BFDs", func(t *testing.T) {
		err = ovnClient.DeleteBFD(lrpName, "")
		require.NoError(t, err)

		bfdList, err := ovnClient.ListBFDs(lrpName, "")
		require.NoError(t, err)
		require.Len(t, bfdList, 0)
	})
}
