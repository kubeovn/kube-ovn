package framework

import (
	"fmt"
	"strings"

	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"
)

func KubectlExec(namespace, pod string, cmd ...string) (stdout, stderr []byte, err error) {
	c := strings.Join(cmd, " ")
	outStr, errStr, err := e2epodoutput.RunHostCmdWithFullOutput(namespace, pod, c)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to exec cmd %q in pod %s/%s: %v\nstderr:\n%s", c, namespace, pod, err, errStr)
	}

	return []byte(outStr), []byte(errStr), nil
}
