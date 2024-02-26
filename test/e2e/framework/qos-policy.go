package framework

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/kubernetes/test/e2e/framework"

	"github.com/onsi/gomega"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	v1 "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/typed/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// QoSPolicyClient is a struct for qosPolicy client.
type QoSPolicyClient struct {
	f *Framework
	v1.QoSPolicyInterface
}

func (f *Framework) QoSPolicyClient() *QoSPolicyClient {
	return &QoSPolicyClient{
		f:                  f,
		QoSPolicyInterface: f.KubeOVNClientSet.KubeovnV1().QoSPolicies(),
	}
}

func (c *QoSPolicyClient) Get(name string) *apiv1.QoSPolicy {
	qosPolicy, err := c.QoSPolicyInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return qosPolicy
}

// Create creates a new qosPolicy according to the framework specifications
func (c *QoSPolicyClient) Create(qosPolicy *apiv1.QoSPolicy) *apiv1.QoSPolicy {
	s, err := c.QoSPolicyInterface.Create(context.TODO(), qosPolicy, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating qosPolicy")
	return s.DeepCopy()
}

// CreateSync creates a new qosPolicy according to the framework specifications, and waits for it to be ready.
func (c *QoSPolicyClient) CreateSync(qosPolicy *apiv1.QoSPolicy) *apiv1.QoSPolicy {
	s := c.Create(qosPolicy)
	ExpectTrue(c.WaitToQoSReady(s.Name))
	// Get the newest qosPolicy after it becomes ready
	return c.Get(s.Name).DeepCopy()
}

// Update updates the qosPolicy
func (c *QoSPolicyClient) Update(qosPolicy *apiv1.QoSPolicy, options metav1.UpdateOptions, timeout time.Duration) *apiv1.QoSPolicy {
	var updatedQoSPolicy *apiv1.QoSPolicy
	err := wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		s, err := c.QoSPolicyInterface.Update(ctx, qosPolicy, options)
		if err != nil {
			return handleWaitingAPIError(err, false, "update qosPolicy %q", qosPolicy.Name)
		}
		updatedQoSPolicy = s
		return true, nil
	})
	if err == nil {
		return updatedQoSPolicy.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to update qosPolicy %s", qosPolicy.Name)
	}
	Failf("error occurred while retrying to update qosPolicy %s: %v", qosPolicy.Name, err)

	return nil
}

// UpdateSync updates the qosPolicy and waits for the qosPolicy to be ready for `timeout`.
// If the qosPolicy doesn't become ready before the timeout, it will fail the test.
func (c *QoSPolicyClient) UpdateSync(qosPolicy *apiv1.QoSPolicy, options metav1.UpdateOptions, timeout time.Duration) *apiv1.QoSPolicy {
	s := c.Update(qosPolicy, options, timeout)
	ExpectTrue(c.WaitToBeUpdated(s, timeout))
	ExpectTrue(c.WaitToBeReady(s.Name, timeout))
	// Get the newest qosPolicy after it becomes ready
	return c.Get(s.Name).DeepCopy()
}

// Patch patches the qosPolicy
func (c *QoSPolicyClient) Patch(original, modified *apiv1.QoSPolicy) *apiv1.QoSPolicy {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedQoSPolicy *apiv1.QoSPolicy
	err = wait.PollUntilContextTimeout(context.Background(), 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		s, err := c.QoSPolicyInterface.Patch(ctx, original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch qosPolicy %q", original.Name)
		}
		patchedQoSPolicy = s
		return true, nil
	})
	if err == nil {
		return patchedQoSPolicy.DeepCopy()
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while retrying to patch qosPolicy %s", original.Name)
	}
	Failf("error occurred while retrying to patch qosPolicy %s: %v", original.Name, err)

	return nil
}

