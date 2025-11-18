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
	n.FillBytes(b.Bytes())
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
	return []byte(b.String()), nil
}

func (b *BigInt) UnmarshalJSON(p []byte) error {
	if string(p) == "null" {
		return nil
	}
	var z big.Int
	_, ok := z.SetString(string(p), 10)
	if !ok {
		return fmt.Errorf("invalid big integer: %q", p)
	}
	b.Int = z
	return nil
}

func (b BigInt) EqualFloat64(f float64) bool {
	other := NewBigIntFromFloat(f)
	return b.Equal(other)
}

func (b BigInt) Float64() float64 {
	f, _ := new(big.Float).SetInt(&b.Int).Float64()
	return f
}

func NewBigInt(n int64) BigInt {
	return BigInt{*big.NewInt(n)}
}

func NewBigIntFromFloat(f float64) BigInt {
	i := new(big.Int)
	new(big.Float).SetFloat64(f).Int(i)
	return BigInt{*i}
}
