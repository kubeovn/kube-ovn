package util

import (
	"os"

	"github.com/kubeovn/kube-ovn/pkg/fileutil"
)

// AtomicWriteFile writes data to a temporary file in the target directory,
// fsyncs it, then renames it to path. Concurrent readers never observe a
// partially written file, and a crash mid-write cannot leave a truncated
// file at the final path.
//
// Deprecated: use fileutil.AtomicWriteFile.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	return fileutil.AtomicWriteFile(path, data, perm)
}
