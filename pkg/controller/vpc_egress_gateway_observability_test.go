package controller

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	appslisters "k8s.io/client-go/listers/apps/v1"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"

	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestAddVpcEgressGatewayObserverUsesRestartableNonRootSidecar(t *testing.T) {
	podSpec := &corev1.PodSpec{}
	config := &kubeovnv1.VpcEgressGatewayObservability{InterfaceMetrics: kubeovnv1.VpcEgressGatewayObservabilityFeature{Enabled: true}}
	addVpcEgressGatewayObserver(podSpec, "kubeovn/kube-ovn:test", config, vpcEgressObserverState{enabled: true, configName: "gateway-observability"})
	require.Len(t, podSpec.InitContainers, 1)
	container := podSpec.InitContainers[0]
	require.Equal(t, vpcEgressObserverContainerName, container.Name)
	require.Equal(t, ptr.To(corev1.ContainerRestartPolicyAlways), container.RestartPolicy)
	require.Contains(t, container.Command[2], "exec sleep infinity")
	require.Equal(t, "20m", container.Resources.Requests.Cpu().String())
	require.Equal(t, "64Mi", container.Resources.Requests.Memory().String())
	require.Equal(t, "200m", container.Resources.Limits.Cpu().String())
	require.Equal(t, "256Mi", container.Resources.Limits.Memory().String())
	require.Equal(t, ptr.To[int64](65534), container.SecurityContext.RunAsUser)
	require.Equal(t, ptr.To[int64](65534), container.SecurityContext.RunAsGroup)
	require.Equal(t, new(true), container.SecurityContext.RunAsNonRoot)
	require.Equal(t, new(true), container.SecurityContext.AllowPrivilegeEscalation)
	require.Equal(t, new(true), container.SecurityContext.ReadOnlyRootFilesystem)
	require.Equal(t, []corev1.Capability{"ALL"}, container.SecurityContext.Capabilities.Drop)
	require.Equal(t, []corev1.Capability{"NET_ADMIN"}, container.SecurityContext.Capabilities.Add)
	require.Nil(t, container.LivenessProbe.HTTPGet)
	require.Equal(t, []string{
		"/bin/sh", "-ec",
		"if [ ! -x /kube-ovn/vpc-egress-gateway-observer ]; then exit 0; fi; exec /kube-ovn/vpc-egress-gateway-observer --health-check",
	}, container.LivenessProbe.Exec.Command)
	require.Nil(t, container.ReadinessProbe)
	require.Nil(t, container.StartupProbe)
}

func TestAddVpcEgressGatewayObserverKeepsFallbackHealthyForDefaultVpc(t *testing.T) {
	podSpec := &corev1.PodSpec{}
	config := &kubeovnv1.VpcEgressGatewayObservability{InterfaceMetrics: kubeovnv1.VpcEgressGatewayObservabilityFeature{Enabled: true}}
	addVpcEgressGatewayObserver(podSpec, "kubeovn/kube-ovn:test", config, vpcEgressObserverState{enabled: true, configName: "gateway-observability"})
	require.Len(t, podSpec.InitContainers, 1)
	probe := podSpec.InitContainers[0].LivenessProbe
	require.Nil(t, probe.HTTPGet)
	require.Equal(t, []string{
		"/bin/sh", "-ec",
		"if [ ! -x /kube-ovn/vpc-egress-gateway-observer ]; then exit 0; fi; exec /kube-ovn/vpc-egress-gateway-observer --health-check",
	}, probe.Exec.Command)
}

func TestCollectorSwitchesDoNotChangeObserverPodSpec(t *testing.T) {
	interfaceMetrics := &kubeovnv1.VpcEgressGatewayObservability{InterfaceMetrics: kubeovnv1.VpcEgressGatewayObservabilityFeature{Enabled: true}}
	conntrackLog := &kubeovnv1.VpcEgressGatewayObservability{Conntrack: kubeovnv1.VpcEgressGatewayConntrackObservability{Log: kubeovnv1.VpcEgressGatewayConntrackLog{Enabled: true}}}
	first, second := &corev1.PodSpec{}, &corev1.PodSpec{}
	state := vpcEgressObserverState{enabled: true, configName: "gateway-observability"}
	addVpcEgressGatewayObserver(first, "kubeovn/kube-ovn:test", interfaceMetrics, state)
	addVpcEgressGatewayObserver(second, "kubeovn/kube-ovn:test", conntrackLog, state)
	require.Equal(t, first, second)
}

