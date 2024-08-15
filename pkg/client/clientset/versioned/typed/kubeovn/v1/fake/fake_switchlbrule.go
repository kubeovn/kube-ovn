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

// FakeSwitchLBRules implements SwitchLBRuleInterface
type FakeSwitchLBRules struct {
	Fake *FakeKubeovnV1
}

var switchlbrulesResource = v1.SchemeGroupVersion.WithResource("switch-lb-rules")

var switchlbrulesKind = v1.SchemeGroupVersion.WithKind("SwitchLBRule")

// Get takes name of the switchLBRule, and returns the corresponding switchLBRule object, and an error if there is any.
func (c *FakeSwitchLBRules) Get(ctx context.Context, name string, options metav1.GetOptions) (result *v1.SwitchLBRule, err error) {
	emptyResult := &v1.SwitchLBRule{}
	obj, err := c.Fake.
		Invokes(testing.NewRootGetActionWithOptions(switchlbrulesResource, name, options), emptyResult)
	if obj == nil {
		return emptyResult, err
	}
	return obj.(*v1.SwitchLBRule), err
}

// List takes label and field selectors, and returns the list of SwitchLBRules that match those selectors.
func (c *FakeSwitchLBRules) List(ctx context.Context, opts metav1.ListOptions) (result *v1.SwitchLBRuleList, err error) {
	emptyResult := &v1.SwitchLBRuleList{}
	obj, err := c.Fake.
		Invokes(testing.NewRootListActionWithOptions(switchlbrulesResource, switchlbrulesKind, opts), emptyResult)
	if obj == nil {
		return emptyResult, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1.SwitchLBRuleList{ListMeta: obj.(*v1.SwitchLBRuleList).ListMeta}
	for _, item := range obj.(*v1.SwitchLBRuleList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested switchLBRules.
func (c *FakeSwitchLBRules) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewRootWatchActionWithOptions(switchlbrulesResource, opts))
}

// Create takes the representation of a switchLBRule and creates it.  Returns the server's representation of the switchLBRule, and an error, if there is any.
func (c *FakeSwitchLBRules) Create(ctx context.Context, switchLBRule *v1.SwitchLBRule, opts metav1.CreateOptions) (result *v1.SwitchLBRule, err error) {
	emptyResult := &v1.SwitchLBRule{}
	obj, err := c.Fake.
		Invokes(testing.NewRootCreateActionWithOptions(switchlbrulesResource, switchLBRule, opts), emptyResult)
	if obj == nil {
		return emptyResult, err
	}
	return obj.(*v1.SwitchLBRule), err
}

// Update takes the representation of a switchLBRule and updates it. Returns the server's representation of the switchLBRule, and an error, if there is any.
func (c *FakeSwitchLBRules) Update(ctx context.Context, switchLBRule *v1.SwitchLBRule, opts metav1.UpdateOptions) (result *v1.SwitchLBRule, err error) {
	emptyResult := &v1.SwitchLBRule{}
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateActionWithOptions(switchlbrulesResource, switchLBRule, opts), emptyResult)
	if obj == nil {
		return emptyResult, err
	}
	return obj.(*v1.SwitchLBRule), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeSwitchLBRules) UpdateStatus(ctx context.Context, switchLBRule *v1.SwitchLBRule, opts metav1.UpdateOptions) (result *v1.SwitchLBRule, err error) {
	emptyResult := &v1.SwitchLBRule{}
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateSubresourceActionWithOptions(switchlbrulesResource, "status", switchLBRule, opts), emptyResult)
	if obj == nil {
		return emptyResult, err
	}
	return obj.(*v1.SwitchLBRule), err
}

// Delete takes name of the switchLBRule and deletes it. Returns an error if one occurs.
func (c *FakeSwitchLBRules) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewRootDeleteActionWithOptions(switchlbrulesResource, name, opts), &v1.SwitchLBRule{})
	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeSwitchLBRules) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	action := testing.NewRootDeleteCollectionActionWithOptions(switchlbrulesResource, opts, listOpts)

	_, err := c.Fake.Invokes(action, &v1.SwitchLBRuleList{})
	return err
}

// Patch applies the patch and returns the patched switchLBRule.
func (c *FakeSwitchLBRules) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.SwitchLBRule, err error) {
	emptyResult := &v1.SwitchLBRule{}
	obj, err := c.Fake.
		Invokes(testing.NewRootPatchSubresourceActionWithOptions(switchlbrulesResource, name, pt, data, opts, subresources...), emptyResult)
	if obj == nil {
		return emptyResult, err
	}
	return obj.(*v1.SwitchLBRule), err
}
