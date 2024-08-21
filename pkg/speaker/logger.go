package speaker

import (
	bgplog "github.com/osrg/gobgp/v3/pkg/log"
	"k8s.io/klog/v2"
)

// bgpLogger is a struct implementing the GoBGP logger interface
// This is useful to inject our custom klog logger into the GoBGP speaker
type bgpLogger struct{}

func (k bgpLogger) Panic(msg string, fields bgplog.Fields) {
	klog.Fatalf("%s %v", msg, fields)
}

func (k bgpLogger) Fatal(msg string, fields bgplog.Fields) {
	klog.Fatalf("%s %v", msg, fields)
}

func (k bgpLogger) Error(msg string, fields bgplog.Fields) {
	klog.Errorf("%s %v", msg, fields)
}

func (k bgpLogger) Warn(msg string, fields bgplog.Fields) {
	klog.Warningf("%s %v", msg, fields)
}

func (k bgpLogger) Info(msg string, fields bgplog.Fields) {
	klog.Infof("%s %v", msg, fields)
}

func (k bgpLogger) Debug(msg string, fields bgplog.Fields) {
	klog.V(5).Infof("%s %v", msg, fields)
}

func (k bgpLogger) SetLevel(_ bgplog.LogLevel) {
}

func (k bgpLogger) GetLevel() bgplog.LogLevel {
	return 0
}
