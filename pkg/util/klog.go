package util

import (
	"fmt"
	"os"
	"time"

	"k8s.io/klog/v2"
)

const klogExitFlushTimeout = 10 * time.Second

func LogFatalAndExit(err error, format string, a ...interface{}) {
	klog.ErrorS(err, fmt.Sprintf(format, a...))

	done := make(chan bool, 1)
	go func() {
		klog.Flush() // calls logging.lockAndFlushAll()
		done <- true
	}()
	select {
	case <-done:
	case <-time.After(klogExitFlushTimeout):
		fmt.Fprintln(os.Stderr, "klog: Flush took longer than", klogExitFlushTimeout)
	}

	os.Exit(1)
}
