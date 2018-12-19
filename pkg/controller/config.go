package controller

import (
	"flag"
	"github.com/spf13/pflag"
	"net"
)

type Configuration struct {
	BindAddress    string
	OvnNbSocket    string
	OvnNbHost      net.IP
	OvnNbPort      int
	KubeConfigFile string
}

func ParseFlags() (*Configuration, error) {
	var (
		argBindAddress    = pflag.String("bind-address", "0.0.0.0:9090", "The address controller bind to.")
		argOvnNbSocket    = pflag.String("ovn-nb-socket", "", "The ovn-nb socket file. (If not set use ovn-nb-address)")
		argOvnNbHost      = pflag.IP("ovn-nb-host", net.IPv4(0, 0, 0, 0), "The ovn-nb host address. (If not set use ovn-nb-socket)")
		argOvnNbPort      = pflag.Int("ovn-nb-port", 6641, "")
		argKubeConfigFile = pflag.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information. If not set use the inCluster token.")
	)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()
	flag.CommandLine.Parse(make([]string, 0)) // Init for glog calls in kubernetes packages

	config := &Configuration{
		BindAddress:    *argBindAddress,
		OvnNbSocket:    *argOvnNbSocket,
		OvnNbHost:      *argOvnNbHost,
		OvnNbPort:      *argOvnNbPort,
		KubeConfigFile: *argKubeConfigFile,
	}
	return config, nil
}
