package types

import (
	"fmt"
	"math/big"
)

// +kubebuilder:validation:Type=string
type BigInt struct {
	big.Int `json:"-"`
}

func (b BigInt) DeepCopyInto(n *BigInt) {
	n.Set(&b.Int)
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

func (b BigInt) String() string {
	return b.Int.String()
}

func (b BigInt) MarshalJSON() ([]byte, error) {
	return b.Int.MarshalJSON()
}

func (b *BigInt) UnmarshalJSON(p []byte) error {
	if string(p) == "null" {
		return nil
	}
	// Remove quotes if present
	s := string(p)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	var z big.Int
	_, ok := z.SetString(s, 10)
	if !ok {
		return fmt.Errorf("invalid big integer: %q", p)
	}
	b.Int = z
	return nil
}

func (b BigInt) Float64() float64 {
	f, _ := new(big.Float).SetInt(&b.Int).Float64()
	return f
}

func NewBigInt(n int64) BigInt {
	return BigInt{*big.NewInt(n)}
}
