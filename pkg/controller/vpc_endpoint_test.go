package controller

import (
	"context"
	"fmt"
	"testing"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovninformers "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestVpcEndpointNaming(t *testing.T) {
	require.Equal(t, "vpc-eps-db-tcp", vpcEndpointServiceLBName("db", "TCP"))
	require.Equal(t, "vpc-ep-client-udp", vpcEndpointLBName("client", "UDP"))
	require.Equal(t, "vpc-ep-client", vpcEndpointVipCRName("client"))
	require.Equal(t, "vpc-eps/db", vpcEndpointServiceIPAMName("db"))
	require.Equal(t, "vpc-ep-snat/tenant-a", vpcEndpointSnatIPAMName("tenant-a"))
	require.Equal(t, "vpc-eps-db", vpcEndpointServiceDeployName("db"))
	require.Equal(t, "vpc-ep-client", vpcEndpointDeployName("client"))
}

func TestVpcEndpointSnatMatch(t *testing.T) {
	require.Equal(t, "ip4.dst == 100.65.1.20", vpcEndpointSnatMatch("100.65.1.20"))
	require.Equal(t, "ip6.dst == fd00:65::20", vpcEndpointSnatMatch("fd00:65::20"))
}

func TestVpcEndpointServiceAllowed(t *testing.T) {
	open := &kubeovnv1.VpcEndpointService{}
	require.True(t, vpcEndpointServiceAllowed(open, "consumer"))

	restricted := &kubeovnv1.VpcEndpointService{
		Spec: kubeovnv1.VpcEndpointServiceSpec{AllowedVpcs: []string{"a", "b"}},
	}
	require.True(t, vpcEndpointServiceAllowed(restricted, "a"))
	require.False(t, vpcEndpointServiceAllowed(restricted, "c"))
}

func TestVpcEndpointPreferIP(t *testing.T) {
	require.Equal(t, "10.0.0.1", vpcEndpointPreferIP("10.0.0.1", "fd00::1"))
	require.Equal(t, "fd00::1", vpcEndpointPreferIP("", "fd00::1"))
	require.Empty(t, vpcEndpointPreferIP("", ""))
}

func TestVpcEndpointServicePorts(t *testing.T) {
	require.Empty(t, vpcEndpointServicePorts(&corev1.Service{}))
	svc := &corev1.Service{Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{
		{Protocol: corev1.ProtocolTCP, Port: 80},
		{Protocol: corev1.ProtocolUDP, Port: 53},
	}}}
	require.Equal(t, "tcp/80,udp/53", vpcEndpointServicePorts(svc))
}

func TestVpcEndpointEffectiveServiceVpc(t *testing.T) {
	require.Equal(t, "ovn-cluster", vpcEndpointEffectiveServiceVpc(&corev1.Service{}, "ovn-cluster"))

	svc := &corev1.Service{Annotations: map[string]string{
		util.LogicalRouterAnnotation: "from-router",
	}}
	require.Equal(t, "from-router", vpcEndpointEffectiveServiceVpc(svc, "ovn-cluster"))

	svc.Annotations[util.VpcAnnotation] = "from-vpc"
	require.Equal(t, "from-vpc", vpcEndpointEffectiveServiceVpc(svc, "ovn-cluster"))
}

func TestValidateVpcEndpointServiceImmutability(t *testing.T) {
	require.NoError(t, validateVpcEndpointServiceImmutability(&kubeovnv1.VpcEndpointService{}))

	eps := &kubeovnv1.VpcEndpointService{
		Labels: map[string]string{
			util.VpcEndpointVpcLabel:     "vpc-a",
			util.VpcEndpointSvcNsLabel:   "ns-a",
			util.VpcEndpointSvcNameLabel: "svc-a",
		},
		Spec: kubeovnv1.VpcEndpointServiceSpec{Vpc: "vpc-a", Namespace: "ns-a", Service: "svc-a"},
	}
	require.NoError(t, validateVpcEndpointServiceImmutability(eps))
	eps.Spec.Vpc = "vpc-b"
	require.ErrorContains(t, validateVpcEndpointServiceImmutability(eps), "vpc is immutable")
	eps.Spec.Vpc = "vpc-a"
	eps.Spec.Namespace = "ns-b"
	require.ErrorContains(t, validateVpcEndpointServiceImmutability(eps), "namespace is immutable")
	eps.Spec.Namespace = "ns-a"
	eps.Spec.Service = "svc-b"
	require.ErrorContains(t, validateVpcEndpointServiceImmutability(eps), "service is immutable")
}

