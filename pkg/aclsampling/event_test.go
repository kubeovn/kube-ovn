package aclsampling

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSampleReference(t *testing.T) {
	tests := []struct {
		name              string
		input             string
		observationDomain *uint32
		applicationID     *uint32
		datapathKey       *uint32
		metadata          uint32
	}{
		{name: "decimal metadata", input: "200", metadata: 200},
		{name: "hex metadata", input: "0xc8", metadata: 200},
		{name: "maximum metadata", input: "4294967295", metadata: 4294967295},
		{
			name:              "decimal cookie",
			input:             strconv.FormatUint(uint64(0x640abcde)<<32|200, 10),
			observationDomain: new(uint32(0x640abcde)),
			applicationID:     new(uint32(100)),
			datapathKey:       new(uint32(0x0abcde)),
			metadata:          200,
		},
		{
			name:              "hex cookie",
			input:             "0x640abcde000000c8",
			observationDomain: new(uint32(0x640abcde)),
			applicationID:     new(uint32(100)),
			datapathKey:       new(uint32(0x0abcde)),
			metadata:          200,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reference, err := ParseSampleReference(test.input)
			require.NoError(t, err)
			require.Equal(t, test.observationDomain, reference.ObservationDomain)
			require.Equal(t, test.applicationID, reference.ApplicationID)
			require.Equal(t, test.datapathKey, reference.DatapathKey)
			require.Equal(t, test.metadata, reference.Metadata)
			require.NoError(t, reference.Validate())
		})
	}
}

func TestParseSampleReferenceRejectsInvalidInput(t *testing.T) {
	tests := []string{
		"",
		"0",
		"0x",
		"-1",
		"+1",
		" 1",
		"1 ",
		"0xgg",
		"18446744073709551616",
		"0x64000000c8",
		"0x0000010000000001",
		"0x6400000000000000",
	}

	for _, input := range tests {
		t.Run(fmt.Sprintf("input_%q", input), func(t *testing.T) {
			_, err := ParseSampleReference(input)
			require.Error(t, err)
		})
	}
}
