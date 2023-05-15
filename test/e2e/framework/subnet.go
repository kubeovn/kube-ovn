package framework

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/onsi/gomega"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	v1 "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/typed/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// SubnetClient is a struct for subnet client.
type SubnetClient struct {
	f *Framework
	v1.SubnetInterface
}

func (f *Framework) SubnetClient() *SubnetClient {
	return &SubnetClient{
		f:               f,
		SubnetInterface: f.KubeOVNClientSet.KubeovnV1().Subnets(),
	}
}

func (s *SubnetClient) Get(name string) *apiv1.Subnet {
	subnet, err := s.SubnetInterface.Get(context.TODO(), name, metav1.GetOptions{})
	ExpectNoError(err)
	return subnet
}

// Create creates a new subnet according to the framework specifications
func (c *SubnetClient) Create(subnet *apiv1.Subnet) *apiv1.Subnet {
	s, err := c.SubnetInterface.Create(context.TODO(), subnet, metav1.CreateOptions{})
	ExpectNoError(err, "Error creating subnet")
	return s.DeepCopy()
}

// CreateSync creates a new subnet according to the framework specifications, and waits for it to be ready.
func (c *SubnetClient) CreateSync(subnet *apiv1.Subnet) *apiv1.Subnet {
	s := c.Create(subnet)
	ExpectTrue(c.WaitToBeReady(s.Name, timeout))
	// Get the newest subnet after it becomes ready
	return c.Get(s.Name).DeepCopy()
}

// Update updates the subnet
func (c *SubnetClient) Update(subnet *apiv1.Subnet, options metav1.UpdateOptions, timeout time.Duration) *apiv1.Subnet {
	var updatedSubnet *apiv1.Subnet
	err := wait.PollImmediate(2*time.Second, timeout, func() (bool, error) {
		s, err := c.SubnetInterface.Update(context.TODO(), subnet, options)
		if err != nil {
			return handleWaitingAPIError(err, false, "update subnet %q", subnet.Name)
		}
		updatedSubnet = s
		return true, nil
	})
	if err == nil {
		return updatedSubnet.DeepCopy()
	}

	if IsTimeout(err) {
		Failf("timed out while retrying to update subnet %s", subnet.Name)
	}
	ExpectNoError(maybeTimeoutError(err, "updating subnet %s", subnet.Name))

	return nil
}

// UpdateSync updates the subnet and waits for the subnet to be ready for `timeout`.
// If the subnet doesn't become ready before the timeout, it will fail the test.
func (c *SubnetClient) UpdateSync(subnet *apiv1.Subnet, options metav1.UpdateOptions, timeout time.Duration) *apiv1.Subnet {
	s := c.Update(subnet, options, timeout)
	ExpectTrue(c.WaitToBeUpdated(s, timeout))
	ExpectTrue(c.WaitToBeReady(s.Name, timeout))
	// Get the newest subnet after it becomes ready
	return c.Get(s.Name).DeepCopy()
}

// Patch patches the subnet
func (c *SubnetClient) Patch(original, modified *apiv1.Subnet, timeout time.Duration) *apiv1.Subnet {
	patch, err := util.GenerateMergePatchPayload(original, modified)
	ExpectNoError(err)

	var patchedSubnet *apiv1.Subnet
	err = wait.PollImmediate(2*time.Second, timeout, func() (bool, error) {
		s, err := c.SubnetInterface.Patch(context.TODO(), original.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "")
		if err != nil {
			return handleWaitingAPIError(err, false, "patch subnet %q", original.Name)
		}
		patchedSubnet = s
		return true, nil
	})
	if err == nil {
		return patchedSubnet.DeepCopy()
	}

	if IsTimeout(err) {
		Failf("timed out while retrying to patch subnet %s", original.Name)
	}
	ExpectNoError(maybeTimeoutError(err, "patching subnet %s", original.Name))

	return nil
}

// PatchSync patches the subnet and waits for the subnet to be ready for `timeout`.
// If the subnet doesn't become ready before the timeout, it will fail the test.
func (c *SubnetClient) PatchSync(original, modified *apiv1.Subnet) *apiv1.Subnet {
	s := c.Patch(original, modified, timeout)
	ExpectTrue(c.WaitToBeUpdated(s, timeout))
	ExpectTrue(c.WaitToBeReady(s.Name, timeout))
	// Get the newest subnet after it becomes ready
	return c.Get(s.Name).DeepCopy()
}

// Delete deletes a subnet if the subnet exists
func (c *SubnetClient) Delete(name string) {
	err := c.SubnetInterface.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		Failf("Failed to delete subnet %q: %v", name, err)
	}
}

// DeleteSync deletes the subnet and waits for the subnet to disappear for `timeout`.
// If the subnet doesn't disappear before the timeout, it will fail the test.
func (c *SubnetClient) DeleteSync(name string) {
	c.Delete(name)
	gomega.Expect(c.WaitToDisappear(name, 2*time.Second, timeout)).To(gomega.Succeed(), "wait for subnet %q to disappear", name)
}

func isSubnetConditionSetAsExpected(subnet *apiv1.Subnet, conditionType apiv1.ConditionType, wantTrue, silent bool) bool {
	for _, cond := range subnet.Status.Conditions {
		if cond.Type == conditionType {
			if (wantTrue && (cond.Status == corev1.ConditionTrue)) || (!wantTrue && (cond.Status != corev1.ConditionTrue)) {
				return true
			}
			if !silent {
				Logf("Condition %s of subnet %s is %v instead of %t. Reason: %v, message: %v",
					conditionType, subnet.Name, cond.Status == corev1.ConditionTrue, wantTrue, cond.Reason, cond.Message)
			}
			return false
		}
	}
	if !silent {
		Logf("Couldn't find condition %v on subnet %v", conditionType, subnet.Name)
	}
	return false
}

