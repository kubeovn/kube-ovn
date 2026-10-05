package ko

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// extractTar confines all writes, including traversal through existing symlinks, to root.
func extractTar(root *os.Root, input io.Reader, limit int64) error {
	reader := tar.NewReader(input)
	var total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := path.Clean(header.Name)
		if path.IsAbs(header.Name) || !filepath.IsLocal(name) || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, "\\") {
			return fmt.Errorf("unsafe archive path %q", header.Name)
		}
		if name == "." && header.Typeflag == tar.TypeDir {
			continue
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(name, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if header.Size < 0 || header.Size > limit-total {
				return errors.New("archive exceeds the collection byte limit")
			}
			total += header.Size
			if err := root.MkdirAll(path.Dir(name), 0o700); err != nil {
				return err
			}
			if err := extractFile(root, name, reader, header.Size); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported archive entry %q (type %d)", name, header.Typeflag)
		}
	}
}

func extractFile(root *os.Root, name string, input io.Reader, size int64) (resultErr error) {
	temporary := ".ko-extract-" + runID()
	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if err := root.Remove(temporary); err != nil && !errors.Is(err, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	_, copyErr := io.CopyN(file, input, size)
	if err := errors.Join(copyErr, file.Close()); err != nil {
		return err
	}
	return root.Rename(temporary, name)
}

func (c *Client) collectDirectory(ctx context.Context, target Target, source, destination string, limit int64) error {
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	root, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer root.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		var stderr boundedBuffer
		err := c.Executor.Exec(ctx, target, []string{"tar", "-C", source, "-cf", "-", "."}, Streams{Out: writer, ErrOut: &stderr})
		if err != nil {
			err = fmt.Errorf("collect %s/%s:%s: %w: %s", target.Namespace, target.Pod, source, err, stderr.String())
		}
		done <- errors.Join(err, writer.CloseWithError(err))
	}()
	unpackErr := extractTar(root, reader, limit)
	if unpackErr != nil {
		cancel()
	} else {
		// tar.Reader stops at the end markers, before GNU tar's record padding.
		// Drain that padding so exec can finish instead of seeing a closed pipe.
		_, unpackErr = io.Copy(io.Discard, reader)
	}
	closeErr := reader.Close()
	return errors.Join(unpackErr, closeErr, <-done)
}

func (c *Client) downloadFile(ctx context.Context, target Target, source, destination string) (checksum string, resultErr error) {
	file, err := os.CreateTemp(filepath.Dir(destination), ".kubectl-ko-download-*")
	if err != nil {
		return "", err
	}
	temporary := file.Name()
	defer func() {
		if err := os.Remove(temporary); err != nil && !errors.Is(err, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	hash := sha256.New()
	var stderr boundedBuffer
	execErr := c.Executor.Exec(ctx, target, []string{"cat", source}, Streams{Out: io.MultiWriter(file, hash), ErrOut: &stderr})
	stat, statErr := file.Stat()
	closeErr := file.Close()
	if err := errors.Join(execErr, statErr, closeErr); err != nil {
		return "", fmt.Errorf("download %s: %w: %s", source, err, stderr.String())
	}
	if stat.Size() == 0 {
		return "", errors.New("downloaded backup is empty")
	}
	checksum = hex.EncodeToString(hash.Sum(nil))
	remoteHash, err := c.capture(ctx, target, "sha256sum", source)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(remoteHash)
	if len(fields) == 0 || fields[0] != checksum {
		return "", errors.New("downloaded backup checksum does not match the remote file")
	}
	// Link publishes atomically without overwriting a pre-existing backup.
	if err := os.Link(temporary, destination); err != nil {
		return "", err
	}
	return checksum, nil
}
