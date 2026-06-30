package kind

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/kubeovn/kube-ovn/test/e2e/framework"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/docker"
	"github.com/kubeovn/kube-ovn/test/e2e/framework/iproute"
)

const NetworkName = "kind"

const (
	labelCluster = "io.x-k8s.kind.cluster"
	labelRole    = "io.x-k8s.kind.role"
)

type Node struct {
	container.Summary
}

func (n *Node) Name() string {
	return strings.TrimPrefix(n.Names[0], "/")
}

func (n *Node) Exec(cmd ...string) (stdout, stderr []byte, err error) {
	return docker.Exec(n.ID, nil, cmd...)
}

func (n *Node) NetworkConnect(networkID string) error {
	for _, settings := range n.NetworkSettings.Networks {
		if settings.NetworkID == networkID {
			return nil
		}
	}
	// Connecting a node to the docker network can transiently fail with
	// "cannot program address ... conflicts with existing route" when a
	// previous spec's provider-network teardown has not finished flushing the
	// orphaned route from the node's network namespace. Retry to absorb that
	// convergence window, and treat an already-connected endpoint as success.
	var lastErr error
	if err := wait.PollUntilContextTimeout(context.Background(), 2*time.Second, time.Minute, true,
		func(context.Context) (bool, error) {
			lastErr = docker.NetworkConnect(networkID, n.ID)
			if lastErr == nil || strings.Contains(lastErr.Error(), "already exists in network") {
				lastErr = nil
				return true, nil
			}
			framework.Logf("retrying to connect node %s to docker network %s: %v", n.Name(), networkID, lastErr)
			return false, nil
		}); err != nil {
		if lastErr != nil {
			return fmt.Errorf("timed out connecting node %s to docker network %s: %w", n.Name(), networkID, lastErr)
		}
		return err
	}
	return nil
}

func (n *Node) NetworkDisconnect(networkID string) error {
	for _, settings := range n.NetworkSettings.Networks {
		if settings.NetworkID == networkID {
			// The node snapshot may be stale: docker can report the container
			// is no longer connected when a previous connect attempt rolled
			// back. Treat that as success (idempotent), and retry transient
			// failures while the endpoint is being released.
			var lastErr error
			if err := wait.PollUntilContextTimeout(context.Background(), 2*time.Second, time.Minute, true,
				func(context.Context) (bool, error) {
					lastErr = docker.NetworkDisconnect(networkID, n.ID)
					if lastErr == nil || strings.Contains(lastErr.Error(), "is not connected to network") {
						lastErr = nil
						return true, nil
					}
					framework.Logf("retrying to disconnect node %s from docker network %s: %v", n.Name(), networkID, lastErr)
					return false, nil
				}); err != nil {
				if lastErr != nil {
					return fmt.Errorf("timed out disconnecting node %s from docker network %s: %w", n.Name(), networkID, lastErr)
				}
				return err
			}
			return nil
		}
	}
	return nil
}

func (n *Node) ListLinks() ([]iproute.Link, error) {
	return iproute.AddressShow("", n.Exec)
}

func (n *Node) ListRoutes(nonLinkLocalUnicast bool) ([]iproute.Route, error) {
	routes, err := iproute.RouteShow("", "", n.Exec)
	if err != nil {
		return nil, err
	}

	if !nonLinkLocalUnicast {
		return routes, nil
	}

	result := make([]iproute.Route, 0, len(routes))
	for _, route := range routes {
		if route.Dst == "default" {
			result = append(result, route)
		}
		if ip := net.ParseIP(strings.Split(route.Dst, "/")[0]); !ip.IsLinkLocalUnicast() {
			result = append(result, route)
		}
	}
	return result, nil
}

func (n *Node) WaitLinkToDisappear(linkName string, interval time.Duration, deadline time.Time) error {
	err := wait.PollUntilContextTimeout(context.Background(), interval, time.Until(deadline), false, func(_ context.Context) (bool, error) {
		framework.Logf("Waiting for link %s in node %s to disappear", linkName, n.Name())
		links, err := n.ListLinks()
		if err != nil {
			return false, err
		}
		for _, link := range links {
			if link.IfName == linkName {
				framework.Logf("link %s still exists", linkName)
				return false, nil
			}
		}
		framework.Logf("link %s no longer exists", linkName)
		return true, nil
	})
	if err == nil {
		return nil
	}

	if errors.Is(err, context.DeadlineExceeded) {
		framework.Failf("timed out while waiting for link %s in node %s to disappear", linkName, n.Name())
	}
	framework.Failf("error occurred while waiting for link %s in node %s to disappear: %v", linkName, n.Name(), err)

	return err
}

func ListClusters() ([]string, error) {
	filters := map[string][]string{"label": {labelCluster}}
	nodeList, err := docker.ContainerList(filters)
	if err != nil {
		return nil, err
	}

	var clusters []string
	for _, node := range nodeList {
		if cluster := node.Labels[labelCluster]; !slices.Contains(clusters, cluster) {
			clusters = append(clusters, node.Labels[labelCluster])
		}
	}

	return clusters, nil
}

func ListNodes(cluster, role string) ([]Node, error) {
	labels := []string{labelCluster + "=" + cluster}
	if role != "" {
		// control-plane or worker
		labels = append(labels, labelRole+"="+role)
	}

	filters := map[string][]string{"label": labels}
	nodeList, err := docker.ContainerList(filters)
	if err != nil {
		return nil, err
	}

	nodes := make([]Node, 0, len(nodeList))
	for _, node := range nodeList {
		nodes = append(nodes, Node{node})
	}

	return nodes, nil
}

func IsKindProvided(providerID string) (string, bool) {
	// kind://docker/kube-ovn/kube-ovn-control-plane
	u, err := url.Parse(providerID)
	if err != nil || u.Scheme != "kind" || u.Host != "docker" {
		return "", false
	}

	fields := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(fields) != 2 {
		return "", false
	}
	return fields[0], true
}

func NetworkConnect(networkID string, nodes []Node) error {
	for _, node := range nodes {
		if err := node.NetworkConnect(networkID); err != nil {
			return err
		}
	}
	return nil
}

func NetworkDisconnect(networkID string, nodes []Node) error {
	for _, node := range nodes {
		if err := node.NetworkDisconnect(networkID); err != nil {
			return err
		}
	}
	return nil
}
