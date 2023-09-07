package framework

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	v1apps "k8s.io/client-go/kubernetes/typed/apps/v1"
	"k8s.io/kubectl/pkg/polymorphichelpers"
	"k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/deployment"

	"github.com/onsi/gomega"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

type DeploymentClient struct {
	f *Framework
	v1apps.DeploymentInterface
	namespace string
}

func (f *Framework) DeploymentClient() *DeploymentClient {
	return f.DeploymentClientNS(f.Namespace.Name)
}

func (f *Framework) DeploymentClientNS(namespace string) *DeploymentClient {
	return &DeploymentClient{
		f:                   f,
		DeploymentInterface: f.ClientSet.AppsV1().Deployments(namespace),
		namespace:           namespace,
	}
}

func (c *DeploymentClient) Get(name string) *appsv1.Deployment {
	deploy, err := c.DeploymentInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return deploy
}

func (c *DeploymentClient) GetPods(deploy *appsv1.Deployment) (*corev1.PodList, error) {
	return deployment.GetPodsForDeployment(context.Background(), c.f.ClientSet, deploy)
}

func (c *DeploymentClient) GetAllPods(deploy *appsv1.Deployment) (*corev1.PodList, error) {
	podSelector, err := metav1.LabelSelectorAsSelector(deploy.Spec.Selector)
	if err != nil {
		return nil, err
	}
	podListOptions := metav1.ListOptions{LabelSelector: podSelector.String()}
	return c.f.ClientSet.CoreV1().Pods(deploy.Namespace).List(context.TODO(), podListOptions)
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
	ExpectNoError(err, "deployment failed to complete")
	// Get the newest deployment
	return c.Get(d.Name).DeepCopy()
}

func (c *DeploymentClient) RolloutStatus(name string) *appsv1.Deployment {
	var deploy *appsv1.Deployment
	WaitUntil(2*time.Second, timeout, func(_ context.Context) (bool, error) {
		var err error
		deploy = c.Get(name)
		unstructured := &unstructured.Unstructured{}
		if unstructured.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(deploy); err != nil {
			return false, err
		}

		dsv := &polymorphichelpers.DeploymentStatusViewer{}
		msg, done, err := dsv.Status(unstructured, 0)
		if err != nil {
			return false, err
		}
		if done {
			return true, nil
		}

		Logf(strings.TrimSpace(msg))
		return false, nil
	}, "")

	return deploy
}

func (c *DeploymentClient) Patch(original, modified *appsv1.Deployment) *appsv1.Deployment {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedDeploy *appsv1.Deployment
	err = wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		deploy, err := c.DeploymentInterface.Patch(ctx, original.Name, types.StrategicMergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch deployment %s/%s", original.Namespace, original.Name)
		}
		patchedDeploy = deploy
		return true, nil
	})
	if err == nil {
		return patchedDeploy.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch deployment %s/%s", original.Namespace, original.Name)
	}
	Failf("error occurred while retrying to patch deployment %s/%s: %v", original.Namespace, original.Name, err)

	return nil
}

func (c *DeploymentClient) PatchSync(original, modified *appsv1.Deployment) *appsv1.Deployment {
	deploy := c.Patch(original, modified)
	return c.RolloutStatus(deploy.Name)
}

// Restart restarts the deployment as kubectl does
func (c *DeploymentClient) Restart(deploy *appsv1.Deployment) *appsv1.Deployment {
	buf, err := polymorphichelpers.ObjectRestarterFn(deploy)
	ExpectNoError(err)

	m := make(map[string]interface{})
	err = json.Unmarshal(buf, &m)
	ExpectNoError(err)

	deploy = new(appsv1.Deployment)
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(m, deploy)
	ExpectNoError(err)

	deploy, err = c.DeploymentInterface.Update(context.TODO(), deploy, metav1.UpdateOptions{})
	ExpectNoError(err)

	return deploy.DeepCopy()
}

// RestartSync restarts the deployment and wait it to be ready
func (c *DeploymentClient) RestartSync(deploy *appsv1.Deployment) *appsv1.Deployment {
	_ = c.Restart(deploy)
	return c.RolloutStatus(deploy.Name)
}

func (c *DeploymentClient) SetScale(deployment string, replicas int32) {
	scale, err := c.GetScale(context.Background(), deployment, metav1.GetOptions{})
	framework.ExpectNoError(err)
	if scale.Spec.Replicas == replicas {
		Logf("repliacs of deployment %s/%s has already been set to %d", c.namespace, deployment, replicas)
		return
	}

	scale.Spec.Replicas = replicas
	_, err = c.UpdateScale(context.Background(), deployment, scale, metav1.UpdateOptions{})
	framework.ExpectNoError(err)
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
func (c *DeploymentClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*appsv1.Deployment, error) {
		deploy, err := c.DeploymentInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return deploy, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected deployment %s to not be found: %w", name, err)
	}
	return nil
}

func MakeDeployment(name string, replicas int32, podLabels, podAnnotations map[string]string, containerName, image string, strategyType appsv1.DeploymentStrategyType) *appsv1.Deployment {
	deploy := deployment.NewDeployment(name, replicas, podLabels, containerName, image, strategyType)
	deploy.Spec.Template.Annotations = podAnnotations
	return deploy
}