func TestReconcileVpcEgressGatewayObservabilityCreatesPerGatewayResources(t *testing.T) {
	kubeClient := k8sfake.NewSimpleClientset()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	controller := &Controller{
		config:                           &Configuration{KubeClient: kubeClient, DynamicClient: dynamicClient},
		addOrUpdateVpcEgressGatewayQueue: workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]()),
	}
	controller.restartableInitContainerSupport = restartableInitContainerSupport{capability: restartableInitContainerCapabilitySupported}
	gw := &kubeovnv1.VpcEgressGateway{
		TypeMeta:   metav1.TypeMeta{APIVersion: kubeovnv1.SchemeGroupVersion.String(), Kind: "VpcEgressGateway"},
		ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "ns", UID: types.UID("gateway-uid"), Generation: 1},
		Spec: kubeovnv1.VpcEgressGatewaySpec{Observability: &kubeovnv1.VpcEgressGatewayObservability{
			InterfaceMetrics: kubeovnv1.VpcEgressGatewayObservabilityFeature{Enabled: true},
			ServiceMonitor:   kubeovnv1.VpcEgressGatewayServiceMonitor{Labels: map[string]string{"team": "network", "app": "must-not-override"}},
		}},
	}
	labels := vegWorkloadLabels(gw.Name)
	state := controller.reconcileVpcEgressGatewayObservability(gw, "ns/external", labels)
	require.True(t, state.enabled)
	configMap, err := kubeClient.CoreV1().ConfigMaps("ns").Get(context.Background(), state.configName, metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, metav1.IsControlledBy(configMap, gw))
	require.Nil(t, metav1.GetControllerOf(configMap).BlockOwnerDeletion)
	require.Contains(t, configMap.Data["config.json"], `"externalNetwork":"ns/external"`)
	service, err := kubeClient.CoreV1().Services("ns").Get(context.Background(), state.configName, metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, metav1.IsControlledBy(service, gw))
	require.Nil(t, metav1.GetControllerOf(service).BlockOwnerDeletion)
	require.Equal(t, corev1.ClusterIPNone, service.Spec.ClusterIP)
	require.True(t, service.Spec.PublishNotReadyAddresses)
	require.Equal(t, int32(10666), service.Spec.Ports[0].TargetPort.IntVal)
	serviceMonitor, err := dynamicClient.Resource(serviceMonitorGVR).Namespace("ns").Get(context.Background(), state.configName, metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, metav1.IsControlledBy(serviceMonitor, gw))
	require.Nil(t, metav1.GetControllerOf(serviceMonitor).BlockOwnerDeletion)
	require.Equal(t, "network", serviceMonitor.GetLabels()["team"])
	require.Equal(t, labels["app"], serviceMonitor.GetLabels()["app"])
	require.Equal(t, corev1.ConditionTrue, gw.Status.Conditions.GetCondition(kubeovnv1.ObservabilityConfigured).Status)
	require.Equal(t, corev1.ConditionTrue, gw.Status.Conditions.GetCondition(kubeovnv1.ServiceMonitorReady).Status)
	state = controller.reconcileVpcEgressGatewayObservability(gw, "ns/external", labels)
	require.True(t, state.enabled)
}

