package framework

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
)

// VMClient represents a KubeVirt VM client
type VMClient struct {
	f *Framework
	kubecli.VirtualMachineInterface
}

func (f *Framework) VMClient() *VMClient {
	return f.VMClientNS(f.Namespace.Name)
}

func (f *Framework) VMClientNS(namespace string) *VMClient {
	return &VMClient{
		f:                       f,
		VirtualMachineInterface: f.KubeVirtClientSet.VirtualMachine(namespace),
	}
}

func (c *VMClient) Get(ctx context.Context, name string) *v1.VirtualMachine {
	ginkgo.GinkgoHelper()
	vm, err := c.VirtualMachineInterface.Get(ctx, name, &metav1.GetOptions{})
	ExpectNoError(err)
	return vm
}

// Create creates a new vm according to the framework specifications
func (c *VMClient) Create(ctx context.Context, vm *v1.VirtualMachine) *v1.VirtualMachine {
	ginkgo.GinkgoHelper()
	v, err := c.VirtualMachineInterface.Create(ctx, vm)
	ExpectNoError(err, "failed to create vm %s", v.Name)
	return c.Get(ctx, v.Name)
}

// CreateSync creates a new vm according to the framework specifications, and waits for it to be ready.
func (c *VMClient) CreateSync(ctx context.Context, vm *v1.VirtualMachine) *v1.VirtualMachine {
	ginkgo.GinkgoHelper()

	v := c.Create(ctx, vm)
	ExpectNoError(c.WaitToBeReady(ctx, v.Name, timeout))
	// Get the newest vm after it becomes ready
	return c.Get(ctx, v.Name).DeepCopy()
}

// Start starts the vm.
func (c *VMClient) Start(ctx context.Context, name string) *v1.VirtualMachine {
	ginkgo.GinkgoHelper()

	vm := c.Get(ctx, name)
	if vm.Spec.Running != nil && *vm.Spec.Running {
		Logf("vm %s has already been started", name)
		return vm
	}

	running := true
	vm.Spec.Running = &running
	_, err := c.VirtualMachineInterface.Update(ctx, vm)
	ExpectNoError(err, "failed to update vm %s", name)
	return c.Get(ctx, name)
}

// StartSync stops the vm and waits for it to be ready.
func (c *VMClient) StartSync(ctx context.Context, name string) *v1.VirtualMachine {
	ginkgo.GinkgoHelper()
	_ = c.Start(ctx, name)
	ExpectNoError(c.WaitToBeReady(ctx, name, 2*time.Minute))
	return c.Get(ctx, name)
}

// Stop stops the vm.
func (c *VMClient) Stop(ctx context.Context, name string) *v1.VirtualMachine {
	ginkgo.GinkgoHelper()

	vm := c.Get(ctx, name)
	if vm.Spec.Running != nil && !*vm.Spec.Running {
		Logf("vm %s has already been stopped", name)
		return vm
	}

	running := false
	vm.Spec.Running = &running
	_, err := c.VirtualMachineInterface.Update(ctx, vm)
	ExpectNoError(err, "failed to update vm %s", name)
	return c.Get(ctx, name)
}

// StopSync stops the vm and waits for it to be stopped.
func (c *VMClient) StopSync(ctx context.Context, name string) *v1.VirtualMachine {
	ginkgo.GinkgoHelper()
	_ = c.Stop(ctx, name)
	ExpectNoError(c.WaitToBeStopped(ctx, name, 2*time.Minute))
	return c.Get(ctx, name)
}

// Delete deletes a vm if the vm exists
func (c *VMClient) Delete(ctx context.Context, name string) {
	ginkgo.GinkgoHelper()
	err := c.VirtualMachineInterface.Delete(ctx, name, &metav1.DeleteOptions{})
	ExpectNoError(err, "failed to delete vm %s", name)
}

// DeleteSync deletes the vm and waits for the vm to disappear for `timeout`.
// If the vm doesn't disappear before the timeout, it will fail the test.
func (c *VMClient) DeleteSync(ctx context.Context, name string) {
	ginkgo.GinkgoHelper()
	c.Delete(ctx, name)
	gomega.Expect(c.WaitToDisappear(ctx, name, timeout)).To(gomega.Succeed(), "wait for vm %q to disappear", name)
}

