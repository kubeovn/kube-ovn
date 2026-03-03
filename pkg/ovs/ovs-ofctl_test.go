package ovs

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenFlowStdinReader(t *testing.T) {
	tests := []struct {
		name  string
		flows []string
	}{
		{
			name:  "empty",
			flows: nil,
		},
		{
			name:  "single flow",
			flows: []string{"table=0,priority=100,actions=output:1"},
		},
		{
			name:  "multiple flows",
			flows: []string{"table=0,priority=100,actions=output:1", "table=0,priority=50,actions=drop"},
		},
		{
			name:  "empty string flow",
			flows: []string{""},
		},
		{
			name:  "trailing empty flow",
			flows: []string{"table=0,priority=100,actions=output:1", ""},
		},
		{
			name:  "many flows",
			flows: makeBenchmarkFlows(100),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expected := strings.Join(tt.flows, "\n")

			reader := &openFlowStdinReader{flows: tt.flows}
			got, err := io.ReadAll(reader)
			require.NoError(t, err)
			require.Equal(t, expected, string(got))
		})
	}
}

func TestOpenFlowStdinReaderSmallBuffer(t *testing.T) {
	flows := []string{"table=0,priority=100,actions=output:1", "table=0,priority=50,actions=drop"}
	expected := strings.Join(flows, "\n")

	reader := &openFlowStdinReader{flows: flows}
	var result []byte
	buf := make([]byte, 3) // deliberately small buffer
	for {
		n, err := reader.Read(buf)
		result = append(result, buf[:n]...)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}
	require.Equal(t, expected, string(result))
}

func TestOpenFlowStdinReaderZeroLenRead(t *testing.T) {
	reader := &openFlowStdinReader{flows: []string{"flow1"}}
	n, err := reader.Read(nil)
	require.Equal(t, 0, n)
	require.NoError(t, err)
}

var benchmarkFlowBytesSink int64

func BenchmarkReplaceFlowsInputRendering(b *testing.B) {
	benchCases := []struct {
		name      string
		flowCount int
	}{
		{name: "100_flows", flowCount: 100},
		{name: "1k_flows", flowCount: 1000},
		{name: "5k_flows", flowCount: 5000},
	}

	for _, tc := range benchCases {
		flows := makeBenchmarkFlows(tc.flowCount)
		totalBytes := benchmarkFlowsBytes(flows)

		b.Run(tc.name+"/join", func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(totalBytes)
			for b.Loop() {
				stdin := strings.NewReader(strings.Join(flows, "\n"))
				written, err := io.Copy(io.Discard, stdin)
				if err != nil {
					b.Fatalf("failed to drain: %v", err)
				}
				benchmarkFlowBytesSink = written
			}
		})

		b.Run(tc.name+"/stream", func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(totalBytes)
			for b.Loop() {
				stdin := &openFlowStdinReader{flows: flows}
				written, err := io.Copy(io.Discard, stdin)
				if err != nil {
					b.Fatalf("failed to drain: %v", err)
				}
				benchmarkFlowBytesSink = written
			}
		})
	}
}

func makeBenchmarkFlows(count int) []string {
	flows := make([]string, count)
	const suffix = ",ip,nw_src=10.128.0.0/14,tp_dst=8080,actions=ct(commit),output:2"
	for i := range flows {
		flows[i] = "table=0,priority=100,in_port=1,reg0=0x1" + suffix
	}
	return flows
}

func benchmarkFlowsBytes(flows []string) int64 {
	if len(flows) == 0 {
		return 0
	}
	total := len(flows) - 1 // newline delimiters
	for _, flow := range flows {
		total += len(flow)
	}
	return int64(total)
}

func BenchmarkOpenFlowStdinReaderEquivalence(b *testing.B) {
	flows := makeBenchmarkFlows(1000)
	expected := []byte(strings.Join(flows, "\n"))

	for b.Loop() {
		reader := &openFlowStdinReader{flows: flows}
		got, err := io.ReadAll(reader)
		if err != nil {
			b.Fatal(err)
		}
		if !bytes.Equal(got, expected) {
			b.Fatal("stream output does not match strings.Join")
		}
	}
}