// PatchSync patches the qosPolicy and waits for the qosPolicy to be ready for `timeout`.
// If the qosPolicy doesn't become ready before the timeout, it will fail the test.
func (c *QoSPolicyClient) PatchSync(original, modified *apiv1.QoSPolicy) *apiv1.QoSPolicy {
	s := c.Patch(original, modified)
	ExpectTrue(c.WaitToBeUpdated(s, timeout))
	ExpectTrue(c.WaitToBeReady(s.Name, timeout))
	// Get the newest qosPolicy after it becomes ready
	return c.Get(s.Name).DeepCopy()
}

// Delete deletes a qosPolicy if the qosPolicy exists
func (c *QoSPolicyClient) Delete(name string) {
	err := c.QoSPolicyInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete qosPolicy %q: %v", name, err)
	}
}

// DeleteSync deletes the qosPolicy and waits for the qosPolicy to disappear for `timeout`.
// If the qosPolicy doesn't disappear before the timeout, it will fail the test.
func (c *QoSPolicyClient) DeleteSync(name string) {
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for qosPolicy %q to disappear", name)
}

func isQoSPolicyConditionSetAsExpected(qosPolicy *apiv1.QoSPolicy, conditionType apiv1.ConditionType, wantTrue, silent bool) bool {
	for _, cond := range qosPolicy.Status.Conditions {
		if cond.Type == conditionType {
			if (wantTrue && (cond.Status == corev1.ConditionTrue)) || (!wantTrue && (cond.Status != corev1.ConditionTrue)) {
				return true
			}
			if !silent {
				Logf("Condition %s of qosPolicy %s is %v instead of %t. Reason: %v, message: %v",
					conditionType, qosPolicy.Name, cond.Status == corev1.ConditionTrue, wantTrue, cond.Reason, cond.Message)
			}
			return false
		}
	}
	if !silent {
		Logf("Couldn't find condition %v on qosPolicy %v", conditionType, qosPolicy.Name)
	}
	return false
}

// IsQoSPolicyConditionSetAsExpected returns a wantTrue value if the qosPolicy has a match to the conditionType,
// otherwise returns an opposite value of the wantTrue with detailed logging.
func IsQoSPolicyConditionSetAsExpected(qosPolicy *apiv1.QoSPolicy, conditionType apiv1.ConditionType, wantTrue bool) bool {
	return isQoSPolicyConditionSetAsExpected(qosPolicy, conditionType, wantTrue, false)
}

// WaitConditionToBe returns whether qosPolicy "name's" condition state matches wantTrue
// within timeout. If wantTrue is true, it will ensure the qosPolicy condition status is
// ConditionTrue; if it's false, it ensures the qosPolicy condition is in any state other
// than ConditionTrue (e.g. not true or unknown).
func (c *QoSPolicyClient) WaitConditionToBe(name string, conditionType apiv1.ConditionType, wantTrue bool, timeout time.Duration) bool {
	Logf("Waiting up to %v for qosPolicy %s condition %s to be %t", timeout, name, conditionType, wantTrue)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		qosPolicy := c.Get(name)
		if IsQoSPolicyConditionSetAsExpected(qosPolicy, conditionType, wantTrue) {
			Logf("QoSPolicy %s reach desired %t condition status", name, wantTrue)
			return true
		}
		Logf("QoSPolicy %s still not reach desired %t condition status", name, wantTrue)
	}
	Logf("QoSPolicy %s didn't reach desired %s condition status (%t) within %v", name, conditionType, wantTrue, timeout)
	return false
}

// WaitToBeReady returns whether the qosPolicy is ready within timeout.
func (c *QoSPolicyClient) WaitToBeReady(name string, timeout time.Duration) bool {
	return c.WaitConditionToBe(name, apiv1.Ready, true, timeout)
}