func TestReconcileVpcEgressGatewayObservabilityDoesNotInjectWhenSidecarsUnsupported(t *testing.T) {
	controller := &Controller{config: &Configuration{KubeClient: k8sfake.NewSimpleClientset()}}
	controller.restartableInitContainerSupport = restartableInitContainerSupport{
		capability: restartableInitContainerCapabilityUnsupported,
		reason:     "UnsupportedKubernetesVersion",
		message:    "restartable init containers require Kubernetes 1.29 or later",
	}
	gw := &kubeovnv1.VpcEgressGateway{ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "ns", Generation: 1}, Spec: kubeovnv1.VpcEgressGatewaySpec{
		Observability: &kubeovnv1.VpcEgressGatewayObservability{Conntrack: kubeovnv1.VpcEgressGatewayConntrackObservability{Log: kubeovnv1.VpcEgressGatewayConntrackLog{Enabled: true}}},
	}}
	state := controller.reconcileVpcEgressGatewayObservability(gw, "ns/external", vegWorkloadLabels(gw.Name))
	require.False(t, state.enabled)
	condition := gw.Status.Conditions.GetCondition(kubeovnv1.ObservabilityConfigured)
	require.Equal(t, corev1.ConditionFalse, condition.Status)
	require.Equal(t, "UnsupportedKubernetesVersion", condition.Reason)
}

func TestReconcileVpcEgressGatewayObservabilitySoftFailsWithoutServiceMonitorCRD(t *testing.T) {
	kubeClient := k8sfake.NewSimpleClientset()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	dynamicClient.PrependReactor("create", "servicemonitors", func(action k8stesting.Action) (bool, runtime.Object, error) {
		name := action.(k8stesting.CreateAction).GetObject().(metav1.Object).GetName()
		return true, nil, k8serrors.NewNotFound(schema.GroupResource{Group: "monitoring.coreos.com", Resource: "servicemonitors"}, name)
	})
	controller := &Controller{
		config:                           &Configuration{KubeClient: kubeClient, DynamicClient: dynamicClient},
		addOrUpdateVpcEgressGatewayQueue: workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]()),
	}
	controller.restartableInitContainerSupport = restartableInitContainerSupport{capability: restartableInitContainerCapabilitySupported}
	gw := &kubeovnv1.VpcEgressGateway{
		TypeMeta:   metav1.TypeMeta{APIVersion: kubeovnv1.SchemeGroupVersion.String(), Kind: "VpcEgressGateway"},
		ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "ns", UID: types.UID("gateway-uid"), Generation: 1},
		Spec: kubeovnv1.VpcEgressGatewaySpec{Observability: &kubeovnv1.VpcEgressGatewayObservability{
			InterfaceMetrics: kubeovnv1.VpcEgressGatewayObservabilityFeature{Enabled: true},
		}},
	}
	state := controller.reconcileVpcEgressGatewayObservability(gw, "ns/external", vegWorkloadLabels(gw.Name))
	require.True(t, state.enabled)
	require.Equal(t, corev1.ConditionTrue, gw.Status.Conditions.GetCondition(kubeovnv1.ObservabilityConfigured).Status)
	condition := gw.Status.Conditions.GetCondition(kubeovnv1.ServiceMonitorReady)
	require.Equal(t, corev1.ConditionFalse, condition.Status)
	require.Equal(t, "ServiceMonitorCRDNotInstalled", condition.Reason)
}

func TestReconcileVpcEgressGatewayObservabilityPreservesExistingObserverOnConfigMapFailure(t *testing.T) {
	gw := observabilityTestGateway()
	name := vpcEgressObserverResourceName(gw)
	configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: gw.Namespace}}
	require.NoError(t, setVpcEgressGatewayControllerReference(gw, configMap))
	kubeClient := k8sfake.NewSimpleClientset(configMap)
	kubeClient.PrependReactor("update", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("transient config map update failure")
	})
	controller := observabilityTestController(t, kubeClient, gw)

	state := controller.reconcileVpcEgressGatewayObservability(gw, "ns/external", vegWorkloadLabels(gw.Name))

	requirePreservedObserverState(t, state)
	require.Equal(t, corev1.ConditionFalse, gw.Status.Conditions.GetCondition(kubeovnv1.ObservabilityConfigured).Status)
}

func TestReconcileVpcEgressGatewayObservabilityPreservesExistingObserverOnCapabilityProbeFailure(t *testing.T) {
	gw := observabilityTestGateway()
	kubeClient := k8sfake.NewSimpleClientset()
	kubeClient.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{GitVersion: "v1.29.0"}
	kubeClient.PrependReactor("create", "deployments", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("transient capability probe failure")
	})
	controller := observabilityTestController(t, kubeClient, gw)
	controller.restartableInitContainerSupport = restartableInitContainerSupport{}

	state := controller.reconcileVpcEgressGatewayObservability(gw, "ns/external", vegWorkloadLabels(gw.Name))

	requirePreservedObserverState(t, state)
	condition := gw.Status.Conditions.GetCondition(kubeovnv1.ObservabilityConfigured)
	require.Equal(t, corev1.ConditionFalse, condition.Status)
	require.Equal(t, "SidecarContainersCapabilityUnknown", condition.Reason)
}

