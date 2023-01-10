package docker

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

type ErrNonZeroExitCode struct {
	cmd  string
	code int
}

func (e ErrNonZeroExitCode) Error() string {
	return fmt.Sprintf("command %q exited with code %d", e.cmd, e.code)
}

func Exec(id string, env []string, cmd ...string) (stdout, stderr []byte, err error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, nil, err
	}
	defer cli.Close()

	framework.Logf("Executing command %q in container %s", strings.Join(cmd, " "), id)
	config := types.ExecConfig{
		Privileged:   true,
		AttachStderr: true,
		AttachStdout: true,
		Env:          env,
		Cmd:          cmd,
	}
	createResp, err := cli.ContainerExecCreate(context.Background(), id, config)
	if err != nil {
		return nil, nil, err
	}

	attachResp, err := cli.ContainerExecAttach(context.Background(), createResp.ID, types.ExecStartCheck{})
	if err != nil {
		return nil, nil, err
	}
	defer attachResp.Close()

	var outBuf, errBuf bytes.Buffer
	if _, err = stdcopy.StdCopy(&outBuf, &errBuf, attachResp.Reader); err != nil {
		return nil, nil, err
	}

	inspectResp, err := cli.ContainerExecInspect(context.Background(), createResp.ID)
	if err != nil {
		return nil, nil, err
	}

	if inspectResp.ExitCode != 0 {
		framework.Logf("command exited with code %d", inspectResp.ExitCode)
		err = ErrNonZeroExitCode{cmd: strings.Join(cmd, " "), code: inspectResp.ExitCode}
	}

	stdout, stderr = outBuf.Bytes(), errBuf.Bytes()
	framework.Logf("stdout: %s", string(stdout))
	framework.Logf("stderr: %s", string(stderr))

	return
}
