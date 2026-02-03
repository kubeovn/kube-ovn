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

// DumpProfile starts goroutines that on SIGUSR1 write a CPU profile and on SIGUSR2 write a heap profile to the temp dir.
func DumpProfile() {
	ch1 := make(chan os.Signal, 1)
	ch2 := make(chan os.Signal, 1)
	signal.Notify(ch1, syscall.SIGUSR1)
	signal.Notify(ch2, syscall.SIGUSR2)
	go func() {
		for {
			<-ch1
			name := fmt.Sprintf("cpu-profile-%s.pprof", time.Now().Format(timeFormat))
			path := filepath.Join(os.TempDir(), name)
			f, err := os.Create(path) // #nosec G303,G304
			if err != nil {
				klog.Errorf("failed to create cpu profile file: %v", err)
				return
			}
			if err = pprof.StartCPUProfile(f); err != nil {
				klog.Errorf("failed to start cpu profile: %v", err)
				if err = f.Close(); err != nil {
					klog.Errorf("failed to close file %q: %v", path, err)
				}
				return
			}
			time.Sleep(CPUProfileDuration)
			pprof.StopCPUProfile()
			if err = f.Close(); err != nil {
				klog.Errorf("failed to close file %q: %v", path, err)
				return
			}
		}
	}()
	go func() {
		for {
			<-ch2
			name := fmt.Sprintf("mem-profile-%s.pprof", time.Now().Format(timeFormat))
			path := filepath.Join(os.TempDir(), name)
			f, err := os.Create(path) // #nosec G303,G304
			if err != nil {
				klog.Errorf("failed to create memory profile file: %v", err)
				return
			}
			if err = pprof.WriteHeapProfile(f); err != nil {
				klog.Errorf("failed to write memory profile file: %v", err)
				if err = f.Close(); err != nil {
					klog.Errorf("failed to close file %q: %v", path, err)
				}
				return
			}
			if err = f.Close(); err != nil {
				klog.Errorf("failed to close file %q: %v", path, err)
				return
			}
		}
	}()
}
