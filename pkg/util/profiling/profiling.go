package profiling

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/pprof"
	"syscall"
	"time"

	"k8s.io/klog/v2"
)

const (
	timeFormat         = "2006-01-02_15:04:05"
	CPUProfileDuration = 30 * time.Second
)

func writeProfileToTemp(pattern string, writeFn func(*os.File) error) {
	path := filepath.Join(os.TempDir(), fmt.Sprintf(pattern, time.Now().Format(timeFormat)))
	f, err := os.Create(path) // #nosec G303,G304
	if err != nil {
		klog.Errorf("failed to create profile file: %v", err)
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			klog.Errorf("failed to close file %q: %v", path, err)
		}
	}()
	if err := writeFn(f); err != nil {
		klog.Errorf("failed to write profile: %v", err)
	}
}

// DumpProfile starts a goroutine that handles SIGUSR1 (CPU profile) and SIGUSR2 (heap profile).
func DumpProfile() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR1, syscall.SIGUSR2)
	go func() {
		for sig := range ch {
			switch sig {
			case syscall.SIGUSR1:
				writeProfileToTemp("cpu-profile-%s.pprof", func(f *os.File) error {
					if err := pprof.StartCPUProfile(f); err != nil {
						return err
					}
					time.Sleep(CPUProfileDuration)
					pprof.StopCPUProfile()
					return nil
				})
			case syscall.SIGUSR2:
				writeProfileToTemp("mem-profile-%s.pprof", func(f *os.File) error {
					return pprof.WriteHeapProfile(f)
				})
			}
		}
	}()
}
