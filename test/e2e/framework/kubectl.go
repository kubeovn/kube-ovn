package framework

import (
	"fmt"
	"strings"

	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"
)

func KubectlExec(namespace, name string, cmd ...string) (stdout, stderr []byte, err error) {
	c := strings.Join(cmd, " ")
	outStr, errStr, err := e2epodoutput.RunHostCmdWithFullOutput(namespace, name, c)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to exec cmd %q in %s of namespace %s: %w\nstderr:\n%s", c, name, namespace, err, errStr)
	}

	return []byte(outStr), []byte(errStr), nil
}

func ovnExecSvc(db string, cmd ...string) (stdout, stderr []byte, err error) {
	c := strings.Join(cmd, " ")
	outStr, errStr, err := e2epodoutput.RunHostCmdWithFullOutput(KubeOvnNamespace, "svc/ovn-"+db, c)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to exec ovn %s cmd %q: %w\nstderr:\n%s", db, c, err, errStr)
	}

	return []byte(outStr), []byte(errStr), nil
}

// NBExec executes the command in svc/ovn-nb and returns the result
func NBExec(cmd ...string) (stdout, stderr []byte, err error) {
	return ovnExecSvc("nb", cmd...)
}

// SBExec executes the command in svc/ovn-sb and returns the result
func SBExec(cmd ...string) (stdout, stderr []byte, err error) {
	return ovnExecSvc("sb", cmd...)
}
