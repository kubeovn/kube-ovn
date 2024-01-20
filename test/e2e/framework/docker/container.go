package docker

import (
	"context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	dockerfilters "github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

func ContainerList(filters map[string][]string) ([]types.Container, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	f := dockerfilters.NewArgs()
	for k, v := range filters {
		for _, v1 := range v {
			f.Add(k, v1)
		}
	}
	return cli.ContainerList(context.Background(), container.ListOptions{All: true, Filters: f})
}

func ContainerCreate(name, image, networkName string, cmd []string) (*types.ContainerJSON, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	containerConfig := &container.Config{
		Image: image,
		Cmd:   cmd,
		Tty:   false,
	}
	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			networkName: new(network.EndpointSettings),
		},
	}

	resp, err := cli.ContainerCreate(context.Background(), containerConfig, nil, networkConfig, nil, name)
	if err != nil {
		return nil, err
	}

	if err = cli.ContainerStart(context.Background(), resp.ID, container.StartOptions{}); err != nil {
		return nil, err
	}

	info, err := cli.ContainerInspect(context.Background(), resp.ID)
	if err != nil {
		return nil, err
	}

	return &info, nil
}

func ContainerInspect(id string) (*types.ContainerJSON, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	result, err := cli.ContainerInspect(context.Background(), id)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

func ContainerRemove(id string) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer cli.Close()

	return cli.ContainerRemove(context.Background(), id, container.RemoveOptions{Force: true})
}
