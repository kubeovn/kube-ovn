package webhook

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestServiceUpdateHook(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1.AddToScheme(scheme))
	hook := &ValidatingHook{decoder: admission.NewDecoder(scheme)}

	service := func(gateway, eip string) *v1.Service {
		return &v1.Service{
			Namespace: "default",
			Name:      "web",
			Annotations: map[string]string{
				util.VpcNatGatewayAnnotation: gateway,
				util.EipAnnotation:           eip,
			},
		}
	}

	t.Run("allows initial binding", func(t *testing.T) {
		resp := hook.ServiceUpdateHook(context.Background(), updateRequest(t, service("", ""), service("gw-a", "eip-a")))
		require.True(t, resp.Allowed)
	})

	t.Run("rejects changing or removing gateway", func(t *testing.T) {
		resp := hook.ServiceUpdateHook(context.Background(), updateRequest(t, service("gw-a", "eip-a"), service("gw-b", "eip-a")))
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "vpc nat gateway cannot change")
		resp = hook.ServiceUpdateHook(context.Background(), updateRequest(t, service("gw-a", "eip-a"), service("", "")))
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "vpc nat gateway cannot change")
	})

	t.Run("rejects changing eip", func(t *testing.T) {
		resp := hook.ServiceUpdateHook(context.Background(), updateRequest(t, service("gw-a", "eip-a"), service("gw-a", "eip-b")))
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "eip cannot change")
	})
}
