package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestResyncVpcNatConfigImagePullSecret(t *testing.T) {
	tests := []struct {
		name           string
		configMap      *corev1.ConfigMap
		initialSecret  string
		expectedSecret string
	}{
		{
			name: "imagePullSecret present is applied",
			configMap: &corev1.ConfigMap{
				Name:      util.VpcNatConfig,
				Namespace: "kube-system",
				Data: map[string]string{
					"image":           "kubeovn/vpc-nat-gateway:v1",
					"imagePullSecret": "my-registry-secret",
				},
			},
			initialSecret:  "",
			expectedSecret: "my-registry-secret",
		},
		{
			name: "imagePullSecret absent resets to empty",
			configMap: &corev1.ConfigMap{
				Name:      util.VpcNatConfig,
				Namespace: "kube-system",
				Data: map[string]string{
					"image": "kubeovn/vpc-nat-gateway:v1",
				},
			},
			initialSecret:  "stale-secret",
			expectedSecret: "",
		},
		{
			name:           "missing configmap does not panic and keeps previous value",
			configMap:      nil,
			initialSecret:  "unchanged-secret",
			expectedSecret: "unchanged-secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var configMaps []*corev1.ConfigMap
			if tt.configMap != nil {
				configMaps = append(configMaps, tt.configMap)
			}

			fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				ConfigMaps: configMaps,
			})
			require.NoError(t, err)
			controller := fakeController.fakeController
			controller.config.PodNamespace = "kube-system"

			oldSecret := vpcNatImagePullSecret
			vpcNatImagePullSecret = tt.initialSecret
			t.Cleanup(func() { vpcNatImagePullSecret = oldSecret })

			controller.resyncVpcNatConfig()

			require.Equal(t, tt.expectedSecret, vpcNatImagePullSecret)
		})
	}
}
