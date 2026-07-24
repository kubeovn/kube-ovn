package service

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/require"
)

func TestOtherNodeName(t *testing.T) {
	nodes := []corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node-b"}},
	}

	require.Equal(t, "node-b", otherNodeName(nodes, "node-a"))
	require.Empty(t, otherNodeName(nodes[:1], "node-a"))
}