func TestReconcileVpcEgressGatewayObservabilityPreservesResourcesOnServiceFailure(t *testing.T) {
	gw := observabilityTestGateway()
	name := vpcEgressObserverResourceName(gw)
	configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: gw.Namespace}}
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: gw.Namespace}, Spec: corev1.ServiceSpec{ClusterIP: corev1.ClusterIPNone}}
	require.NoError(t, setVpcEgressGatewayControllerReference(gw, configMap))
	require.NoError(t, setVpcEgressGatewayControllerReference(gw, service))
	kubeClient := k8sfake.NewSimpleClientset(configMap, service)
	kubeClient.PrependReactor("update", "services", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("transient service update failure")
	})
	controller := observabilityTestController(t, kubeClient, gw)

	state := controller.reconcileVpcEgressGatewayObservability(gw, "ns/external", vegWorkloadLabels(gw.Name))

	requirePreservedObserverState(t, state)
	_, err := kubeClient.CoreV1().ConfigMaps(gw.Namespace).Get(context.Background(), name, metav1.GetOptions{})
	require.NoError(t, err, "the last-known-good observer ConfigMap must be retained")
}

func requirePreservedObserverState(t *testing.T, state vpcEgressObserverState) {
	t.Helper()
	require.True(t, state.enabled, "a transient observability error must not remove an existing sidecar")
	require.NotNil(t, state.preservedContainer)
	require.Equal(t, "kubeovn/kube-ovn:existing", state.preservedContainer.Image)
	require.Len(t, state.preservedVolumes, 2)
	require.ElementsMatch(t, []string{vpcEgressObserverConfigVolume, vpcEgressObserverPodInfoVolume}, []string{
		state.preservedVolumes[0].Name,
		state.preservedVolumes[1].Name,
	})

	podSpec := &corev1.PodSpec{}
	changedConfig := &kubeovnv1.VpcEgressGatewayObservability{
		InterfaceMetrics: kubeovnv1.VpcEgressGatewayObservabilityFeature{Enabled: true},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")},
		},
	}
	addVpcEgressGatewayObserver(podSpec, "kubeovn/kube-ovn:new", changedConfig, state)
	require.Equal(t, "kubeovn/kube-ovn:existing", podSpec.InitContainers[0].Image)
	require.Equal(t, state.preservedContainer.Resources, podSpec.InitContainers[0].Resources)
	require.Equal(t, state.preservedVolumes, podSpec.Volumes)
}

func TestVpcEgressGatewayBFDAndObservabilityPortNamesAreUnique(t *testing.T) {
	podSpec := corev1.PodSpec{Containers: []corev1.Container{
		genVpcEgressGatewayBFDDContainer("kubeovn/kube-ovn:test", "10.255.255.255", 100, 100, 3, true),
	}}
	config := &kubeovnv1.VpcEgressGatewayObservability{
		InterfaceMetrics: kubeovnv1.VpcEgressGatewayObservabilityFeature{Enabled: true},
	}
	addVpcEgressGatewayObserver(&podSpec, "kubeovn/kube-ovn:test", config, vpcEgressObserverState{
		enabled: true, configName: "gateway-observability",
	})

	portNames := map[string]string{}
	for _, container := range append(podSpec.Containers, podSpec.InitContainers...) {
		for _, port := range container.Ports {
			require.NotContains(t, portNames, port.Name, "port name %q is reused by %s and %s", port.Name, portNames[port.Name], container.Name)
			portNames[port.Name] = container.Name
		}
	}
}

