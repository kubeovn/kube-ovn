package v1

import (
	"encoding/json"
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestBandwidthRateJSON(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  *BandwidthRate
	}{
		{name: "integer", value: `1024`, want: BandwidthRateFromInt64(1024)},
		{name: "integer beyond int32", value: `2147483648`, want: BandwidthRateFromInt64(math.MaxInt32 + 1)},
		{name: "maximum int64 integer", value: `9223372036854775807`, want: BandwidthRateFromInt64(math.MaxInt64)},
		{name: "quantity string", value: `"1Gi"`, want: BandwidthRateFromString("1Gi")},
		{name: "numeric string", value: `"9223372036854775807"`, want: BandwidthRateFromString("9223372036854775807")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got BandwidthRate
			require.NoError(t, json.Unmarshal([]byte(tt.value), &got))
			require.Equal(t, *tt.want, got)

			marshaled, err := json.Marshal(got)
			require.NoError(t, err)
			require.JSONEq(t, tt.value, string(marshaled))
		})
	}

	var rate BandwidthRate
	require.Error(t, json.Unmarshal([]byte(`9223372036854775808`), &rate))
	require.Error(t, json.Unmarshal([]byte(`1.5`), &rate))
	require.Error(t, json.Unmarshal([]byte(`null`), &rate))
	_, err := json.Marshal(BandwidthRate{Type: 2})
	require.ErrorContains(t, err, "unsupported bandwidth rate type")
}

func TestBandwidthLimitUnmarshalJSON(t *testing.T) {
	var bandwidth BandwidthLimit
	require.NoError(t, json.Unmarshal([]byte(`{"ingress":2147483648,"egress":"1Gi"}`), &bandwidth))
	require.NotNil(t, bandwidth.Ingress)
	require.Equal(t, intstr.Int, bandwidth.Ingress.Type)
	require.Equal(t, int64(math.MaxInt32+1), bandwidth.Ingress.IntVal)
	require.NotNil(t, bandwidth.Egress)
	require.Equal(t, intstr.String, bandwidth.Egress.Type)
	require.Equal(t, "1Gi", bandwidth.Egress.StrVal)
}

