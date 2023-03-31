package webhook

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (v *ValidatingHook) ValidateVpcNatConfig(ctx context.Context) error {
	cm := &corev1.ConfigMap{}
	cmKey := types.NamespacedName{Namespace: "kube-system", Name: util.VpcNatConfig}
	if err := v.cache.Get(ctx, cmKey, cm); err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("configMap \"%s\" not configured", util.VpcNatConfig)
		}
		return err
	}

	if cm.Data["image"] == "" {
		err := fmt.Errorf("parameter \"image\" in ConfigMap \"%s\" cannot be empty", util.VpcNatConfig)
		return err
	}

	return nil
}
