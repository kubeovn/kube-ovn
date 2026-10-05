package ko

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func archive(t *testing.T, name string, kind byte, content string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	header := &tar.Header{Name: name, Typeflag: kind, Mode: 0o600}
	if kind == tar.TypeReg {
		header.Size = int64(len(content))
	} else {
		header.Linkname = "../outside"
	}
	require.NoError(t, writer.WriteHeader(header))
	if kind == tar.TypeReg {
		_, err := io.WriteString(writer, content)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return buffer.Bytes()
}

func TestArchiveConfinement(t *testing.T) {
	for _, name := range []string{"../outside", "/absolute", "a/../../outside", "a\\..\\outside"} {
		t.Run(name, func(t *testing.T) {
			root, err := os.OpenRoot(t.TempDir())
			require.NoError(t, err)
			defer root.Close()
			require.Error(t, extractTar(root, bytes.NewReader(archive(t, name, tar.TypeReg, "secret")), 100))
		})
	}
	for _, kind := range []byte{tar.TypeSymlink, tar.TypeLink} {
		t.Run(strconv.FormatUint(uint64(kind), 10), func(t *testing.T) {
			root, err := os.OpenRoot(t.TempDir())
			require.NoError(t, err)
			defer root.Close()
			require.Error(t, extractTar(root, bytes.NewReader(archive(t, "escape", kind, "")), 100))
		})
	}
	directory := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(directory, "escape")))
	root, err := os.OpenRoot(directory)
	require.NoError(t, err)
	defer root.Close()
	require.Error(t, extractTar(root, bytes.NewReader(archive(t, "escape/file", tar.TypeReg, "secret")), 100))
	_, err = os.Stat(filepath.Join(outside, "file"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestArchiveExtractionAndLimit(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	require.NoError(t, err)
	defer root.Close()
	require.NoError(t, extractTar(root, bytes.NewReader(archive(t, "sub/log", tar.TypeReg, "log\x00\n")), 100))
	data, err := root.ReadFile("sub/log")
	require.NoError(t, err)
	require.Equal(t, "log\x00\n", string(data))
	require.NoError(t, extractTar(root, bytes.NewReader(archive(t, "sub/log", tar.TypeReg, "updated\x00\n")), 100))
	data, err = root.ReadFile("sub/log")
	require.NoError(t, err)
	require.Equal(t, "updated\x00\n", string(data), "subsequent collections must replace files on every platform")
	require.ErrorContains(t, extractTar(root, bytes.NewReader(archive(t, "large", tar.TypeReg, "too large")), 2), "byte limit")
}

func TestBackupTransferChecksIntegrityAndDoesNotOverwrite(t *testing.T) {
	data := []byte("OVSDB\x00\xff\n")
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, remoteHash := range []string{hash, "wrong"} {
		t.Run(remoteHash, func(t *testing.T) {
			executor := &recordingExecutor{run: func(_ context.Context, _ Target, argv []string, s Streams) error {
				if argv[0] == "cat" {
					_, err := s.Out.Write(data)
					return err
				}
				_, err := fmt.Fprintf(s.Out, "%s  /backup\n", remoteHash)
				return err
			}}
			client := &Client{Executor: executor}
			destination := filepath.Join(t.TempDir(), "backup with spaces")
			_, err := client.downloadFile(t.Context(), Target{}, "/backup", destination)
			if remoteHash != hash {
				require.Error(t, err)
				_, err = os.Stat(destination)
				require.ErrorIs(t, err, os.ErrNotExist)
				return
			}
			require.NoError(t, err)
			got, err := os.ReadFile(destination)
			require.NoError(t, err)
			require.Equal(t, data, got)
			_, err = client.downloadFile(t.Context(), Target{}, "/backup", destination)
			require.Error(t, err)
			entries, err := os.ReadDir(filepath.Dir(destination))
			require.NoError(t, err)
			require.Len(t, entries, 1)
		})
	}
}

func TestCaptureOutputLimit(t *testing.T) {
	client := &Client{Executor: &recordingExecutor{run: func(_ context.Context, _ Target, _ []string, s Streams) error {
		_, err := io.Copy(s.Out, strings.NewReader(strings.Repeat("x", maxQueryOutput+1)))
		return err
	}}}
	_, err := client.capture(t.Context(), Target{}, "query")
	require.ErrorContains(t, err, "8 MiB")
}
