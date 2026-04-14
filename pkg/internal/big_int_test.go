package internal

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBigInt(t *testing.T) {
	tests := []struct {
		name  string
		value int64
	}{
		{"zero", 0},
		{"positive", 42},
		{"negative", -100},
		{"large", 1 << 53},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBigInt(tt.value)
			assert.Equal(t, tt.value, b.Int64())
		})
	}
}

func TestBigInt_DeepCopyInto(t *testing.T) {
	tests := []struct {
		name  string
		value int64
	}{
		{"zero", 0},
		{"positive", 12345},
		{"negative", -42},
		{"large", 1<<53 + 1}, // exceeds float64 exact range
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := BigInt{Int: *big.NewInt(tt.value)}
			var copied BigInt
			original.DeepCopyInto(&copied)

			// Values must be equal
			assert.Equal(t, 0, original.Cmp(copied), "copied value should equal original")

			// Must be independent: mutating copy must not affect original
			copied.SetInt64(999)
			assert.Equal(t, tt.value, original.Int64(), "original must be unchanged after mutating copy")
		})
	}
}

func TestBigInt_Clone(t *testing.T) {
	original := BigInt{Int: *big.NewInt(42)}
	cloned := original.Clone()

	assert.Equal(t, 0, original.Cmp(cloned))

	cloned.SetInt64(999)
	assert.Equal(t, int64(42), original.Int64(), "original must be unchanged after mutating clone")
}

func TestBigInt_Equal(t *testing.T) {
	a := BigInt{Int: *big.NewInt(100)}
	b := BigInt{Int: *big.NewInt(100)}
	c := BigInt{Int: *big.NewInt(200)}

	assert.True(t, a.Equal(b))
	assert.False(t, a.Equal(c))

	// Zero from different origins
	z1 := BigInt{}
	z2 := BigInt{Int: *big.NewInt(0)}
	z3 := BigInt{Int: *new(big.Int).Sub(big.NewInt(5), big.NewInt(5))}
	assert.True(t, z1.Equal(z2))
	assert.True(t, z1.Equal(z3))
}

func TestBigInt_EqualInt64(t *testing.T) {
	assert.True(t, BigInt{}.EqualInt64(0))
	assert.True(t, BigInt{Int: *big.NewInt(42)}.EqualInt64(42))
	assert.False(t, BigInt{Int: *big.NewInt(42)}.EqualInt64(43))
}

func TestBigInt_Cmp(t *testing.T) {
	a := BigInt{Int: *big.NewInt(10)}
	b := BigInt{Int: *big.NewInt(20)}
	assert.Equal(t, -1, a.Cmp(b))
	assert.Equal(t, 1, b.Cmp(a))
	c := BigInt{Int: *big.NewInt(10)}
	assert.Equal(t, 0, a.Cmp(c))
}

func TestBigInt_Add(t *testing.T) {
	a := BigInt{Int: *big.NewInt(10)}
	b := BigInt{Int: *big.NewInt(20)}
	result := a.Add(b)
	assert.True(t, result.EqualInt64(30))
	// Original unchanged
	assert.True(t, a.EqualInt64(10))
}

func TestBigInt_Sub(t *testing.T) {
	a := BigInt{Int: *big.NewInt(30)}
	b := BigInt{Int: *big.NewInt(10)}
	result := a.Sub(b)
	assert.True(t, result.EqualInt64(20))
	assert.True(t, a.EqualInt64(30))
}

func TestBigInt_AddInt(t *testing.T) {
	a := BigInt{Int: *big.NewInt(10)}
	result := a.AddInt(5)
	assert.True(t, result.EqualInt64(15))
	assert.True(t, a.EqualInt64(10))
}

func TestBigInt_SubInt(t *testing.T) {
	a := BigInt{Int: *big.NewInt(10)}
	result := a.SubInt(3)
	assert.True(t, result.EqualInt64(7))
	assert.True(t, a.EqualInt64(10))
}

func TestBigInt_Float64(t *testing.T) {
	assert.Equal(t, float64(0), BigInt{}.Float64())
	assert.Equal(t, float64(42), BigInt{Int: *big.NewInt(42)}.Float64())
	assert.Equal(t, float64(-1), BigInt{Int: *big.NewInt(-1)}.Float64())
	// Large value within float64 exact range
	assert.Equal(t, float64(1<<53), BigInt{Int: *big.NewInt(1 << 53)}.Float64())
}