func TestValidateVpcEndpointImmutability(t *testing.T) {
	require.NoError(t, validateVpcEndpointImmutability(&kubeovnv1.VpcEndpoint{}))

	ep := &kubeovnv1.VpcEndpoint{
		Labels: map[string]string{
			util.VpcEndpointVpcLabel:     "vpc-a",
			util.VpcEndpointServiceLabel: "eps-a",
		},
		Spec: kubeovnv1.VpcEndpointSpec{Vpc: "vpc-a", EndpointService: "eps-a"},
	}
	require.NoError(t, validateVpcEndpointImmutability(ep))
	ep.Spec.Vpc = "vpc-b"
	require.ErrorContains(t, validateVpcEndpointImmutability(ep), "vpc is immutable")
	ep.Spec.Vpc = "vpc-a"
	ep.Spec.EndpointService = "eps-b"
	require.ErrorContains(t, validateVpcEndpointImmutability(ep), "endpointService is immutable")
}

func TestEnqueueVpcEndpointServiceHandlers(t *testing.T) {
	c := &Controller{
		addOrUpdateVpcEndpointServiceQueue: newTypedRateLimitingQueue[string]("AddOrUpdateVpcEndpointService", nil),
	}
	t.Cleanup(c.addOrUpdateVpcEndpointServiceQueue.ShutDown)

	eps := &kubeovnv1.VpcEndpointService{Name: "db"}
	c.enqueueAddVpcEndpointService(eps)
	require.Equal(t, 1, c.addOrUpdateVpcEndpointServiceQueue.Len())
	key, _ := c.addOrUpdateVpcEndpointServiceQueue.Get()
	c.addOrUpdateVpcEndpointServiceQueue.Done(key)
	require.Equal(t, "db", key)

	updated := eps.DeepCopy()
	updated.ResourceVersion = "2"
	c.enqueueUpdateVpcEndpointService(eps, updated)
	require.Equal(t, 1, c.addOrUpdateVpcEndpointServiceQueue.Len())
	key, _ = c.addOrUpdateVpcEndpointServiceQueue.Get()
	c.addOrUpdateVpcEndpointServiceQueue.Done(key)

	c.enqueueUpdateVpcEndpointService(eps, eps)
	require.Zero(t, c.addOrUpdateVpcEndpointServiceQueue.Len())

	c.enqueueDeleteVpcEndpointService(eps)
	require.Equal(t, 1, c.addOrUpdateVpcEndpointServiceQueue.Len())
}

func TestEnqueueVpcEndpointHandlers(t *testing.T) {
	c := &Controller{
		addOrUpdateVpcEndpointQueue: newTypedRateLimitingQueue[string]("AddOrUpdateVpcEndpoint", nil),
	}
	t.Cleanup(c.addOrUpdateVpcEndpointQueue.ShutDown)

	ep := &kubeovnv1.VpcEndpoint{Name: "client"}
	c.enqueueAddVpcEndpoint(ep)
	require.Equal(t, 1, c.addOrUpdateVpcEndpointQueue.Len())
	key, _ := c.addOrUpdateVpcEndpointQueue.Get()
	c.addOrUpdateVpcEndpointQueue.Done(key)
	require.Equal(t, "client", key)

	updated := ep.DeepCopy()
	updated.ResourceVersion = "2"
	c.enqueueUpdateVpcEndpoint(ep, updated)
	require.Equal(t, 1, c.addOrUpdateVpcEndpointQueue.Len())
	key, _ = c.addOrUpdateVpcEndpointQueue.Get()
	c.addOrUpdateVpcEndpointQueue.Done(key)

	c.enqueueUpdateVpcEndpoint(ep, ep)
	require.Zero(t, c.addOrUpdateVpcEndpointQueue.Len())

	c.enqueueDeleteVpcEndpoint(ep)
	require.Equal(t, 1, c.addOrUpdateVpcEndpointQueue.Len())
}

func TestVpcEndpointProviderPortMappings(t *testing.T) {
	port := corev1.ServicePort{Protocol: corev1.ProtocolTCP, Port: 80}
	require.Empty(t, vpcEndpointProviderPortMappings(port, nil))

	got := vpcEndpointProviderPortMappings(port, []string{"10.210.0.3:80", "10.210.0.2:80", "10.210.0.4:80"})
	require.Equal(t, []string{
		"tcp:80:10.210.0.2:80",
		"tcp:80:10.210.0.3:80",
		"tcp:80:10.210.0.4:80",
	}, got)

	got = vpcEndpointProviderPortMappings(corev1.ServicePort{Port: 443}, []string{"10.0.0.9:8443"})
	require.Equal(t, []string{"tcp:443:10.0.0.9:8443"}, got)
}

