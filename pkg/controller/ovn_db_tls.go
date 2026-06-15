package controller

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/controller/ovndbtls"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	ovnDBTLSReconcileInterval = 10 * time.Minute
	ovnDBTLSRolloutAnnotation = "kube-ovn.io/ovn-db-tls-rollout-hash"
)

func (c *Controller) reconcileOVNDBTLSSecrets(ctx context.Context) error {
	if !c.shouldManageOVNDBTLSCert() {
		return nil
	}

	manager := ovndbtls.NewManager(ovndbtls.Config{
		Client:    c.config.KubeClient,
		Namespace: c.config.PodNamespace,
	})
	if err := manager.Reconcile(ctx); err != nil {
		return err
	}
	if err := c.rolloutOVNDBTLSWorkloads(ctx); err != nil {
		return err
	}
	return c.waitOVNDBTLSClientVolume(ctx)
}

func (c *Controller) startOVNDBTLSManager(ctx context.Context) {
	if !c.shouldManageOVNDBTLSCert() {
		return
	}

	manager := ovndbtls.NewManager(ovndbtls.Config{
		Client:    c.config.KubeClient,
		Namespace: c.config.PodNamespace,
	})
	go wait.UntilWithContext(ctx, func(ctx context.Context) {
		if err := manager.Reconcile(ctx); err != nil {
			klog.Errorf("failed to reconcile OVN DB TLS secrets: %v", err)
			return
		}
		if err := c.rolloutOVNDBTLSWorkloads(ctx); err != nil {
			klog.Errorf("failed to rollout workloads for OVN DB TLS secrets: %v", err)
		}
	}, ovnDBTLSReconcileInterval)
}

func (c *Controller) rolloutOVNDBTLSWorkloads(ctx context.Context) error {
	serverHash, err := c.ovnDBTLSSecretHash(ctx, ovndbtls.ServerSecretName)
	if err != nil {
		return err
	}
	clientHash, err := c.ovnDBTLSSecretHash(ctx, ovndbtls.ClientSecretName)
	if err != nil {
		return err
	}

	centralHash := fmt.Sprintf("%s.%s", serverHash, clientHash)
	for _, name := range []string{"ovn-central"} {
		if err := c.updateDeploymentOVNDBTLSHash(ctx, name, centralHash); err != nil {
			return err
		}
	}
	for _, name := range []string{"kube-ovn-controller", "kube-ovn-monitor", "ovn-ic-controller"} {
		if err := c.updateDeploymentOVNDBTLSHash(ctx, name, clientHash); err != nil {
			return err
		}
	}
	for _, name := range []string{"ovs-ovn", "ovs-ovn-dpdk", "kube-ovn-pinger"} {
		if err := c.updateDaemonSetOVNDBTLSHash(ctx, name, clientHash); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) ovnDBTLSSecretHash(ctx context.Context, name string) (string, error) {
	secret, err := c.config.KubeClient.CoreV1().Secrets(c.config.PodNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	hash := secret.Annotations[ovndbtls.AnnotationCertHash]
	if hash == "" {
		return "", fmt.Errorf("secret %s/%s missing annotation %s", c.config.PodNamespace, name, ovndbtls.AnnotationCertHash)
	}
	return hash, nil
}

func (c *Controller) updateDeploymentOVNDBTLSHash(ctx context.Context, name, hash string) error {
	deployments := c.config.KubeClient.AppsV1().Deployments(c.config.PodNamespace)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		deployment, err := deployments.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if deployment.Spec.Template.Annotations[ovnDBTLSRolloutAnnotation] == hash {
			return nil
		}
		deployment = deployment.DeepCopy()
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = map[string]string{}
		}
		deployment.Spec.Template.Annotations[ovnDBTLSRolloutAnnotation] = hash
		_, err = deployments.Update(ctx, deployment, metav1.UpdateOptions{})
		return err
	})
}

func (c *Controller) updateDaemonSetOVNDBTLSHash(ctx context.Context, name, hash string) error {
	daemonSets := c.config.KubeClient.AppsV1().DaemonSets(c.config.PodNamespace)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		daemonSet, err := daemonSets.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if daemonSet.Spec.Template.Annotations[ovnDBTLSRolloutAnnotation] == hash {
			return nil
		}
		daemonSet = daemonSet.DeepCopy()
		if daemonSet.Spec.Template.Annotations == nil {
			daemonSet.Spec.Template.Annotations = map[string]string{}
		}
		daemonSet.Spec.Template.Annotations[ovnDBTLSRolloutAnnotation] = hash
		_, err = daemonSets.Update(ctx, daemonSet, metav1.UpdateOptions{})
		return err
	})
}

func (c *Controller) waitOVNDBTLSClientVolume(ctx context.Context) error {
	return wait.PollUntilContextTimeout(ctx, time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		secret, err := c.config.KubeClient.CoreV1().Secrets(c.config.PodNamespace).Get(ctx, ovndbtls.ClientSecretName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		matches, err := filesMatchSecret(secret.Data)
		if err != nil {
			klog.Infof("waiting for OVN DB TLS client Secret volume: %v", err)
			return false, nil
		}
		return matches, nil
	})
}

func filesMatchSecret(data map[string][]byte) (bool, error) {
	checks := map[string]string{
		ovndbtls.KeyClientCert: util.SslClientCertPath,
		ovndbtls.KeyClientKey:  util.SslClientKeyPath,
		ovndbtls.KeyCACert:     util.SslCAPath,
	}
	for key, path := range checks {
		content, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}
		if !bytes.Equal(content, data[key]) {
			return false, nil
		}
	}
	return true, nil
}