func observabilityTestGateway() *kubeovnv1.VpcEgressGateway {
	return &kubeovnv1.VpcEgressGateway{
		TypeMeta:   metav1.TypeMeta{APIVersion: kubeovnv1.SchemeGroupVersion.String(), Kind: "VpcEgressGateway"},
		ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "ns", UID: types.UID("gateway-uid"), Generation: 1},
		Spec: kubeovnv1.VpcEgressGatewaySpec{Observability: &kubeovnv1.VpcEgressGatewayObservability{
			InterfaceMetrics: kubeovnv1.VpcEgressGatewayObservabilityFeature{Enabled: true},
		}},
	}
}

func observabilityTestController(t *testing.T, kubeClient *k8sfake.Clientset, gw *kubeovnv1.VpcEgressGateway) *Controller {
	t.Helper()
	podSpec := corev1.PodSpec{}
	addVpcEgressGatewayObserver(&podSpec, "kubeovn/kube-ovn:existing", gw.Spec.Observability, vpcEgressObserverState{enabled: true, configName: vpcEgressObserverResourceName(gw)})
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: gw.Spec.Prefix + gw.Name, Namespace: gw.Namespace},
		Spec:       appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: podSpec}},
	}
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, indexer.Add(deployment))
	controller := &Controller{
		config:                           &Configuration{KubeClient: kubeClient},
		deploymentsLister:                appslisters.NewDeploymentLister(indexer),
		addOrUpdateVpcEgressGatewayQueue: workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]()),
	}
	controller.restartableInitContainerSupport = restartableInitContainerSupport{capability: restartableInitContainerCapabilitySupported}
	return controller
}

func TestSupportsRestartableInitContainersProbesFeatureGate(t *testing.T) {
	kubeClient := k8sfake.NewSimpleClientset()
	kubeClient.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{GitVersion: "v1.29.0"}
	controller := &Controller{config: &Configuration{KubeClient: kubeClient, PodNamespace: "kube-system"}}

	support := controller.supportsRestartableInitContainers()
	require.True(t, support.supported())
	require.True(t, support.definitive())
	require.Empty(t, support.reason)
	require.Empty(t, support.message)
	require.Equal(t, restartableInitContainerCapabilitySupported, controller.restartableInitContainerSupport.capability)
	require.Len(t, kubeClient.Actions(), 2)
}

func TestSupportsRestartableInitContainersDetectsDroppedPolicy(t *testing.T) {
	kubeClient := k8sfake.NewSimpleClientset()
	kubeClient.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{GitVersion: "v1.29.0"}
	kubeClient.PrependReactor("create", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		deployment := action.(k8stesting.CreateAction).GetObject().(*appsv1.Deployment).DeepCopy()
		deployment.Spec.Template.Spec.InitContainers[0].RestartPolicy = nil
		return true, deployment, nil
	})
	controller := &Controller{config: &Configuration{KubeClient: kubeClient, PodNamespace: "kube-system"}}

	support := controller.supportsRestartableInitContainers()
	require.False(t, support.supported())
	require.True(t, support.definitive())
	require.Equal(t, "SidecarContainersDisabled", support.reason)
	require.Contains(t, support.message, "SidecarContainers")
}

func TestSupportsRestartableInitContainersRetriesUnknownCapability(t *testing.T) {
	kubeClient := k8sfake.NewSimpleClientset()
	kubeClient.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{GitVersion: "v1.29.0"}
	createAttempts := 0
	kubeClient.PrependReactor("create", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		createAttempts++
		if createAttempts == 1 {
			return true, nil, errors.New("transient admission failure")
		}
		return true, action.(k8stesting.CreateAction).GetObject(), nil
	})
	controller := &Controller{config: &Configuration{KubeClient: kubeClient, PodNamespace: "kube-system"}}

	support := controller.supportsRestartableInitContainers()
	require.False(t, support.supported())
	require.False(t, support.definitive())
	require.Equal(t, "SidecarContainersCapabilityUnknown", support.reason)
	require.Contains(t, support.message, "transient admission failure")
	require.Equal(t, restartableInitContainerCapabilityUnknown, controller.restartableInitContainerSupport.capability)

	support = controller.supportsRestartableInitContainers()
	require.True(t, support.supported())
	require.True(t, support.definitive())
	require.Empty(t, support.reason)
	require.Empty(t, support.message)
	require.Equal(t, 2, createAttempts)
}