// WaitToBeUpdated returns whether the qosPolicy is updated within timeout.
func (c *QoSPolicyClient) WaitToBeUpdated(qosPolicy *apiv1.QoSPolicy, timeout time.Duration) bool {
	Logf("Waiting up to %v for qosPolicy %s to be updated", timeout, qosPolicy.Name)
	rv, _ := big.NewInt(0).SetString(qosPolicy.ResourceVersion, 10)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		s := c.Get(qosPolicy.Name)
		if current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10); current.Cmp(rv) > 0 {
			Logf("QoSPolicy %s updated", qosPolicy.Name)
			return true
		}
		Logf("QoSPolicy %s still not updated", qosPolicy.Name)
	}
	Logf("QoSPolicy %s was not updated within %v", qosPolicy.Name, timeout)
	return false
}

// WaitUntil waits the given timeout duration for the specified condition to be met.
func (c *QoSPolicyClient) WaitUntil(name string, cond func(s *apiv1.QoSPolicy) (bool, error), condDesc string, interval, timeout time.Duration) *apiv1.QoSPolicy {
	var qosPolicy *apiv1.QoSPolicy
	err := wait.PollUntilContextTimeout(context.Background(), interval, timeout, true, func(_ context.Context) (bool, error) {
		Logf("Waiting for qosPolicy %s to meet condition %q", name, condDesc)
		qosPolicy = c.Get(name).DeepCopy()
		met, err := cond(qosPolicy)
		if err != nil {
			return false, fmt.Errorf("failed to check condition for qosPolicy %s: %v", name, err)
		}
		return met, nil
	})
	if err == nil {
		return qosPolicy
	}

	if errors.Is(err, context.DeadlineExceeded) {
		Failf("timed out while waiting for qosPolicy %s to meet condition %q", name, condDesc)
	}
	Failf("error occurred while waiting for qosPolicy %s to meet condition %q: %v", name, condDesc, err)

	return nil
}

// WaitToDisappear waits the given timeout duration for the specified qosPolicy to disappear.
func (c *QoSPolicyClient) WaitToDisappear(name string, _, timeout time.Duration) error {
	err := framework.Gomega().Eventually(context.Background(), framework.HandleRetry(func(ctx context.Context) (*apiv1.QoSPolicy, error) {
		qosPolicy, err := c.QoSPolicyInterface.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return qosPolicy, err
	})).WithTimeout(timeout).Should(gomega.BeNil())
	if err != nil {
		return fmt.Errorf("expected qosPolicy %s to not be found: %w", name, err)
	}
	return nil
}

// WaitToQoSReady returns whether the qos is ready within timeout.
func (c *QoSPolicyClient) WaitToQoSReady(name string) bool {
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		qos := c.Get(name)
		if len(qos.Spec.BandwidthLimitRules) != len(qos.Status.BandwidthLimitRules) {
			Logf("qos %s is not ready", name)
			continue
		}
		sort.Slice(qos.Spec.BandwidthLimitRules, func(i, j int) bool {
			return qos.Spec.BandwidthLimitRules[i].Name < qos.Spec.BandwidthLimitRules[j].Name
		})
		sort.Slice(qos.Status.BandwidthLimitRules, func(i, j int) bool {
			return qos.Status.BandwidthLimitRules[i].Name < qos.Status.BandwidthLimitRules[j].Name
		})
		equalCount := 0
		for index, specRule := range qos.Spec.BandwidthLimitRules {
			statusRule := qos.Status.BandwidthLimitRules[index]
			if reflect.DeepEqual(specRule, statusRule) {
				equalCount++
			}
		}

		if equalCount == len(qos.Spec.BandwidthLimitRules) {
			Logf("qos %s is ready", name)
			return true
		}
		Logf("qos %s is not ready", name)
	}
	return false
}

func MakeQoSPolicy(name string, shared bool, qosType apiv1.QoSPolicyBindingType, rules apiv1.QoSPolicyBandwidthLimitRules) *apiv1.QoSPolicy {
	qosPolicy := &apiv1.QoSPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.QoSPolicySpec{
			BandwidthLimitRules: rules,
			Shared:              shared,
			BindingType:         qosType,
		},
	}
	return qosPolicy
}
