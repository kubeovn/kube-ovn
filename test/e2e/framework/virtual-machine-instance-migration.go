package framework

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
)

// VMIMigrationClient represents a KubeVirt VirtualMachineInstanceMigration client
type VMIMigrationClient struct {
	f *Framework
	kubecli.VirtualMachineInstanceMigrationInterface
}

func (f *Framework) VMIMigrationClient() *VMIMigrationClient {
	return f.VMIMigrationClientNS(f.Namespace.Name)
}

func (f *Framework) VMIMigrationClientNS(namespace string) *VMIMigrationClient {
	return &VMIMigrationClient{
		f:                                        f,
		VirtualMachineInstanceMigrationInterface: f.KubeVirtClientSet.VirtualMachineInstanceMigration(namespace),
	}
}

func (c *VMIMigrationClient) Get(name string) *v1.VirtualMachineInstanceMigration {
	ginkgo.GinkgoHelper()
	m, err := c.VirtualMachineInstanceMigrationInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return m
}

// Create creates a new VirtualMachineInstanceMigration.
func (c *VMIMigrationClient) Create(migration *v1.VirtualMachineInstanceMigration) *v1.VirtualMachineInstanceMigration {
	ginkgo.GinkgoHelper()
	m, err := c.VirtualMachineInstanceMigrationInterface.Create(context.TODO(), migration, metav1.CreateOptions{})
	ExpectNoError(err, "failed to create migration %s", migration.Name)
	return c.Get(m.Name)
}

// Delete deletes a VirtualMachineInstanceMigration if it exists.
func (c *VMIMigrationClient) Delete(name string) {
	ginkgo.GinkgoHelper()
	err := c.VirtualMachineInstanceMigrationInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		Logf("migration %s not found, skip deleting", name)
		return
	}
	ExpectNoError(err, "failed to delete migration %s", name)
}

// DeleteSync deletes the migration and waits for it to disappear.
func (c *VMIMigrationClient) DeleteSync(name string) {
	ginkgo.GinkgoHelper()
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, poll, timeout)).To(gomega.Succeed(), "wait for migration %q to disappear", name)
}

// WaitForPhase waits until the migration reaches the specified phase.
func (c *VMIMigrationClient) WaitForPhase(name string, phase v1.VirtualMachineInstanceMigrationPhase, timeout time.Duration) error {
	err := k8sframework.Gomega().Eventually(context.TODO(), k8sframework.RetryNotFound(func(ctx context.Context) (*v1.VirtualMachineInstanceMigration, error) {
		return c.VirtualMachineInstanceMigrationInterface.Get(ctx, name, metav1.GetOptions{})
	})).WithTimeout(timeout).Should(
		k8sframework.MakeMatcher(func(m *v1.VirtualMachineInstanceMigration) (func() string, error) {
			if m.Status.Phase == phase {
				return nil, nil
			}
			return func() string {
				return fmt.Sprintf("expected migration phase %s, got %s instead:\n%s",
					phase, m.Status.Phase, format.Object(m.Status, 1))
			}, nil
		}))
	if err != nil {
		return fmt.Errorf("expected migration %s to reach phase %s: %w", name, phase, err)
	}
	return nil
}

// WaitToDisappear waits for the migration to be fully deleted.
func (c *VMIMigrationClient) WaitToDisappear(name string, poll, timeout time.Duration) error {
	err := k8sframework.Gomega().Eventually(context.Background(), k8sframework.HandleRetry(func(ctx context.Context) (*v1.VirtualMachineInstanceMigration, error) {
		m, err := c.VirtualMachineInstanceMigrationInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return m, err
	})).WithPolling(poll).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected migration %s to not be found: %w", name, err)
	}
	return nil
}

// MakeVMIMigration returns a VirtualMachineInstanceMigration object targeting the given VMI.
func MakeVMIMigration(name, vmiName string) *v1.VirtualMachineInstanceMigration {
	return &v1.VirtualMachineInstanceMigration{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.VirtualMachineInstanceMigrationSpec{
			VMIName: vmiName,
		},
	}
}
