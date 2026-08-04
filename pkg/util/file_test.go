package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteFile(t *testing.T) {
	tests := []struct {
		name     string
		perm     os.FileMode
		existing string
		data     string
	}{
		{
			name: "create new file",
			perm: 0o600,
			data: "hello",
		},
		{
			name:     "overwrite existing file",
			perm:     0o600,
			existing: "old content longer than the new one",
			data:     "new",
		},
		{
			name: "custom permission",
			perm: 0o644,
			data: "perm test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "target.conf")
			if tt.existing != "" {
				if err := os.WriteFile(path, []byte(tt.existing), 0o600); err != nil {
					t.Fatalf("failed to prepare existing file: %v", err)
				}
			}

			if err := AtomicWriteFile(path, []byte(tt.data), tt.perm); err != nil {
				t.Fatalf("AtomicWriteFile: %v", err)
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read result: %v", err)
			}
			if string(got) != tt.data {
				t.Errorf("content = %q, want %q", string(got), tt.data)
			}

			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("failed to stat result: %v", err)
			}
			if info.Mode().Perm() != tt.perm {
				t.Errorf("perm = %o, want %o", info.Mode().Perm(), tt.perm)
			}

			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("failed to read dir: %v", err)
			}
			if len(entries) != 1 {
				t.Errorf("expected only the target file in dir, got %d entries", len(entries))
			}
		})
	}

	t.Run("missing directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "no-such-dir", "target.conf")
		if err := AtomicWriteFile(path, []byte("x"), 0o600); err == nil {
			t.Error("expected error when the target directory does not exist")
		}
	})
}
