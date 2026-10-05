package controller

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestIPsecCASeparatesPublicTrustAndPrivateSigner(t *testing.T) {
	client := fake.NewClientset()
	c := &Controller{config: &Configuration{KubeClient: client, PodNamespace: "kube-system"}}
	require.NoError(t, c.InitDefaultOVNIPsecCA())
	secrets := client.CoreV1().Secrets("kube-system")
	trust, err := secrets.Get(t.Context(), util.DefaultOVNIPSecCA, metav1.GetOptions{})
	require.NoError(t, err)
	require.NotContains(t, trust.Data, "cakey")
	signer, err := secrets.Get(t.Context(), util.DefaultOVNIPSecSigner, metav1.GetOptions{})
	require.NoError(t, err)
	require.NoError(t, validateIPsecCA(signer.Data["cacert"], signer.Data["cakey"]))
	require.Equal(t, trust.Data["cacert"], signer.Data["cacert"])
	require.NoError(t, c.InitDefaultOVNIPsecCA())
	again, err := secrets.Get(t.Context(), util.DefaultOVNIPSecSigner, metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, bytes.Equal(signer.Data["cakey"], again.Data["cakey"]))
}

func TestIPsecCAMigrationKeepsExistingRoot(t *testing.T) {
	cert, key, err := newIPsecCA()
	require.NoError(t, err)
	legacy := &corev1.Secret{Name: util.DefaultOVNIPSecCA, Namespace: "kube-system", Data: map[string][]byte{"cacert": cert, "cakey": key}}
	client := fake.NewClientset(legacy)
	c := &Controller{config: &Configuration{KubeClient: client, PodNamespace: "kube-system"}}
	require.NoError(t, c.InitDefaultOVNIPsecCA())
	secrets := client.CoreV1().Secrets("kube-system")
	signer, err := secrets.Get(t.Context(), util.DefaultOVNIPSecSigner, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, legacy.Data, signer.Data)
	trust, err := secrets.Get(t.Context(), util.DefaultOVNIPSecCA, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, legacy.Data, trust.Data, "old controllers retain access until compatibility is acknowledged")
}

func TestIPsecCAMissingPrivateSignerDoesNotRegenerateRoot(t *testing.T) {
	cert, _, err := newIPsecCA()
	require.NoError(t, err)
	public := &corev1.Secret{Name: util.DefaultOVNIPSecCA, Namespace: "kube-system", Data: map[string][]byte{"cacert": cert}}
	client := fake.NewClientset(public)
	c := &Controller{config: &Configuration{KubeClient: client, PodNamespace: "kube-system"}}
	require.ErrorContains(t, c.InitDefaultOVNIPsecCA(), "refusing to replace")
	_, err = client.CoreV1().Secrets("kube-system").Get(t.Context(), util.DefaultOVNIPSecSigner, metav1.GetOptions{})
	require.Error(t, err)
}

func TestIPsecCAMigrationRequiresLiveOwnedAcknowledgements(t *testing.T) {
	cert, key, err := newIPsecCA()
	require.NoError(t, err)
	for _, tc := range []struct {
		name    string
		change  func(*appsv1.Deployment, *appsv1.ReplicaSet, []*corev1.Pod)
		removed bool
		fails   bool
	}{
		{name: "all-new", removed: true},
		{name: "mixed-controllers", change: func(_ *appsv1.Deployment, _ *appsv1.ReplicaSet, pods []*corev1.Pod) {
			delete(pods[1].Annotations, ipsecSignerVersionAnnotation)
		}},
		{name: "terminal-pods", fails: true, change: func(_ *appsv1.Deployment, _ *appsv1.ReplicaSet, pods []*corev1.Pod) {
			for _, pod := range pods {
				pod.Status.Phase = corev1.PodFailed
			}
		}},
		{name: "missing-replica", fails: true, change: func(deployment *appsv1.Deployment, _ *appsv1.ReplicaSet, _ []*corev1.Pod) {
			deployment.Spec.Replicas = new(int32(3))
		}},
		{name: "wrong-owner", fails: true, change: func(_ *appsv1.Deployment, rs *appsv1.ReplicaSet, _ []*corev1.Pod) {
			rs.OwnerReferences[0].UID = "another-deployment"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deployment := &appsv1.Deployment{Name: "kube-ovn-controller", Namespace: "kube-system", UID: "deployment-uid", Spec: appsv1.DeploymentSpec{Replicas: new(int32(2)), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "controller"}}}}
			rs := &appsv1.ReplicaSet{Name: "controller-rs", Namespace: "kube-system", UID: "rs-uid", OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: deployment.Name, UID: deployment.UID, Controller: new(true)}}}
			pods := make([]*corev1.Pod, 2)
			for n, name := range []string{"controller-a", "controller-b"} {
				pods[n] = &corev1.Pod{Name: name, Namespace: "kube-system", Labels: map[string]string{"app": "controller"}, Annotations: map[string]string{ipsecSignerVersionAnnotation: "2"}, OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: new(true)}}, Spec: corev1.PodSpec{ServiceAccountName: "ovn"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
			}
			if tc.change != nil {
				tc.change(deployment, rs, pods)
			}
			trust := &corev1.Secret{Name: util.DefaultOVNIPSecCA, Namespace: "kube-system", Data: map[string][]byte{"cacert": cert, "cakey": key}}
			signer := trust.DeepCopy()
			signer.Name = util.DefaultOVNIPSecSigner
			objects := []runtime.Object{deployment, rs, trust, signer}
			for _, pod := range pods {
				objects = append(objects, pod)
			}
			client := fake.NewClientset(objects...)
			c := &Controller{config: &Configuration{KubeClient: client, PodNamespace: "kube-system", PodName: pods[0].Name}}
			err := c.finalizeIPsecCAFormat(t.Context())
			if tc.fails {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			after, err := client.CoreV1().Secrets("kube-system").Get(t.Context(), trust.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, tc.removed, len(after.Data["cakey"]) == 0)
			require.Equal(t, cert, after.Data["cacert"])
			if tc.removed {
				require.NoError(t, c.finalizeIPsecCAFormat(t.Context()), "finalization is idempotent")
			}
		})
	}
}

