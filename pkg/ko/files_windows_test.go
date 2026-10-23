package ko

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArchiveRejectsWindowsDeviceVolumeAndStreamPaths(t *testing.T) {
	for _, name := range []string{"C:/outside", "C:outside", "//server/share/outside", "file:stream", "NUL", "CON", "nested/AUX", "CONIN$", "CONOUT$", "LPT1"} {
		t.Run(name, func(t *testing.T) {
			root, err := os.OpenRoot(t.TempDir())
			require.NoError(t, err)
			defer root.Close()
			require.Error(t, extractTar(root, bytes.NewReader(archive(t, name, tar.TypeReg, "secret")), 100))
		})
	}
}

func TestArchiveWindowsDeviceNameWithExtension(t *testing.T) {
	// Windows 11 permits CON.txt as a regular file. Older Windows versions
	// still reserve it; follow the OS policy exposed by filepath.IsLocal.
	root, err := os.OpenRoot(t.TempDir())
	require.NoError(t, err)
	defer root.Close()
	err = extractTar(root, bytes.NewReader(archive(t, "CON.txt", tar.TypeReg, "ordinary file")), 100)
	if !filepath.IsLocal("CON.txt") {
		require.Error(t, err)
		return
	}
	require.NoError(t, err)
	data, err := root.ReadFile("CON.txt")
	require.NoError(t, err)
	require.Equal(t, "ordinary file", string(data))
}
