package kind

import (
	"errors"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecOnNodesUntilFirstError(t *testing.T) {
	nodes := []Node{
		{Summary: container.Summary{Names: []string{"/node-1"}}},
		{Summary: container.Summary{Names: []string{"/node-2"}}},
		{Summary: container.Summary{Names: []string{"/node-3"}}},
	}

	processed := make([]string, 0, len(nodes))
	expectedErr := errors.New("boom")
	err := execOnNodesUntilFirstError(nodes, func(n Node) error {
		processed = append(processed, n.Name())
		if n.Name() == "node-2" {
			return expectedErr
		}
		return nil
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, expectedErr)
	assert.Equal(t, []string{"node-1", "node-2"}, processed)
}
