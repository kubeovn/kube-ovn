package filesystem

import (
	"os"
	"path/filepath"
	"time"
)

var Fs Filesystem = DefaultFs{}

// Filesystem is an interface that we can use to mock various filesystem operations
type Filesystem interface {
	// from "os"
	Stat(name string) (os.FileInfo, error)
	Create(name string) (File, error)
	Rename(oldpath, newpath string) error
	MkdirAll(path string, perm os.FileMode) error
	Chtimes(name string, atime time.Time, mtime time.Time) error
	RemoveAll(path string) error
	Remove(name string) error

	// from "io/ioutil"
	ReadFile(filename string) ([]byte, error)
	TempDir(dir, prefix string) (string, error)
	TempFile(dir, prefix string) (File, error)
	ReadDir(dirname string) ([]os.FileInfo, error)
	Walk(root string, walkFn filepath.WalkFunc) error
}

// File is an interface that we can use to mock various filesystem operations typically
// accessed through the File object from the "os" package
type File interface {
	// for now, the only os.File methods used are those below, add more as necessary
	Name() string
	Write(b []byte) (n int, err error)
	Sync() error
	Close() error
}
