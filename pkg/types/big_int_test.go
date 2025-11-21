package types

import (
	"encoding/json"
	"math/big"
	"testing"
)

func TestBigIntMarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		value    BigInt
		expected string
	}{
		{
			name:     "zero",
			value:    NewBigInt(0),
			expected: `"0"`,
		},
		{
			name:     "positive small",
			value:    NewBigInt(253),
			expected: `"253"`,
		},
		{
			name:     "positive large",
			value:    NewBigInt(1<<62 - 1),
			expected: `"4611686018427387903"`,
		},
		{
			name:     "negative",
			value:    NewBigInt(-100),
			expected: `"-100"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatalf("MarshalJSON failed: %v", err)
			}
			if string(data) != tt.expected {
				t.Errorf("MarshalJSON() = %q, want %q", string(data), tt.expected)
			}
		})
	}
}

func TestBigIntUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected BigInt
		wantErr  bool
	}{
		{
			name:     "quoted zero",
			input:    `"0"`,
			expected: NewBigInt(0),
		},
		{
			name:     "quoted positive",
			input:    `"253"`,
			expected: NewBigInt(253),
		},
		{
			name:     "quoted large",
			input:    `"4611686018427387903"`,
			expected: NewBigInt(1<<62 - 1),
		},
		{
			name:     "quoted negative",
			input:    `"-100"`,
			expected: NewBigInt(-100),
		},
		{
			name:     "unquoted number (backward compat)",
			input:    `253`,
			expected: NewBigInt(253),
		},
		{
			name:    "invalid string",
			input:   `"abc"`,
			wantErr: true,
		},
		{
			name:     "null",
			input:    `null`,
			expected: NewBigInt(0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b BigInt
			err := json.Unmarshal([]byte(tt.input), &b)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("UnmarshalJSON() should have failed")
				}
				return
			}
			if err != nil {
				t.Fatalf("UnmarshalJSON() failed: %v", err)
			}
			if !b.Equal(tt.expected) {
				t.Errorf("UnmarshalJSON() = %v, want %v", b.String(), tt.expected.String())
			}
		})
	}
}

func TestBigIntRoundTrip(t *testing.T) {
	original := NewBigInt(123456789)

	// Marshal
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Unmarshal
	var decoded BigInt
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Verify
	if !decoded.Equal(original) {
		t.Errorf("Round trip failed: got %v, want %v", decoded.String(), original.String())
	}
}

func TestBigIntInStruct(t *testing.T) {
	type TestStruct struct {
		Count BigInt `json:"count"`
	}

	// Test marshal
	s := TestStruct{Count: NewBigInt(9999)}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	expected := `{"count":"9999"}`
	if string(data) != expected {
		t.Errorf("Marshal struct = %q, want %q", string(data), expected)
	}

	// Test unmarshal
	var decoded TestStruct
	if err := json.Unmarshal([]byte(expected), &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if !decoded.Count.Equal(s.Count) {
		t.Errorf("Unmarshal struct failed: got %v, want %v", decoded.Count.String(), s.Count.String())
	}
}

func TestBigIntArithmetic(t *testing.T) {
	a := NewBigInt(100)
	b := NewBigInt(50)

	// Test Add
	sum := a.Add(b)
	if !sum.EqualInt64(150) {
		t.Errorf("Add: got %v, want 150", sum.String())
	}

	// Test Sub
	diff := a.Sub(b)
	if !diff.EqualInt64(50) {
		t.Errorf("Sub: got %v, want 50", diff.String())
	}

	// Test original values unchanged
	if !a.EqualInt64(100) {
		t.Errorf("Add/Sub modified original: a = %v, want 100", a.String())
	}
	if !b.EqualInt64(50) {
		t.Errorf("Add/Sub modified original: b = %v, want 50", b.String())
	}
}

func TestBigIntComparison(t *testing.T) {
	tests := []struct {
		name    string
		a       BigInt
		b       BigInt
		wantCmp int
		wantEq  bool
	}{
		{
			name:    "equal",
			a:       NewBigInt(100),
			b:       NewBigInt(100),
			wantCmp: 0,
			wantEq:  true,
		},
		{
			name:    "less than",
			a:       NewBigInt(50),
			b:       NewBigInt(100),
			wantCmp: -1,
			wantEq:  false,
		},
		{
			name:    "greater than",
			a:       NewBigInt(200),
			b:       NewBigInt(100),
			wantCmp: 1,
			wantEq:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Cmp(tt.b); got != tt.wantCmp {
				t.Errorf("Cmp() = %v, want %v", got, tt.wantCmp)
			}
			if got := tt.a.Equal(tt.b); got != tt.wantEq {
				t.Errorf("Equal() = %v, want %v", got, tt.wantEq)
			}
		})
	}
}

func TestBigIntEqualInt64(t *testing.T) {
	b := NewBigInt(253)
	if !b.EqualInt64(253) {
		t.Errorf("EqualInt64(253) = false, want true")
	}
	if b.EqualInt64(100) {
		t.Errorf("EqualInt64(100) = true, want false")
	}
}

func TestBigIntDeepCopy(t *testing.T) {
	original := NewBigInt(12345)
	var copied BigInt
	original.DeepCopyInto(&copied)

	if !copied.Equal(original) {
		t.Errorf("DeepCopyInto failed: got %v, want %v", copied.String(), original.String())
	}

	// Modify copy, original should be unchanged
	copied = copied.Add(NewBigInt(1))
	if !original.EqualInt64(12345) {
		t.Errorf("DeepCopyInto created shallow copy: original = %v", original.String())
	}
	if !copied.EqualInt64(12346) {
		t.Errorf("Modified copy = %v, want 12346", copied.String())
	}
}

func TestBigIntFloat64(t *testing.T) {
	tests := []struct {
		name     string
		value    BigInt
		expected float64
	}{
		{
			name:     "zero",
			value:    NewBigInt(0),
			expected: 0.0,
		},
		{
			name:     "positive",
			value:    NewBigInt(253),
			expected: 253.0,
		},
		{
			name:     "negative",
			value:    NewBigInt(-100),
			expected: -100.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.Float64(); got != tt.expected {
				t.Errorf("Float64() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBigIntString(t *testing.T) {
	tests := []struct {
		value    BigInt
		expected string
	}{
		{NewBigInt(0), "0"},
		{NewBigInt(253), "253"},
		{NewBigInt(-100), "-100"},
		{BigInt{*big.NewInt(1 << 62)}, "4611686018427387904"},
	}

	for _, tt := range tests {
		if got := tt.value.String(); got != tt.expected {
			t.Errorf("String() = %q, want %q", got, tt.expected)
		}
	}
}
