package util

import (
	"fmt"

	"k8s.io/klog/v2"
)

func LogFatalAndExit(err error, format string, a ...any) {
	klog.ErrorS(err, fmt.Sprintf(format, a...))
	klog.FlushAndExit(klog.ExitFlushTimeout, 1)
}
