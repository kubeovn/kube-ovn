package health_check

import (
	"flag"
	"net"
	"os"
	"time"

	"github.com/spf13/pflag"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func CmdMain() {
	port := pflag.Int32("port", 0, "Target port")
	tls := pflag.Bool("tls", false, "Dial the server with TLS")

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

	if *port <= 0 {
		klog.Errorf("invalid port: %d", port)
		os.Exit(1)
	}

	ip := os.Getenv("POD_IP")
	if net.ParseIP(ip) == nil {
		klog.Errorf("invalid ip: %q", ip)
		os.Exit(1)
	}

	addr := util.JoinHostPort(ip, *port)
	if *tls {
		addr = "tls://" + addr
	} else {
		addr = "tcp://" + addr
	}
	if err := util.DialTCP(addr, 100*time.Millisecond, false); err != nil {
		util.LogFatalAndExit(err, "failed to probe the socket")
	}
}
