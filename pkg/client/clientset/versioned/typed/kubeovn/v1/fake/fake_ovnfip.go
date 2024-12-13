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
	v1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/typed/kubeovn/v1"
	gentype "k8s.io/client-go/gentype"
)

// fakeOvnFips implements OvnFipInterface
type fakeOvnFips struct {
	*gentype.FakeClientWithList[*v1.OvnFip, *v1.OvnFipList]
	Fake *FakeKubeovnV1
}

func newFakeOvnFips(fake *FakeKubeovnV1) kubeovnv1.OvnFipInterface {
	return &fakeOvnFips{
		gentype.NewFakeClientWithList[*v1.OvnFip, *v1.OvnFipList](
			fake.Fake,
			"",
			v1.SchemeGroupVersion.WithResource("ovn-fips"),
			v1.SchemeGroupVersion.WithKind("OvnFip"),
			func() *v1.OvnFip { return &v1.OvnFip{} },
			func() *v1.OvnFipList { return &v1.OvnFipList{} },
			func(dst, src *v1.OvnFipList) { dst.ListMeta = src.ListMeta },
			func(list *v1.OvnFipList) []*v1.OvnFip { return gentype.ToPointerSlice(list.Items) },
			func(list *v1.OvnFipList, items []*v1.OvnFip) { list.Items = gentype.FromPointerSlice(items) },
		),
		fake,
	}
}
