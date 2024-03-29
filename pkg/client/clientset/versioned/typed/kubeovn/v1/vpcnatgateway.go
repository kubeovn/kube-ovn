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

package v1

import (
	"context"
	"time"

	v1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	scheme "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// VpcNatGatewaysGetter has a method to return a VpcNatGatewayInterface.
// A group's client should implement this interface.
type VpcNatGatewaysGetter interface {
	VpcNatGateways() VpcNatGatewayInterface
}

// VpcNatGatewayInterface has methods to work with VpcNatGateway resources.
type VpcNatGatewayInterface interface {
	Create(ctx context.Context, vpcNatGateway *v1.VpcNatGateway, opts metav1.CreateOptions) (*v1.VpcNatGateway, error)
	Update(ctx context.Context, vpcNatGateway *v1.VpcNatGateway, opts metav1.UpdateOptions) (*v1.VpcNatGateway, error)
	UpdateStatus(ctx context.Context, vpcNatGateway *v1.VpcNatGateway, opts metav1.UpdateOptions) (*v1.VpcNatGateway, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1.VpcNatGateway, error)
	List(ctx context.Context, opts metav1.ListOptions) (*v1.VpcNatGatewayList, error)
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.VpcNatGateway, err error)
	VpcNatGatewayExpansion
}

// vpcNatGateways implements VpcNatGatewayInterface
type vpcNatGateways struct {
	client rest.Interface
}

// newVpcNatGateways returns a VpcNatGateways
func newVpcNatGateways(c *KubeovnV1Client) *vpcNatGateways {
	return &vpcNatGateways{
		client: c.RESTClient(),
	}
}

// Get takes name of the vpcNatGateway, and returns the corresponding vpcNatGateway object, and an error if there is any.
func (c *vpcNatGateways) Get(ctx context.Context, name string, options metav1.GetOptions) (result *v1.VpcNatGateway, err error) {
	result = &v1.VpcNatGateway{}
	err = c.client.Get().
		Resource("vpc-nat-gateways").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of VpcNatGateways that match those selectors.
func (c *vpcNatGateways) List(ctx context.Context, opts metav1.ListOptions) (result *v1.VpcNatGatewayList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1.VpcNatGatewayList{}
	err = c.client.Get().
		Resource("vpc-nat-gateways").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested vpcNatGateways.
func (c *vpcNatGateways) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Resource("vpc-nat-gateways").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a vpcNatGateway and creates it.  Returns the server's representation of the vpcNatGateway, and an error, if there is any.
func (c *vpcNatGateways) Create(ctx context.Context, vpcNatGateway *v1.VpcNatGateway, opts metav1.CreateOptions) (result *v1.VpcNatGateway, err error) {
	result = &v1.VpcNatGateway{}
	err = c.client.Post().
		Resource("vpc-nat-gateways").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(vpcNatGateway).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a vpcNatGateway and updates it. Returns the server's representation of the vpcNatGateway, and an error, if there is any.
func (c *vpcNatGateways) Update(ctx context.Context, vpcNatGateway *v1.VpcNatGateway, opts metav1.UpdateOptions) (result *v1.VpcNatGateway, err error) {
	result = &v1.VpcNatGateway{}
	err = c.client.Put().
		Resource("vpc-nat-gateways").
		Name(vpcNatGateway.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(vpcNatGateway).
		Do(ctx).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *vpcNatGateways) UpdateStatus(ctx context.Context, vpcNatGateway *v1.VpcNatGateway, opts metav1.UpdateOptions) (result *v1.VpcNatGateway, err error) {
	result = &v1.VpcNatGateway{}
	err = c.client.Put().
		Resource("vpc-nat-gateways").
		Name(vpcNatGateway.Name).
		SubResource("status").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(vpcNatGateway).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the vpcNatGateway and deletes it. Returns an error if one occurs.
func (c *vpcNatGateways) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return c.client.Delete().
		Resource("vpc-nat-gateways").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *vpcNatGateways) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Resource("vpc-nat-gateways").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched vpcNatGateway.
func (c *vpcNatGateways) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.VpcNatGateway, err error) {
	result = &v1.VpcNatGateway{}
	err = c.client.Patch(pt).
		Resource("vpc-nat-gateways").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}
