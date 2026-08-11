package fileutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// AtomicWriteFile writes data to a temporary file in the target directory,
// fsyncs it, then renames it to path. Concurrent readers never observe a
// partially written file, and a crash mid-write cannot leave a truncated
// file at the final path.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) (resultErr error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	tmpClosed := false
	defer func() {
		if !tmpClosed {
			if err := tmp.Close(); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("failed to close temporary file %q: %w", tmpName, err))
			}
		}
		if err := os.Remove(tmpName); err != nil && !os.IsNotExist(err) {
			resultErr = errors.Join(resultErr, fmt.Errorf("failed to remove temporary file %q: %w", tmpName, err))
		}
	}()

	if err = tmp.Chmod(perm); err != nil {
		return fmt.Errorf("failed to set temporary file permissions: %w", err)
	}
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("failed to write temporary file: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("failed to sync temporary file: %w", err)
	}
	closeErr := tmp.Close()
	tmpClosed = true
	if closeErr != nil {
		return fmt.Errorf("failed to close temporary file: %w", closeErr)
	}
	if err = os.Rename(tmpName, path); err != nil { // #nosec G703 -- writing to the caller-selected destination is the purpose of this helper.
		return fmt.Errorf("failed to replace target file: %w", err)
	}

	// Fsync the parent directory so the rename itself survives a power loss.
	d, err := os.Open(dir) // #nosec G304
	if err != nil {
		return fmt.Errorf("failed to open target directory: %w", err)
	}
	syncErr := d.Sync()
	closeErr = d.Close()
	if syncErr != nil {
		syncErr = fmt.Errorf("failed to sync target directory: %w", syncErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("failed to close target directory: %w", closeErr)
	}
	return errors.Join(syncErr, closeErr)
}
