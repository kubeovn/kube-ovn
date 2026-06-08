package controller

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// Test_handleAddNamespace_orphanedSubnet is a regression guard for the bug where
// a subnet referencing a non-existent VPC aborted evaluation of the whole subnet
// list. The orphan handling used to `break` the `for _, s := range subnets` loop,
// so any valid subnet returned by the lister after the orphaned one was never
// bound to the namespace. The fix replaces `break` with `continue`, so the valid
// subnet must always be bound regardless of the (random) lister iteration order.
func Test_handleAddNamespace_orphanedSubnet(t *testing.T) {
	const nsName = "test-ns"
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: nsName},
	}
	// A broken subnet whose referenced VPC does not exist - it must not stop the
	// loop from reaching the valid subnet below.
	orphanSubnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: "orphan-subnet"},
		Spec: kubeovnv1.SubnetSpec{
			Vpc:       "ghost-vpc",
			CIDRBlock: "10.16.0.0/16",
		},
	}
	// A valid subnet that binds the namespace via Spec.Namespaces.
	validSubnet := &kubeovnv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{Name: "valid-subnet"},
		Spec: kubeovnv1.SubnetSpec{
			Namespaces: []string{nsName},
			CIDRBlock:  "10.17.0.0/16",
		},
	}

	fakeCtrl, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
		Namespaces: []*corev1.Namespace{ns},
		Subnets:    []*kubeovnv1.Subnet{orphanSubnet, validSubnet},
	})
	require.NoError(t, err)
	ctrl := fakeCtrl.fakeController

	require.NoError(t, ctrl.handleAddNamespace(nsName))

	got, err := ctrl.config.KubeClient.CoreV1().Namespaces().Get(context.Background(), nsName, metav1.GetOptions{})
	require.NoError(t, err)

	lss := strings.Split(got.Annotations[util.LogicalSwitchAnnotation], ",")
	require.Contains(t, lss, validSubnet.Name, "valid subnet must be bound even when an orphaned subnet is present")
	require.NotContains(t, lss, orphanSubnet.Name, "broken subnet must not be bound to the namespace")
}
