package docker

import (
	"context"

	"github.com/docker/docker/api/types"
	dockerfilters "github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

func ListContainers(filters map[string][]string) ([]types.Container, error) {
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
	return cli.ContainerList(context.Background(), types.ContainerListOptions{All: true, Filters: f})
}
