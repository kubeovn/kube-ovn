package webhook

import (
	"context"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func vpcNatConfigName() string {
	if name := strings.TrimSpace(os.Getenv(util.EnvVpcNatConfig)); name != "" {
		return name
	}
	return util.VpcNatConfig
}

func (v *ValidatingHook) ValidateVpcNatConfig(ctx context.Context) error {
	cm := &corev1.ConfigMap{}
	configName := vpcNatConfigName()
	cmKey := client.ObjectKey{Namespace: metav1.NamespaceSystem, Name: configName}
	if err := v.cache.Get(ctx, cmKey, cm); err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("configMap \"%s\" not configured", configName)
		}
		return err
	}

	if cm.Data["image"] == "" {
		err := fmt.Errorf("parameter \"image\" in ConfigMap \"%s\" cannot be empty", configName)
		return err
	}

	return nil
}
