package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnfake "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func controllerOwnerReference(apiVersion, kind, name string, uid types.UID) metav1.OwnerReference {
	controller := true
	return metav1.OwnerReference{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		UID:        uid,
		Controller: &controller,
	}
}

func TestNatGatewayWorkloadListOptions(t *testing.T) {
	options := &metav1.ListOptions{}
	natGatewayWorkloadListOptions(options)
	require.True(t, options.AllowWatchBookmarks)
	selector, err := labels.Parse(options.LabelSelector)
	require.NoError(t, err)
	require.True(t, selector.Matches(labels.Set{util.VpcNatGatewayLabel: "true"}))
	require.False(t, selector.Matches(labels.Set{}))
}

func TestVpcNatGatewayOwnerFromStatefulSetPod(t *testing.T) {
	const (
		gwName    = "dynamic-gw"
		namespace = "default"
	)
	gwUID := types.UID("gw-uid")
	stsUID := types.UID("sts-uid")
	stsName := util.GenNatGwName(gwName)
	gw := &kubeovnv1.VpcNatGateway{Name: gwName, UID: gwUID}
	sts := &appsv1.StatefulSet{
		Name:            stsName,
		Namespace:       namespace,
		UID:             stsUID,
		Labels:          util.GenNatGwLabels(gwName),
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(kubeovnv1.SchemeGroupVersion.String(), util.KindVpcNatGateway, gwName, gwUID)},
	}
	pod := &corev1.Pod{
		Name:            stsName + "-0",
		Namespace:       namespace,
		Labels:          util.GenNatGwLabels(gwName),
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(appsv1.SchemeGroupVersion.String(), util.KindStatefulSet, stsName, stsUID)},
	}

	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{gw},
		StatefulSets:   []*appsv1.StatefulSet{sts},
		Pods:           []*corev1.Pod{pod},
	})
	require.NoError(t, err)

	owner, err := fakeController.fakeController.vpcNatGatewayStatefulSetOwner(pod)
	require.NoError(t, err)
	require.Equal(t, gwName, owner.Name)
	require.Equal(t, gwUID, owner.UID)
	staleOwner := owner.DeepCopy()
	staleOwner.UID = "stale-gw-uid"
	fakeController.fakeController.enqueueVpcNatGatewayOwner(staleOwner, "test")
	require.Zero(t, fakeController.fakeController.addOrUpdateVpcNatGatewayQueue.Len())
	fakeController.fakeController.enqueueVpcNatGatewayForWorkload(sts)
	require.Equal(t, 1, fakeController.fakeController.addOrUpdateVpcNatGatewayQueue.Len())
	// Delete tombstones resolve to the same gateway, which is already queued.
	fakeController.fakeController.enqueueVpcNatGatewayForWorkload(cache.DeletedFinalStateUnknown{Obj: sts})
	require.Equal(t, 1, fakeController.fakeController.addOrUpdateVpcNatGatewayQueue.Len())

	oldRegularPod := &corev1.Pod{Name: "regular", Namespace: namespace}
	newRegularPod := oldRegularPod.DeepCopy()
	newRegularPod.Annotations = map[string]string{"changed": "true"}
	fakeController.fakeController.enqueueUpdateVpcNatGatewayForWorkload(oldRegularPod, newRegularPod)
	require.Equal(t, 1, fakeController.fakeController.addOrUpdateVpcNatGatewayQueue.Len())

	pod.OwnerReferences[0].UID = "stale-sts-uid"
	_, err = fakeController.fakeController.vpcNatGatewayStatefulSetOwner(pod)
	require.ErrorContains(t, err, "does not match")
}

