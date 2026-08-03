package ovs

import (
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestParseAndScaleBandwidthRate(t *testing.T) {
	t.Parallel()
	maxBandwidth := strconv.FormatInt(kubeovnv1.MaxBandwidthMbps, 10)
	overMaxBandwidth := strconv.FormatInt(kubeovnv1.MaxBandwidthMbps+1, 10)

	tests := []struct {
		name    string
		rate    string
		scale   int64
		want    int64
		wantErr string
	}{
		{name: "empty is zero", rate: "", scale: 1_000_000, want: 0},
		{name: "zero", rate: "0", scale: 1_000_000, want: 0},
		{name: "normal", rate: "100", scale: 1_000_000, want: 100_000_000},
		{name: "unified maximum in Kbit", rate: maxBandwidth, scale: 1000, want: 9_223_372_036_854_000},
		{name: "unified maximum in bits", rate: maxBandwidth, scale: 1_000_000, want: 9_223_372_036_854_000_000},
		{name: "one over unified maximum in Kbit", rate: overMaxBandwidth, scale: 1000, wantErr: "overflows"},
		{name: "one over unified maximum in bits", rate: overMaxBandwidth, scale: 1_000_000, wantErr: "overflows"},
		{name: "invalid", rate: "invalid", scale: 1_000_000, wantErr: "invalid bandwidth rate"},
		{name: "negative", rate: "-1", scale: 1_000_000, wantErr: "must not be negative"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAndScaleBandwidthRate(tt.rate, tt.scale)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func (suite *OvnClientTestSuite) testSetInterfaceBandwidth() {
	t := suite.T()
	t.Parallel()

	err := SetInterfaceBandwidth("podName", "podNS", "eth0", "10", "10", "", "")
	// no ovs-vsctl command
	require.Error(t, err)
}

func TestSetInterfaceBandwidthRejectsInvalidRatesBeforeOVS(t *testing.T) {
	t.Parallel()
	overMaxBandwidth := strconv.FormatInt(kubeovnv1.MaxBandwidthMbps+1, 10)

	tests := []struct {
		name    string
		ingress string
		egress  string
		wantErr string
	}{
		{name: "invalid ingress", ingress: "invalid", egress: "0", wantErr: "invalid ingress bandwidth"},
		{name: "negative ingress", ingress: "-1", egress: "0", wantErr: "must not be negative"},
		{name: "ingress above unified maximum", ingress: overMaxBandwidth, egress: "0", wantErr: "overflows"},
		{name: "invalid egress", ingress: "0", egress: "invalid", wantErr: "invalid egress bandwidth"},
		{name: "negative egress", ingress: "0", egress: "-1", wantErr: "must not be negative"},
		{name: "overflowing egress", ingress: "0", egress: overMaxBandwidth, wantErr: "overflows"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SetInterfaceBandwidth("podName", "podNS", "eth0", tt.ingress, tt.egress, "", "")
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestComputeIngressPolicingBurstKbit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rateKbit int64
		burst    string
		want     int64
	}{
		{name: "default 80% when burst empty", rateKbit: 10000, burst: "", want: 8000},
		{name: "literal zero is honored", rateKbit: 10000, burst: "0", want: 0},
		{name: "explicit value in Mbit", rateKbit: 10000, burst: "5", want: 5000},
		{name: "default with zero rate", rateKbit: 0, burst: "", want: 0},
		{name: "non-empty burst with zero rate is forced to 0", rateKbit: 0, burst: "5", want: 0},
		{name: "unparseable burst falls back to default", rateKbit: 10000, burst: "abc", want: 8000},
		{name: "overflowing burst falls back to default", rateKbit: 10000, burst: strconv.FormatInt(math.MaxInt64, 10), want: 8000},
		{name: "maximum rate uses overflow-safe default", rateKbit: math.MaxInt64, burst: "", want: 7_378_697_629_483_820_645},
		{name: "maximum rate uses overflow-safe invalid burst fallback", rateKbit: math.MaxInt64, burst: "abc", want: 7_378_697_629_483_820_645},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, computeIngressPolicingBurstKbit(tt.rateKbit, tt.burst))
		})
	}
}

func TestComputeHtbBurstBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rateBPS int64
		burst   string
		want    int64
	}{
		{name: "default 80% when burst empty", rateBPS: 100_000_000, burst: "", want: 10_000_000},
		{name: "literal zero is honored", rateBPS: 100_000_000, burst: "0", want: 0},
		{name: "explicit value in Mbit", rateBPS: 100_000_000, burst: "5", want: 625_000},
		{name: "non-empty burst with zero rate is forced to 0", rateBPS: 0, burst: "5", want: 0},
		{name: "unparseable burst falls back to default", rateBPS: 100_000_000, burst: "abc", want: 10_000_000},
		{name: "overflowing burst falls back to default", rateBPS: 100_000_000, burst: strconv.FormatInt(math.MaxInt64, 10), want: 10_000_000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, computeHtbBurstBytes(tt.rateBPS, tt.burst))
		})
	}
}

