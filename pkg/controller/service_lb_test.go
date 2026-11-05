package controller

import (
	"testing"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestCreateLbSvcPodSecurity(t *testing.T) {
	previousImage := vpcNatImage
	vpcNatImage = "kubeovn/vpc-nat-gateway:non-root"
	t.Cleanup(func() { vpcNatImage = previousImage })

	for _, legacy := range []bool{false, true} {
		name := "create"
		if legacy {
			name = "upgrade existing privileged deployment"
		}
		t.Run(name, func(t *testing.T) {
			client := fake.NewClientset()
			cmIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
			require.NoError(t, cmIndexer.Add(&corev1.ConfigMap{
				Name: util.VpcNatConfig, Namespace: metav1.NamespaceSystem,
				Data: map[string]string{"nodeSelector": "kubernetes.io/os: linux"},
			}))
			c := &Controller{
				config:           &Configuration{KubeClient: client, PodNamespace: metav1.NamespaceSystem},
				configMapsLister: corelisters.NewConfigMapLister(cmIndexer),
			}
			svc := &corev1.Service{
				Name: "tenant-lb", Namespace: "tenant",
				Annotations: map[string]string{util.AttachmentProvider: "external.kube-system"},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer, LoadBalancerIP: "192.0.2.10",
				},
			}
			nad := &nadv1.NetworkAttachmentDefinition{}
			deployments := client.AppsV1().Deployments(svc.Namespace)
			if legacy {
				dp := c.genLbSvcDeployment(svc, nad)
				dp.Namespace = svc.Namespace
				dp.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
					Privileged: new(true), AllowPrivilegeEscalation: new(true),
					SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
				}
				dp.Spec.Template.Spec.AutomountServiceAccountToken = nil
				dp.Spec.Template.Spec.Containers[0].Image = "kubeovn/vpc-nat-gateway:legacy"
				_, err := deployments.Create(t.Context(), dp, metav1.CreateOptions{})
				require.NoError(t, err)
			}

			require.NoError(t, c.createLbSvcPod(svc, nad))
			dp, err := deployments.Get(t.Context(), genLbSvcDpName(svc.Name), metav1.GetOptions{})
			require.NoError(t, err)
			podSpec := dp.Spec.Template.Spec
			require.Len(t, podSpec.Containers, 1)
			sc := podSpec.Containers[0].SecurityContext
			require.NotNil(t, sc)
			require.Equal(t, new(false), sc.Privileged)
			require.Equal(t, new(false), sc.AllowPrivilegeEscalation)
			require.Equal(t, new(true), sc.RunAsNonRoot)
			require.Equal(t, new(int64(65532)), sc.RunAsUser)
			require.Equal(t, new(int64(65532)), sc.RunAsGroup)
			require.Equal(t, vpcNatImage, podSpec.Containers[0].Image)
			require.NotNil(t, sc.Capabilities)
			require.ElementsMatch(t, []corev1.Capability{"ALL"}, sc.Capabilities.Drop)
			require.ElementsMatch(t, []corev1.Capability{"NET_ADMIN", "NET_RAW"}, sc.Capabilities.Add)
			if legacy {
				require.Equal(t, &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}, sc.SeccompProfile)
			}
			require.Equal(t, new(false), podSpec.AutomountServiceAccountToken)
			require.False(t, podSpec.HostNetwork)
			require.False(t, podSpec.HostPID)
			require.False(t, podSpec.HostIPC)
			require.Empty(t, podSpec.Volumes)
			require.Equal(t, "external.kube-system", getAttachNetworkProvider(svc))
			require.Equal(t, "kube-system/external", dp.Spec.Template.Annotations[nadv1.NetworkAttachmentAnnot])
			require.Equal(t, svc.Spec.LoadBalancerIP, dp.Spec.Template.Annotations["external.kube-system.kubernetes.io/ip_address"])

			// Reconciliation must converge after the template has been hardened.
			require.Nil(t, c.updateLbSvcDeployment(svc, dp.DeepCopy()))
		})
	}
}