func TestGetNatGwObservedLanIPsForHAPods(t *testing.T) {
	const (
		gwName    = "ha-gw"
		namespace = "default"
		subnet    = "ha-subnet"
	)
	deployName := util.GenNatGwName(gwName)
	gw := &kubeovnv1.VpcNatGateway{
		Name: gwName, UID: "gw-uid",
		Spec: kubeovnv1.VpcNatGatewaySpec{
			Namespace: namespace,
			Subnet:    subnet,
			Replicas:  2,
		},
	}
	// HA replicas are owned by a ReplicaSet, which this controller does not watch: they are
	// matched by label only, so no owner chain has to be resolvable.
	pod := &corev1.Pod{
		Name:      deployName + "-abc-xyz",
		Namespace: namespace,
		Labels:    util.GenNatGwLabels(gwName),
		Annotations: map[string]string{
			util.IPAddressAnnotation:         "10.20.0.10,fd00::10",
			util.VpcNatGatewayInitAnnotation: "true",
		},
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(appsv1.SchemeGroupVersion.String(), "ReplicaSet", deployName+"-abc", "rs-uid")},
		Status:          corev1.PodStatus{Phase: corev1.PodRunning},
	}

	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{gw},
		Subnets: []*kubeovnv1.Subnet{{
			Name: subnet,
			Spec: kubeovnv1.SubnetSpec{Provider: util.OvnProvider, Protocol: kubeovnv1.ProtocolDual},
		}},
		Pods: []*corev1.Pod{pod},
	})
	require.NoError(t, err)

	lanIPs, err := fakeController.fakeController.getNatGwObservedLanIPs(gw, []*corev1.Pod{pod})
	require.NoError(t, err)
	require.Equal(t, []string{"10.20.0.10", "fd00::10"}, lanIPs)
}

func TestGetNatGwObservedLanIPsRetriesOnOwnerCacheMiss(t *testing.T) {
	const (
		gwName    = "cache-miss-gw"
		namespace = "default"
		subnet    = "nat-subnet"
	)
	stsName := util.GenNatGwName(gwName)
	gw := &kubeovnv1.VpcNatGateway{
		Name: gwName, UID: "gw-uid",
		Spec: kubeovnv1.VpcNatGatewaySpec{Namespace: namespace, Subnet: subnet, Replicas: 1},
	}
	// The StatefulSet is deliberately absent from the informer cache: the Pod is running
	// and holds an address, but its owner cannot be resolved yet.
	pod := &corev1.Pod{
		Name: stsName + "-0", Namespace: namespace, Labels: util.GenNatGwLabels(gwName),
		Annotations:     map[string]string{util.IPAddressAnnotation: "10.20.0.10"},
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(appsv1.SchemeGroupVersion.String(), util.KindStatefulSet, stsName, "sts-uid")},
		Status:          corev1.PodStatus{Phase: corev1.PodRunning},
	}

	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{gw},
		Subnets: []*kubeovnv1.Subnet{{
			Name: subnet,
			Spec: kubeovnv1.SubnetSpec{Provider: util.OvnProvider, Protocol: kubeovnv1.ProtocolIPv4},
		}},
		Pods: []*corev1.Pod{pod},
	})
	require.NoError(t, err)
	controller := fakeController.fakeController

	_, err = controller.getNatGwObservedLanIPs(gw, []*corev1.Pod{pod})
	require.ErrorContains(t, err, "failed to resolve owner of nat gateway pod", "a cache miss must be retried, not silently skipped")
	require.ErrorContains(t, patchNatGwStatusFromAPI(t, controller, gwName), "failed to resolve owner of nat gateway pod")

	// A Pod owned by something else is not transient and must not block the gateway.
	foreignPod := pod.DeepCopy()
	foreignPod.OwnerReferences[0].Kind = "Job"
	foreignController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{gw},
		Subnets: []*kubeovnv1.Subnet{{
			Name: subnet,
			Spec: kubeovnv1.SubnetSpec{Provider: util.OvnProvider, Protocol: kubeovnv1.ProtocolIPv4},
		}},
		Pods: []*corev1.Pod{foreignPod},
	})
	require.NoError(t, err)
	lanIPs, err := foreignController.fakeController.getNatGwObservedLanIPs(gw, []*corev1.Pod{foreignPod})
	require.NoError(t, err)
	require.Empty(t, lanIPs)
}

