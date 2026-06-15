package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kubeovn/kube-ovn/pkg/controller/ovndbtls"
)

func TestRolloutOVNDBTLSWorkloads(t *testing.T) {
	const namespace = "kube-system"
	client := fake.NewSimpleClientset(
		testOVNDBTLSSecret(namespace, ovndbtls.ServerSecretName, "server-hash"),
		testOVNDBTLSSecret(namespace, ovndbtls.ClientSecretName, "client-hash"),
		testDeployment(namespace, "ovn-central"),
		testDeployment(namespace, "kube-ovn-controller"),
		testDeployment(namespace, "kube-ovn-monitor"),
		testDaemonSet(namespace, "ovs-ovn"),
		testDaemonSet(namespace, "kube-ovn-pinger"),
	)
	c := &Controller{config: &Configuration{KubeClient: client, PodNamespace: namespace}}

	if err := c.rolloutOVNDBTLSWorkloads(context.Background()); err != nil {
		t.Fatalf("rolloutOVNDBTLSWorkloads returned error: %v", err)
	}

	deployment, err := client.AppsV1().Deployments(namespace).Get(context.Background(), "ovn-central", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get ovn-central deployment: %v", err)
	}
	if got, want := deployment.Spec.Template.Annotations[ovnDBTLSRolloutAnnotation], "server-hash.client-hash"; got != want {
		t.Fatalf("ovn-central rollout hash = %q, want %q", got, want)
	}

	deployment, err = client.AppsV1().Deployments(namespace).Get(context.Background(), "kube-ovn-controller", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get kube-ovn-controller deployment: %v", err)
	}
	if got, want := deployment.Spec.Template.Annotations[ovnDBTLSRolloutAnnotation], "client-hash"; got != want {
		t.Fatalf("kube-ovn-controller rollout hash = %q, want %q", got, want)
	}

	daemonSet, err := client.AppsV1().DaemonSets(namespace).Get(context.Background(), "ovs-ovn", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get ovs-ovn daemonset: %v", err)
	}
	if got, want := daemonSet.Spec.Template.Annotations[ovnDBTLSRolloutAnnotation], "client-hash"; got != want {
		t.Fatalf("ovs-ovn rollout hash = %q, want %q", got, want)
	}
}

func testOVNDBTLSSecret(namespace, name, hash string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				ovndbtls.AnnotationCertHash: hash,
			},
		},
	}
}

func testDeployment(namespace, name string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}},
			},
		},
	}
}

func testDaemonSet(namespace, name string) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}},
			},
		},
	}
}
