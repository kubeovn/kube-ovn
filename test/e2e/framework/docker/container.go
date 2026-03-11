package docker

import (
	"context"
	"slices"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

func ContainerList(filters map[string][]string) ([]container.Summary, error) {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil, err
	}
	defer cli.Close()

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
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil, err
	}
	defer cli.Close()

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

	return new(info.Container), nil
}

func ContainerInspect(id string) (*container.InspectResponse, error) {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	result, err := cli.ContainerInspect(context.Background(), id, client.ContainerInspectOptions{})
	if err != nil {
		return nil, err
	}

	return new(result.Container), nil
}

func ContainerRemove(id string) error {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return err
	}
	defer cli.Close()

	_, err = cli.ContainerRemove(context.Background(), id, client.ContainerRemoveOptions{Force: true})
	return err
}