func TestPatchNatGwStatusPersistsDynamicLanIP(t *testing.T) {
	for _, tt := range []struct {
		name          string
		protocol      string
		podAnnotation string
		wantLanIP     string
	}{
		{name: "IPv4", protocol: kubeovnv1.ProtocolIPv4, podAnnotation: "10.20.0.10", wantLanIP: "10.20.0.10"},
		{name: "IPv6", protocol: kubeovnv1.ProtocolIPv6, podAnnotation: "fd00::10", wantLanIP: "fd00::10"},
		{name: "dual stack prefers IPv4", protocol: kubeovnv1.ProtocolDual, podAnnotation: "10.20.0.10,fd00::10", wantLanIP: "10.20.0.10"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			testPatchNatGwStatusPersistsDynamicLanIP(t, tt.protocol, tt.podAnnotation, tt.wantLanIP)
		})
	}
}

func testPatchNatGwStatusPersistsDynamicLanIP(t *testing.T, protocol, podAnnotation, wantLanIP string) {
	const (
		gwName    = "dynamic-gw"
		namespace = "default"
		subnet    = "nat-subnet"
	)
	gwUID := types.UID("gw-uid")
	stsUID := types.UID("sts-uid")
	stsName := util.GenNatGwName(gwName)
	labels := util.GenNatGwLabels(gwName)
	gw := &kubeovnv1.VpcNatGateway{
		Name: gwName, UID: gwUID,
		Spec: kubeovnv1.VpcNatGatewaySpec{
			Namespace: namespace,
			Subnet:    subnet,
			Replicas:  1,
		},
	}
	sts := &appsv1.StatefulSet{
		Name:            stsName,
		Namespace:       namespace,
		UID:             stsUID,
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(kubeovnv1.SchemeGroupVersion.String(), util.KindVpcNatGateway, gwName, gwUID)},
		Spec:            appsv1.StatefulSetSpec{Selector: &metav1.LabelSelector{MatchLabels: labels}},
	}
	pod := &corev1.Pod{
		Name:            stsName + "-0",
		Namespace:       namespace,
		Labels:          labels,
		Annotations:     map[string]string{util.IPAddressAnnotation: podAnnotation},
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(appsv1.SchemeGroupVersion.String(), util.KindStatefulSet, stsName, stsUID)},
		// Still pending: the address annotation is written when the address is allocated, and
		// the gateway must not wait for the Pod to run before pinning it.
		Status: corev1.PodStatus{Phase: corev1.PodPending},
	}

	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{gw},
		Subnets: []*kubeovnv1.Subnet{{
			Name: subnet,
			Spec: kubeovnv1.SubnetSpec{
				Provider: util.OvnProvider,
				Protocol: protocol,
			},
		}},
		StatefulSets: []*appsv1.StatefulSet{sts},
		Pods:         []*corev1.Pod{pod},
	})
	require.NoError(t, err)
	controller := fakeController.fakeController

	require.NoError(t, patchNatGwStatusFromAPI(t, controller, gwName))
	var patchSubresources []string
	for _, action := range controller.config.KubeOvnClient.(*kubeovnfake.Clientset).Actions() {
		if action.GetVerb() == "patch" && action.GetResource().Resource == "vpc-nat-gateways" {
			patchSubresources = append(patchSubresources, action.GetSubresource())
		}
	}
	require.Equal(t, []string{"status", ""}, patchSubresources, "status must be published before spec.lanIp")

	updated, err := controller.config.KubeOvnClient.KubeovnV1().VpcNatGateways().Get(context.Background(), gwName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, wantLanIP, updated.Status.LanIP)
	require.Equal(t, wantLanIP, updated.Spec.LanIP)
}