// IsSubnetConditionSetAsExpected returns a wantTrue value if the subnet has a match to the conditionType,
// otherwise returns an opposite value of the wantTrue with detailed logging.
func IsSubnetConditionSetAsExpected(subnet *apiv1.Subnet, conditionType apiv1.ConditionType, wantTrue bool) bool {
	return isSubnetConditionSetAsExpected(subnet, conditionType, wantTrue, false)
}

// WaitConditionToBe returns whether subnet "name's" condition state matches wantTrue
// within timeout. If wantTrue is true, it will ensure the subnet condition status is
// ConditionTrue; if it's false, it ensures the subnet condition is in any state other
// than ConditionTrue (e.g. not true or unknown).
func (c *SubnetClient) WaitConditionToBe(name string, conditionType apiv1.ConditionType, wantTrue bool, timeout time.Duration) bool {
	Logf("Waiting up to %v for subnet %s condition %s to be %t", timeout, name, conditionType, wantTrue)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		subnet := c.Get(name)
		if IsSubnetConditionSetAsExpected(subnet, conditionType, wantTrue) {
			Logf("Subnet %s reach desired %t condition status", name, wantTrue)
			return true
		}
		Logf("Subnet %s still not reach desired %t condition status", name, wantTrue)
	}
	Logf("Subnet %s didn't reach desired %s condition status (%t) within %v", name, conditionType, wantTrue, timeout)
	return false
}

// WaitToBeReady returns whether the subnet is ready within timeout.
func (c *SubnetClient) WaitToBeReady(name string, timeout time.Duration) bool {
	return c.WaitConditionToBe(name, apiv1.Ready, true, timeout)
}

// WaitToBeUpdated returns whether the subnet is updated within timeout.
func (c *SubnetClient) WaitToBeUpdated(subnet *apiv1.Subnet, timeout time.Duration) bool {
	Logf("Waiting up to %v for subnet %s to be updated", timeout, subnet.Name)
	rv, _ := big.NewInt(0).SetString(subnet.ResourceVersion, 10)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(poll) {
		s := c.Get(subnet.Name)
		if current, _ := big.NewInt(0).SetString(s.ResourceVersion, 10); current.Cmp(rv) > 0 {
			Logf("Subnet %s updated", subnet.Name)
			return true
		}
		Logf("Subnet %s still not updated", subnet.Name)
	}
	Logf("Subnet %s was not updated within %v", subnet.Name, timeout)
	return false
}

// WaitUntil waits the given timeout duration for the specified condition to be met.
func (c *SubnetClient) WaitUntil(name string, cond func(s *apiv1.Subnet) (bool, error), condDesc string, interval, timeout time.Duration) *apiv1.Subnet {
	var subnet *apiv1.Subnet
	err := wait.PollImmediate(interval, timeout, func() (bool, error) {
		Logf("Waiting for subnet %s to meet condition %q", name, condDesc)
		subnet = c.Get(name).DeepCopy()
		met, err := cond(subnet)
		if err != nil {
			return false, fmt.Errorf("failed to check condition for subnet %s: %v", name, err)
		}
		return met, nil
	})
	if err == nil {
		return subnet
	}
	if IsTimeout(err) {
		Failf("timed out while waiting for subnet %s to meet condition %q", name, condDesc)
	}
	Fail(maybeTimeoutError(err, "waiting for subnet %s to meet condition %q", name, condDesc).Error())
	return nil
}

// WaitToDisappear waits the given timeout duration for the specified subnet to disappear.
func (c *SubnetClient) WaitToDisappear(name string, interval, timeout time.Duration) error {
	var lastSubnet *apiv1.Subnet
	err := wait.PollImmediate(interval, timeout, func() (bool, error) {
		Logf("Waiting for subnet %s to disappear", name)
		subnets, err := c.List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return handleWaitingAPIError(err, true, "listing subnets")
		}
		found := false
		for i, subnet := range subnets.Items {
			if subnet.Name == name {
				Logf("Subnet %s still exists", name)
				found = true
				lastSubnet = &(subnets.Items[i])
				break
			}
		}
		if !found {
			Logf("Subnet %s no longer exists", name)
			return true, nil
		}
		return false, nil
	})
	if err == nil {
		return nil
	}
	if IsTimeout(err) {
		return TimeoutError(fmt.Sprintf("timed out while waiting for subnet %s to disappear", name),
			lastSubnet,
		)
	}
	return maybeTimeoutError(err, "waiting for subnet %s to disappear", name)
}

func MakeSubnet(name, vlan, cidr, gateway, vpc, provider string, excludeIPs, gatewayNodes, namespaces []string) *apiv1.Subnet {
	subnet := &apiv1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.SubnetSpec{
			Vpc:         vpc,
			Vlan:        vlan,
			CIDRBlock:   cidr,
			Gateway:     gateway,
			Protocol:    util.CheckProtocol(cidr),
			Provider:    provider,
			ExcludeIps:  excludeIPs,
			GatewayNode: strings.Join(gatewayNodes, ","),
			Namespaces:  namespaces,
		},
	}
	if provider == "" || strings.HasSuffix(provider, util.OvnProvider) {
		if len(gatewayNodes) != 0 {
			subnet.Spec.GatewayType = apiv1.GWCentralizedType
		} else {
			subnet.Spec.GatewayType = apiv1.GWDistributedType
		}
	}
	return subnet
}
