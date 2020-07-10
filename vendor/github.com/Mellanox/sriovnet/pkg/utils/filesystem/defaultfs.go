package filesystem

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"time"
)

// DefaultFs implements Filesystem using same-named functions from "os" and "io/ioutil"
type DefaultFs struct{}

// Stat via os.Stat
func (DefaultFs) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

// Create via os.Create
func (DefaultFs) Create(name string) (File, error) {
	file, err := os.Create(name)
	if err != nil {
		return nil, err
	}
	return &defaultFile{file}, nil
}

// Rename via os.Rename
func (DefaultFs) Rename(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}

// MkdirAll via os.MkdirAll
func (DefaultFs) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// Chtimes via os.Chtimes
func (DefaultFs) Chtimes(name string, atime time.Time, mtime time.Time) error {
	return os.Chtimes(name, atime, mtime)
}

// RemoveAll via os.RemoveAll
func (DefaultFs) RemoveAll(path string) error {
	return os.RemoveAll(path)
}

// Remove via os.RemoveAll
func (DefaultFs) Remove(name string) error {
	return os.Remove(name)
}

// ReadFile via ioutil.ReadFile
func (DefaultFs) ReadFile(filename string) ([]byte, error) {
	return ioutil.ReadFile(filename)
}

// TempDir via ioutil.TempDir
func (DefaultFs) TempDir(dir, prefix string) (string, error) {
	return ioutil.TempDir(dir, prefix)
}

// TempFile via ioutil.TempFile
func (DefaultFs) TempFile(dir, prefix string) (File, error) {
	file, err := ioutil.TempFile(dir, prefix)
	if err != nil {
		return nil, err
	}
	return &defaultFile{file}, nil
}

// ReadDir via ioutil.ReadDir
func (DefaultFs) ReadDir(dirname string) ([]os.FileInfo, error) {
	return ioutil.ReadDir(dirname)
}

// Walk via filepath.Walk
func (DefaultFs) Walk(root string, walkFn filepath.WalkFunc) error {
	return filepath.Walk(root, walkFn)
}

// defaultFile implements File using same-named functions from "os"
type defaultFile struct {
	file *os.File
}

// Name via os.File.Name
func (file *defaultFile) Name() string {
	return file.file.Name()
}

// Write via os.File.Write
func (file *defaultFile) Write(b []byte) (n int, err error) {
	return file.file.Write(b)
}

// Sync via os.File.Sync
func (file *defaultFile) Sync() error {
	return file.file.Sync()
}

// Close via os.File.Close
func (file *defaultFile) Close() error {
	return file.file.Close()
}
