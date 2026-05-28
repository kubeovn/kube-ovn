package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestVpcEgressGatewayContainerBFDDDefaultResources(t *testing.T) {
	container := vpcEgressGatewayContainerBFDD("kube-ovn", "10.255.255.255", 100, 100, 5)

	require.Equal(t, "200m", container.Resources.Requests.Cpu().String())
	require.Equal(t, "200m", container.Resources.Limits.Cpu().String())
	require.Equal(t, "50Mi", container.Resources.Requests.Memory().String())
	require.Equal(t, "50Mi", container.Resources.Limits.Memory().String())
	ephemeralStorage := container.Resources.Limits[corev1.ResourceEphemeralStorage]
	require.Equal(t, "1Gi", ephemeralStorage.String())
}
