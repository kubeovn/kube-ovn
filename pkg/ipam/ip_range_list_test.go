package ipam

import (
	"fmt"
	"math/rand/v2"
	"net"
	"slices"
	"strings"
	"testing"

	"github.com/scylladb/go-set/strset"
	"github.com/scylladb/go-set/u32set"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestNewIPRangeList(t *testing.T) {
	// test ipv4 contains
	v4RangeStart1, err := NewIP("10.0.0.5")
	require.NoError(t, err)
	v4RangeEnd1, err := NewIP("10.0.0.5")
	require.NoError(t, err)
	v4RangeStart2, err := NewIP("10.0.0.13")
	require.NoError(t, err)
	v4RangeEnd2, err := NewIP("10.0.0.18")
	require.NoError(t, err)

	v4, err := NewIPRangeList(v4RangeStart1)
	require.ErrorContains(t, err, "length of ips must be an even number")
	require.Nil(t, v4)

	v4, err = NewIPRangeList(v4RangeStart1, v4RangeEnd1, v4RangeStart2, v4RangeEnd2)
	require.NoError(t, err)

	fakeV4RangeItem1, err := NewIP("10.0.0.4")
	require.NoError(t, err)
	require.False(t, v4.Contains(fakeV4RangeItem1))
	require.True(t, v4.Contains(v4RangeStart1))
	fakeV4RangeItem2, err := NewIP("10.0.0.6")
	require.NoError(t, err)
	require.False(t, v4.Contains(fakeV4RangeItem2))
	fakeV4RangeItem3, err := NewIP("10.0.0.12")
	require.NoError(t, err)
	require.False(t, v4.Contains(fakeV4RangeItem3))
	require.True(t, v4.Contains(v4RangeStart2))
	realV4RangeItem1, err := NewIP("10.0.0.14")
	require.NoError(t, err)
	require.True(t, v4.Contains(realV4RangeItem1))
	realV4RangeItem2, err := NewIP("10.0.0.17")
	require.NoError(t, err)
	require.True(t, v4.Contains(realV4RangeItem2))
	realV4RangeItem3, err := NewIP("10.0.0.18")
	require.NoError(t, err)
	require.True(t, v4.Contains(realV4RangeItem3))
	fakeV4RangeItem4, err := NewIP("10.0.0.19")
	require.NoError(t, err)
	require.False(t, v4.Contains(fakeV4RangeItem4))
	// test ipv6 contains
	v6RangeStart1, err := NewIP("2001:db8::5")
	require.NoError(t, err)
	v6RangeEnd1, err := NewIP("2001:db8::5")
	require.NoError(t, err)
	v6RangeStart2, err := NewIP("2001:db8::13")
	require.NoError(t, err)
	v6RangeEnd2, err := NewIP("2001:db8::18")
	require.NoError(t, err)
	v6, err := NewIPRangeList(v6RangeStart1, v6RangeEnd1, v6RangeStart2, v6RangeEnd2)
	require.NoError(t, err)
	fakeV6RangeItem1, err := NewIP("2001:db8::4")
	require.NoError(t, err)
	require.False(t, v6.Contains(fakeV6RangeItem1))
	require.True(t, v6.Contains(v6RangeStart1))
	fakeV6RangeItem2, err := NewIP("2001:db8::6")
	require.NoError(t, err)
	require.False(t, v6.Contains(fakeV6RangeItem2))
	fakeV6RangeItem3, err := NewIP("2001:db8::12")
	require.NoError(t, err)
	require.False(t, v6.Contains(fakeV6RangeItem3))
	require.True(t, v6.Contains(v6RangeStart2))
	realV6RangeItem1, err := NewIP("2001:db8::14")
	require.NoError(t, err)
	require.True(t, v6.Contains(realV6RangeItem1))
	realV6RangeItem2, err := NewIP("2001:db8::17")
	require.NoError(t, err)
	require.True(t, v6.Contains(realV6RangeItem2))
	realV6RangeItem3, err := NewIP("2001:db8::18")
	require.NoError(t, err)
	require.True(t, v6.Contains(realV6RangeItem3))
	fakeV6RangeItem4, err := NewIP("2001:db8::19")
	require.NoError(t, err)
	require.False(t, v6.Contains(fakeV6RangeItem4))
	// test ipv4 add
	v4RangeStart1, err = NewIP("10.0.0.5")
	require.NoError(t, err)
	v4RangeEnd1, err = NewIP("10.0.0.5")
	require.NoError(t, err)
	v4RangeStart2, err = NewIP("10.0.0.13")
	require.NoError(t, err)
	v4RangeEnd2, err = NewIP("10.0.0.18")
	require.NoError(t, err)
	v4, err = NewIPRangeList(v4RangeStart1, v4RangeEnd1, v4RangeStart2, v4RangeEnd2)
	require.NoError(t, err)
	v4AddIP1, err := NewIP("10.0.0.4")
	require.NoError(t, err)
	require.True(t, v4.Add(v4AddIP1))
	// re add
	require.False(t, v4.Add(v4AddIP1))
	v4AddIP2, err := NewIP("10.0.0.5")
	require.NoError(t, err)
	require.False(t, v4.Add(v4AddIP2))
	v4AddIP3, err := NewIP("10.0.0.6")
	require.NoError(t, err)
	require.True(t, v4.Add(v4AddIP3))

	v4AddIP4, err := NewIP("10.0.0.12")
	require.NoError(t, err)
	require.True(t, v4.Add(v4AddIP4))
	v4AddIP5, err := NewIP("10.0.0.13")
	require.NoError(t, err)
	require.False(t, v4.Add(v4AddIP5))
	v4AddIP6, err := NewIP("10.0.0.14")
	require.NoError(t, err)
	require.False(t, v4.Add(v4AddIP6))
	v4AddIP7, err := NewIP("10.0.0.17")
	require.NoError(t, err)
	require.False(t, v4.Add(v4AddIP7))
	v4AddIP8, err := NewIP("10.0.0.18")
	require.NoError(t, err)
	require.False(t, v4.Add(v4AddIP8))
	v4AddIP9, err := NewIP("10.0.0.19")
	require.NoError(t, err)
	require.True(t, v4.Add(v4AddIP9))

	v4AddExpectRangeStart1, err := NewIP("10.0.0.4")
	require.NoError(t, err)
	v4AddExpectRangeEnd1, err := NewIP("10.0.0.6")
	require.NoError(t, err)
	v4AddExpectRangeStart2, err := NewIP("10.0.0.12")
	require.NoError(t, err)
	v4AddExpectRangeEnd2, err := NewIP("10.0.0.19")
	require.NoError(t, err)
	v4AddExpect, err := NewIPRangeList(v4AddExpectRangeStart1, v4AddExpectRangeEnd1, v4AddExpectRangeStart2, v4AddExpectRangeEnd2)
	require.NoError(t, err)

	require.True(t, v4.Equal(v4AddExpect))
	// test ipv6 add
	v6RangeStart1, err = NewIP("2001:db8::5")
	require.NoError(t, err)
	v6RangeEnd1, err = NewIP("2001:db8::5")
	require.NoError(t, err)
	v6RangeStart2, err = NewIP("2001:db8::13")
	require.NoError(t, err)
	v6RangeEnd2, err = NewIP("2001:db8::18")
	require.NoError(t, err)
	v6, err = NewIPRangeList(v6RangeStart1, v6RangeEnd1, v6RangeStart2, v6RangeEnd2)
	require.NoError(t, err)
	v6AddIP1, err := NewIP("2001:db8::4")
	require.NoError(t, err)
	require.True(t, v6.Add(v6AddIP1))
	// re add
	require.False(t, v6.Add(v6AddIP1))
	v6AddIP2, err := NewIP("2001:db8::5")
	require.NoError(t, err)
	require.False(t, v6.Add(v6AddIP2))
	v6AddIP3, err := NewIP("2001:db8::6")
	require.NoError(t, err)
	require.True(t, v6.Add(v6AddIP3))

	v6AddIP4, err := NewIP("2001:db8::12")
	require.NoError(t, err)
	require.True(t, v6.Add(v6AddIP4))
	v6AddIP5, err := NewIP("2001:db8::13")
	require.NoError(t, err)
	require.False(t, v6.Add(v6AddIP5))
	v6AddIP6, err := NewIP("2001:db8::14")
	require.NoError(t, err)
	require.False(t, v6.Add(v6AddIP6))
	v6AddIP7, err := NewIP("2001:db8::17")
	require.NoError(t, err)
	require.False(t, v6.Add(v6AddIP7))
	v6AddIP8, err := NewIP("2001:db8::18")
	require.NoError(t, err)
	require.False(t, v6.Add(v6AddIP8))
	v6AddIP9, err := NewIP("2001:db8::19")
	require.NoError(t, err)
	require.True(t, v6.Add(v6AddIP9))
	v6AddExpectRangeStart1, err := NewIP("2001:db8::4")
	require.NoError(t, err)
	v6AddExpectRangeEnd1, err := NewIP("2001:db8::6")
	require.NoError(t, err)
	v6AddExpectRangeStart2, err := NewIP("2001:db8::12")
	require.NoError(t, err)
	v6AddExpectRangeEnd2, err := NewIP("2001:db8::19")
	require.NoError(t, err)
	v6AddExpect, err := NewIPRangeList(v6AddExpectRangeStart1, v6AddExpectRangeEnd1, v6AddExpectRangeStart2, v6AddExpectRangeEnd2)
	require.NoError(t, err)

	require.True(t, v6.Equal(v6AddExpect))
	// test ipv4 remove
	v4RangeStart1, err = NewIP("10.0.0.5")
	require.NoError(t, err)
	v4RangeEnd1, err = NewIP("10.0.0.5")
	require.NoError(t, err)
	v4RangeStart2, err = NewIP("10.0.0.13")
	require.NoError(t, err)
	v4RangeEnd2, err = NewIP("10.0.0.18")
	require.NoError(t, err)
	v4, err = NewIPRangeList(v4RangeStart1, v4RangeEnd1, v4RangeStart2, v4RangeEnd2)
	require.NoError(t, err)
	v4RemoveIP1, err := NewIP("10.0.0.4")
	require.NoError(t, err)
	require.False(t, v4.Remove(v4RemoveIP1))
	v4RemoveIP2, err := NewIP("10.0.0.5")
	require.NoError(t, err)
	require.True(t, v4.Remove(v4RemoveIP2))
	v4RemoveIP3, err := NewIP("10.0.0.6")
	require.NoError(t, err)
	require.False(t, v4.Remove(v4RemoveIP3))

	v4RemoveIP4, err := NewIP("10.0.0.12")
	require.NoError(t, err)
	require.False(t, v4.Remove(v4RemoveIP4))
	v4RemoveIP5, err := NewIP("10.0.0.13")
	require.NoError(t, err)
	require.True(t, v4.Remove(v4RemoveIP5))
	v4RemoveIP6, err := NewIP("10.0.0.14")
	require.NoError(t, err)
	require.True(t, v4.Remove(v4RemoveIP6))
	v4RemoveIP7, err := NewIP("10.0.0.17")
	require.NoError(t, err)
	require.True(t, v4.Remove(v4RemoveIP7))
	v4RemoveIP8, err := NewIP("10.0.0.18")
	require.NoError(t, err)
	require.True(t, v4.Remove(v4RemoveIP8))
	v4RemoveIP9, err := NewIP("10.0.0.19")
	require.NoError(t, err)
	require.False(t, v4.Remove(v4RemoveIP9))
	v4RemoveExpectRangeStart1, err := NewIP("10.0.0.15")
	require.NoError(t, err)
	v4RemoveExpectRangeEnd1, err := NewIP("10.0.0.16")
	require.NoError(t, err)
	v4RemoveExpect, err := NewIPRangeList(v4RemoveExpectRangeStart1, v4RemoveExpectRangeEnd1)
	require.NoError(t, err)

	require.True(t, v4.Equal(v4RemoveExpect))
	// split the range
	v4RangeStart1, err = NewIP("10.0.0.10")
	require.NoError(t, err)
	v4RangeEnd1, err = NewIP("10.0.0.20")
	require.NoError(t, err)
	v4, err = NewIPRangeList(v4RangeStart1, v4RangeEnd1)
	require.NoError(t, err)
	v4RemoveIP1, err = NewIP("10.0.0.15")
	require.NoError(t, err)
	require.True(t, v4.Remove(v4RemoveIP1))
	v4SplitedExpectRangeStart1, err := NewIP("10.0.0.10")
	require.NoError(t, err)
	v4SplitedExpectRangeEnd1, err := NewIP("10.0.0.14")
	require.NoError(t, err)
	v4SplitedExpectRangeStart2, err := NewIP("10.0.0.16")
	require.NoError(t, err)
	v4SplitedExpectRangeEnd2, err := NewIP("10.0.0.20")
	require.NoError(t, err)
	v4SplitedExpect, err := NewIPRangeList(v4SplitedExpectRangeStart1, v4SplitedExpectRangeEnd1, v4SplitedExpectRangeStart2, v4SplitedExpectRangeEnd2)
	require.NoError(t, err)
	require.True(t, v4.Equal(v4SplitedExpect))

	// test ipv6 remove
	v6RangeStart1, err = NewIP("2001:db8::5")
	require.NoError(t, err)
	v6RangeEnd1, err = NewIP("2001:db8::5")
	require.NoError(t, err)
	v6RangeStart2, err = NewIP("2001:db8::13")
	require.NoError(t, err)
	v6RangeEnd2, err = NewIP("2001:db8::18")
	require.NoError(t, err)
	v6, err = NewIPRangeList(v6RangeStart1, v6RangeEnd1, v6RangeStart2, v6RangeEnd2)
	require.NoError(t, err)
	v6RemoveIP1, err := NewIP("2001:db8::4")
	require.NoError(t, err)
	require.False(t, v6.Remove(v6RemoveIP1))
	v6RemoveIP2, err := NewIP("2001:db8::5")
	require.NoError(t, err)
	require.True(t, v6.Remove(v6RemoveIP2))
	v6RemoveIP3, err := NewIP("2001:db8::6")
	require.NoError(t, err)
	require.False(t, v6.Remove(v6RemoveIP3))

	v6RemoveIP4, err := NewIP("2001:db8::12")
	require.NoError(t, err)
	require.False(t, v6.Remove(v6RemoveIP4))
	v6RemoveIP5, err := NewIP("2001:db8::13")
	require.NoError(t, err)
	require.True(t, v6.Remove(v6RemoveIP5))
	v6RemoveIP6, err := NewIP("2001:db8::14")
	require.NoError(t, err)
	require.True(t, v6.Remove(v6RemoveIP6))
	v6RemoveIP7, err := NewIP("2001:db8::17")
	require.NoError(t, err)
	require.True(t, v6.Remove(v6RemoveIP7))
	v6RemoveIP8, err := NewIP("2001:db8::18")
	require.NoError(t, err)
	require.True(t, v6.Remove(v6RemoveIP8))
	v6RemoveIP9, err := NewIP("2001:db8::19")
	require.NoError(t, err)
	require.False(t, v6.Remove(v6RemoveIP9))

	v6RemoveExpectRangeStart1, err := NewIP("2001:db8::15")
	require.NoError(t, err)
	v6RemoveExpectRangeEnd1, err := NewIP("2001:db8::16")
	require.NoError(t, err)
	v6RemoveExpect, err := NewIPRangeList(v6RemoveExpectRangeStart1, v6RemoveExpectRangeEnd1)
	require.NoError(t, err)

	require.True(t, v6.Equal(v6RemoveExpect))
	// split the range
	v6RangeStart1, err = NewIP("2001:db8::10")
	require.NoError(t, err)
	v6RangeEnd1, err = NewIP("2001:db8::20")
	require.NoError(t, err)
	v6, err = NewIPRangeList(v6RangeStart1, v6RangeEnd1)
	require.NoError(t, err)
	v6RemoveIP1, err = NewIP("2001:db8::15")
	require.NoError(t, err)
	require.True(t, v6.Remove(v6RemoveIP1))
	v6SplitedExpectRangeStart1, err := NewIP("2001:db8::10")
	require.NoError(t, err)
	v6SplitedExpectRangeEnd1, err := NewIP("2001:db8::14")
	require.NoError(t, err)
	v6SplitedExpectRangeStart2, err := NewIP("2001:db8::16")
	require.NoError(t, err)
	v6SplitedExpectRangeEnd2, err := NewIP("2001:db8::20")
	require.NoError(t, err)
	v6SplitedExpect, err := NewIPRangeList(v6SplitedExpectRangeStart1, v6SplitedExpectRangeEnd1, v6SplitedExpectRangeStart2, v6SplitedExpectRangeEnd2)
	require.NoError(t, err)
	require.True(t, v6.Equal(v6SplitedExpect))

	// test ipv4 separate
	v41RangeStart1, err := NewIP("10.0.0.1")
	require.NoError(t, err)
	v41RangeEnd1, err := NewIP("10.0.0.1")
	require.NoError(t, err)
	v41RangeStart2, err := NewIP("10.0.0.5")
	require.NoError(t, err)
	v41RangeEnd2, err := NewIP("10.0.0.5")
	require.NoError(t, err)
	v41RangeStart3, err := NewIP("10.0.0.13")
	require.NoError(t, err)
	v41RangeEnd3, err := NewIP("10.0.0.18")
	require.NoError(t, err)
	v41RangeStart4, err := NewIP("10.0.0.23")
	require.NoError(t, err)
	v41RangeEnd4, err := NewIP("10.0.0.28")
	require.NoError(t, err)
	v41RangeStart5, err := NewIP("10.0.0.33")
	require.NoError(t, err)
	v41RangeEnd5, err := NewIP("10.0.0.38")
	require.NoError(t, err)
	v41RangeStart6, err := NewIP("10.0.0.43")
	require.NoError(t, err)
	v41RangeEnd6, err := NewIP("10.0.0.48")
	require.NoError(t, err)
	v41RangeStart7, err := NewIP("10.0.0.53")
	require.NoError(t, err)
	v41RangeEnd7, err := NewIP("10.0.0.58")
	require.NoError(t, err)
	v41RangeStart8, err := NewIP("10.0.0.63")
	require.NoError(t, err)
	v41RangeEnd8, err := NewIP("10.0.0.68")
	require.NoError(t, err)
	v41, err := NewIPRangeList(
		v41RangeStart1, v41RangeEnd1, v41RangeStart2, v41RangeEnd2,
		v41RangeStart3, v41RangeEnd3, v41RangeStart4, v41RangeEnd4,
		v41RangeStart5, v41RangeEnd5, v41RangeStart6, v41RangeEnd6,
		v41RangeStart7, v41RangeEnd7, v41RangeStart8, v41RangeEnd8)
	require.NoError(t, err)

	v42RangeStart1, err := NewIP("10.0.0.1")
	require.NoError(t, err)
	v42RangeEnd1, err := NewIP("10.0.0.1")
	require.NoError(t, err)
	v42RangeStart2, err := NewIP("10.0.0.11")
	require.NoError(t, err)
	v42RangeEnd2, err := NewIP("10.0.0.15")
	require.NoError(t, err)
	v42RangeStart3, err := NewIP("10.0.0.17")
	require.NoError(t, err)
	v42RangeEnd3, err := NewIP("10.0.0.19")
	require.NoError(t, err)
	v42RangeStart4, err := NewIP("10.0.0.23")
	require.NoError(t, err)
	v42RangeEnd4, err := NewIP("10.0.0.25")
	require.NoError(t, err)
	v42RangeStart5, err := NewIP("10.0.0.27")
	require.NoError(t, err)
	v42RangeEnd5, err := NewIP("10.0.0.28")
	require.NoError(t, err)
	v42RangeStart6, err := NewIP("10.0.0.33")
	require.NoError(t, err)
	v42RangeEnd6, err := NewIP("10.0.0.38")
	require.NoError(t, err)
	v42RangeStart7, err := NewIP("10.0.0.42")
	require.NoError(t, err)
	v42RangeEnd7, err := NewIP("10.0.0.49")
	require.NoError(t, err)
	v42RangeStart8, err := NewIP("10.0.0.53")
	require.NoError(t, err)
	v42RangeEnd8, err := NewIP("10.0.0.58")
	require.NoError(t, err)

	v42, err := NewIPRangeList(
		v42RangeStart1, v42RangeEnd1, v42RangeStart2, v42RangeEnd2,
		v42RangeStart3, v42RangeEnd3, v42RangeStart4, v42RangeEnd4,
		v42RangeStart5, v42RangeEnd5, v42RangeStart6, v42RangeEnd6,
		v42RangeStart7, v42RangeEnd7, v42RangeStart8, v42RangeEnd8)
	require.NoError(t, err)

	v43RangeStart1, err := NewIP("10.0.0.5")
	require.NoError(t, err)
	v43RangeEnd1, err := NewIP("10.0.0.5")
	require.NoError(t, err)
	v43RangeStart2, err := NewIP("10.0.0.16")
	require.NoError(t, err)
	v43RangeEnd2, err := NewIP("10.0.0.16")
	require.NoError(t, err)
	v43RangeStart3, err := NewIP("10.0.0.26")
	require.NoError(t, err)
	v43RangeEnd3, err := NewIP("10.0.0.26")
	require.NoError(t, err)
	v43RangeStart4, err := NewIP("10.0.0.63")
	require.NoError(t, err)
	v43RangeEnd4, err := NewIP("10.0.0.68")
	require.NoError(t, err)

	expected, err := NewIPRangeList(v43RangeStart1, v43RangeEnd1, v43RangeStart2, v43RangeEnd2,
		v43RangeStart3, v43RangeEnd3, v43RangeStart4, v43RangeEnd4)
	require.NoError(t, err)
	separated := v41.Separate(v42)
	require.True(t, separated.Equal(expected))

	// test ipv6 separate
	v61RangeStart1, err := NewIP("2001:db8::1")
	require.NoError(t, err)
	v61RangeEnd1, err := NewIP("2001:db8::1")
	require.NoError(t, err)
	v61RangeStart2, err := NewIP("2001:db8::5")
	require.NoError(t, err)
	v61RangeEnd2, err := NewIP("2001:db8::5")
	require.NoError(t, err)
	v61RangeStart3, err := NewIP("2001:db8::13")
	require.NoError(t, err)
	v61RangeEnd3, err := NewIP("2001:db8::18")
	require.NoError(t, err)
	v61RangeStart4, err := NewIP("2001:db8::23")
	require.NoError(t, err)
	v61RangeEnd4, err := NewIP("2001:db8::28")
	require.NoError(t, err)
	v61RangeStart5, err := NewIP("2001:db8::33")
	require.NoError(t, err)
	v61RangeEnd5, err := NewIP("2001:db8::38")
	require.NoError(t, err)
	v61RangeStart6, err := NewIP("2001:db8::43")
	require.NoError(t, err)
	v61RangeEnd6, err := NewIP("2001:db8::48")
	require.NoError(t, err)
	v61RangeStart7, err := NewIP("2001:db8::53")
	require.NoError(t, err)
	v61RangeEnd7, err := NewIP("2001:db8::58")
	require.NoError(t, err)
	v61RangeStart8, err := NewIP("2001:db8::63")
	require.NoError(t, err)
	v61RangeEnd8, err := NewIP("2001:db8::68")
	require.NoError(t, err)
	v61, err := NewIPRangeList(
		v61RangeStart1, v61RangeEnd1, v61RangeStart2, v61RangeEnd2,
		v61RangeStart3, v61RangeEnd3, v61RangeStart4, v61RangeEnd4,
		v61RangeStart5, v61RangeEnd5, v61RangeStart6, v61RangeEnd6,
		v61RangeStart7, v61RangeEnd7, v61RangeStart8, v61RangeEnd8)
	require.NoError(t, err)
	v62RangeStart1, err := NewIP("2001:db8::1")
	require.NoError(t, err)
	v62RangeEnd1, err := NewIP("2001:db8::1")
	require.NoError(t, err)
	v62RangeStart2, err := NewIP("2001:db8::11")
	require.NoError(t, err)
	v62RangeEnd2, err := NewIP("2001:db8::15")
	require.NoError(t, err)
	v62RangeStart3, err := NewIP("2001:db8::17")
	require.NoError(t, err)
	v62RangeEnd3, err := NewIP("2001:db8::19")
	require.NoError(t, err)
	v62RangeStart4, err := NewIP("2001:db8::23")
	require.NoError(t, err)
	v62RangeEnd4, err := NewIP("2001:db8::25")
	require.NoError(t, err)
	v62RangeStart5, err := NewIP("2001:db8::27")
	require.NoError(t, err)
	v62RangeEnd5, err := NewIP("2001:db8::28")
	require.NoError(t, err)
	v62RangeStart6, err := NewIP("2001:db8::33")
	require.NoError(t, err)
	v62RangeEnd6, err := NewIP("2001:db8::38")
	require.NoError(t, err)
	v62RangeStart7, err := NewIP("2001:db8::42")
	require.NoError(t, err)
	v62RangeEnd7, err := NewIP("2001:db8::49")
	require.NoError(t, err)
	v62RangeStart8, err := NewIP("2001:db8::53")
	require.NoError(t, err)
	v62RangeEnd8, err := NewIP("2001:db8::58")
	require.NoError(t, err)

	v62, err := NewIPRangeList(
		v62RangeStart1, v62RangeEnd1, v62RangeStart2, v62RangeEnd2,
		v62RangeStart3, v62RangeEnd3, v62RangeStart4, v62RangeEnd4,
		v62RangeStart5, v62RangeEnd5, v62RangeStart6, v62RangeEnd6,
		v62RangeStart7, v62RangeEnd7, v62RangeStart8, v62RangeEnd8)
	require.NoError(t, err)

	v63RangeStart1, err := NewIP("2001:db8::5")
	require.NoError(t, err)
	v63RangeEnd1, err := NewIP("2001:db8::5")
	require.NoError(t, err)
	v63RangeStart2, err := NewIP("2001:db8::16")
	require.NoError(t, err)
	v63RangeEnd2, err := NewIP("2001:db8::16")
	require.NoError(t, err)
	v63RangeStart3, err := NewIP("2001:db8::26")
	require.NoError(t, err)
	v63RangeEnd3, err := NewIP("2001:db8::26")
	require.NoError(t, err)
	v63RangeStart4, err := NewIP("2001:db8::63")
	require.NoError(t, err)
	v63RangeEnd4, err := NewIP("2001:db8::68")
	require.NoError(t, err)
	expected, err = NewIPRangeList(
		v63RangeStart1, v63RangeEnd1, v63RangeStart2, v63RangeEnd2,
		v63RangeStart3, v63RangeEnd3, v63RangeStart4, v63RangeEnd4)
	require.NoError(t, err)
	separated = v61.Separate(v62)
	require.True(t, separated.Equal(expected))

	// test ipv4 merge
	v41RangeStart1, err = NewIP("10.0.0.1")
	require.NoError(t, err)
	v41RangeEnd1, err = NewIP("10.0.0.1")
	require.NoError(t, err)
	v41RangeStart2, err = NewIP("10.0.0.3")
	require.NoError(t, err)
	v41RangeEnd2, err = NewIP("10.0.0.3")
	require.NoError(t, err)
	v41RangeStart3, err = NewIP("10.0.0.5")
	require.NoError(t, err)
	v41RangeEnd3, err = NewIP("10.0.0.5")
	require.NoError(t, err)
	v41RangeStart4, err = NewIP("10.0.0.13")
	require.NoError(t, err)
	v41RangeEnd4, err = NewIP("10.0.0.18")
	require.NoError(t, err)
	v41RangeStart5, err = NewIP("10.0.0.23")
	require.NoError(t, err)
	v41RangeEnd5, err = NewIP("10.0.0.28")
	require.NoError(t, err)
	v41RangeStart6, err = NewIP("10.0.0.33")
	require.NoError(t, err)
	v41RangeEnd6, err = NewIP("10.0.0.38")
	require.NoError(t, err)
	v41RangeStart7, err = NewIP("10.0.0.43")
	require.NoError(t, err)
	v41RangeEnd7, err = NewIP("10.0.0.48")
	require.NoError(t, err)
	v41RangeStart8, err = NewIP("10.0.0.53")
	require.NoError(t, err)
	v41RangeEnd8, err = NewIP("10.0.0.58")
	require.NoError(t, err)
	v41RangeStart9, err := NewIP("10.0.0.63")
	require.NoError(t, err)
	v41RangeEnd9, err := NewIP("10.0.0.68")
	require.NoError(t, err)
	v41RangeStart10, err := NewIP("10.0.0.73")
	require.NoError(t, err)
	v41RangeEnd10, err := NewIP("10.0.0.78")
	require.NoError(t, err)
	v41RangeStart11, err := NewIP("10.0.0.83")
	require.NoError(t, err)
	v41RangeEnd11, err := NewIP("10.0.0.88")
	require.NoError(t, err)
	v41RangeStart12, err := NewIP("10.0.0.93")
	require.NoError(t, err)
	v41RangeEnd12, err := NewIP("10.0.0.95")
	require.NoError(t, err)

	v41, err = NewIPRangeList(
		v41RangeStart1, v41RangeEnd1, v41RangeStart2, v41RangeEnd2,
		v41RangeStart3, v41RangeEnd3, v41RangeStart4, v41RangeEnd4,
		v41RangeStart5, v41RangeEnd5, v41RangeStart6, v41RangeEnd6,
		v41RangeStart7, v41RangeEnd7, v41RangeStart8, v41RangeEnd8,
		v41RangeStart9, v41RangeEnd9, v41RangeStart10, v41RangeEnd10,
		v41RangeStart11, v41RangeEnd11, v41RangeStart12, v41RangeEnd12)
	require.NoError(t, err)

	v42RangeStart1, err = NewIP("10.0.0.1")
	require.NoError(t, err)
	v42RangeEnd1, err = NewIP("10.0.0.1")
	require.NoError(t, err)
	v42RangeStart2, err = NewIP("10.0.0.4")
	require.NoError(t, err)
	v42RangeEnd2, err = NewIP("10.0.0.4")
	require.NoError(t, err)
	v42RangeStart3, err = NewIP("10.0.0.11")
	require.NoError(t, err)
	v42RangeEnd3, err = NewIP("10.0.0.15")
	require.NoError(t, err)
	v42RangeStart4, err = NewIP("10.0.0.17")
	require.NoError(t, err)
	v42RangeEnd4, err = NewIP("10.0.0.19")
	require.NoError(t, err)
	v42RangeStart5, err = NewIP("10.0.0.23")
	require.NoError(t, err)
	v42RangeEnd5, err = NewIP("10.0.0.25")
	require.NoError(t, err)
	v42RangeStart6, err = NewIP("10.0.0.27")
	require.NoError(t, err)
	v42RangeEnd6, err = NewIP("10.0.0.28")
	require.NoError(t, err)
	v42RangeStart7, err = NewIP("10.0.0.33")
	require.NoError(t, err)
	v42RangeEnd7, err = NewIP("10.0.0.38")
	require.NoError(t, err)
	v42RangeStart8, err = NewIP("10.0.0.42")
	require.NoError(t, err)
	v42RangeEnd8, err = NewIP("10.0.0.49")
	require.NoError(t, err)
	v42RangeStart9, err := NewIP("10.0.0.53")
	require.NoError(t, err)
	v42RangeEnd9, err := NewIP("10.0.0.58")
	require.NoError(t, err)
	v42RangeStart10, err := NewIP("10.0.0.75")
	require.NoError(t, err)
	v42RangeEnd10, err := NewIP("10.0.0.85")
	require.NoError(t, err)
	v42RangeStart11, err := NewIP("10.0.0.96")
	require.NoError(t, err)
	v42RangeEnd11, err := NewIP("10.0.0.98")
	require.NoError(t, err)

	v42, err = NewIPRangeList(
		v42RangeStart1, v42RangeEnd1, v42RangeStart2, v42RangeEnd2,
		v42RangeStart3, v42RangeEnd3, v42RangeStart4, v42RangeEnd4,
		v42RangeStart5, v42RangeEnd5, v42RangeStart6, v42RangeEnd6,
		v42RangeStart7, v42RangeEnd7, v42RangeStart8, v42RangeEnd8,
		v42RangeStart9, v42RangeEnd9, v42RangeStart10, v42RangeEnd10,
		v42RangeStart11, v42RangeEnd11)
	require.NoError(t, err)

	v43RangeStart1, err = NewIP("10.0.0.1")
	require.NoError(t, err)
	v43RangeEnd1, err = NewIP("10.0.0.1")
	require.NoError(t, err)
	v43RangeStart2, err = NewIP("10.0.0.3")
	require.NoError(t, err)
	v43RangeEnd2, err = NewIP("10.0.0.5")
	require.NoError(t, err)
	v43RangeStart3, err = NewIP("10.0.0.11")
	require.NoError(t, err)
	v43RangeEnd3, err = NewIP("10.0.0.19")
	require.NoError(t, err)
	v43RangeStart4, err = NewIP("10.0.0.23")
	require.NoError(t, err)
	v43RangeEnd4, err = NewIP("10.0.0.28")
	require.NoError(t, err)
	v43RangeStart5, err := NewIP("10.0.0.33")
	require.NoError(t, err)
	v43RangeEnd5, err := NewIP("10.0.0.38")
	require.NoError(t, err)
	v43RangeStart6, err := NewIP("10.0.0.42")
	require.NoError(t, err)
	v43RangeEnd6, err := NewIP("10.0.0.49")
	require.NoError(t, err)
	v43RangeStart7, err := NewIP("10.0.0.53")
	require.NoError(t, err)
	v43RangeEnd7, err := NewIP("10.0.0.58")
	require.NoError(t, err)
	v43RangeStart8, err := NewIP("10.0.0.63")
	require.NoError(t, err)
	v43RangeEnd8, err := NewIP("10.0.0.68")
	require.NoError(t, err)
	v43RangeStart9, err := NewIP("10.0.0.73")
	require.NoError(t, err)
	v43RangeEnd9, err := NewIP("10.0.0.88")
	require.NoError(t, err)
	v43RangeStart10, err := NewIP("10.0.0.93")
	require.NoError(t, err)
	v43RangeEnd10, err := NewIP("10.0.0.98")
	require.NoError(t, err)

	expected, err = NewIPRangeList(v43RangeStart1, v43RangeEnd1, v43RangeStart2, v43RangeEnd2,
		v43RangeStart3, v43RangeEnd3, v43RangeStart4, v43RangeEnd4,
		v43RangeStart5, v43RangeEnd5, v43RangeStart6, v43RangeEnd6,
		v43RangeStart7, v43RangeEnd7, v43RangeStart8, v43RangeEnd8,
		v43RangeStart9, v43RangeEnd9, v43RangeStart10, v43RangeEnd10)

	require.NoError(t, err)
	merged := v41.Merge(v42)
	require.True(t, merged.Equal(expected))
}

func TestNewIPRangeListFrom(t *testing.T) {
	n := 40 + rand.IntN(20)
	cidrList := make([]*net.IPNet, 0, n)
	cidrSet := u32set.NewWithSize(n * 2)
	for len(cidrList) != cap(cidrList) {
		_, cidr, err := net.ParseCIDR(fmt.Sprintf("%s/%d", util.Uint32ToIPv4(rand.Uint32()), 16+rand.IntN(16)))
		require.NoError(t, err)

		var invalid bool
		for _, c := range cidrList {
			if c.Contains(cidr.IP) || cidr.Contains(c.IP) {
				invalid = true
				break
			}
		}
		if !invalid {
			cidrList = append(cidrList, cidr)
			cidrSet.Add(util.IPv4ToUint32(cidr.IP))
			bcast := make(net.IP, len(cidr.IP))
			for i := range bcast {
				bcast[i] = cidr.IP[i] | ^cidr.Mask[i]
			}
			cidrSet.Add(util.IPv4ToUint32(bcast))
		}
	}

	n = 80 + rand.IntN(40)
	set := u32set.NewWithSize(cidrSet.Size() + n)
	for set.Size() != n {
		v := rand.Uint32()
		ip := net.ParseIP(util.Uint32ToIPv4(v))
		var invalid bool
		for _, cidr := range cidrList {
			if cidr.Contains(ip) {
				invalid = true
				break
			}
		}
		if !invalid {
			set.Add(v)
		}
	}
	set.Merge(cidrSet)

	ints := set.List()
	slices.Sort(ints)

	ips := make([]string, 0, len(cidrList)+set.Size())
	mergedInts := make([]uint32, 0, set.Size()*2)
	var expectedCount uint32
	for i := 0; i < len(ints); i++ {
		if cidrSet.Has(ints[i]) {
			expectedCount += ints[i+1] - ints[i] + 1
			if i != 0 && ints[i] == ints[i-1]+1 {
				mergedInts[len(mergedInts)-1] = ints[i+1]
			} else {
				mergedInts = append(mergedInts, ints[i], ints[i+1])
			}
			i++
			continue
		}

		start := util.Uint32ToIPv4(ints[i])
		if cidrSet.Has(ints[i]) || (rand.Int()%2 == 0 && i+1 != len(ints) && !cidrSet.Has(ints[i+1])) {
			if !cidrSet.Has(ints[i]) {
				end := util.Uint32ToIPv4(ints[i+1])
				ips = append(ips, fmt.Sprintf("%s..%s", start, end))
			}
			if i != 0 && ints[i] == ints[i-1]+1 {
				mergedInts[len(mergedInts)-1] = ints[i+1]
			} else {
				mergedInts = append(mergedInts, ints[i], ints[i+1])
			}
			expectedCount += ints[i+1] - ints[i] + 1
			i++
		} else {
			if rand.Int()%8 == 0 {
				start += "/32"
			}
			ips = append(ips, start)
			if i != 0 && ints[i] == ints[i-1]+1 {
				mergedInts[len(mergedInts)-1] = ints[i]
			} else {
				mergedInts = append(mergedInts, ints[i], ints[i])
			}
			expectedCount++
		}
	}

	for _, cidr := range cidrList {
		ips = append(ips, cidr.String())
	}

	mergedIPs := make([]string, len(mergedInts)/2)
	for i := range len(mergedInts) / 2 {
		if mergedInts[i*2] == mergedInts[i*2+1] {
			mergedIPs[i] = util.Uint32ToIPv4(mergedInts[i*2])
		} else {
			mergedIPs[i] = fmt.Sprintf("%s-%s", util.Uint32ToIPv4(mergedInts[i*2]), util.Uint32ToIPv4(mergedInts[i*2+1]))
		}
	}

	ipList, err := NewIPRangeListFrom(strset.New(ips...).List()...)
	require.NoError(t, err)
	require.Equal(t, ipList.Len(), len(mergedIPs))
	require.Equal(t, ipList.String(), strings.Join(mergedIPs, ","))

	count := ipList.Count()
	require.Equal(t, count.Int64(), int64(expectedCount))

	for _, s := range mergedIPs {
		fields := strings.Split(s, "-")
		start, err := NewIP(fields[0])
		require.NoError(t, err)
		require.True(t, ipList.Contains(start))

		end := start
		if len(fields) != 1 {
			end, err = NewIP(fields[1])
			require.NoError(t, err)
			require.True(t, ipList.Contains(end))
		}

		if start.String() != "0.0.0.0" {
			require.False(t, ipList.Contains(start.Sub(1)))
		}
		if end.String() != "255.255.255.255" {
			require.False(t, ipList.Contains(end.Add(1)))
		}

		if !start.Equal(end) {
			require.True(t, ipList.Contains(start.Add(1)))
			require.True(t, ipList.Contains(end.Sub(1)))
		}
	}

	ipList, err = NewIPRangeListFrom("192.168.1.2..192.168.1.1")
	require.ErrorContains(t, err, "invalid ip range \"192.168.1.2..192.168.1.1\": 192.168.1.2 is greater than 192.168.1.1")
	require.Nil(t, ipList)

	ipList, err = NewIPRangeListFrom("invalidIP..192.168.1.1")
	require.ErrorContains(t, err, "invalid IP address")
	require.Nil(t, ipList)

	ipList, err = NewIPRangeListFrom("192.168.1.2..invalidIP")
	require.ErrorContains(t, err, "invalid IP address")
	require.Nil(t, ipList)

	ipList, err = NewIPRangeListFrom("invalidCIDR/24")
	require.ErrorContains(t, err, "invalid CIDR address: invalidCIDR/24")
	require.Nil(t, ipList)
}

func TestRemove(t *testing.T) {
	// ipv4
	v4Cidr := "10.0.0.0/24"
	_, v4IPNet, err := net.ParseCIDR(v4Cidr)
	require.NoError(t, err)
	v4IPRange := NewIPRangeFromCIDR(*v4IPNet)
	require.Equal(t, v4IPRange.start.String(), "10.0.0.0")
	require.Equal(t, v4IPRange.end.String(), "10.0.0.255")
	v4Start, v4End := v4IPRange.Start(), v4IPRange.End()
	removed, ok := v4IPRange.Remove(v4Start)
	require.True(t, ok)
	require.Equal(t, removed[0].start.String(), "10.0.0.1")
	removed, ok = v4IPRange.Remove(v4End)
	require.True(t, ok)
	require.Equal(t, removed[0].end.String(), "10.0.0.254")
	// ipv6
	v6Cidr := "2001:db8::/120"
	_, v6IPNet, err := net.ParseCIDR(v6Cidr)
	require.NoError(t, err)
	v6IPRange := NewIPRangeFromCIDR(*v6IPNet)
	require.Equal(t, v6IPRange.start.String(), "2001:db8::")
	require.Equal(t, v6IPRange.end.String(), "2001:db8::ff")
	v6Start, v6End := v6IPRange.Start(), v6IPRange.End()
	removed, ok = v6IPRange.Remove(v6Start)
	require.True(t, ok)
	require.Equal(t, removed[0].start.String(), "2001:db8::1")
	removed, ok = v6IPRange.Remove(v6End)
	require.True(t, ok)
	require.Equal(t, removed[0].end.String(), "2001:db8::fe")
}

func TestMergeRange(t *testing.T) {
	// ipv4
	v4StartIP1 := "10.0.0.50"
	v4EndIP1 := "10.0.0.100"
	rl := NewEmptyIPRangeList()
	v4RangeStart1, err := NewIP(v4StartIP1)
	require.NoError(t, err)
	v4RangeEnd1, err := NewIP(v4EndIP1)
	require.NoError(t, err)
	v4MergedRangeList := rl.MergeRange(NewIPRange(v4RangeStart1, v4RangeEnd1))
	require.Equal(t, v4MergedRangeList.Len(), 1)
	require.Equal(t, v4MergedRangeList.String(), "10.0.0.50-10.0.0.100")
	// tail append ipv4
	v4StartIP2 := "10.0.0.101"
	v4EndIP2 := "10.0.0.200"
	v4RangeStart2, err := NewIP(v4StartIP2)
	require.NoError(t, err)
	v4RangeEnd2, err := NewIP(v4EndIP2)
	require.NoError(t, err)
	v4MergedRangeList = v4MergedRangeList.MergeRange(NewIPRange(v4RangeStart2, v4RangeEnd2))
	require.Equal(t, v4MergedRangeList.Len(), 1)
	require.Equal(t, v4MergedRangeList.String(), "10.0.0.50-10.0.0.200")
	// head append ipv4
	v4StartIP3 := "10.0.0.20"
	v4EndIP3 := "10.0.0.49"
	v4RangeStart3, err := NewIP(v4StartIP3)
	require.NoError(t, err)
	v4RangeEnd3, err := NewIP(v4EndIP3)
	require.NoError(t, err)
	v4MergedRangeList = v4MergedRangeList.MergeRange(NewIPRange(v4RangeStart3, v4RangeEnd3))
	require.Equal(t, v4MergedRangeList.Len(), 1)
	require.Equal(t, v4MergedRangeList.String(), "10.0.0.20-10.0.0.200")
	// ipv6
	v6StartIP1 := "2001:db8::50"
	v6EndIP1 := "2001:db8::100"
	v6RangeStart1, err := NewIP(v6StartIP1)
	require.NoError(t, err)
	v6RangeEnd1, err := NewIP(v6EndIP1)
	require.NoError(t, err)
	v6MergedRangeList := rl.MergeRange(NewIPRange(v6RangeStart1, v6RangeEnd1))
	require.Equal(t, v6MergedRangeList.Len(), 1)
	require.Equal(t, v6MergedRangeList.String(), "2001:db8::50-2001:db8::100")
	// tail append ipv6
	v6StartIP2 := "2001:db8::101"
	v6EndIP2 := "2001:db8::200"
	v6RangeStart2, err := NewIP(v6StartIP2)
	require.NoError(t, err)
	v6RangeEnd2, err := NewIP(v6EndIP2)
	require.NoError(t, err)
	v6MergedRangeList = v6MergedRangeList.MergeRange(NewIPRange(v6RangeStart2, v6RangeEnd2))
	require.Equal(t, v6MergedRangeList.Len(), 1)
	require.Equal(t, v6MergedRangeList.String(), "2001:db8::50-2001:db8::200")
	// head append ipv6
	v6StartIP3 := "2001:db8::20"
	v6EndIP3 := "2001:db8::4f"
	v6RangeStart3, err := NewIP(v6StartIP3)
	require.NoError(t, err)
	v6RangeEnd3, err := NewIP(v6EndIP3)
	require.NoError(t, err)
	v6MergedRangeList = v6MergedRangeList.MergeRange(NewIPRange(v6RangeStart3, v6RangeEnd3))
	require.Equal(t, v6MergedRangeList.Len(), 1)
	require.Equal(t, v6MergedRangeList.String(), "2001:db8::20-2001:db8::200")
}

func TestIntersect(t *testing.T) {
	// ipv4
	v4StartIP1 := "10.0.0.50"
	v4EndIP1 := "10.0.0.100"
	v4RangeStart1, err := NewIP(v4StartIP1)
	require.NoError(t, err)
	v4RangeEnd1, err := NewIP(v4EndIP1)
	require.NoError(t, err)
	rl := NewEmptyIPRangeList()
	v4MergedRangeList1 := rl.MergeRange(NewIPRange(v4RangeStart1, v4RangeEnd1))

	v4StartIP2 := "10.0.0.50"
	v4EndIP2 := "10.0.0.60"
	v4RangeStart2, err := NewIP(v4StartIP2)
	require.NoError(t, err)
	v4RangeEnd2, err := NewIP(v4EndIP2)
	require.NoError(t, err)
	r2 := NewEmptyIPRangeList()
	v4MergedRangeList2 := r2.MergeRange(NewIPRange(v4RangeStart2, v4RangeEnd2))
	v4Intersect2 := v4MergedRangeList1.Intersect(v4MergedRangeList2)
	require.Equal(t, v4Intersect2.Len(), 1)
	require.Equal(t, v4Intersect2.String(), "10.0.0.50-10.0.0.60")

	v4StartIP3 := "10.0.0.90"
	v4EndIP3 := "10.0.0.100"
	v4RangeStart3, err := NewIP(v4StartIP3)
	require.NoError(t, err)
	v4RangeEnd3, err := NewIP(v4EndIP3)
	require.NoError(t, err)
	r3 := NewEmptyIPRangeList()
	v4MergedRangeList3 := r3.MergeRange(NewIPRange(v4RangeStart3, v4RangeEnd3))
	v4Intersect3 := v4MergedRangeList1.Intersect(v4MergedRangeList3)
	require.Equal(t, v4Intersect3.Len(), 1)
	require.Equal(t, v4Intersect3.String(), "10.0.0.90-10.0.0.100")

	v4StartIP4 := "10.0.0.70"
	v4EndIP4 := "10.0.0.80"
	v4RangeStart4, err := NewIP(v4StartIP4)
	require.NoError(t, err)
	v4RangeEnd4, err := NewIP(v4EndIP4)
	require.NoError(t, err)
	r4 := NewEmptyIPRangeList()
	v4MergedRangeList4 := r4.MergeRange(NewIPRange(v4RangeStart4, v4RangeEnd4))
	v4Intersect4 := v4MergedRangeList1.Intersect(v4MergedRangeList4)
	require.Equal(t, v4Intersect4.Len(), 1)
	require.Equal(t, v4Intersect4.String(), "10.0.0.70-10.0.0.80")

	// ipv6
	v6StartIP1 := "2001:db8::50"
	v6EndIP1 := "2001:db8::100"
	v6RangeStart1, err := NewIP(v6StartIP1)
	require.NoError(t, err)
	v6RangeEnd1, err := NewIP(v6EndIP1)
	require.NoError(t, err)
	v6MergedRangeList1 := rl.MergeRange(NewIPRange(v6RangeStart1, v6RangeEnd1))

	v6StartIP2 := "2001:db8::50"
	v6EndIP2 := "2001:db8::60"
	v6RangeStart2, err := NewIP(v6StartIP2)
	require.NoError(t, err)
	v6RangeEnd2, err := NewIP(v6EndIP2)
	require.NoError(t, err)
	r2 = NewEmptyIPRangeList()
	v6MergedRangeList2 := r2.MergeRange(NewIPRange(v6RangeStart2, v6RangeEnd2))
	v6Intersect2 := v6MergedRangeList1.Intersect(v6MergedRangeList2)
	require.Equal(t, v6Intersect2.Len(), 1)

	v6StartIP3 := "2001:db8::90"
	v6EndIP3 := "2001:db8::100"
	v6RangeStart3, err := NewIP(v6StartIP3)
	require.NoError(t, err)
	v6RangeEnd3, err := NewIP(v6EndIP3)
	require.NoError(t, err)
	r3 = NewEmptyIPRangeList()
	v6MergedRangeList3 := r3.MergeRange(NewIPRange(v6RangeStart3, v6RangeEnd3))
	v6Intersect3 := v6MergedRangeList1.Intersect(v6MergedRangeList3)
	require.Equal(t, v6Intersect3.Len(), 1)

	v6StartIP4 := "2001:db8::70"
	v6EndIP4 := "2001:db8::80"
	v6RangeStart4, err := NewIP(v6StartIP4)
	require.NoError(t, err)
	v6RangeEnd4, err := NewIP(v6EndIP4)
	require.NoError(t, err)
	r4 = NewEmptyIPRangeList()
	v6MergedRangeList4 := r4.MergeRange(NewIPRange(v6RangeStart4, v6RangeEnd4))
	v6Intersect4 := v6MergedRangeList1.Intersect(v6MergedRangeList4)
	require.Equal(t, v6Intersect4.Len(), 1)
}

func TestAt(t *testing.T) {
	// ipv4
	v4StartIP1 := "10.0.0.50"
	v4EndIP1 := "10.0.0.100"
	v4RangeStart1, err := NewIP(v4StartIP1)
	require.NoError(t, err)
	v4RangeEnd1, err := NewIP(v4EndIP1)
	require.NoError(t, err)
	rl := NewEmptyIPRangeList()
	v4RangeList1 := rl.MergeRange(NewIPRange(v4RangeStart1, v4RangeEnd1))
	v4IPRange1 := v4RangeList1.At(0)
	require.Equal(t, v4IPRange1.String(), "10.0.0.50-10.0.0.100")
	v4IPRangeNil := v4RangeList1.At(1)
	require.Nil(t, v4IPRangeNil)

	// ipv6
	v6StartIP1 := "2001:db8::50"
	v6EndIP1 := "2001:db8::100"
	v6RangeStart1, err := NewIP(v6StartIP1)
	require.NoError(t, err)
	v6RangeEnd1, err := NewIP(v6EndIP1)
	require.NoError(t, err)
	v6RangeList1 := rl.MergeRange(NewIPRange(v6RangeStart1, v6RangeEnd1))
	v6IPRange1 := v6RangeList1.At(0)
	require.Equal(t, v6IPRange1.String(), "2001:db8::50-2001:db8::100")
	v6IPRangeNil := v6RangeList1.At(1)
	require.Nil(t, v6IPRangeNil)
}

func TestEqual(t *testing.T) {
	// ipv4
	v4StartIP1 := "10.0.0.50"
	v4EndIP1 := "10.0.0.100"
	v4RangeStart1, err := NewIP(v4StartIP1)
	require.NoError(t, err)
	v4RangeEnd1, err := NewIP(v4EndIP1)
	require.NoError(t, err)
	v4RL1 := NewEmptyIPRangeList()
	v4RangeList1 := v4RL1.MergeRange(NewIPRange(v4RangeStart1, v4RangeEnd1))

	v4StartIP2 := "10.0.0.50"
	v4EndIP2 := "10.0.0.100"
	v4RangeStart2, err := NewIP(v4StartIP2)
	require.NoError(t, err)
	v4RangeEnd2, err := NewIP(v4EndIP2)
	require.NoError(t, err)
	v4RL2 := NewEmptyIPRangeList()
	v4RangeList2 := v4RL2.MergeRange(NewIPRange(v4RangeStart2, v4RangeEnd2))
	require.True(t, v4RangeList1.Equal(v4RangeList2))

	v4StartIP3 := "10.0.0.51"
	v4EndIP3 := "10.0.0.100"
	v4RangeStart3, err := NewIP(v4StartIP3)
	require.NoError(t, err)
	v4RangeEnd3, err := NewIP(v4EndIP3)
	require.NoError(t, err)
	v4RL3 := NewEmptyIPRangeList()
	v4RangeList3 := v4RL3.MergeRange(NewIPRange(v4RangeStart3, v4RangeEnd3))
	require.False(t, v4RangeList1.Equal(v4RangeList3))

	v4StartIP4 := "10.0.0.50"
	v4EndIP4 := "10.0.0.101"
	v4RangeStart4, err := NewIP(v4StartIP4)
	require.NoError(t, err)
	v4RangeEnd4, err := NewIP(v4EndIP4)
	require.NoError(t, err)
	v4RL4 := NewEmptyIPRangeList()
	v4RangeList4 := v4RL4.MergeRange(NewIPRange(v4RangeStart4, v4RangeEnd4))
	require.False(t, v4RangeList1.Equal(v4RangeList4))

	v4RL5 := NewEmptyIPRangeList()
	require.False(t, v4RangeList1.Equal(v4RL5))

	// ipv6
	v6StartIP1 := "2001:db8::50"
	v6EndIP1 := "2001:db8::100"
	v6RangeStart1, err := NewIP(v6StartIP1)
	require.NoError(t, err)
	v6RangeEnd1, err := NewIP(v6EndIP1)
	require.NoError(t, err)
	v6RL1 := NewEmptyIPRangeList()
	v6RangeList1 := v6RL1.MergeRange(NewIPRange(v6RangeStart1, v6RangeEnd1))

	v6StartIP2 := "2001:db8::50"
	v6EndIP2 := "2001:db8::100"
	v6RangeStart2, err := NewIP(v6StartIP2)
	require.NoError(t, err)
	v6RangeEnd2, err := NewIP(v6EndIP2)
	require.NoError(t, err)
	v6RL2 := NewEmptyIPRangeList()
	v6RangeList2 := v6RL2.MergeRange(NewIPRange(v6RangeStart2, v6RangeEnd2))
	require.True(t, v6RangeList1.Equal(v6RangeList2))

	v6StartIP3 := "2001:db8::51"
	v6EndIP3 := "2001:db8::100"
	v6RangeStart3, err := NewIP(v6StartIP3)
	require.NoError(t, err)
	v6RangeEnd3, err := NewIP(v6EndIP3)
	require.NoError(t, err)
	v6RL3 := NewEmptyIPRangeList()
	v6RangeList3 := v6RL3.MergeRange(NewIPRange(v6RangeStart3, v6RangeEnd3))
	require.False(t, v6RangeList1.Equal(v6RangeList3))

	v6StartIP4 := "2001:db8::50"
	v6EndIP4 := "2001:db8::101"
	v6RangeStart4, err := NewIP(v6StartIP4)
	require.NoError(t, err)
	v6RangeEnd4, err := NewIP(v6EndIP4)
	require.NoError(t, err)
	v6RL4 := NewEmptyIPRangeList()
	v6RangeList4 := v6RL4.MergeRange(NewIPRange(v6RangeStart4, v6RangeEnd4))
	require.False(t, v6RangeList1.Equal(v6RangeList4))

	v6RL5 := NewEmptyIPRangeList()
	require.False(t, v6RangeList1.Equal(v6RL5))
}

func TestAllocate(t *testing.T) {
	v4RangeStart, err := NewIP("10.0.0.1")
	require.NoError(t, err)
	require.NotNil(t, v4RangeStart)
	v4RangeEnd, err := NewIP("10.0.0.4")
	require.NoError(t, err)
	require.NotNil(t, v4RangeEnd)
	v4Range := NewIPRange(v4RangeStart, v4RangeEnd)
	v4RangeList := NewEmptyIPRangeList().MergeRange(v4Range)

	t.Run("Allocate from empty range", func(t *testing.T) {
		emptyRange := NewEmptyIPRangeList()
		allocated := emptyRange.Allocate(nil)
		require.Nil(t, allocated)
	})

	t.Run("Allocate without skipped IPs", func(t *testing.T) {
		allocated := v4RangeList.Allocate(nil)
		require.Equal(t, "10.0.0.1", allocated.String())
		require.False(t, v4RangeList.Contains(allocated))
	})

	t.Run("Allocate with skipped IPs", func(t *testing.T) {
		skipped1, err := NewIP("10.0.0.2")
		require.NoError(t, err)
		skipped2, err := NewIP("10.0.0.3")
		require.NoError(t, err)
		allocated := v4RangeList.Allocate([]IP{skipped1, skipped2})
		require.Equal(t, "10.0.0.4", allocated.String())
		require.False(t, v4RangeList.Contains(allocated))
	})

	t.Run("Allocate all IPs", func(t *testing.T) {
		skipped1, err := NewIP("10.0.0.1")
		require.NoError(t, err)
		skipped2, err := NewIP("10.0.0.2")
		require.NoError(t, err)
		skipped3, err := NewIP("10.0.0.3")
		require.NoError(t, err)
		skipped4, err := NewIP("10.0.0.4")
		require.NoError(t, err)
		allocated := v4RangeList.Allocate([]IP{skipped1, skipped2, skipped3, skipped4})
		require.Nil(t, allocated)
	})

	t.Run("Allocate from IPv6 range", func(t *testing.T) {
		v6RangeStart, err := NewIP("2001:db8::1")
		require.NoError(t, err)
		v6RangeEnd, err := NewIP("2001:db8::10")
		require.NoError(t, err)
		v6Range := NewIPRange(v6RangeStart, v6RangeEnd)
		v6RangeList := NewEmptyIPRangeList().MergeRange(v6Range)

		allocated := v6RangeList.Allocate(nil)
		require.Equal(t, "2001:db8::1", allocated.String())
		require.False(t, v6RangeList.Contains(allocated))

		skipped, err := NewIP("2001:db8::2")
		require.NoError(t, err)
		allocated = v6RangeList.Allocate([]IP{skipped})
		require.Equal(t, "2001:db8::3", allocated.String())
		require.False(t, v6RangeList.Contains(allocated))
	})
}

func TestIPRangeListToCIDRs(t *testing.T) {
	t.Run("Empty list", func(t *testing.T) {
		emptyList := NewEmptyIPRangeList()
		result, err := emptyList.ToCIDRs()
		require.NoError(t, err)
		require.Nil(t, result)
	})

	t.Run("Single IPv4 IP", func(t *testing.T) {
		rangeList, err := NewIPRangeListFrom("10.0.0.1")
		require.NoError(t, err)
		result, err := rangeList.ToCIDRs()
		require.NoError(t, err)
		require.Equal(t, []string{"10.0.0.1/32"}, result)
	})

	t.Run("Single IPv6 IP", func(t *testing.T) {
		rangeList, err := NewIPRangeListFrom("2001:db8::1")
		require.NoError(t, err)
		result, err := rangeList.ToCIDRs()
		require.NoError(t, err)
		require.Equal(t, []string{"2001:db8::1/128"}, result)
	})

	t.Run("IPv4 CIDR", func(t *testing.T) {
		rangeList, err := NewIPRangeListFrom("10.0.0.0/24")
		require.NoError(t, err)
		result, err := rangeList.ToCIDRs()
		require.NoError(t, err)
		require.Equal(t, []string{"10.0.0.0/24"}, result)
	})

	t.Run("IPv6 CIDR", func(t *testing.T) {
		rangeList, err := NewIPRangeListFrom("2001:db8::/64")
		require.NoError(t, err)
		result, err := rangeList.ToCIDRs()
		require.NoError(t, err)
		require.Equal(t, []string{"2001:db8::/64"}, result)
	})

	t.Run("IPv4 aligned range", func(t *testing.T) {
		rangeList, err := NewIPRangeListFrom("10.0.0.0..10.0.0.3")
		require.NoError(t, err)
		result, err := rangeList.ToCIDRs()
		require.NoError(t, err)
		require.Equal(t, []string{"10.0.0.0/30"}, result)
	})

	t.Run("IPv4 unaligned range", func(t *testing.T) {
		rangeList, err := NewIPRangeListFrom("10.0.0.1..10.0.0.5")
		require.NoError(t, err)
		result, err := rangeList.ToCIDRs()
		require.NoError(t, err)
		require.Equal(t, []string{
			"10.0.0.1/32",
			"10.0.0.2/31",
			"10.0.0.4/31",
		}, result)
	})

	t.Run("IPv6 range", func(t *testing.T) {
		rangeList, err := NewIPRangeListFrom("2001:db8::1..2001:db8::4")
		require.NoError(t, err)
		result, err := rangeList.ToCIDRs()
		require.NoError(t, err)
		require.Equal(t, []string{
			"2001:db8::1/128",
			"2001:db8::2/127",
			"2001:db8::4/128",
		}, result)
	})

	t.Run("Multiple ranges merged", func(t *testing.T) {
		// NewIPRangeListFrom merges overlapping ranges
		rangeList, err := NewIPRangeListFrom("10.0.0.1..10.0.0.5", "10.0.0.3..10.0.0.10")
		require.NoError(t, err)
		result, err := rangeList.ToCIDRs()
		require.NoError(t, err)
		// Should be merged into 10.0.0.1..10.0.0.10
		// Which converts to: 10.0.0.1/32, 10.0.0.2/31, 10.0.0.4/30, 10.0.0.8/31, 10.0.0.10/32
		require.NotEmpty(t, result)
		require.Contains(t, result, "10.0.0.1/32")
		require.Contains(t, result, "10.0.0.2/31")
		require.Contains(t, result, "10.0.0.4/30")
		require.Contains(t, result, "10.0.0.8/31")
		require.Contains(t, result, "10.0.0.10/32")
	})

	t.Run("Multiple separate ranges", func(t *testing.T) {
		rangeList, err := NewIPRangeListFrom("10.0.0.1..10.0.0.2", "10.0.0.5..10.0.0.6")
		require.NoError(t, err)
		result, err := rangeList.ToCIDRs()
		require.NoError(t, err)
		require.NotEmpty(t, result)
		// Check sorted order
		require.True(t, result[0] <= result[len(result)-1])
	})

	t.Run("Mixed single IPs and ranges", func(t *testing.T) {
		rangeList, err := NewIPRangeListFrom("10.0.0.1", "10.0.0.5..10.0.0.8", "10.0.0.10")
		require.NoError(t, err)
		result, err := rangeList.ToCIDRs()
		require.NoError(t, err)
		require.NotEmpty(t, result)
		require.Contains(t, result, "10.0.0.1/32")
		require.Contains(t, result, "10.0.0.10/32")
	})

	t.Run("Large IPv4 range", func(t *testing.T) {
		rangeList, err := NewIPRangeListFrom("10.0.0.0..10.0.0.255")
		require.NoError(t, err)
		result, err := rangeList.ToCIDRs()
		require.NoError(t, err)
		require.Equal(t, []string{"10.0.0.0/24"}, result)
	})

	t.Run("Results are sorted", func(t *testing.T) {
		rangeList, err := NewIPRangeListFrom("10.0.0.10", "10.0.0.1", "10.0.0.5")
		require.NoError(t, err)
		result, err := rangeList.ToCIDRs()
		require.NoError(t, err)
		require.Equal(t, []string{
			"10.0.0.1/32",
			"10.0.0.10/32",
			"10.0.0.5/32",
		}, result)
	})
}

func TestIPRangeListToCIDRsEdgeCases(t *testing.T) {
	t.Run("Maximum IPv4 address", func(t *testing.T) {
		rangeList, err := NewIPRangeListFrom("255.255.255.255")
		require.NoError(t, err)
		result, err := rangeList.ToCIDRs()
		require.NoError(t, err)
		require.Equal(t, []string{"255.255.255.255/32"}, result)
	})

	t.Run("Minimum IPv4 address", func(t *testing.T) {
		rangeList, err := NewIPRangeListFrom("0.0.0.0")
		require.NoError(t, err)
		result, err := rangeList.ToCIDRs()
		require.NoError(t, err)
		require.Equal(t, []string{"0.0.0.0/32"}, result)
	})

	t.Run("IPv6 loopback", func(t *testing.T) {
		rangeList, err := NewIPRangeListFrom("::1")
		require.NoError(t, err)
		result, err := rangeList.ToCIDRs()
		require.NoError(t, err)
		require.Equal(t, []string{"::1/128"}, result)
	})

	t.Run("IPv6 zero address", func(t *testing.T) {
		rangeList, err := NewIPRangeListFrom("::")
		require.NoError(t, err)
		result, err := rangeList.ToCIDRs()
		require.NoError(t, err)
		require.Equal(t, []string{"::/128"}, result)
	})
}