func TestBigInt_String(t *testing.T) {
	assert.Equal(t, "0", BigInt{}.String())
	assert.Equal(t, "42", BigInt{Int: *big.NewInt(42)}.String())
	assert.Equal(t, "-1", BigInt{Int: *big.NewInt(-1)}.String())
}

func TestBigInt_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		value    int64
		expected string
	}{
		{"zero", 0, `0`},
		{"positive", 42, `42`},
		{"negative", -1, `-1`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := BigInt{Int: *big.NewInt(tt.value)}
			data, err := b.MarshalJSON()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, string(data))
		})
	}
}

func TestBigInt_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int64
		wantErr  bool
	}{
		{"integer", "42", 42, false},
		{"string", `"42"`, 42, false},
		{"zero", "0", 0, false},
		{"string_zero", `"0"`, 0, false},
		{"negative", "-1", -1, false},
		{"string_negative", `"-1"`, -1, false},
		{"null", "null", 0, false},
		{"float_whole", "100.0", 100, false},
		{"string_float_whole", `"100.0"`, 100, false},
		{"scientific", "1e2", 100, false},
		{"string_scientific", `"1e2"`, 100, false},
		{"float_fractional", "1.5", 0, true},
		{"invalid", "\"abc\"", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b BigInt
			err := b.UnmarshalJSON([]byte(tt.input))
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, b.Int64())
			}
		})
	}
}

func TestBigInt_MarshalUnmarshalRoundTrip(t *testing.T) {
	original := BigInt{Int: *big.NewInt(123456789)}
	data, err := original.MarshalJSON()
	require.NoError(t, err)

	var restored BigInt
	require.NoError(t, restored.UnmarshalJSON(data))
	assert.True(t, original.Equal(restored))
}

// TestBigInt_UpgradeFromFloat64 verifies that BigInt can unmarshal JSON values
// produced by the old float64-based SubnetStatus fields. During upgrade, etcd
// contains numbers serialized by encoding/json from float64 (e.g. 254, 0, 1e+20).
func TestBigInt_UpgradeFromFloat64(t *testing.T) {
	tests := []struct {
		name     string
		json     string // raw JSON as stored in etcd from old float64 fields
		expected int64
	}{
		{"zero", "0", 0},
		{"small_int", "254", 254},
		{"medium_int", "65534", 65534},
		{"large_int", "9007199254740992", 1 << 53},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b BigInt
			require.NoError(t, b.UnmarshalJSON([]byte(tt.json)))
			assert.Equal(t, tt.expected, b.Int64())
		})
	}
}

// TestBigInt_UpgradeSubnetStatusJSON simulates reading a full SubnetStatus JSON
// object as stored in etcd by the old controller (float64 fields) and verifying
// it can be unmarshaled into the new struct (BigInt fields).
func TestBigInt_UpgradeSubnetStatusJSON(t *testing.T) {
	// This JSON represents what the old controller wrote to etcd:
	// v4availableIPs/v4usingIPs etc. were float64, serialized as JSON numbers.
	oldJSON := `{
		"v4availableIPs": 254,
		"v4usingIPs": 10,
		"v6availableIPs": 0,
		"v6usingIPs": 0
	}`

	type statusSlice struct {
		V4AvailableIPs BigInt `json:"v4availableIPs"`
		V4UsingIPs     BigInt `json:"v4usingIPs"`
		V6AvailableIPs BigInt `json:"v6availableIPs"`
		V6UsingIPs     BigInt `json:"v6usingIPs"`
	}

	var s statusSlice
	require.NoError(t, json.Unmarshal([]byte(oldJSON), &s))
	assert.True(t, s.V4AvailableIPs.EqualInt64(254))
	assert.True(t, s.V4UsingIPs.EqualInt64(10))
	assert.True(t, s.V6AvailableIPs.EqualInt64(0))
	assert.True(t, s.V6UsingIPs.EqualInt64(0))

	// Verify re-marshaling produces valid JSON that can round-trip
	data, err := json.Marshal(s)
	require.NoError(t, err)

	var s2 statusSlice
	require.NoError(t, json.Unmarshal(data, &s2))
	assert.True(t, s.V4AvailableIPs.Equal(s2.V4AvailableIPs))
	assert.True(t, s.V4UsingIPs.Equal(s2.V4UsingIPs))
	assert.True(t, s.V6AvailableIPs.Equal(s2.V6AvailableIPs))
	assert.True(t, s.V6UsingIPs.Equal(s2.V6UsingIPs))
}