func TestVpcEndpointStitcherScriptEmbedded(t *testing.T) {
	require.Contains(t, vpcEndpointStitcherScriptData, "provider_sync()")
	require.Contains(t, vpcEndpointStitcherScriptData, "statistic --mode nth")
}

func TestEndpointSlicePortMatchesServicePort(t *testing.T) {
	tcp := corev1.ProtocolTCP
	udp := corev1.ProtocolUDP
	http := "http"
	port80 := int32(80)

	unnamedSvc := corev1.ServicePort{Protocol: tcp, Port: 80}
	emptyName := ""
	require.True(t, endpointSlicePortMatchesServicePort(discoveryv1.EndpointPort{Port: &port80, Protocol: &tcp}, unnamedSvc))
	require.True(t, endpointSlicePortMatchesServicePort(discoveryv1.EndpointPort{Port: &port80, Protocol: &tcp, Name: &emptyName}, unnamedSvc))
	require.False(t, endpointSlicePortMatchesServicePort(discoveryv1.EndpointPort{Port: &port80, Protocol: &tcp, Name: &http}, unnamedSvc))
	require.False(t, endpointSlicePortMatchesServicePort(discoveryv1.EndpointPort{Protocol: &tcp}, unnamedSvc))
	require.False(t, endpointSlicePortMatchesServicePort(discoveryv1.EndpointPort{Port: &port80, Protocol: &udp}, unnamedSvc))

	namedSvc := corev1.ServicePort{Name: "http", Protocol: tcp, Port: 80}
	require.True(t, endpointSlicePortMatchesServicePort(discoveryv1.EndpointPort{Port: &port80, Protocol: &tcp, Name: &http}, namedSvc))
	require.False(t, endpointSlicePortMatchesServicePort(discoveryv1.EndpointPort{Port: &port80, Protocol: &tcp}, namedSvc))
}

func TestVpcEndpointTransitProvider(t *testing.T) {
	require.Equal(t, "vpc-endpoint-transit.kube-system.ovn", vpcEndpointTransitProvider())
}

func TestVpcEndpointPodReady(t *testing.T) {
	require.False(t, vpcEndpointPodReady(&corev1.Pod{}))
	require.False(t, vpcEndpointPodReady(&corev1.Pod{Status: corev1.PodStatus{
		Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse}},
	}}))
	require.True(t, vpcEndpointPodReady(&corev1.Pod{Status: corev1.PodStatus{
		Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
	}}))
}

func TestFirstIPFromNetworkStatus(t *testing.T) {
	require.Empty(t, firstIPFromNetworkStatus("not-json", "vpc-endpoint-transit"))
	require.Empty(t, firstIPFromNetworkStatus(`[{"name":"kube-ovn","ips":["10.0.0.1"]}]`, "vpc-endpoint-transit"))
	require.Equal(t, "100.65.0.4", firstIPFromNetworkStatus(
		`[{"name":"kube-ovn","ips":["10.0.0.1"]},{"name":"kube-system/vpc-endpoint-transit","ips":["100.65.0.4","100.65.0.5"]}]`,
		"vpc-endpoint-transit",
	))
}

func TestVpcEndpointStitcherIPs(t *testing.T) {
	c := &Controller{config: &Configuration{VpcEndpointTransitSwitch: "vpc-endpoint-transit"}}
	transitProvider := vpcEndpointTransitProvider()

	vpcIP, transitIP, err := c.vpcEndpointStitcherIPs(&corev1.Pod{}, util.OvnProvider, transitProvider)
	require.NoError(t, err)
	require.Empty(t, vpcIP)
	require.Empty(t, transitIP)

	pod := &corev1.Pod{
		Annotations: map[string]string{
			util.IPAddressAnnotation: "10.210.0.5/24",
			fmt.Sprintf(util.IPAddressAnnotationTemplate, transitProvider): "100.65.0.4/16",
		},
		Status: corev1.PodStatus{PodIP: "10.210.0.9"},
	}
	vpcIP, transitIP, err = c.vpcEndpointStitcherIPs(pod, util.OvnProvider, transitProvider)
	require.NoError(t, err)
	require.Equal(t, "10.210.0.5", vpcIP)
	require.Equal(t, "100.65.0.4", transitIP)

	pod = &corev1.Pod{
		Annotations: map[string]string{
			nadv1.NetworkStatusAnnot: `[{"name":"kube-system/vpc-endpoint-transit","ips":["100.65.0.7"]}]`,
		},
		Status: corev1.PodStatus{PodIP: "10.210.0.8"},
	}
	vpcIP, transitIP, err = c.vpcEndpointStitcherIPs(pod, util.OvnProvider, transitProvider)
	require.NoError(t, err)
	require.Equal(t, "10.210.0.8", vpcIP)
	require.Equal(t, "100.65.0.7", transitIP)
}

