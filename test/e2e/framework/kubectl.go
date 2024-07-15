package framework

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	e2ekubectl "k8s.io/kubernetes/test/e2e/framework/kubectl"
)

func KubectlConfigUseContext(ctx context.Context, kubeContext string) {
	ginkgo.GinkgoHelper()

	var timeout <-chan time.Time
	if dl, ok := ctx.Deadline(); ok {
		timeout = time.After(time.Until(dl.Truncate(2 * time.Second)))
	}

	_, err := e2ekubectl.NewKubectlCommand("", "config", "use-context", kubeContext).WithTimeout(timeout).Exec()
	ExpectNoError(err)
}

func KubectlKo(ctx context.Context, subCommand string) string {
	ginkgo.GinkgoHelper()

	var timeout <-chan time.Time
	if dl, ok := ctx.Deadline(); ok {
		timeout = time.After(time.Until(dl.Truncate(2 * time.Second)))
	}

	args := strings.Fields(subCommand)
	args = slices.Insert(args, 0, "ko")
	stdout, err := e2ekubectl.NewKubectlCommand("", args...).WithTimeout(timeout).Exec()
	ExpectNoError(err)
	return strings.TrimSpace(stdout)
}

func KubectlExec(ctx context.Context, namespace, name, cmd string) (stdout, stderr []byte, err error) {
	var timeout <-chan time.Time
	if dl, ok := ctx.Deadline(); ok {
		timeout = time.After(time.Until(dl.Truncate(2 * time.Second)))
	}

	outStr, errStr, err := e2ekubectl.NewKubectlCommand(namespace, "exec", name, "--", "/bin/sh", "-x", "-c", cmd).WithTimeout(timeout).ExecWithFullOutput()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to exec cmd %q in %s of namespace %s: %v\nstderr:\n%s", cmd, name, namespace, err, errStr)
	}

	return []byte(outStr), []byte(errStr), nil
}

func KubectlExecOrDie(ctx context.Context, namespace, name, cmd string) (stdout, stderr []byte) {
	var timeout <-chan time.Time
	if dl, ok := ctx.Deadline(); ok {
		timeout = time.After(time.Until(dl.Truncate(2 * time.Second)))
	}

	outStr, errStr, err := e2ekubectl.NewKubectlCommand(namespace, "exec", name, "--", "/bin/sh", "-x", "-c", cmd).WithTimeout(timeout).ExecWithFullOutput()
	ExpectNoError(err)
	return []byte(outStr), []byte(errStr)
}

func ovnExecSvc(ctx context.Context, db, cmd string) (stdout, stderr []byte, err error) {
	return KubectlExec(ctx, KubeOvnNamespace, "svc/ovn-"+db, cmd)
}

// NBExec executes the command in svc/ovn-nb and returns the result
func NBExec(ctx context.Context, cmd string) (stdout, stderr []byte, err error) {
	return ovnExecSvc(ctx, "nb", cmd)
}

// SBExec executes the command in svc/ovn-sb and returns the result
func SBExec(ctx context.Context, cmd string) (stdout, stderr []byte, err error) {
	return ovnExecSvc(ctx, "sb", cmd)
}
