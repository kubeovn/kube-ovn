package controller

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) InitDefaultOVNIPsecCA() error {
	namespace := os.Getenv(util.EnvPodNamespace)
	_, err := c.config.KubeClient.CoreV1().Secrets(namespace).Get(context.TODO(), util.DefaultOVNIPSecCA, metav1.GetOptions{})
	if err == nil {
		klog.Infof("ovn ipsec CA secret already exists, skip")
		return nil
	}

	if !k8serrors.IsNotFound(err) {
		return err
	}

	output, err := exec.Command("ovs-pki", "init", "--force").CombinedOutput()
	if err != nil {
		klog.Errorf("ovs-pki init failed: %s", string(output))
		return err
	}

	if _, err = os.Stat(util.DefaultOVSCACertPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("CA Cert not exist: %s", util.DefaultOVSCACertPath)
		}
		return err
	}
	if _, err = os.Stat(util.DefaultOVSCACertKeyPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("CA Cert Key not exist: %s", util.DefaultOVSCACertKeyPath)
		}
		return err
	}

	cacert, err := os.ReadFile(util.DefaultOVSCACertPath)
	if err != nil {
		return err
	}
	cakey, err := os.ReadFile(util.DefaultOVSCACertKeyPath)
	if err != nil {
		return err
	}

	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.DefaultOVNIPSecCA,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"cacert": cacert,
			"cakey":  cakey,
		},
	}

	if _, err = c.config.KubeClient.CoreV1().Secrets(namespace).Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
		return err
	}

	klog.Infof("OVN IPSec CA secret init successfully")
	return nil
}
