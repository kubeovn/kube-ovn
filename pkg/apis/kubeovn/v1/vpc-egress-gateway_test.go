package v1

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestBandwidthLimitUnmarshalJSON(t *testing.T) {
	var bandwidth BandwidthLimit
	require.NoError(t, json.Unmarshal([]byte(`{"ingress":1024,"egress":"1Gi"}`), &bandwidth))
	require.Equal(t, intstr.Int, bandwidth.Ingress.Type)
	require.Equal(t, int32(1024), bandwidth.Ingress.IntVal)
	require.Equal(t, intstr.String, bandwidth.Egress.Type)
	require.Equal(t, "1Gi", bandwidth.Egress.StrVal)
}

func TestBandwidthRateToMbps(t *testing.T) {
	tests := []struct {
		name    string
		rate    intstr.IntOrString
		want    int64
		wantErr string
	}{
		{name: "integer Mbps", rate: intstr.FromInt32(1024), want: 1024},
		{name: "numeric string Mbps", rate: intstr.FromString("1024"), want: 1024},
		{name: "decimal SI megabit quantity", rate: intstr.FromString("100M"), want: 100},
		{name: "binary SI mebibit quantity", rate: intstr.FromString("100Mi"), want: 105},
		{name: "decimal SI quantity", rate: intstr.FromString("1G"), want: 1000},
		{name: "binary SI quantity", rate: intstr.FromString("1Gi"), want: 1074},
		{name: "sub-Mbps quantity", rate: intstr.FromString("500k"), want: 1},
		{name: "zero", rate: intstr.FromInt32(0), want: 0},
		{name: "trimmed numeric string", rate: intstr.FromString(" 1024 "), want: 1024},
		{name: "negative integer", rate: intstr.FromInt32(-1), wantErr: "must not be negative"},
		{name: "negative quantity", rate: intstr.FromString("-1G"), wantErr: "must not be negative"},
		{name: "empty string", rate: intstr.FromString(""), wantErr: "must not be empty"},
		{name: "invalid quantity", rate: intstr.FromString("fast"), wantErr: "invalid Kubernetes quantity"},
		{name: "overflow", rate: intstr.FromString(strconv.FormatInt(maxBandwidthMbps+1, 10)), wantErr: "exceeds the supported maximum"},
		{name: "unsupported type", rate: intstr.IntOrString{Type: 2}, wantErr: "unsupported IntOrString type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := bandwidthRateToMbps(tt.rate)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestBandwidthLimitMbps(t *testing.T) {
	var noBandwidth *BandwidthLimit
	ingress, egress, err := noBandwidth.Mbps()
	require.NoError(t, err)
	require.Zero(t, ingress)
	require.Zero(t, egress)

	ingress, egress, err = (&BandwidthLimit{
		Ingress: intstr.FromInt32(1024),
		Egress:  intstr.FromString("1Gi"),
	}).Mbps()
	require.NoError(t, err)
	require.Equal(t, int64(1024), ingress)
	require.Equal(t, int64(1074), egress)

	_, _, err = (&BandwidthLimit{Ingress: intstr.FromString("invalid")}).Mbps()
	require.ErrorContains(t, err, "invalid ingress bandwidth")

	_, _, err = (&BandwidthLimit{Egress: intstr.FromString("invalid")}).Mbps()
	require.ErrorContains(t, err, "invalid egress bandwidth")
}
