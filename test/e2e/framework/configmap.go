package framework

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/onsi/gomega"
)

// ConfigMapClient is a struct for ConfigMap client.
type ConfigMapClient struct {
	f *Framework
	v1core.ConfigMapInterface
}

func (f *Framework) ConfigMapClient() *ConfigMapClient {
	return f.ConfigMapClientNS(f.Namespace.Name)
}

func (f *Framework) ConfigMapClientNS(namespace string) *ConfigMapClient {
	return &ConfigMapClient{
		f:                  f,
		ConfigMapInterface: f.ClientSet.CoreV1().ConfigMaps(namespace),
	}
}

func (c *ConfigMapClient) Get(name string) *corev1.ConfigMap {
	ConfigMap, err := c.ConfigMapInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return ConfigMap
}

// Create creates a new ConfigMap according to the framework specifications
func (c *ConfigMapClient) Create(ConfigMap *corev1.ConfigMap) *corev1.ConfigMap {
	s, err := c.ConfigMapInterface.Create(context.TODO(), ConfigMap, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating ConfigMap")
	return s.DeepCopy()
}

// Patch patches the ConfigMap
func (c *ConfigMapClient) Patch(original, modified *corev1.ConfigMap) *corev1.ConfigMap {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedConfigMap *corev1.ConfigMap
	err = wait.PollImmediate(2*time.Second, timeout, func() (bool, error) {
		s, err := c.ConfigMapInterface.Patch(context.TODO(), original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch ConfigMap %q", original.Name)
		}
		patchedConfigMap = s
		return true, nil
	})
	if err == nil {
		return patchedConfigMap.DeepCopy()
	}

	if IsTimeout(err) {
		Failf("timed out while retrying to patch ConfigMap %s", original.Name)
	}
	ExpectNoError(maybeTimeoutError(err, "patching ConfigMap %s", original.Name))

	return nil
}

// PatchSync patches the ConfigMap and waits the ConfigMap to meet the condition
func (c *ConfigMapClient) PatchSync(original, modified *corev1.ConfigMap, cond func(s *corev1.ConfigMap) (bool, error), condDesc string) *corev1.ConfigMap {
	_ = c.Patch(original, modified)
	return c.WaitUntil(original.Name, cond, condDesc, 2*time.Second, timeout)
}

// Delete deletes a ConfigMap if the ConfigMap exists
func (c *ConfigMapClient) Delete(name string) {
	err := c.ConfigMapInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete ConfigMap %q: %v", name, err)
	}
}

// DeleteSync deletes the ConfigMap and waits for the ConfigMap to disappear for `timeout`.
// If the ConfigMap doesn't disappear before the timeout, it will fail the test.
func (c *ConfigMapClient) DeleteSync(name string) {
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for ConfigMap %q to disappear", name)
}

// WaitUntil waits the given timeout duration for the specified condition to be met.
func (c *ConfigMapClient) WaitUntil(name string, cond func(s *corev1.ConfigMap) (bool, error), condDesc string, interval, timeout time.Duration) *corev1.ConfigMap {
	var ConfigMap *corev1.ConfigMap
	err := wait.PollImmediate(interval, timeout, func() (bool, error) {
		Logf("Waiting for ConfigMap %s to meet condition %q", name, condDesc)
		ConfigMap = c.Get(name).DeepCopy()
		met, err := cond(ConfigMap)
		if err != nil {
			return false, fmt.Errorf("failed to check condition for ConfigMap %s: %v", name, err)
		}
		return met, nil
	})
	if err == nil {
		return ConfigMap
	}
	if IsTimeout(err) {
		Failf("timed out while waiting for ConfigMap %s to meet condition %q", name, condDesc)
	}
	Fail(maybeTimeoutError(err, "waiting for ConfigMap %s to meet condition %q", name, condDesc).Error())
	return nil
}

// WaitToDisappear waits the given timeout duration for the specified ConfigMap to disappear.
func (c *ConfigMapClient) WaitToDisappear(name string, interval, timeout time.Duration) error {
	var lastConfigMap *corev1.ConfigMap
	err := wait.PollImmediate(interval, timeout, func() (bool, error) {
		Logf("Waiting for ConfigMap %s to disappear", name)
		ConfigMaps, err := c.List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return handleWaitingAPIError(err, true, "listing ConfigMaps")
		}
		found := false
		for i, ConfigMap := range ConfigMaps.Items {
			if ConfigMap.Name == name {
				Logf("ConfigMap %s still exists", name)
				found = true
				lastConfigMap = &(ConfigMaps.Items[i])
				break
			}
		}
		if !found {
			Logf("ConfigMap %s no longer exists", name)
			return true, nil
		}
		return false, nil
	})
	if err == nil {
		return nil
	}
	if IsTimeout(err) {
		return TimeoutError(fmt.Sprintf("timed out while waiting for ConfigMap %s to disappear", name),
			lastConfigMap,
		)
	}
	return maybeTimeoutError(err, "waiting for ConfigMap %s to disappear", name)
}

func MakeConfigMap(name, ns string, annotations, data map[string]string) *corev1.ConfigMap {
	ConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   ns,
			Annotations: annotations,
		},
		Data: data,
	}

	return ConfigMap
}
