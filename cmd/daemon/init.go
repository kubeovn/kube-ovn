//go:build !windows
// +build !windows

package daemon

func initForOS() error {
	// nothing to do on Linux
	return nil
}