func TestIPsecSignerAcknowledgementPreservesOtherPodFields(t *testing.T) {
	pod := &corev1.Pod{Name: "controller", Namespace: "kube-system", UID: "pod-uid", Annotations: map[string]string{"unrelated": "keep"}, Spec: corev1.PodSpec{NodeName: "node-a", ServiceAccountName: "ovn"}}
	client := fake.NewClientset(pod)
	config := &Configuration{KubeClient: client, PodNamespace: pod.Namespace, PodName: pod.Name, EnableOVNIPSec: true}
	require.NoError(t, MarkIPsecSignerCompatibility(t.Context(), config))
	after, err := client.CoreV1().Pods(pod.Namespace).Get(t.Context(), pod.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "2", after.Annotations[ipsecSignerVersionAnnotation])
	require.Equal(t, "keep", after.Annotations["unrelated"])
	require.Equal(t, pod.Spec, after.Spec)
	require.NoError(t, MarkIPsecSignerCompatibility(t.Context(), config))
}

func TestIPsecCARejectsMismatchedKeyAndPreservesOverlap(t *testing.T) {
	cert, key, err := newIPsecCA()
	require.NoError(t, err)
	otherCert, otherKey, err := newIPsecCA()
	require.NoError(t, err)
	require.Error(t, validateIPsecCA(cert, otherKey))
	bundle := append(bytes.Clone(cert), otherCert...)
	trust := &corev1.Secret{Name: util.DefaultOVNIPSecCA, Namespace: "kube-system", Data: map[string][]byte{"cacert": bundle}}
	signer := &corev1.Secret{Name: util.DefaultOVNIPSecSigner, Namespace: "kube-system", Data: map[string][]byte{"cacert": cert, "cakey": key}}
	client := fake.NewClientset(trust, signer)
	c := &Controller{config: &Configuration{KubeClient: client, PodNamespace: trust.Namespace}}
	require.NoError(t, c.InitDefaultOVNIPsecCA())
	after, err := client.CoreV1().Secrets(trust.Namespace).Get(t.Context(), trust.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, bundle, after.Data["cacert"])
}

func TestIPsecCAMigratesLegacyOverlapUsingTheMatchingSigner(t *testing.T) {
	cert, key, err := newIPsecCA()
	require.NoError(t, err)
	other, _, err := newIPsecCA()
	require.NoError(t, err)
	bundle := append(bytes.Clone(other), cert...)
	trust := &corev1.Secret{Name: util.DefaultOVNIPSecCA, Namespace: "kube-system", Data: map[string][]byte{"cacert": bundle, "cakey": key}}
	client := fake.NewClientset(trust)
	c := &Controller{config: &Configuration{KubeClient: client, PodNamespace: trust.Namespace}}
	require.NoError(t, c.InitDefaultOVNIPsecCA())
	signer, err := client.CoreV1().Secrets(trust.Namespace).Get(t.Context(), util.DefaultOVNIPSecSigner, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, cert, signer.Data["cacert"])
	require.Equal(t, key, signer.Data["cakey"])
	after, err := client.CoreV1().Secrets(trust.Namespace).Get(t.Context(), trust.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, trust.Data, after.Data)
}

func TestIPsecCATrustCreationRaceDoesNotOverwriteAnotherRoot(t *testing.T) {
	cert, key, err := newIPsecCA()
	require.NoError(t, err)
	other, _, err := newIPsecCA()
	require.NoError(t, err)
	client := fake.NewClientset(&corev1.Secret{Name: util.DefaultOVNIPSecSigner, Namespace: "kube-system", Data: map[string][]byte{"cacert": cert, "cakey": key}})
	client.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		obj := action.(k8stesting.CreateAction).GetObject().(*corev1.Secret)
		if obj.Name != util.DefaultOVNIPSecCA {
			return false, nil, nil
		}
		raced := &corev1.Secret{Name: util.DefaultOVNIPSecCA, Namespace: "kube-system", Data: map[string][]byte{"cacert": other}}
		require.NoError(t, client.Tracker().Add(raced))
		return true, nil, k8serrors.NewAlreadyExists(schema.GroupResource{Resource: "secrets"}, obj.Name)
	})
	c := &Controller{config: &Configuration{KubeClient: client, PodNamespace: "kube-system"}}
	require.ErrorContains(t, c.InitDefaultOVNIPsecCA(), "does not contain")
	after, err := client.CoreV1().Secrets("kube-system").Get(t.Context(), util.DefaultOVNIPSecCA, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, other, after.Data["cacert"])
}
