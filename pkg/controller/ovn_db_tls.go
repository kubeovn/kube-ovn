package controller

import (
	"bytes"
	"context"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/controller/ovndbtls"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const ovnDBTLSReconcileInterval = 10 * time.Minute

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
		}
	}, ovnDBTLSReconcileInterval)
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
