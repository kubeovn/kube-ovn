package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadInCsumErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snmp")
	content := "Tcp: RtoAlgorithm RtoMin InCsumErrors\nTcp: 1 200 17\nUdp: InDatagrams InCsumErrors\nUdp: 2 23\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	value, err := readInCsumErrors(path)
	require.NoError(t, err)
	require.Equal(t, 23, value)
}