func TestBandwidthLimitJSON(t *testing.T) {
	tests := []struct {
		name      string
		bandwidth BandwidthLimit
		wantJSON  string
	}{
		{name: "empty", bandwidth: BandwidthLimit{}, wantJSON: `{}`},
		{name: "ingress only", bandwidth: BandwidthLimit{Ingress: BandwidthRateFromString("1Gi")}, wantJSON: `{"ingress":"1Gi"}`},
		{name: "explicit zero", bandwidth: BandwidthLimit{Ingress: BandwidthRateFromInt64(0)}, wantJSON: `{"ingress":0}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := json.Marshal(tt.bandwidth)
			require.NoError(t, err)
			require.Equal(t, tt.wantJSON, string(value))

			var got BandwidthLimit
			require.NoError(t, json.Unmarshal(value, &got))
			require.Equal(t, tt.bandwidth, got)
		})
	}
}

func TestBandwidthRateMbps(t *testing.T) {
	const formatErr = "must be an integer in Mbps or a quantity with unit M, Mi, G, or Gi"
	maxBandwidth := strconv.FormatInt(MaxBandwidthMbps, 10)
	overMaxBandwidth := strconv.FormatInt(MaxBandwidthMbps+1, 10)

	tests := []struct {
		name    string
		rate    *BandwidthRate
		want    int64
		wantErr string
	}{
		{name: "nil", rate: nil, want: 0},
		{name: "integer Mbps", rate: BandwidthRateFromInt64(1024), want: 1024},
		{name: "maximum integer Mbps", rate: BandwidthRateFromInt64(MaxBandwidthMbps), want: MaxBandwidthMbps},
		{name: "one over maximum integer Mbps", rate: BandwidthRateFromInt64(MaxBandwidthMbps + 1), wantErr: "exceeds the supported maximum"},
		{name: "numeric string Mbps", rate: BandwidthRateFromString("1024"), want: 1024},
		{name: "maximum numeric string Mbps", rate: BandwidthRateFromString(maxBandwidth), want: MaxBandwidthMbps},
		{name: "one over maximum numeric string Mbps", rate: BandwidthRateFromString(overMaxBandwidth), wantErr: "exceeds the supported maximum"},
		{name: "decimal SI megabit quantity", rate: BandwidthRateFromString("100M"), want: 100},
		{name: "binary SI mebibit quantity", rate: BandwidthRateFromString("100Mi"), want: 105},
		{name: "decimal SI quantity", rate: BandwidthRateFromString("1G"), want: 1000},
		{name: "binary SI quantity", rate: BandwidthRateFromString("1Gi"), want: 1074},
		{name: "fractional decimal SI quantity", rate: BandwidthRateFromString("1.5G"), want: 1500},
		{name: "fractional binary SI quantity", rate: BandwidthRateFromString(".5Gi"), want: 537},
		{name: "maximum suffixed quantity", rate: BandwidthRateFromString(maxBandwidth + "M"), want: MaxBandwidthMbps},
		{name: "one over maximum suffixed quantity", rate: BandwidthRateFromString(overMaxBandwidth + "M"), wantErr: "exceeds the supported maximum"},
		{name: "zero", rate: BandwidthRateFromInt64(0), want: 0},
		{name: "negative integer", rate: BandwidthRateFromInt64(-1), wantErr: "must not be negative"},
		{name: "negative quantity", rate: BandwidthRateFromString("-1G"), wantErr: "must not be negative"},
		{name: "empty string", rate: BandwidthRateFromString(""), wantErr: "must not be empty"},
		{name: "whitespace", rate: BandwidthRateFromString(" 1024 "), wantErr: formatErr},
		{name: "decimal without suffix", rate: BandwidthRateFromString("1.5"), wantErr: formatErr},
		{name: "milli suffix", rate: BandwidthRateFromString("1m"), wantErr: formatErr},
		{name: "kilo suffix", rate: BandwidthRateFromString("500k"), wantErr: formatErr},
		{name: "kibi suffix", rate: BandwidthRateFromString("1Ki"), wantErr: formatErr},
		{name: "tera suffix", rate: BandwidthRateFromString("1T"), wantErr: formatErr},
		{name: "decimal exponent", rate: BandwidthRateFromString("1e6"), wantErr: formatErr},
		{name: "invalid suffix", rate: BandwidthRateFromString("1GB"), wantErr: formatErr},
		{name: "invalid quantity", rate: BandwidthRateFromString("fast"), wantErr: formatErr},
		{name: "plain numeric overflow", rate: BandwidthRateFromString("9223372036854775808"), wantErr: "exceeds the supported maximum"},
		{name: "suffixed quantity overflow", rate: BandwidthRateFromString("9223372036854775808M"), wantErr: "exceeds the supported maximum"},
		{name: "unsupported type", rate: &BandwidthRate{Type: 2}, wantErr: "unsupported bandwidth rate type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.rate.Mbps()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestMaxBandwidthMbpsFitsOVSEgressRate(t *testing.T) {
	require.Equal(t, int64(9_223_372_036_854), MaxBandwidthMbps)
	require.LessOrEqual(t, MaxBandwidthMbps, int64(math.MaxInt64/1_000_000))
	require.Greater(t, MaxBandwidthMbps+1, int64(math.MaxInt64/1_000_000))
}

func TestBandwidthLimitMbps(t *testing.T) {
	var noBandwidth *BandwidthLimit
	ingress, egress, err := noBandwidth.Mbps()
	require.NoError(t, err)
	require.Zero(t, ingress)
	require.Zero(t, egress)

	ingress, egress, err = (&BandwidthLimit{}).Mbps()
	require.NoError(t, err)
	require.Zero(t, ingress)
	require.Zero(t, egress)

	ingress, egress, err = (&BandwidthLimit{Ingress: BandwidthRateFromInt64(1024)}).Mbps()
	require.NoError(t, err)
	require.Equal(t, int64(1024), ingress)
	require.Zero(t, egress)

	ingress, egress, err = (&BandwidthLimit{
		Ingress: BandwidthRateFromInt64(1024),
		Egress:  BandwidthRateFromString("1Gi"),
	}).Mbps()
	require.NoError(t, err)
	require.Equal(t, int64(1024), ingress)
	require.Equal(t, int64(1074), egress)

	_, _, err = (&BandwidthLimit{Ingress: BandwidthRateFromString("invalid")}).Mbps()
	require.ErrorContains(t, err, "invalid ingress bandwidth")

	_, _, err = (&BandwidthLimit{Egress: BandwidthRateFromString("invalid")}).Mbps()
	require.ErrorContains(t, err, "invalid egress bandwidth")
}
