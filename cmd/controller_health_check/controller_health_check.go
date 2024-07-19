package controller_health_check

import (
	"flag"
	"os"
	"time"

	"github.com/spf13/pflag"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func CmdMain() {
	tls := pflag.Bool("tls", false, "Whether kube-ovn-controller uses TLS")

	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)

	// sync the glog and klog flags.
	pflag.CommandLine.VisitAll(func(f1 *pflag.Flag) {
		f2 := klogFlags.Lookup(f1.Name)
		if f2 != nil {
			value := f1.Value.String()
			if err := f2.Value.Set(value); err != nil {
				util.LogFatalAndExit(err, "failed to set pflag")
			}
		}
	})

	pflag.CommandLine.AddGoFlagSet(klogFlags)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	addr := "127.0.0.1:10660"
	if os.Getenv("ENABLE_BIND_LOCAL_IP") == "true" {
		addr = util.JoinHostPort(os.Getenv("POD_IP"), 10660)
	}

	if *tls {
		addr = "tls://" + addr
	} else {
		addr = "tcp://" + addr
	}

	if err := util.DialTCP(addr, time.Second, false); err != nil {
		util.LogFatalAndExit(err, "failed to probe the socket")
	}
}
