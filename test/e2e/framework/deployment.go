package framework

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	v1apps "k8s.io/client-go/kubernetes/typed/apps/v1"
	"k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/deployment"

	"github.com/onsi/gomega"
)

type DeploymentClient struct {
	f *Framework
	v1apps.DeploymentInterface
}

func (f *Framework) DeploymentClient() *DeploymentClient {
	return f.DeploymentClientNS(f.Namespace.Name)
}

func (f *Framework) DeploymentClientNS(namespace string) *DeploymentClient {
	return &DeploymentClient{
		f:                   f,
		DeploymentInterface: f.ClientSet.AppsV1().Deployments(namespace),
	}
}

func (c *DeploymentClient) Get(name string) *appsv1.Deployment {
	deploy, err := c.DeploymentInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return deploy
}

func (c *DeploymentClient) GetPods(deploy *appsv1.Deployment) (*corev1.PodList, error) {
	return deployment.GetPodsForDeployment(c.f.ClientSet, deploy)
}

// Create creates a new deployment according to the framework specifications
func (c *DeploymentClient) Create(deploy *appsv1.Deployment) *appsv1.Deployment {
	d, err := c.DeploymentInterface.Create(context.TODO(), deploy, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating deployment")
	return d.DeepCopy()
}

// CreateSync creates a new deployment according to the framework specifications, and waits for it to complete.
func (c *DeploymentClient) CreateSync(deploy *appsv1.Deployment) *appsv1.Deployment {
	d := c.Create(deploy)
	err := c.WaitToComplete(d)
	framework.ExpectNoError(err, "deployment failed to complete")
	// Get the newest deployment
	return c.Get(d.Name).DeepCopy()
}

// Delete deletes a deployment if the deployment exists
func (c *DeploymentClient) Delete(name string) {
	err := c.DeploymentInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete deployment %q: %v", name, err)
	}
}

// DeleteSync deletes the deployment and waits for the deployment to disappear for `timeout`.
// If the deployment doesn't disappear before the timeout, it will fail the test.
func (c *DeploymentClient) DeleteSync(name string) {
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for deployment %q to disappear", name)
}

func (c *DeploymentClient) WaitToComplete(deploy *appsv1.Deployment) error {
	return deployment.WaitForDeploymentComplete(c.f.ClientSet, deploy)
}

// WaitToDisappear waits the given timeout duration for the specified deployment to disappear.
func (c *DeploymentClient) WaitToDisappear(name string, interval, timeout time.Duration) error {
	var lastDeployment *appsv1.Deployment
	err := wait.PollImmediate(interval, timeout, func() (bool, error) {
		Logf("Waiting for deployment %s to disappear", name)
		deployments, err := c.List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return handleWaitingAPIError(err, true, "listing deployments")
		}
		found := false
		for i, deploy := range deployments.Items {
			if deploy.Name == name {
				Logf("Deployment %s still exists", name)
				found = true
				lastDeployment = &(deployments.Items[i])
				break
			}
		}
		if !found {
			Logf("Deployment %s no longer exists", name)
			return true, nil
		}
		return false, nil
	})
	if err == nil {
		return nil
	}
	if IsTimeout(err) {
		return TimeoutError(fmt.Sprintf("timed out while waiting for deployment %s to disappear", name),
			lastDeployment,
		)
	}
	return maybeTimeoutError(err, "waiting for deployment %s to disappear", name)
}

func MakeDeployment(name string, replicas int32, podLabels, podAnnotations map[string]string, containerName, image string, strategyType appsv1.DeploymentStrategyType) *appsv1.Deployment {
	deploy := deployment.NewDeployment(name, replicas, podLabels, containerName, image, strategyType)
	deploy.Spec.Template.Annotations = podAnnotations
	return deploy
}

func RestartSystemDeployment(name string) {
	restartCmd := fmt.Sprintf("kubectl rollout restart deployment %s -n kube-system", name)
	_, err := exec.Command("bash", "-c", restartCmd).CombinedOutput()
	framework.ExpectNoError(err, fmt.Sprintf("restart %s failed", name))
}
