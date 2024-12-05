package util

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPodRoutes(t *testing.T) {
	routes := NewPodRoutes()
	require.NotNil(t, routes)
	require.Empty(t, routes)
	annotations, err := routes.ToAnnotations()
	require.NoError(t, err)
	require.Len(t, annotations, 0)

	routes.Add("foo", "0.0.0.1", "1.1.1.1")
	routes.Add("foo", "0.0.1.0/24", "1.1.1.1")
	routes.Add("foo", "0.1.0.0/16", "1.1.1.2")
	require.Len(t, routes, 1)
	require.Len(t, routes["foo"], 2)
	require.Len(t, routes["foo"]["1.1.1.1"], 2)
	require.Len(t, routes["foo"]["1.1.1.2"], 1)
	annotations, err = routes.ToAnnotations()
	require.NoError(t, err)
	require.Len(t, annotations, 1)

	routes.Add("foo", "0.0.0.1", "")
	routes.Add("foo", "", "1.1.1.3")
	routes.Add("foo", "", "")
	require.Len(t, routes, 1)
	require.Len(t, routes["foo"], 2)
	require.Len(t, routes["foo"]["1.1.1.1"], 2)
	require.Len(t, routes["foo"]["1.1.1.2"], 1)
	annotations, err = routes.ToAnnotations()
	require.NoError(t, err)
	require.Len(t, annotations, 1)

	routes.Add("bar", "192.168.0.1/32", "2.2.2.2")
	require.Len(t, routes, 2)
	require.Len(t, routes["foo"], 2)
	require.Len(t, routes["foo"]["1.1.1.1"], 2)
	require.Len(t, routes["foo"]["1.1.1.2"], 1)
	require.Len(t, routes["bar"], 1)
	require.Len(t, routes["bar"]["2.2.2.2"], 1)
	annotations, err = routes.ToAnnotations()
	require.NoError(t, err)
	require.Len(t, annotations, 2)

	// empty routes
	routes = PodRoutes{"foo": PodProviderRoutes{}}
	annotations, err = routes.ToAnnotations()
	require.NoError(t, err)
	require.Empty(t, annotations)

	// empty gateway
	routes["foo"] = PodProviderRoutes{"": []string{"1.1.1.1"}}
	annotations, err = routes.ToAnnotations()
	require.NoError(t, err)
	require.Empty(t, annotations)
}
