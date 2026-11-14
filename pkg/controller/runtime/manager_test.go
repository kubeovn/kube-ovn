package controllerruntime

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/controller"
)

func TestNewSubnetWriterFenceRequiresIdentityAndClient(t *testing.T) {
	_, err := newSubnetWriterFence(nil)
	require.EqualError(t, err, "subnet writer fence configuration is incomplete")
	_, err = newSubnetWriterFence(&controller.Configuration{KubeClient: k8sfake.NewSimpleClientset(), PodNamespace: "kube-system", PodName: "pod"})
	require.EqualError(t, err, "subnet writer durable fence is not configured")
}

func TestNewSubnetWriterFenceUsesControllerIdentity(t *testing.T) {
	fence, err := newSubnetWriterFence(&controller.Configuration{
		KubeClient:     k8sfake.NewSimpleClientset(),
		PodNamespace:   "kube-system",
		PodName:        "pod-a",
		LeaderElection: controller.LeaderElectionConfiguration{LeaseDuration: time.Minute},
	})
	require.NoError(t, err)
	require.NotNil(t, fence)
}

func TestNewSchemeRegistersMigrationTypes(t *testing.T) {
	scheme, err := NewScheme()
	require.NoError(t, err)

	for _, kind := range []string{"Vlan", "Subnet", "IPPool", "IP"} {
		gvk := kubeovnv1.SchemeGroupVersion.WithKind(kind)
		_, err := scheme.New(gvk)
		require.NoErrorf(t, err, "scheme should register %s", gvk)
	}

	_, err = scheme.New(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"})
	require.NoError(t, err)
}

func TestNewManagerRejectsMissingConfiguration(t *testing.T) {
	_, err := NewManager(nil)
	require.EqualError(t, err, "controller-runtime manager configuration is nil")

	_, err = NewManager(&controller.Configuration{})
	require.EqualError(t, err, "controller-runtime manager Kubernetes REST config is nil")
}

func TestNewManagerBuildsWithoutStartingBusinessControllers(t *testing.T) {
	mgr, err := NewManager(&controller.Configuration{
		KubeRestConfig: &rest.Config{Host: "https://127.0.0.1"},
	})
	require.NoError(t, err)
	require.NotNil(t, mgr)
}

func TestSetupSubnetShadowReconciler(t *testing.T) {
	require.EqualError(t, setupSubnetShadowReconciler(nil), "controller-runtime manager is nil")

	mgr, err := NewManager(&controller.Configuration{
		KubeRestConfig: &rest.Config{Host: "https://127.0.0.1"},
	})
	require.NoError(t, err)
	require.NoError(t, setupSubnetShadowReconciler(mgr))
}

func TestSetupSubnetStatusReconcilerRequiresBackend(t *testing.T) {
	require.EqualError(t, setupSubnetStatusReconciler(nil, nil), "controller-runtime manager is nil")

	mgr, err := NewManager(&controller.Configuration{
		KubeRestConfig: &rest.Config{Host: "https://127.0.0.1"},
	})
	require.NoError(t, err)
	require.EqualError(t, setupSubnetStatusReconciler(mgr, nil), "subnet status runtime backend is nil")
}

func TestTrimCacheObjectUsesControllerTransforms(t *testing.T) {
	pod := &corev1.Pod{Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "cache"}}}}
	pod.ManagedFields = []metav1.ManagedFieldsEntry{{Manager: "controller"}}
	ret, err := trimCacheObject(pod)
	require.NoError(t, err)
	trimmedPod, ok := ret.(*corev1.Pod)
	require.True(t, ok)
	require.Empty(t, trimmedPod.ManagedFields)
	require.Empty(t, trimmedPod.Spec.Volumes)

	configMap := &corev1.ConfigMap{}
	configMap.ManagedFields = []metav1.ManagedFieldsEntry{{Manager: "controller"}}
	ret, err = trimCacheObject(configMap)
	require.NoError(t, err)
	trimmedConfigMap, ok := ret.(*corev1.ConfigMap)
	require.True(t, ok)
	require.Empty(t, trimmedConfigMap.ManagedFields)
}
