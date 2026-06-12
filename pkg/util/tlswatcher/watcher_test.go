package tlswatcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWatcherRunsCallbackOnceForStableContent(t *testing.T) {
	dir := t.TempDir()
	cert := filepath.Join(dir, "client.crt")
	key := filepath.Join(dir, "client.key")
	if err := os.WriteFile(cert, []byte("cert-a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("key-a"), 0o600); err != nil {
		t.Fatal(err)
	}

	calls := 0
	watcher := New([]string{cert, key}, func(context.Context) error {
		calls++
		return nil
	})
	if err := watcher.Check(context.Background()); err != nil {
		t.Fatalf("first Check returned error: %v", err)
	}
	if err := watcher.Check(context.Background()); err != nil {
		t.Fatalf("second Check returned error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("callback calls = %d, want 1", calls)
	}
}

func TestWatcherDoesNotAdvanceHashAfterCallbackFailure(t *testing.T) {
	dir := t.TempDir()
	cert := filepath.Join(dir, "client.crt")
	if err := os.WriteFile(cert, []byte("cert-a"), 0o600); err != nil {
		t.Fatal(err)
	}

	calls := 0
	watcher := New([]string{cert}, func(context.Context) error {
		calls++
		if calls == 1 {
			return errors.New("reload failed")
		}
		return nil
	})
	if err := watcher.Check(context.Background()); err == nil {
		t.Fatal("first Check returned nil, want reload error")
	}
	if err := watcher.Check(context.Background()); err != nil {
		t.Fatalf("second Check returned error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("callback calls = %d, want 2", calls)
	}
}

func TestWatcherRequiresAllFiles(t *testing.T) {
	dir := t.TempDir()
	cert := filepath.Join(dir, "client.crt")
	if err := os.WriteFile(cert, []byte("cert-a"), 0o600); err != nil {
		t.Fatal(err)
	}

	calls := 0
	watcher := New([]string{cert, filepath.Join(dir, "missing.key")}, func(context.Context) error {
		calls++
		return nil
	})
	if err := watcher.Check(context.Background()); err == nil {
		t.Fatal("Check returned nil, want missing file error")
	}
	if calls != 0 {
		t.Fatalf("callback calls = %d, want 0", calls)
	}
}
