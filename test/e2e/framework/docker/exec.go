package docker

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

type ErrNonZeroExitCode struct {
	Cmd      []string
	ExitCode int
}

func (e ErrNonZeroExitCode) Error() string {
	return fmt.Sprintf("command %q exited with code %d", e.Cmd, e.ExitCode)
}

func Exec(id string, env []string, cmd ...string) (stdout, stderr []byte, err error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, nil, err
	}
	defer cli.Close()

	framework.Logf("Executing command %q in container %s", strings.Join(cmd, " "), id)
	config := container.ExecOptions{
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

	attachResp, err := cli.ContainerExecAttach(context.Background(), createResp.ID, container.ExecStartOptions{})
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
		err = ErrNonZeroExitCode{Cmd: slices.Clone(cmd), ExitCode: inspectResp.ExitCode}
	}

	stdout, stderr = bytes.TrimSpace(outBuf.Bytes()), bytes.TrimSpace(errBuf.Bytes())
	if len(stdout) != 0 {
		framework.Logf("stdout:\n%s", string(stdout))
	}
	if len(stderr) != 0 {
		framework.Logf("stderr:\n%s", string(stderr))
	}

	return stdout, stderr, err
}
