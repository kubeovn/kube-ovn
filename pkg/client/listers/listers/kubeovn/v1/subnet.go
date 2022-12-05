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

// Code generated by lister-gen. DO NOT EDIT.

package v1

import (
	v1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// SubnetLister helps list Subnets.
type SubnetLister interface {
	// List lists all Subnets in the indexer.
	List(selector labels.Selector) (ret []*v1.Subnet, err error)
	// Get retrieves the Subnet from the index for a given name.
	Get(name string) (*v1.Subnet, error)
	SubnetListerExpansion
}

// subnetLister implements the SubnetLister interface.
type subnetLister struct {
	indexer cache.Indexer
}

// NewSubnetLister returns a new SubnetLister.
func NewSubnetLister(indexer cache.Indexer) SubnetLister {
	return &subnetLister{indexer: indexer}
}

// List lists all Subnets in the indexer.
func (s *subnetLister) List(selector labels.Selector) (ret []*v1.Subnet, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1.Subnet))
	})
	return ret, err
}

// Get retrieves the Subnet from the index for a given name.
func (s *subnetLister) Get(name string) (*v1.Subnet, error) {
	obj, exists, err := s.indexer.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1.Resource("subnet"), name)
	}
	return obj.(*v1.Subnet), nil
}
