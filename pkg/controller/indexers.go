package controller

import (
	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	IndexPodByNode    = "byNodeName"
	IndexEPSByService = "byServiceName"
)

func indexPodByNode(obj any) ([]string, error) {
	pod, ok := obj.(*v1.Pod)
	if !ok || pod.Spec.NodeName == "" {
		return nil, nil
	}
	return []string{pod.Spec.NodeName}, nil
}

func indexEPSByService(obj any) ([]string, error) {
	eps, ok := obj.(*discoveryv1.EndpointSlice)
	if !ok {
		return nil, nil
	}
	svc := getServiceForEndpointSlice(eps)
	if svc == "" {
		return nil, nil
	}
	return []string{eps.Namespace + "/" + svc}, nil
}

// setupIndexers registers custom informer indexers used by hot-path
// reconciliation loops to avoid O(N) full-store scans. Must be called before
// the informer factory is started.
func (c *Controller) setupIndexers(podInformer, epsInformer cache.SharedIndexInformer) error {
	if err := podInformer.AddIndexers(cache.Indexers{IndexPodByNode: indexPodByNode}); err != nil {
		return err
	}
	if err := epsInformer.AddIndexers(cache.Indexers{IndexEPSByService: indexEPSByService}); err != nil {
		return err
	}
	c.podIndexer = podInformer.GetIndexer()
	c.epsIndexer = epsInformer.GetIndexer()
	return nil
}
