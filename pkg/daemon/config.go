package daemon

import (
	"flag"
	"github.com/spf13/pflag"
	"k8s.io/klog"
)

type Configuration struct {
	BindSocket        string
	ControllerAddress string
	OvsSocket         string
	KubeConfigFile    string
}

// TODO: validate configuration
func ParseFlags() (*Configuration, error) {
	var (
		argBindSocket        = pflag.String("bind-socket", "/var/run/cniserver.sock", "The socket daemon bind to.")
		argControllerAddress = pflag.String("controller-address", "", "The address to controller")
		argOvsSocket         = pflag.String("ovs-socket", "", "The socket to local ovs-server")
		argKubeConfigFile    = pflag.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information. If not set use the inCluster token.")
	)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()
	flag.CommandLine.Parse(make([]string, 0)) // Init for glog calls in kubernetes packages

	config := &Configuration{
		BindSocket:        *argBindSocket,
		ControllerAddress: *argControllerAddress,
		OvsSocket:         *argOvsSocket,
		KubeConfigFile:    *argKubeConfigFile,
	}
	klog.Infof("bind socket: %s", config.BindSocket)
	klog.Infof("controller address at %s", config.ControllerAddress)
	klog.Infof("ovs socket at %s", config.OvsSocket)
	return config, nil
}
