package util

import (
	"os"
	"path/filepath"
)

// AtomicWriteFile writes data to a temporary file in the target directory,
// fsyncs it, then renames it to path. Concurrent readers never observe a
// partially written file, and a crash mid-write cannot leave a truncated
// file at the final path.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err = tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmpName, path); err != nil {
		return err
	}

	// fsync the parent directory so the rename itself survives a power loss;
	// otherwise the journal may replay the rename before the data blocks are
	// on disk, leaving a zero-length file.
	d, err := os.Open(dir) // #nosec G304
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
