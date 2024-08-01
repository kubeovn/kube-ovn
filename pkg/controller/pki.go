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

func (c *Controller) InitOVNIPsecCA() error {
	_, err := c.config.KubeClient.CoreV1().Secrets("kube-system").Get(context.TODO(), util.OVNIPSecCASecret, metav1.GetOptions{})
	if err == nil {
		klog.Infof("ovn ipsec CA secret already exists, skip")
		return nil
	}

	if !k8serrors.IsNotFound(err) {
		return err
	}

	cmd := exec.Command("ovs-pki", "init", "--force")
	_, err = cmd.Output()
	if err != nil {
		return err
	}

	_, err = os.Stat(util.DefaultOVSCACertPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("CA Cert not exist: %s", util.DefaultOVSCACertPath)
		}
		return err
	}

	_, err = os.Stat(util.DefaultOVSCACertKeyPath)
	if err != nil {
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
			Name:      util.OVNIPSecCASecret,
			Namespace: "kube-system",
		},
		Data: map[string][]byte{
			"cacert": cacert,
			"cakey":  cakey,
		},
	}

	_, err = c.config.KubeClient.CoreV1().Secrets("kube-system").Create(context.TODO(), secret, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	klog.Infof("OVN IPSec CA secret init successfully")
	return nil
}
