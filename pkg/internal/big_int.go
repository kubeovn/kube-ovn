package internal

import (
	"fmt"
	"math/big"
)

// +kubebuilder:validation:Type=number
type BigInt struct {
	big.Int `json:"-"`
}

func NewBigInt(n int64) BigInt {
	return BigInt{*big.NewInt(n)}
}

func (b BigInt) DeepCopyInto(n *BigInt) {
	n.Set(&b.Int)
}

func (b BigInt) Clone() BigInt {
	var n BigInt
	n.Set(&b.Int)
	return n
}

func (b BigInt) AddInt(n int) BigInt {
	return BigInt{*new(big.Int).Add(&b.Int, big.NewInt(int64(n)))}
}

func (b BigInt) SubInt(n int) BigInt {
	return BigInt{*new(big.Int).Sub(&b.Int, big.NewInt(int64(n)))}
}

func (b BigInt) Equal(n BigInt) bool {
	return b.Cmp(n) == 0
}

func (b BigInt) EqualInt64(n int64) bool {
	return b.Int.Cmp(big.NewInt(n)) == 0
}

func (b BigInt) Cmp(n BigInt) int {
	return b.Int.Cmp(&n.Int)
}

func (b BigInt) Add(n BigInt) BigInt {
	return BigInt{*big.NewInt(0).Add(&b.Int, &n.Int)}
}

func (b BigInt) Sub(n BigInt) BigInt {
	return BigInt{*big.NewInt(0).Sub(&b.Int, &n.Int)}
}

// Float64 converts to float64 for Prometheus metrics export.
// Precision is lost for values exceeding 2^53 (e.g. large IPv6 subnets).
func (b BigInt) Float64() float64 {
	f, _ := new(big.Float).SetInt(&b.Int).Float64()
	return f
}

func (b BigInt) String() string {
	return b.Int.String()
}

func (b BigInt) MarshalJSON() ([]byte, error) {
	return []byte(b.String()), nil
}

func (b *BigInt) UnmarshalJSON(p []byte) error {
	if string(p) == "null" {
		return nil
	}

	// Remove quotes if present (support both "123" and 123 for compatibility)
	s := string(p)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}

	var z big.Int
	_, ok := z.SetString(s, 10)
	if !ok {
		var f big.Float
		if _, ok := f.SetString(s); ok {
			intVal, _ := f.Int(nil)
			if f.Cmp(new(big.Float).SetInt(intVal)) == 0 {
				b.Int = *intVal
				return nil
			}
		}
		return fmt.Errorf("invalid big integer: %q", p)
	}
	b.Int = z
	return nil
}
