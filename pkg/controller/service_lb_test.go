package controller

import (
	"testing"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
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
				Annotations: map[string]string{
					util.AttachmentProvider:      "external.kube-system",
					util.LogicalSwitchAnnotation: "tenant-primary",
				},
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
			require.Equal(t, "tenant-primary", dp.Spec.Template.Annotations[util.LogicalSwitchAnnotation])
			require.Equal(t, svc.Spec.LoadBalancerIP, dp.Spec.Template.Annotations["external.kube-system.kubernetes.io/ip_address"])

			// Reconciliation must converge after the template has been hardened.
			require.Nil(t, c.updateLbSvcDeployment(svc, dp.DeepCopy()))
		})
	}
}

func TestUpdatePodLbSvcTemplateDrift(t *testing.T) {
	previousImage := vpcNatImage
	vpcNatImage = "kubeovn/vpc-nat-gateway:non-root"
	t.Cleanup(func() { vpcNatImage = previousImage })

	for _, name := range []string{"image", "security", "unchanged"} {
		t.Run(name, func(t *testing.T) {
			svc := &corev1.Service{
				Name: "tenant-lb", Namespace: "tenant",
				Annotations: map[string]string{util.AttachmentProvider: "external.kube-system"},
				Spec:        corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
			}
			dp := &appsv1.Deployment{
				Name: genLbSvcDpName(svc.Name), Namespace: svc.Namespace,
				Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{
					Annotations: map[string]string{
						nadv1.NetworkAttachmentAnnot:                        "kube-system/external",
						"external.kube-system.kubernetes.io/logical_switch": "",
					},
					Spec: corev1.PodSpec{
						AutomountServiceAccountToken: new(false),
						Containers: []corev1.Container{{
							Name: "lb-svc", Image: vpcNatImage, SecurityContext: lbSvcSecurityContext(nil),
						}},
					},
				}},
			}
			switch name {
			case "image":
				dp.Spec.Template.Spec.Containers[0].Image = "kubeovn/vpc-nat-gateway:old"
			case "security":
				dp.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{Privileged: new(true)}
			}
			queue := newTypedRateLimitingQueue[string]("test-lb-svc-template", nil)
			t.Cleanup(queue.ShutDown)
			c := &Controller{
				config:          &Configuration{KubeClient: fake.NewClientset(dp), EnablePodLbSvc: true},
				addServiceQueue: queue,
				podsLister:      corelisters.NewPodLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})),
			}
			key := svc.Namespace + "/" + svc.Name
			require.NoError(t, c.updatePodLbSvcWorkload(svc, &updateSvcObject{
				oldPorts: []corev1.ServicePort{{Port: 80}},
				newPorts: []corev1.ServicePort{{Port: 81}},
			}, key))
			if name == "unchanged" {
				require.Zero(t, queue.Len(), "a converged template must not recreate the gateway")
				return
			}
			require.Equal(t, 1, queue.Len(), "template drift must enter the full Deployment/Pod reconciliation")
			queuedKey, shutdown := queue.Get()
			require.False(t, shutdown)
			require.Equal(t, key, queuedKey)
			queue.Done(queuedKey)
			require.NoError(t, c.createLbSvcPod(svc, &nadv1.NetworkAttachmentDefinition{}))
			changed, err := c.checkLbSvcDeploymentChanged(svc)
			require.NoError(t, err)
			require.False(t, changed, "applying the queued template must resolve the drift")
		})
	}
}
