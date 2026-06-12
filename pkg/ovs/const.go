package ovs

import (
	"os"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func CmdSSLArgs() []string {
	if fileExists(util.SslCAPath) && fileExists(util.SslClientCertPath) && fileExists(util.SslClientKeyPath) {
		return []string{
			"-C", util.SslCAPath,
			"-p", util.SslClientKeyPath,
			"-c", util.SslClientCertPath,
		}
	}
	return []string{
		"-C", util.SslCACert,
		"-p", util.SslKeyPath,
		"-c", util.SslCertPath,
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
