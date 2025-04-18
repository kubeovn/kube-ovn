/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2017, 2018 Red Hat, Inc.
 *
 */

package informer

import (
	"math/rand/v2"
	"sync"
	"time"

	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	aggregatorclient "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset"

	kubev1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"
	"kubevirt.io/client-go/log"
)

type newSharedInformer func() cache.SharedIndexInformer

type KubeVirtInformerFactory interface {
	// Starts any informers that have not been started yet
	// This function is thread safe and idempotent
	Start(stopCh <-chan struct{})

	// Waits for all informers to sync
	WaitForCacheSync(stopCh <-chan struct{})

	// Watches VirtualMachineInstanceMigration objects
	VirtualMachineInstanceMigration() cache.SharedIndexInformer
}

type kubeInformerFactory struct {
	restClient       *rest.RESTClient
	clientSet        kubecli.KubevirtClient
	aggregatorClient aggregatorclient.Interface
	lock             sync.Mutex
	defaultResync    time.Duration

	informers         map[string]cache.SharedIndexInformer
	startedInformers  map[string]bool
	kubevirtNamespace string
	k8sInformers      informers.SharedInformerFactory
}

func NewKubeVirtInformerFactory(restClient *rest.RESTClient, clientSet kubecli.KubevirtClient, aggregatorClient aggregatorclient.Interface, kubevirtNamespace string) KubeVirtInformerFactory {
	return &kubeInformerFactory{
		restClient:       restClient,
		clientSet:        clientSet,
		aggregatorClient: aggregatorClient,
		// Resulting resync period will be between 12 and 24 hours, like the default for k8s
		defaultResync:     resyncPeriod(12 * time.Hour),
		informers:         make(map[string]cache.SharedIndexInformer),
		startedInformers:  make(map[string]bool),
		kubevirtNamespace: kubevirtNamespace,
		k8sInformers:      informers.NewSharedInformerFactoryWithOptions(clientSet, 0),
	}
}

// Start can be called from multiple controllers in different go routines safely.
// Only informers that have not started are triggered by this function.
// Multiple calls to this function are idempotent.
func (f *kubeInformerFactory) Start(stopCh <-chan struct{}) {
	f.lock.Lock()
	defer f.lock.Unlock()

	for name, informer := range f.informers {
		if f.startedInformers[name] {
			// skip informers that have already started.
			log.Log.Infof("SKIPPING informer %s", name)
			continue
		}
		log.Log.Infof("STARTING informer %s", name)
		go informer.Run(stopCh)
		f.startedInformers[name] = true
	}
	f.k8sInformers.Start(stopCh)
}

func (f *kubeInformerFactory) WaitForCacheSync(stopCh <-chan struct{}) {
	syncs := []cache.InformerSynced{}

	f.lock.Lock()
	for name, informer := range f.informers {
		log.Log.Infof("Waiting for cache sync of informer %s", name)
		syncs = append(syncs, informer.HasSynced)
	}
	f.lock.Unlock()

	cache.WaitForCacheSync(stopCh, syncs...)
}

// internal function used to retrieve an already created informer
// or create a new informer if one does not already exist.
// Thread safe
func (f *kubeInformerFactory) getInformer(key string, newFunc newSharedInformer) cache.SharedIndexInformer {
	f.lock.Lock()
	defer f.lock.Unlock()

	informer, exists := f.informers[key]
	if exists {
		return informer
	}
	informer = newFunc()
	f.informers[key] = informer

	return informer
}

func (f *kubeInformerFactory) Namespace() cache.SharedIndexInformer {
	return f.getInformer("namespaceInformer", func() cache.SharedIndexInformer {
		lw := cache.NewListWatchFromClient(f.clientSet.CoreV1().RESTClient(), "namespaces", k8sv1.NamespaceAll, fields.Everything())
		return cache.NewSharedIndexInformer(
			lw,
			&k8sv1.Namespace{},
			f.defaultResync,
			cache.Indexers{
				"namespace_name": func(obj any) ([]string, error) {
					return []string{obj.(*k8sv1.Namespace).GetName()}, nil
				},
			},
		)
	})
}

func (f *kubeInformerFactory) VirtualMachineInstanceMigration() cache.SharedIndexInformer {
	return f.getInformer("vmimInformer", func() cache.SharedIndexInformer {
		lw := cache.NewListWatchFromClient(f.restClient, "virtualmachineinstancemigrations", k8sv1.NamespaceAll, fields.Everything())
		return cache.NewSharedIndexInformer(lw, &kubev1.VirtualMachineInstanceMigration{}, f.defaultResync, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	})
}

// resyncPeriod computes the time interval a shared informer waits before resyncing with the api server
func resyncPeriod(minResyncPeriod time.Duration) time.Duration {
	// #nosec no need for better randomness
	factor := rand.Float64() + 1
	return time.Duration(float64(minResyncPeriod.Nanoseconds()) * factor)
}
