package daemon

import (
	current "github.com/containernetworking/cni/pkg/types/100"

	cniexec "github.com/kubeovn/kube-ovn/pkg/cni"
	"github.com/kubeovn/kube-ovn/pkg/request"
)

// CNIExecutorConfig is kept as a source-compatible alias for consumers that
// used the executor before it moved out of the daemon package.
type CNIExecutorConfig = cniexec.ExecutorConfig

// CNIExecutor is kept as a source-compatible alias for the standalone CNI
// executor implementation.
type CNIExecutor = cniexec.Executor

// NewCNIExecutor forwards to the standalone CNI executor package.
func NewCNIExecutor(config CNIExecutorConfig) *CNIExecutor {
	return cniexec.NewExecutor(config)
}

// CNIResultFromPlan forwards to the standalone CNI result conversion helper.
func CNIResultFromPlan(plan *request.CNIPlan, execution *request.CNIExecutionResult) (current.Result, error) {
	return cniexec.ResultFromPlan(plan, execution)
}
