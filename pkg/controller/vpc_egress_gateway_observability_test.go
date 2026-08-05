package controller

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
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
	require.Equal(t, new(true), container.SecurityContext.RunAsNonRoot)
	require.Equal(t, new(true), container.SecurityContext.AllowPrivilegeEscalation)
	require.Equal(t, []corev1.Capability{"ALL"}, container.SecurityContext.Capabilities.Drop)
	require.Equal(t, []corev1.Capability{"NET_ADMIN"}, container.SecurityContext.Capabilities.Add)
	require.NotNil(t, container.LivenessProbe.HTTPGet)
	require.Nil(t, container.ReadinessProbe)
	require.Nil(t, container.StartupProbe)
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
	controller.restartableInitContainersChecked = true
	controller.restartableInitContainers = true
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
	controller.restartableInitContainersChecked = true
	controller.restartableInitContainersReason = "UnsupportedKubernetesVersion"
	controller.restartableInitContainersMessage = "restartable init containers require Kubernetes 1.29 or later"
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
	controller.restartableInitContainersChecked = true
	controller.restartableInitContainers = true
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

func TestSupportsRestartableInitContainersProbesFeatureGate(t *testing.T) {
	kubeClient := k8sfake.NewSimpleClientset()
	kubeClient.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{GitVersion: "v1.29.0"}
	controller := &Controller{config: &Configuration{KubeClient: kubeClient, PodNamespace: "kube-system"}}

	supported, definitive, reason, message := controller.supportsRestartableInitContainers()
	require.True(t, supported)
	require.True(t, definitive)
	require.Empty(t, reason)
	require.Empty(t, message)
	require.True(t, controller.restartableInitContainersChecked)
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

	supported, definitive, reason, message := controller.supportsRestartableInitContainers()
	require.False(t, supported)
	require.True(t, definitive)
	require.Equal(t, "SidecarContainersDisabled", reason)
	require.Contains(t, message, "SidecarContainers")
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

	supported, definitive, reason, message := controller.supportsRestartableInitContainers()
	require.False(t, supported)
	require.False(t, definitive)
	require.Equal(t, "SidecarContainersCapabilityUnknown", reason)
	require.Contains(t, message, "transient admission failure")
	require.False(t, controller.restartableInitContainersChecked)

	supported, definitive, reason, message = controller.supportsRestartableInitContainers()
	require.True(t, supported)
	require.True(t, definitive)
	require.Empty(t, reason)
	require.Empty(t, message)
	require.Equal(t, 2, createAttempts)
}
