package ovs

import "github.com/kubeovn/kube-ovn/pkg/util"

func CmdSSLArgs() []string {
	return []string{
		"-C", util.SslCACert,
		"-p", util.SslKeyPath,
		"-c", util.SslCertPath,
	}
}