// WaitToDisappear waits the given timeout duration for the specified vm to be ready.
func (c *VMClient) WaitToBeReady(ctx context.Context, name string, timeout time.Duration) error {
	err := k8sframework.Gomega().Eventually(ctx, k8sframework.RetryNotFound(func(ctx context.Context) (*v1.VirtualMachine, error) {
		return c.VirtualMachineInterface.Get(ctx, name, &metav1.GetOptions{})
	})).WithTimeout(timeout).Should(
		k8sframework.MakeMatcher(func(vm *v1.VirtualMachine) (func() string, error) {
			if vm.Status.Ready {
				return nil, nil
			}
			return func() string {
				return fmt.Sprintf("expected vm status to be ready, got status instead:\n%s", format.Object(vm.Status, 1))
			}, nil
		}))
	if err != nil {
		return fmt.Errorf("expected vm %s to be ready: %w", name, err)
	}
	return nil
}

// WaitToDisappear waits the given timeout duration for the specified vm to be stopped.
func (c *VMClient) WaitToBeStopped(ctx context.Context, name string, timeout time.Duration) error {
	err := k8sframework.Gomega().Eventually(ctx, k8sframework.RetryNotFound(func(ctx context.Context) (*v1.VirtualMachine, error) {
		return c.VirtualMachineInterface.Get(ctx, name, &metav1.GetOptions{})
	})).WithTimeout(timeout).Should(
		k8sframework.MakeMatcher(func(vm *v1.VirtualMachine) (func() string, error) {
			if !vm.Status.Created {
				return nil, nil
			}
			return func() string {
				return fmt.Sprintf("expected vm status to be stopped, got status instead:\n%s", format.Object(vm.Status, 1))
			}, nil
		}))
	if err != nil {
		return fmt.Errorf("expected vm %s to be stopped: %w", name, err)
	}
	return nil
}

// WaitToDisappear waits the given timeout duration for the specified vm to disappear.
func (c *VMClient) WaitToDisappear(ctx context.Context, name string, timeout time.Duration) error {
	err := k8sframework.Gomega().Eventually(ctx, k8sframework.HandleRetry(func(ctx context.Context) (*v1.VirtualMachine, error) {
		vm, err := c.VirtualMachineInterface.Get(ctx, name, &metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return vm, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected vm %s to not be found: %w", name, err)
	}
	return nil
}

func MakeVM(name, image, size string, running bool) *v1.VirtualMachine {
	vm := &v1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.VirtualMachineSpec{
			Running: &running,
			Template: &v1.VirtualMachineInstanceTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"kubevirt.io/size":   size,
						"kubevirt.io/domain": name,
					},
				},
				Spec: v1.VirtualMachineInstanceSpec{
					Domain: v1.DomainSpec{
						Devices: v1.Devices{
							Disks: []v1.Disk{
								{
									Name: "containerdisk",
									DiskDevice: v1.DiskDevice{
										Disk: &v1.DiskTarget{
											Bus: v1.DiskBusVirtio,
										},
									},
								},
								{
									Name: "cloudinitdisk",
									DiskDevice: v1.DiskDevice{
										Disk: &v1.DiskTarget{
											Bus: v1.DiskBusVirtio,
										},
									},
								},
							},
							Interfaces: []v1.Interface{
								{
									Name:                   "default",
									InterfaceBindingMethod: v1.DefaultMasqueradeNetworkInterface().InterfaceBindingMethod,
								},
							},
						},
						Resources: v1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("64M"),
							},
						},
					},
					Networks: []v1.Network{
						{
							Name:          "default",
							NetworkSource: v1.DefaultPodNetwork().NetworkSource,
						},
					},
					Volumes: []v1.Volume{
						{
							Name: "containerdisk",
							VolumeSource: v1.VolumeSource{
								ContainerDisk: &v1.ContainerDiskSource{
									Image:           image,
									ImagePullPolicy: corev1.PullIfNotPresent,
								},
							},
						},
						{
							Name: "cloudinitdisk",
							VolumeSource: v1.VolumeSource{
								CloudInitNoCloud: &v1.CloudInitNoCloudSource{
									UserDataBase64: "SGkuXG4=",
								},
							},
						},
					},
				},
			},
		},
	}
	return vm
}
