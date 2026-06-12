package tlswatcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

type (
	Callback     func(context.Context) error
	ValidateFunc func() error
)

type Watcher struct {
	paths    []string
	interval time.Duration
	validate ValidateFunc
	reload   Callback

	lastHash string
}

type Option func(*Watcher)

func WithInterval(interval time.Duration) Option {
	return func(w *Watcher) {
		if interval > 0 {
			w.interval = interval
		}
	}
}

func WithValidate(validate ValidateFunc) Option {
	return func(w *Watcher) {
		w.validate = validate
	}
}

func New(paths []string, reload Callback, options ...Option) *Watcher {
	w := &Watcher{
		paths:    append([]string(nil), paths...),
		interval: 30 * time.Second,
		reload:   reload,
	}
	for _, option := range options {
		option(w)
	}
	return w
}

func (w *Watcher) Run(ctx context.Context) {
	wait.UntilWithContext(ctx, func(ctx context.Context) {
		_ = w.Check(ctx)
	}, w.interval)
}

func (w *Watcher) Check(ctx context.Context) error {
	hash, err := HashFiles(w.paths)
	if err != nil {
		return err
	}
	if hash == w.lastHash {
		return nil
	}
	if w.validate != nil {
		if err := w.validate(); err != nil {
			return err
		}
	}
	if w.reload != nil {
		if err := w.reload(ctx); err != nil {
			return err
		}
	}
	w.lastHash = hash
	return nil
}

func HashFiles(paths []string) (string, error) {
	h := sha256.New()
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", path, err)
		}
		h.Write([]byte(path))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
