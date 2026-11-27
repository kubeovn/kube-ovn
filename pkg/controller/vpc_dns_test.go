package controller

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	corelisters "k8s.io/client-go/listers/core/v1"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func newTestDNSDeployment(namespace, name, k8sApp, containerName, image string) *appsv1.Deployment {
	return &appsv1.Deployment{
		Namespace: namespace,
		Name:      name,
		Labels:    map[string]string{CorednsLabelKey: k8sApp},
		Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: containerName, Image: image}},
		}}},
	}
}

// saveVpcDNSGlobals restores the package level vpc-dns config when the test ends
func saveVpcDNSGlobals(t *testing.T) {
	t.Helper()
	image, vip, nad, provider, version, host, port, enabled := corednsImage, corednsVip, nadName, nadProvider, cmVersion, k8sServiceHost, k8sServicePort, enableCoreDNS
	t.Cleanup(func() {
		corednsImage, corednsVip, nadName, nadProvider, cmVersion, k8sServiceHost, k8sServicePort, enableCoreDNS = image, vip, nad, provider, version, host, port, enabled
	})
}

func newVpcDNSTestController(t *testing.T, kubeObjects []runtime.Object, configMaps ...*corev1.ConfigMap) *Controller {
	t.Helper()
	cmIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, cm := range configMaps {
		require.NoError(t, cmIndexer.Add(cm))
	}
	return &Controller{
		config: &Configuration{
			PodNamespace: metav1.NamespaceSystem,
			KubeClient:   fake.NewClientset(kubeObjects...),
		},
		configMapsLister: corelisters.NewConfigMapLister(cmIndexer),
		vpcDNSLister:     kubeovnlister.NewVpcDnsLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})),
	}
}

func TestGetDefaultCoreDNSImage(t *testing.T) {
	ns := metav1.NamespaceSystem
	tests := []struct {
		name        string
		objects     []runtime.Object
		expected    string
		expectedErr string
	}{
		{
			name: "kubeadm deployment named coredns",
			objects: []runtime.Object{
				newTestDNSDeployment(ns, "coredns", "kube-dns", "coredns", "registry.k8s.io/coredns/coredns:v1.12.0"),
				newTestDNSDeployment(ns, "another-dns", "kube-dns", "coredns", "example.com/coredns:other"),
			},
			expected: "registry.k8s.io/coredns/coredns:v1.12.0",
		},
		{
			name: "deployment named coredns without coredns container",
			objects: []runtime.Object{
				newTestDNSDeployment(ns, "coredns", "kube-dns", "dns", "example.com/dns:v1"),
				newTestDNSDeployment(ns, "another-dns", "kube-dns", "coredns", "example.com/coredns:other"),
			},
			expectedErr: "coredns container not found",
		},
		{
			name: "rke2 deployment found by label",
			objects: []runtime.Object{
				newTestDNSDeployment(ns, "rke2-coredns-rke2-coredns", "kube-dns", "coredns", "rancher/hardened-coredns:v1.14.7-build20261020"),
				newTestDNSDeployment(ns, "rke2-coredns-rke2-coredns-autoscaler", "kube-dns-autoscaler", "autoscaler", "rancher/hardened-cluster-autoscaler:v1"),
			},
			expected: "rancher/hardened-coredns:v1.14.7-build20261020",
		},
		{
			name: "first labeled deployment by name with a coredns container",
			objects: []runtime.Object{
				newTestDNSDeployment(ns, "0-dns", "other", "coredns", "example.com/coredns:unlabeled"),
				newTestDNSDeployment(ns, "c-dns", "kube-dns", "coredns", "example.com/coredns:c"),
				newTestDNSDeployment(ns, "a-dns", "kube-dns", "dns", "example.com/dns:a"),
				newTestDNSDeployment(ns, "b-dns", "kube-dns", "coredns", "example.com/coredns:b"),
			},
			expected: "example.com/coredns:b",
		},
		{
			name: "no coredns deployment in kube-system",
			objects: []runtime.Object{
				newTestDNSDeployment(metav1.NamespaceDefault, "coredns", "kube-dns", "coredns", "example.com/coredns:default"),
			},
			expectedErr: "no deployment named coredns or labeled k8s-app=kube-dns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newVpcDNSTestController(t, tt.objects)
			// the fake tracker lists sorted by name, reverse the list so the sort in getDefaultCoreDNSImage is tested
			client := c.config.KubeClient.(*fake.Clientset)
			client.PrependReactor("list", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
				obj, err := client.Tracker().List(appsv1.SchemeGroupVersion.WithResource("deployments"),
					appsv1.SchemeGroupVersion.WithKind("Deployment"), action.GetNamespace())
				if err != nil {
					return true, nil, err
				}
				list := obj.(*appsv1.DeploymentList)
				slices.Reverse(list.Items)
				return true, list, nil
			})

			image, err := c.getDefaultCoreDNSImage()
			if tt.expectedErr != "" {
				require.ErrorContains(t, err, tt.expectedErr)
				require.Empty(t, image)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.expected, image)
		})
	}
}

func TestResyncVpcDNSConfigImage(t *testing.T) {
	rke2DNS := newTestDNSDeployment(metav1.NamespaceSystem, "rke2-coredns-rke2-coredns", "kube-dns", "coredns", "rancher/hardened-coredns:v1.14.7-build20261020")
	tests := []struct {
		name          string
		data          map[string]string
		objects       []runtime.Object
		expectedImage string
		expectEnabled bool
	}{
		{
			name:          "coredns-image set",
			data:          map[string]string{"enable-vpc-dns": "true", "coredns-image": "example.com/coredns:custom"},
			objects:       []runtime.Object{rke2DNS},
			expectedImage: "example.com/coredns:custom",
			expectEnabled: true,
		},
		{
			name:          "coredns-image not set, rke2 deployment",
			data:          map[string]string{"enable-vpc-dns": "true"},
			objects:       []runtime.Object{rke2DNS},
			expectedImage: "rancher/hardened-coredns:v1.14.7-build20261020",
			expectEnabled: true,
		},
		{
			name:          "coredns-image not set, no coredns deployment",
			data:          map[string]string{"enable-vpc-dns": "true"},
			expectedImage: "",
			expectEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saveVpcDNSGlobals(t)
			cmVersion, enableCoreDNS, corednsImage = "", false, "example.com/coredns:stale"
			cm := &corev1.ConfigMap{
				Namespace:       metav1.NamespaceSystem,
				Name:            util.VpcDNSConfig,
				ResourceVersion: "1",
				Data:            tt.data,
			}
			c := newVpcDNSTestController(t, tt.objects, cm)

			c.resyncVpcDNSConfig()

			require.Equal(t, tt.expectedImage, corednsImage)
			require.Equal(t, tt.expectEnabled, enableCoreDNS)
			require.Equal(t, "1", cmVersion)
			if _, ok := tt.data["coredns-image"]; ok {
				require.Empty(t, c.config.KubeClient.(*fake.Clientset).Actions(), "no coredns deployment lookup expected")
			}
		})
	}
}
