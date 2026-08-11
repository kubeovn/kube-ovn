package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteFileCompatibility(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target.conf")
	if err := AtomicWriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read result: %v", err)
	}
	if string(data) != "data" {
		t.Fatalf("content = %q, want %q", data, "data")
	}
}
