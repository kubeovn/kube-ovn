package util

import (
	ps "github.com/bhendo/go-powershell"
	"github.com/bhendo/go-powershell/backend"
	"k8s.io/klog/v2"
)

func Powershell(cmd string) (string, error) {
	shell, err := ps.New(&backend.Local{})
	if err != nil {
		return "", err
	}
	defer shell.Exit()

	stdout, stderr, err := shell.Execute(cmd)
	if err != nil {
		klog.Errorf(`failed to execute command "%s" in powershell, err: %v, stderr: %s`, cmd, err, stderr)
		return "", err
	}
	return stdout, nil
}
