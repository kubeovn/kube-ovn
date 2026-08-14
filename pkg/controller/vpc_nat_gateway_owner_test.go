package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

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

func TestVpcNatGatewayOwnerFromStatefulSetPod(t *testing.T) {
	const (
		gwName    = "dynamic-gw"
		namespace = "default"
	)
	gwUID := types.UID("gw-uid")
	stsUID := types.UID("sts-uid")
	stsName := util.GenNatGwName(gwName)
	gw := &kubeovnv1.VpcNatGateway{ObjectMeta: metav1.ObjectMeta{Name: gwName, UID: gwUID}}
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            stsName,
			Namespace:       namespace,
			UID:             stsUID,
			OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(kubeovnv1.SchemeGroupVersion.String(), util.KindVpcNatGateway, gwName, gwUID)},
		},
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:            stsName + "-0",
		Namespace:       namespace,
		Labels:          util.GenNatGwLabels(gwName),
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(appsv1.SchemeGroupVersion.String(), util.KindStatefulSet, stsName, stsUID)},
	}}

	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{gw},
		StatefulSets:   []*appsv1.StatefulSet{sts},
		Pods:           []*corev1.Pod{pod},
	})
	require.NoError(t, err)

	owner, err := fakeController.fakeController.vpcNatGatewayOwnerFromPod(pod)
	require.NoError(t, err)
	require.Equal(t, gwName, owner.Name)
	require.Equal(t, gwUID, owner.UID)
	fakeController.fakeController.enqueueVpcNatGatewayForPod(pod)
	require.Equal(t, 1, fakeController.fakeController.addOrUpdateVpcNatGatewayQueue.Len())

	oldRegularPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "regular", Namespace: namespace}}
	newRegularPod := oldRegularPod.DeepCopy()
	newRegularPod.Annotations = map[string]string{"changed": "true"}
	fakeController.fakeController.enqueueUpdateVpcNatGatewayForPod(oldRegularPod, newRegularPod)
	require.Equal(t, 1, fakeController.fakeController.addOrUpdateVpcNatGatewayQueue.Len())

	pod.OwnerReferences[0].UID = "stale-sts-uid"
	_, err = fakeController.fakeController.vpcNatGatewayOwnerFromPod(pod)
	require.ErrorContains(t, err, "does not match")
}

func TestVpcNatGatewayOwnerFromDeploymentPod(t *testing.T) {
	const (
		gwName    = "ha-gw"
		namespace = "default"
		subnet    = "ha-subnet"
	)
	gwUID := types.UID("gw-uid")
	deployUID := types.UID("deploy-uid")
	rsUID := types.UID("rs-uid")
	deployName := util.GenNatGwName(gwName)
	gw := &kubeovnv1.VpcNatGateway{
		ObjectMeta: metav1.ObjectMeta{Name: gwName, UID: gwUID},
		Spec: kubeovnv1.VpcNatGatewaySpec{
			Namespace: namespace,
			Subnet:    subnet,
			Replicas:  2,
		},
	}
	deploy := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Name:            deployName,
		Namespace:       namespace,
		UID:             deployUID,
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(kubeovnv1.SchemeGroupVersion.String(), util.KindVpcNatGateway, gwName, gwUID)},
	}}
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
		Name:            deployName + "-abc",
		Namespace:       namespace,
		UID:             rsUID,
		OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(appsv1.SchemeGroupVersion.String(), "Deployment", deployName, deployUID)},
	}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rs.Name + "-xyz",
			Namespace: namespace,
			Labels:    util.GenNatGwLabels(gwName),
			Annotations: map[string]string{
				util.IPAddressAnnotation:         "10.20.0.10,fd00::10",
				util.VpcNatGatewayInitAnnotation: "true",
			},
			OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(appsv1.SchemeGroupVersion.String(), "ReplicaSet", rs.Name, rsUID)},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{gw},
		Subnets: []*kubeovnv1.Subnet{{
			ObjectMeta: metav1.ObjectMeta{Name: subnet},
			Spec:       kubeovnv1.SubnetSpec{Provider: util.OvnProvider, Protocol: kubeovnv1.ProtocolDual},
		}},
		Deployments: []*appsv1.Deployment{deploy},
		ReplicaSets: []*appsv1.ReplicaSet{rs},
		Pods:        []*corev1.Pod{pod},
	})
	require.NoError(t, err)

	owner, err := fakeController.fakeController.vpcNatGatewayOwnerFromPod(pod)
	require.NoError(t, err)
	require.Equal(t, gwName, owner.Name)
	require.Equal(t, gwUID, owner.UID)

	lanIPs, needsInit, err := fakeController.fakeController.getNatGwObservedState(gw)
	require.NoError(t, err)
	require.Equal(t, []string{"10.20.0.10", "fd00::10"}, lanIPs)
	require.False(t, needsInit)
}

