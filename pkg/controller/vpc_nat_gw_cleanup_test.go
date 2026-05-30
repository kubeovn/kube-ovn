package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	fakeversioned "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/fake"
	kubeovninformerfactory "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions"
)

// newNatGwCleanupController builds a minimal Controller exposing only the
// dependencies needed by the in-pod cleanup helpers: vpcNatGatewayLister to act
// as the deletion sentinel and an (always empty) podsLister so that getNatGwPod
// reports the gateway pod as not found. When gwExists is true a VpcNatGateway
// CRD named dp is registered; otherwise the lister returns NotFound for it.
func newNatGwCleanupController(t *testing.T, dp string, gwExists bool) *Controller {
	t.Helper()

	kubeOvnClient := fakeversioned.NewSimpleClientset()
	kubeOvnFactory := kubeovninformerfactory.NewSharedInformerFactory(kubeOvnClient, 0)
	gwInformer := kubeOvnFactory.Kubeovn().V1().VpcNatGateways()
	if gwExists {
		gw := &kubeovnv1.VpcNatGateway{ObjectMeta: metav1.ObjectMeta{Name: dp}}
		require.NoError(t, gwInformer.Informer().GetIndexer().Add(gw))
	}

	kubeClient := fake.NewSimpleClientset()
	kubeFactory := informers.NewSharedInformerFactory(kubeClient, 0)
	podInformer := kubeFactory.Core().V1().Pods()

	return &Controller{
		config:              &Configuration{PodNamespace: "kube-system"},
		vpcNatGatewayLister: gwInformer.Lister(),
		podsLister:          podInformer.Lister(),
	}
}

func TestNatGwDeleted(t *testing.T) {
	t.Run("CRD missing returns true", func(t *testing.T) {
		c := newNatGwCleanupController(t, "gw1", false)
		deleted, err := c.natGwDeleted("gw1")
		require.NoError(t, err)
		require.True(t, deleted)
	})
	t.Run("CRD present returns false", func(t *testing.T) {
		c := newNatGwCleanupController(t, "gw1", true)
		deleted, err := c.natGwDeleted("gw1")
		require.NoError(t, err)
		require.False(t, deleted)
	})
}

// Each in-pod cleanup helper must skip cleanup (return nil) when the gateway CRD
// is gone, and must return an error (so the reconciler retries) when the CRD
// still exists but the gateway pod is temporarily absent.
func TestDeleteEipInPod(t *testing.T) {
	t.Run("gateway gone skips cleanup", func(t *testing.T) {
		c := newNatGwCleanupController(t, "gw1", false)
		require.NoError(t, c.deleteEipInPod("gw1", "10.0.0.1"))
	})
	t.Run("gateway present but pod absent retries", func(t *testing.T) {
		c := newNatGwCleanupController(t, "gw1", true)
		err := c.deleteEipInPod("gw1", "10.0.0.1")
		require.Error(t, err)
		require.True(t, k8serrors.IsNotFound(err))
	})
}

func TestDelEipQoSInPod(t *testing.T) {
	t.Run("gateway gone skips cleanup", func(t *testing.T) {
		c := newNatGwCleanupController(t, "gw1", false)
		require.NoError(t, c.delEipQoSInPod("gw1", "10.0.0.1", kubeovnv1.QoSDirectionIngress))
	})
	t.Run("gateway present but pod absent retries", func(t *testing.T) {
		c := newNatGwCleanupController(t, "gw1", true)
		err := c.delEipQoSInPod("gw1", "10.0.0.1", kubeovnv1.QoSDirectionIngress)
		require.Error(t, err)
		require.True(t, k8serrors.IsNotFound(err))
	})
}

func TestDeleteFipInPod(t *testing.T) {
	t.Run("gateway gone skips cleanup", func(t *testing.T) {
		c := newNatGwCleanupController(t, "gw1", false)
		require.NoError(t, c.deleteFipInPod("gw1", "10.0.0.1"))
	})
	t.Run("gateway present but pod absent retries", func(t *testing.T) {
		c := newNatGwCleanupController(t, "gw1", true)
		err := c.deleteFipInPod("gw1", "10.0.0.1")
		require.Error(t, err)
		require.True(t, k8serrors.IsNotFound(err))
	})
}

func TestDeleteDnatInPod(t *testing.T) {
	t.Run("gateway gone skips cleanup", func(t *testing.T) {
		c := newNatGwCleanupController(t, "gw1", false)
		require.NoError(t, c.deleteDnatInPod("gw1", "tcp", "10.0.0.1", "80"))
	})
	t.Run("gateway present but pod absent retries", func(t *testing.T) {
		c := newNatGwCleanupController(t, "gw1", true)
		err := c.deleteDnatInPod("gw1", "tcp", "10.0.0.1", "80")
		require.Error(t, err)
		require.True(t, k8serrors.IsNotFound(err))
	})
}

func TestDeleteSnatInPod(t *testing.T) {
	t.Run("gateway gone skips cleanup", func(t *testing.T) {
		c := newNatGwCleanupController(t, "gw1", false)
		require.NoError(t, c.deleteSnatInPod("gw1", "10.0.0.1", "10.16.0.0/16"))
	})
	t.Run("gateway present but pod absent retries", func(t *testing.T) {
		c := newNatGwCleanupController(t, "gw1", true)
		err := c.deleteSnatInPod("gw1", "10.0.0.1", "10.16.0.0/16")
		require.Error(t, err)
		require.True(t, k8serrors.IsNotFound(err))
	})
}
