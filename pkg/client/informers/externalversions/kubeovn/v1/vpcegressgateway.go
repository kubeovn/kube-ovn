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

// Code generated by informer-gen. DO NOT EDIT.

package v1

import (
	"context"
	time "time"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	versioned "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned"
	internalinterfaces "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions/internalinterfaces"
	v1 "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// VpcEgressGatewayInformer provides access to a shared informer and lister for
// VpcEgressGateways.
type VpcEgressGatewayInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1.VpcEgressGatewayLister
}

type vpcEgressGatewayInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewVpcEgressGatewayInformer constructs a new informer for VpcEgressGateway type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewVpcEgressGatewayInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredVpcEgressGatewayInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredVpcEgressGatewayInformer constructs a new informer for VpcEgressGateway type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredVpcEgressGatewayInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.KubeovnV1().VpcEgressGateways(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.KubeovnV1().VpcEgressGateways(namespace).Watch(context.TODO(), options)
			},
		},
		&kubeovnv1.VpcEgressGateway{},
		resyncPeriod,
		indexers,
	)
}

func (f *vpcEgressGatewayInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredVpcEgressGatewayInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *vpcEgressGatewayInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&kubeovnv1.VpcEgressGateway{}, f.defaultInformer)
}

func (f *vpcEgressGatewayInformer) Lister() v1.VpcEgressGatewayLister {
	return v1.NewVpcEgressGatewayLister(f.Informer().GetIndexer())
}