func TestPatchNatGwStatusKeepsStaticLanIPWithoutPod(t *testing.T) {
	// Compatibility: a static gateway has always reported spec.lanIp in its status, including
	// while its Pod is gone, e.g. during a recreation.
	const (
		gwName    = "static-gw"
		namespace = "default"
		subnet    = "nat-subnet"
	)
	gw := &kubeovnv1.VpcNatGateway{
		Name: gwName, UID: "gw-uid",
		Spec: kubeovnv1.VpcNatGatewaySpec{
			Namespace: namespace,
			Subnet:    subnet,
			Replicas:  1,
			LanIP:     "10.20.0.10",
		},
	}
	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{gw},
		Subnets: []*kubeovnv1.Subnet{{
			Name: subnet,
			Spec: kubeovnv1.SubnetSpec{Provider: util.OvnProvider, Protocol: kubeovnv1.ProtocolIPv4},
		}},
	})
	require.NoError(t, err)
	controller := fakeController.fakeController

	require.NoError(t, patchNatGwStatusFromAPI(t, controller, gwName))
	updated, err := controller.config.KubeOvnClient.KubeovnV1().VpcNatGateways().Get(context.Background(), gwName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, gw.Spec.LanIP, updated.Status.LanIP)
}

func TestSelectNatGwLanIP(t *testing.T) {
	for _, tt := range []struct {
		name, ip, protocol, want string
	}{
		{name: "IPv4", ip: "10.0.0.2,fd00::2", protocol: kubeovnv1.ProtocolIPv4, want: "10.0.0.2"},
		{name: "IPv6", ip: "10.0.0.2,fd00::2", protocol: kubeovnv1.ProtocolIPv6, want: "fd00::2"},
		{name: "dual stack prefers IPv4", ip: "10.0.0.2,fd00::2", protocol: kubeovnv1.ProtocolDual, want: "10.0.0.2"},
		{name: "dual stack falls back to IPv6", ip: "fd00::2", protocol: kubeovnv1.ProtocolDual, want: "fd00::2"},
		{name: "invalid", ip: "invalid", protocol: kubeovnv1.ProtocolIPv4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, selectNatGwLanIP(tt.ip, tt.protocol))
		})
	}
}

func TestVpcNatGatewayControllerReferencePatch(t *testing.T) {
	gw := &kubeovnv1.VpcNatGateway{Name: "gw", UID: "gw-uid"}
	legacy := &appsv1.StatefulSet{
		Name:      "gw-sts",
		Namespace: "default",
		OwnerReferences: []metav1.OwnerReference{
			{APIVersion: kubeovnv1.SchemeGroupVersion.String(), Kind: util.KindVpcNatGateway, Name: gw.Name, UID: gw.UID},
			{APIVersion: appsv1.SchemeGroupVersion.String(), Kind: util.KindDeployment, Name: "other", UID: "other-uid"},
		},
	}
	desired := legacy.DeepCopy()
	desired.OwnerReferences = []metav1.OwnerReference{
		controllerOwnerReference(kubeovnv1.SchemeGroupVersion.String(), util.KindVpcNatGateway, gw.Name, gw.UID),
	}

	patch, needed, err := vpcNatGatewayControllerReferencePatch(legacy, desired, gw)
	require.NoError(t, err)
	require.True(t, needed)
	require.Contains(t, string(patch), `"controller":true`)

	client := k8sfake.NewSimpleClientset(legacy)
	_, err = client.AppsV1().StatefulSets(legacy.Namespace).Patch(
		context.Background(), legacy.Name, types.MergePatchType, patch, metav1.PatchOptions{},
	)
	require.NoError(t, err)
	migrated, err := client.AppsV1().StatefulSets(legacy.Namespace).Get(
		context.Background(), legacy.Name, metav1.GetOptions{},
	)
	require.NoError(t, err)
	require.Equal(t, gw.UID, metav1.GetControllerOf(migrated).UID)
	require.Len(t, migrated.OwnerReferences, 2)

	other := legacy.DeepCopy()
	other.OwnerReferences = []metav1.OwnerReference{
		controllerOwnerReference(appsv1.SchemeGroupVersion.String(), "Deployment", "other", "other-uid"),
	}
	_, _, err = vpcNatGatewayControllerReferencePatch(other, desired, gw)
	require.ErrorContains(t, err, "already controlled")

	stale := legacy.DeepCopy()
	stale.OwnerReferences = []metav1.OwnerReference{
		{APIVersion: kubeovnv1.SchemeGroupVersion.String(), Kind: util.KindVpcNatGateway, Name: gw.Name, UID: "stale-gw-uid"},
	}
	_, _, err = vpcNatGatewayControllerReferencePatch(stale, desired, gw)
	require.ErrorContains(t, err, "already references")
}