func TestGenVpcEndpointStitcherDeployment(t *testing.T) {
	oldImage := vpcNatImage
	t.Cleanup(func() { vpcNatImage = oldImage })
	vpcNatImage = "kubeovn/vpc-nat-gateway:test"

	c := &Controller{config: &Configuration{Image: "kubeovn/kube-ovn:fallback"}}
	labels := map[string]string{"app": "vpc-eps-db"}
	annotations := map[string]string{util.VpcAnnotation: "provider"}
	deploy := c.genVpcEndpointStitcherDeployment("vpc-eps-db", "ns-a", labels, annotations)
	require.Equal(t, "vpc-eps-db", deploy.Name)
	require.Equal(t, "ns-a", deploy.Namespace)
	require.Equal(t, appsv1.RecreateDeploymentStrategyType, deploy.Spec.Strategy.Type)
	require.Equal(t, int32(1), *deploy.Spec.Replicas)
	require.Equal(t, "kubeovn/vpc-nat-gateway:test", deploy.Spec.Template.Spec.Containers[0].Image)
	require.Equal(t, vpcEndpointStitcherContainer, deploy.Spec.Template.Spec.Containers[0].Name)
	require.Equal(t, vpcEndpointStitcherScriptDir, deploy.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath)
	require.True(t, *deploy.Spec.Template.Spec.Containers[0].SecurityContext.Privileged)
}

