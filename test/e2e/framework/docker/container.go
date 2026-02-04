package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"k8s.io/utils/ptr"
)

var (
	dockerClient     *client.Client
	dockerClientOnce sync.Once
	dockerClientErr  error
)

// getDockerClient returns a shared Docker client instance.
// The client is initialized once and reused across all calls.
// Note: The client is not explicitly closed as it's intended for E2E tests
// where the process exits after tests complete.
func getDockerClient() (*client.Client, error) {
	dockerClientOnce.Do(func() {
		dockerClient, dockerClientErr = client.New(client.FromEnv)
	})
	return dockerClient, dockerClientErr
}

func ContainerList(filters map[string][]string) ([]container.Summary, error) {
	cli, err := getDockerClient()
	if err != nil {
		return nil, err
	}

	f := make(client.Filters, len(filters))
	for k, v := range filters {
		f.Add(k, v...)
	}
	result, err := cli.ContainerList(context.Background(), client.ContainerListOptions{All: true, Filters: f})
	if err != nil {
		return nil, err
	}

	return slices.Clone(result.Items), nil
}

func ContainerCreate(name, image, networkName string, cmd []string) (*container.InspectResponse, error) {
	cli, err := getDockerClient()
	if err != nil {
		return nil, err
	}

	options := client.ContainerCreateOptions{
		Name: name,
		Config: &container.Config{
			Image: image,
			Cmd:   cmd,
			Tty:   false,
		},
		HostConfig: &container.HostConfig{
			Privileged: true,
		},
		NetworkingConfig: &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				networkName: new(network.EndpointSettings),
			},
		},
	}

	result, err := cli.ContainerCreate(context.Background(), options)
	if err != nil {
		return nil, err
	}

	if _, err = cli.ContainerStart(context.Background(), result.ID, client.ContainerStartOptions{}); err != nil {
		return nil, err
	}

	info, err := cli.ContainerInspect(context.Background(), result.ID, client.ContainerInspectOptions{})
	if err != nil {
		return nil, err
	}

	return ptr.To(info.Container), nil
}

func ContainerInspect(id string) (*container.InspectResponse, error) {
	cli, err := getDockerClient()
	if err != nil {
		return nil, err
	}

	result, err := cli.ContainerInspect(context.Background(), id, client.ContainerInspectOptions{})
	if err != nil {
		return nil, err
	}

	return ptr.To(result.Container), nil
}

func ContainerRemove(id string) error {
	cli, err := getDockerClient()
	if err != nil {
		return err
	}

	_, err = cli.ContainerRemove(context.Background(), id, client.ContainerRemoveOptions{Force: true})
	return err
}

// CopyToContainer copies content to a container at the specified path.
// The content is provided as a byte slice and will be written to dstPath inside the container.
func CopyToContainer(containerID, dstPath string, content []byte, filename string) error {
	cli, err := getDockerClient()
	if err != nil {
		return err
	}

	// Create a tar archive containing the file
	tarContent, err := createTarArchive(filename, content)
	if err != nil {
		return err
	}

	_, err = cli.CopyToContainer(context.Background(), containerID, client.CopyToContainerOptions{
		DestinationPath: dstPath,
		Content:         tarContent,
	})
	return err
}

// CopyFileToContainer copies a file from the local filesystem to a container.
func CopyFileToContainer(containerID, srcPath, dstPath string) error {
	content, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}

	filename := filepath.Base(srcPath)
	return CopyToContainer(containerID, dstPath, content, filename)
}

// createTarArchive creates a tar archive containing a single file with the given content.
func createTarArchive(filename string, content []byte) (*bytes.Buffer, error) {
	buf := new(bytes.Buffer)
	tw := tar.NewWriter(buf)

	hdr := &tar.Header{
		Name: filename,
		Mode: 0o755,
		Size: int64(len(content)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return nil, err
	}
	if _, err := tw.Write(content); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}

	return buf, nil
}
