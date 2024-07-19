/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	"context"

	v1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeQoSPolicies implements QoSPolicyInterface
type FakeQoSPolicies struct {
	Fake *FakeKubeovnV1
}

var qospoliciesResource = v1.SchemeGroupVersion.WithResource("qos-policies")

var qospoliciesKind = v1.SchemeGroupVersion.WithKind("QoSPolicy")

// Get takes name of the qoSPolicy, and returns the corresponding qoSPolicy object, and an error if there is any.
func (c *FakeQoSPolicies) Get(ctx context.Context, name string, options metav1.GetOptions) (result *v1.QoSPolicy, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootGetAction(qospoliciesResource, name), &v1.QoSPolicy{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1.QoSPolicy), err
}

// List takes label and field selectors, and returns the list of QoSPolicies that match those selectors.
func (c *FakeQoSPolicies) List(ctx context.Context, opts metav1.ListOptions) (result *v1.QoSPolicyList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootListAction(qospoliciesResource, qospoliciesKind, opts), &v1.QoSPolicyList{})
	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1.QoSPolicyList{ListMeta: obj.(*v1.QoSPolicyList).ListMeta}
	for _, item := range obj.(*v1.QoSPolicyList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested qoSPolicies.
func (c *FakeQoSPolicies) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewRootWatchAction(qospoliciesResource, opts))
}

// Create takes the representation of a qoSPolicy and creates it.  Returns the server's representation of the qoSPolicy, and an error, if there is any.
func (c *FakeQoSPolicies) Create(ctx context.Context, qoSPolicy *v1.QoSPolicy, opts metav1.CreateOptions) (result *v1.QoSPolicy, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootCreateAction(qospoliciesResource, qoSPolicy), &v1.QoSPolicy{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1.QoSPolicy), err
}

// Update takes the representation of a qoSPolicy and updates it. Returns the server's representation of the qoSPolicy, and an error, if there is any.
func (c *FakeQoSPolicies) Update(ctx context.Context, qoSPolicy *v1.QoSPolicy, opts metav1.UpdateOptions) (result *v1.QoSPolicy, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateAction(qospoliciesResource, qoSPolicy), &v1.QoSPolicy{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1.QoSPolicy), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeQoSPolicies) UpdateStatus(ctx context.Context, qoSPolicy *v1.QoSPolicy, opts metav1.UpdateOptions) (*v1.QoSPolicy, error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateSubresourceAction(qospoliciesResource, "status", qoSPolicy), &v1.QoSPolicy{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1.QoSPolicy), err
}

// Delete takes name of the qoSPolicy and deletes it. Returns an error if one occurs.
func (c *FakeQoSPolicies) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewRootDeleteActionWithOptions(qospoliciesResource, name, opts), &v1.QoSPolicy{})
	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeQoSPolicies) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	action := testing.NewRootDeleteCollectionAction(qospoliciesResource, listOpts)

	_, err := c.Fake.Invokes(action, &v1.QoSPolicyList{})
	return err
}

// Patch applies the patch and returns the patched qoSPolicy.
func (c *FakeQoSPolicies) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.QoSPolicy, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootPatchSubresourceAction(qospoliciesResource, name, pt, data, subresources...), &v1.QoSPolicy{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1.QoSPolicy), err
}
