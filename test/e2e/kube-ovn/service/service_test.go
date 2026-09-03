package service

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/stretchr/testify/require"
)

func TestOtherNodeName(t *testing.T) {
	nodes := []corev1.Node{
		{Name: "node-a"},
		{Name: "node-b"},
	}

	require.Equal(t, "node-b", otherNodeName(nodes, "node-a"))
	require.Empty(t, otherNodeName(nodes[:1], "node-a"))
}