func TestEnsureVpcEndpointStitcherConfigMap(t *testing.T) {
	client := fake.NewSimpleClientset()
	c := &Controller{config: &Configuration{
		KubeClient:   client,
		PodNamespace: metav1.NamespaceSystem,
	}}

	require.NoError(t, c.ensureVpcEndpointStitcherConfigMap())
	cm, err := client.CoreV1().ConfigMaps(metav1.NamespaceSystem).Get(context.Background(), vpcEndpointStitcherCMName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, cm.Data[vpcEndpointStitcherScriptKey], "provider_sync()")

	// Idempotent when content matches.
	require.NoError(t, c.ensureVpcEndpointStitcherConfigMap())

	// Update stale content.
	cm.Data[vpcEndpointStitcherScriptKey] = "stale"
	_, err = client.CoreV1().ConfigMaps(metav1.NamespaceSystem).Update(context.Background(), cm, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.NoError(t, c.ensureVpcEndpointStitcherConfigMap())
	cm, err = client.CoreV1().ConfigMaps(metav1.NamespaceSystem).Get(context.Background(), vpcEndpointStitcherCMName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, cm.Data[vpcEndpointStitcherScriptKey], "statistic --mode nth")
}

func TestEnsureVpcEndpointStitcherConfigMapIn(t *testing.T) {
	client := fake.NewSimpleClientset()
	c := &Controller{config: &Configuration{
		KubeClient:   client,
		PodNamespace: metav1.NamespaceSystem,
	}}
	require.NoError(t, c.ensureVpcEndpointStitcherConfigMapIn(metav1.NamespaceSystem))
	require.NoError(t, c.ensureVpcEndpointStitcherConfigMapIn("ep-provider"))
	cm, err := client.CoreV1().ConfigMaps("ep-provider").Get(context.Background(), vpcEndpointStitcherCMName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, cm.Data[vpcEndpointStitcherScriptKey], "consumer_sync()")

	// Second call is a no-op when already synced.
	require.NoError(t, c.ensureVpcEndpointStitcherConfigMapIn("ep-provider"))
}

func TestCreateOrUpdateVpcEndpointDeployment(t *testing.T) {
	oldImage := vpcNatImage
	t.Cleanup(func() { vpcNatImage = oldImage })
	vpcNatImage = "img:test"

	client := fake.NewSimpleClientset()
	c := &Controller{config: &Configuration{KubeClient: client, Image: "img:fallback"}}
	desired := c.genVpcEndpointStitcherDeployment("vpc-eps-db", "ns-a", map[string]string{"app": "vpc-eps-db"}, nil)

	created, err := c.createOrUpdateVpcEndpointDeployment("ns-a", desired)
	require.NoError(t, err)
	require.Equal(t, appsv1.RecreateDeploymentStrategyType, created.Spec.Strategy.Type)

	// Simulate prior RollingUpdate deploy and ensure Recreate clears rollingUpdate.
	existing, err := client.AppsV1().Deployments("ns-a").Get(context.Background(), "vpc-eps-db", metav1.GetOptions{})
	require.NoError(t, err)
	existing.Spec.Strategy = appsv1.DeploymentStrategy{
		Type:          appsv1.RollingUpdateDeploymentStrategyType,
		RollingUpdate: &appsv1.RollingUpdateDeployment{},
	}
	_, err = client.AppsV1().Deployments("ns-a").Update(context.Background(), existing, metav1.UpdateOptions{})
	require.NoError(t, err)

	desired.Spec.Template.Annotations = map[string]string{"k": "v"}
	updated, err := c.createOrUpdateVpcEndpointDeployment("ns-a", desired)
	require.NoError(t, err)
	require.Equal(t, appsv1.RecreateDeploymentStrategyType, updated.Spec.Strategy.Type)
	require.Nil(t, updated.Spec.Strategy.RollingUpdate)
	require.Equal(t, "v", updated.Spec.Template.Annotations["k"])
}

func TestWaitVpcEndpointStitcherPod(t *testing.T) {
	deploy := &appsv1.Deployment{
		Name:      "vpc-eps-db",
		Namespace: "ns-a",
	}
	readyPod := &corev1.Pod{
		Name:      "vpc-eps-db-abc",
		Namespace: "ns-a",
		Labels:    map[string]string{"app": "vpc-eps-db"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
	terminating := readyPod.DeepCopy()
	terminating.Name = "vpc-eps-db-old"
	now := metav1.Now()
	terminating.DeletionTimestamp = &now

	factory := informers.NewSharedInformerFactory(fake.NewSimpleClientset(), 0)
	podInformer := factory.Core().V1().Pods()
	require.NoError(t, podInformer.Informer().GetStore().Add(readyPod))
	require.NoError(t, podInformer.Informer().GetStore().Add(terminating))

	c := &Controller{podsLister: podInformer.Lister()}
	pod, err := c.waitVpcEndpointStitcherPod(deploy)
	require.NoError(t, err)
	require.Equal(t, "vpc-eps-db-abc", pod.Name)

	_, err = c.waitVpcEndpointStitcherPod(&appsv1.Deployment{
		Name:      "missing",
		Namespace: "ns-a",
	})
	require.ErrorContains(t, err, "waiting for stitcher pod")
}

func TestEnqueueVpcEndpointsForService(t *testing.T) {
	labeled := &kubeovnv1.VpcEndpoint{
		Name:   "labeled",
		Labels: map[string]string{util.VpcEndpointServiceLabel: "eps-a"},
		Spec:   kubeovnv1.VpcEndpointSpec{EndpointService: "eps-a"},
	}
	unlabeled := &kubeovnv1.VpcEndpoint{
		Name: "unlabeled",
		Spec: kubeovnv1.VpcEndpointSpec{EndpointService: "eps-a"},
	}
	other := &kubeovnv1.VpcEndpoint{
		Name: "other",
		Spec: kubeovnv1.VpcEndpointSpec{EndpointService: "eps-b"},
	}

	factory := kubeovninformers.NewSharedInformerFactory(kubeovnfake.NewSimpleClientset(), 0)
	vepInformer := factory.Kubeovn().V1().VpcEndpoints()
	require.NoError(t, vepInformer.Informer().GetStore().Add(labeled))
	require.NoError(t, vepInformer.Informer().GetStore().Add(unlabeled))
	require.NoError(t, vepInformer.Informer().GetStore().Add(other))

	c := &Controller{
		vpcEndpointLister:           vepInformer.Lister(),
		addOrUpdateVpcEndpointQueue: newTypedRateLimitingQueue[string]("AddOrUpdateVpcEndpoint", nil),
	}
	t.Cleanup(c.addOrUpdateVpcEndpointQueue.ShutDown)

	c.enqueueVpcEndpointsForService("eps-a")
	require.Equal(t, 2, c.addOrUpdateVpcEndpointQueue.Len())
	seen := map[string]struct{}{}
	for c.addOrUpdateVpcEndpointQueue.Len() > 0 {
		key, _ := c.addOrUpdateVpcEndpointQueue.Get()
		seen[key] = struct{}{}
		c.addOrUpdateVpcEndpointQueue.Done(key)
	}
	require.Contains(t, seen, "labeled")
	require.Contains(t, seen, "unlabeled")
	require.NotContains(t, seen, "other")
}

func TestEnqueueVpcEndpointDeleteTombstone(t *testing.T) {
	c := &Controller{
		addOrUpdateVpcEndpointQueue:        newTypedRateLimitingQueue[string]("AddOrUpdateVpcEndpoint", nil),
		addOrUpdateVpcEndpointServiceQueue: newTypedRateLimitingQueue[string]("AddOrUpdateVpcEndpointService", nil),
	}
	t.Cleanup(func() {
		c.addOrUpdateVpcEndpointQueue.ShutDown()
		c.addOrUpdateVpcEndpointServiceQueue.ShutDown()
	})

	c.enqueueDeleteVpcEndpoint(cache.DeletedFinalStateUnknown{
		Key: "client",
		Obj: &kubeovnv1.VpcEndpoint{Name: "client"},
	})
	require.Equal(t, 1, c.addOrUpdateVpcEndpointQueue.Len())

	c.enqueueDeleteVpcEndpointService(cache.DeletedFinalStateUnknown{
		Key: "db",
		Obj: &kubeovnv1.VpcEndpointService{Name: "db"},
	})
	require.Equal(t, 1, c.addOrUpdateVpcEndpointServiceQueue.Len())
}

func TestEnqueueVpcEndpointServiceFromServiceKey(t *testing.T) {
	eps := &kubeovnv1.VpcEndpointService{
		Name: "db",
		Labels: map[string]string{
			util.VpcEndpointSvcNsLabel:   "ns-a",
			util.VpcEndpointSvcNameLabel: "svc-a",
		},
		Spec: kubeovnv1.VpcEndpointServiceSpec{Namespace: "ns-a", Service: "svc-a"},
	}
	factory := kubeovninformers.NewSharedInformerFactory(kubeovnfake.NewSimpleClientset(), 0)
	vesInformer := factory.Kubeovn().V1().VpcEndpointServices()
	require.NoError(t, vesInformer.Informer().GetStore().Add(eps))

	c := &Controller{
		vpcEndpointServiceLister:           vesInformer.Lister(),
		addOrUpdateVpcEndpointServiceQueue: newTypedRateLimitingQueue[string]("AddOrUpdateVpcEndpointService", nil),
	}
	t.Cleanup(c.addOrUpdateVpcEndpointServiceQueue.ShutDown)

	c.enqueueVpcEndpointServiceFromServiceKey("bad-key")
	require.Zero(t, c.addOrUpdateVpcEndpointServiceQueue.Len())

	c.enqueueVpcEndpointServiceFromServiceKey("ns-a/svc-a")
	require.Equal(t, 1, c.addOrUpdateVpcEndpointServiceQueue.Len())
	key, _ := c.addOrUpdateVpcEndpointServiceQueue.Get()
	c.addOrUpdateVpcEndpointServiceQueue.Done(key)
	require.Equal(t, "db", key)
}

func TestVpcEndpointConsumerNamespace(t *testing.T) {
	vpc := &kubeovnv1.Vpc{
		Name: "consumer",
		Spec: kubeovnv1.VpcSpec{Namespaces: []string{"ep-consumer"}},
	}
	empty := &kubeovnv1.Vpc{Name: "empty"}
	factory := kubeovninformers.NewSharedInformerFactory(kubeovnfake.NewSimpleClientset(), 0)
	vpcInformer := factory.Kubeovn().V1().Vpcs()
	require.NoError(t, vpcInformer.Informer().GetStore().Add(vpc))
	require.NoError(t, vpcInformer.Informer().GetStore().Add(empty))

	c := &Controller{vpcsLister: vpcInformer.Lister()}
	ns, err := c.vpcEndpointConsumerNamespace("consumer")
	require.NoError(t, err)
	require.Equal(t, "ep-consumer", ns)

	_, err = c.vpcEndpointConsumerNamespace("empty")
	require.ErrorContains(t, err, "has no namespaces")
}