// patchNatGwStatusFromAPI reconciles the gateway status the way its callers do: from the
// gateway and the freshly read gateway Pods.
func patchNatGwStatusFromAPI(t *testing.T, c *Controller, name string) error {
	t.Helper()
	gw, err := c.vpcNatGatewayLister.Get(name)
	require.NoError(t, err)
	pods, err := c.listNatGwPods(gw)
	require.NoError(t, err)
	return c.patchNatGwStatus(gw, pods)
}

func TestPatchNatGwStatusRejectsConflictBeforeStatusUpdate(t *testing.T) {
	const (
		gwName    = "conflict-gw"
		namespace = "default"
		subnet    = "nat-subnet"
	)
	gwUID := types.UID("gw-uid")
	stsUID := types.UID("sts-uid")
	stsName := util.GenNatGwName(gwName)
	labels := util.GenNatGwLabels(gwName)
	gw := &kubeovnv1.VpcNatGateway{
		Name: gwName, UID: gwUID,
		Spec: kubeovnv1.VpcNatGatewaySpec{
			Namespace: namespace,
			Subnet:    subnet,
			Replicas:  1,
			LanIP:     "10.20.0.10",
		},
	}
	sts := &appsv1.StatefulSet{
		Name:            stsName,
		Namespace:       namespace,
		UID:             stsUID,
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(kubeovnv1.SchemeGroupVersion.String(), util.KindVpcNatGateway, gwName, gwUID)},
		Spec:            appsv1.StatefulSetSpec{Selector: &metav1.LabelSelector{MatchLabels: labels}},
	}
	pod := &corev1.Pod{
		Name:            stsName + "-0",
		Namespace:       namespace,
		Labels:          labels,
		Annotations:     map[string]string{util.IPAddressAnnotation: "10.20.0.11"},
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(appsv1.SchemeGroupVersion.String(), util.KindStatefulSet, stsName, stsUID)},
		Status:          corev1.PodStatus{Phase: corev1.PodRunning},
	}

	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{gw},
		Subnets: []*kubeovnv1.Subnet{{
			Name: subnet,
			Spec: kubeovnv1.SubnetSpec{Provider: util.OvnProvider, Protocol: kubeovnv1.ProtocolIPv4},
		}},
		StatefulSets: []*appsv1.StatefulSet{sts},
		Pods:         []*corev1.Pod{pod},
	})
	require.NoError(t, err)
	controller := fakeController.fakeController

	require.ErrorContains(t, patchNatGwStatusFromAPI(t, controller, gwName), "conflicts")
	for _, action := range controller.config.KubeOvnClient.(*kubeovnfake.Clientset).Actions() {
		require.NotEqual(t, "patch", action.GetVerb(), "status must not be published on conflict")
	}
}

func TestPersistNatGwLanIPKeepsPersistedAddress(t *testing.T) {
	// A conflict is reported by patchNatGwStatus before this is reached; here the persisted
	// address must simply be left alone, as spec.lanIp is immutable.
	controller := &Controller{}
	gw := &kubeovnv1.VpcNatGateway{
		Name: "gw",
		Spec: kubeovnv1.VpcNatGatewaySpec{LanIP: "10.20.0.10", Replicas: 1},
	}
	require.NoError(t, controller.persistNatGwLanIP(gw, "10.20.0.11"))
}