func TestPatchNatGwStatusPersistsDynamicLanIP(t *testing.T) {
	const (
		gwName    = "dynamic-gw"
		namespace = "default"
		subnet    = "nat-subnet"
		lanIP     = "10.20.0.10"
	)
	gwUID := types.UID("gw-uid")
	stsUID := types.UID("sts-uid")
	stsName := util.GenNatGwName(gwName)
	labels := util.GenNatGwLabels(gwName)
	gw := &kubeovnv1.VpcNatGateway{
		ObjectMeta: metav1.ObjectMeta{Name: gwName, UID: gwUID},
		Spec: kubeovnv1.VpcNatGatewaySpec{
			Namespace: namespace,
			Subnet:    subnet,
			Replicas:  1,
		},
	}
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            stsName,
			Namespace:       namespace,
			UID:             stsUID,
			OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(kubeovnv1.SchemeGroupVersion.String(), util.KindVpcNatGateway, gwName, gwUID)},
		},
		Spec: appsv1.StatefulSetSpec{Selector: &metav1.LabelSelector{MatchLabels: labels}},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            stsName + "-0",
			Namespace:       namespace,
			Labels:          labels,
			Annotations:     map[string]string{util.IPAddressAnnotation: lanIP},
			OwnerReferences: []metav1.OwnerReference{controllerOwnerReference(appsv1.SchemeGroupVersion.String(), util.KindStatefulSet, stsName, stsUID)},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	fakeController, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		VpcNatGateways: []*kubeovnv1.VpcNatGateway{gw},
		Subnets: []*kubeovnv1.Subnet{{
			ObjectMeta: metav1.ObjectMeta{Name: subnet},
			Spec: kubeovnv1.SubnetSpec{
				Provider: util.OvnProvider,
				Protocol: kubeovnv1.ProtocolIPv4,
			},
		}},
		StatefulSets: []*appsv1.StatefulSet{sts},
		Pods:         []*corev1.Pod{pod},
	})
	require.NoError(t, err)
	controller := fakeController.fakeController

	require.NoError(t, controller.patchNatGwStatus(gwName))
	var patchSubresources []string
	for _, action := range controller.config.KubeOvnClient.(*kubeovnfake.Clientset).Actions() {
		if action.GetVerb() == "patch" && action.GetResource().Resource == "vpc-nat-gateways" {
			patchSubresources = append(patchSubresources, action.GetSubresource())
		}
	}
	require.Equal(t, []string{"status", ""}, patchSubresources, "status must be published before spec.lanIp")

	updated, err := controller.config.KubeOvnClient.KubeovnV1().VpcNatGateways().Get(context.Background(), gwName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, lanIP, updated.Status.LanIP)
	require.Equal(t, lanIP, updated.Spec.LanIP)
	require.Equal(t, 1, controller.initVpcNatGatewayQueue.Len())
}

func TestVpcNatGatewayControllerReferencePatch(t *testing.T) {
	gw := &kubeovnv1.VpcNatGateway{ObjectMeta: metav1.ObjectMeta{Name: "gw", UID: "gw-uid"}}
	legacy := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{
		Name:            "gw-sts",
		Namespace:       "default",
		OwnerReferences: []metav1.OwnerReference{{APIVersion: kubeovnv1.SchemeGroupVersion.String(), Kind: util.KindVpcNatGateway, Name: gw.Name, UID: gw.UID}},
	}}
	desired := legacy.DeepCopy()
	desired.OwnerReferences = []metav1.OwnerReference{
		controllerOwnerReference(kubeovnv1.SchemeGroupVersion.String(), util.KindVpcNatGateway, gw.Name, gw.UID),
	}

	patch, needed, err := vpcNatGatewayControllerReferencePatch(legacy, desired, gw)
	require.NoError(t, err)
	require.True(t, needed)
	require.Contains(t, string(patch), `"controller":true`)

	other := legacy.DeepCopy()
	other.OwnerReferences = []metav1.OwnerReference{
		controllerOwnerReference(appsv1.SchemeGroupVersion.String(), "Deployment", "other", "other-uid"),
	}
	_, _, err = vpcNatGatewayControllerReferencePatch(other, desired, gw)
	require.ErrorContains(t, err, "already controlled")
}

func TestPersistNatGwLanIPRejectsObservedConflict(t *testing.T) {
	controller := &Controller{}
	gw := &kubeovnv1.VpcNatGateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw"},
		Spec:       kubeovnv1.VpcNatGatewaySpec{LanIP: "10.20.0.10", Replicas: 1},
	}
	require.ErrorContains(t, controller.persistNatGwLanIP(gw, "10.20.0.11"), "conflicts")
}
