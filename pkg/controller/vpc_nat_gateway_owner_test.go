package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
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