func (suite *OvnClientTestSuite) testClearHtbQosQueue() {
	t := suite.T()
	t.Parallel()
	err := ClearHtbQosQueue("podName", "podNS", "eth0")
	// no ovs-vsctl command
	require.Error(t, err)
}

func (suite *OvnClientTestSuite) testIsHtbQos() {
	t := suite.T()
	t.Parallel()
	isHtbQos, err := IsHtbQos("eth0")
	// no ovs-vsctl command
	require.Error(t, err)
	require.False(t, isHtbQos)
}

func (suite *OvnClientTestSuite) testSetHtbQosQueueRecord() {
	t := suite.T()
	t.Parallel()
	// get a new id
	id, err := SetHtbQosQueueRecord("podName", "podNS", "eth0", 10, 0, nil)
	// no ovs-vsctl command
	require.Error(t, err)
	require.Empty(t, id)
	// get a exist id
	queueIfaceUIDMap := make(map[string]string)
	queueIfaceUIDMap["eth0"] = "123"
	id, err = SetHtbQosQueueRecord("podName", "podNS", "eth0", 10, 8, queueIfaceUIDMap)
	// no ovs-vsctl command
	require.Error(t, err)
	require.Empty(t, id)
}

func (suite *OvnClientTestSuite) testSetQosQueueBinding() {
	t := suite.T()
	t.Parallel()
	// get a new id
	err := SetQosQueueBinding("podName", "podNS", "podName.podNS", "eth0", "123", nil)
	// no ovs-vsctl command
	require.Error(t, err)
	// get a exist id
	queueIfaceUIDMap := make(map[string]string)
	queueIfaceUIDMap["eth0"] = "123"
	err = SetQosQueueBinding("podName", "podNS", "podName.podNS", "eth0", "123", queueIfaceUIDMap)
	// no ovs-vsctl command
	require.Error(t, err)
}

func (suite *OvnClientTestSuite) testSetNetemQos() {
	t := suite.T()
	t.Parallel()
	err := SetNetemQos("podName", "podNS", "eth0", "10", "10", "10", "10")
	// no ovs-vsctl command
	require.Error(t, err)
}

func (suite *OvnClientTestSuite) testGetNetemQosConfig() {
	t := suite.T()
	t.Parallel()
	latency, loss, limit, jitter, err := getNetemQosConfig("qosID")
	// no ovs-vsctl command
	require.Error(t, err)
	require.Empty(t, latency)
	require.Empty(t, loss)
	require.Empty(t, limit)
	require.Empty(t, jitter)
}

func (suite *OvnClientTestSuite) testDeleteNetemQosByID() {
	t := suite.T()
	t.Parallel()
	err := deleteNetemQosByID("qosID", "eth0", "podName", "podNS")
	require.Nil(t, err)
}

func (suite *OvnClientTestSuite) testIsUserspaceDataPath() {
	t := suite.T()
	t.Parallel()
	isUserspace, err := IsUserspaceDataPath()
	// no ovs-vsctl command
	require.Error(t, err)
	require.False(t, isUserspace)
}

func (suite *OvnClientTestSuite) testCheckAndUpdateHtbQos() {
	t := suite.T()
	t.Parallel()
	// get a new id
	err := CheckAndUpdateHtbQos("podName", "podNS", "eth0", nil)
	require.Nil(t, err)

	// get a exist id
	queueIfaceUIDMap := make(map[string]string)
	queueIfaceUIDMap["eth0"] = "123"
	err = CheckAndUpdateHtbQos("podName", "podNS", "eth0", queueIfaceUIDMap)
	// no ovs-vsctl command
	require.Error(t, err)
}
