package controller

import (
	"slices"

	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/client-go/tools/cache"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

const (
	IndexPodByNode             = "byNodeName"
	IndexEPSByService          = "byServiceName"
	IndexDNSNameResolverByName = "byDNSName"
	IndexIPBySubnet            = "bySubnet"
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

func indexDNSNameResolverByName(obj any) ([]string, error) {
	r, ok := obj.(*kubeovnv1.DNSNameResolver)
	if !ok || r.Spec.Name == "" {
		return nil, nil
	}
	return []string{string(r.Spec.Name)}, nil
}

// indexIPBySubnet indexes an IP CR by its primary subnet plus any attach
// subnets, mirroring the subnets enqueued to updateSubnetStatusQueue on IP
// add/update/delete. This lets calcSubnetStatusIP look up the IPs of a single
// subnet in O(matched) instead of scanning the whole IP store.
func indexIPBySubnet(obj any) ([]string, error) {
	ip, ok := obj.(*kubeovnv1.IP)
	if !ok {
		return nil, nil
	}
	subnets := make([]string, 0, 1+len(ip.Spec.AttachSubnets))
	if ip.Spec.Subnet != "" {
		subnets = append(subnets, ip.Spec.Subnet)
	}
	for _, as := range ip.Spec.AttachSubnets {
		if as != "" && !slices.Contains(subnets, as) {
			subnets = append(subnets, as)
		}
	}
	return subnets, nil
}

// setupIndexers registers custom informer indexers used by hot-path
// reconciliation loops to avoid O(N) full-store scans. Must be called before
// the informer factory is started.
func (c *Controller) setupIndexers(podInformer, epsInformer, ipInformer cache.SharedIndexInformer) error {
	if err := podInformer.AddIndexers(cache.Indexers{IndexPodByNode: indexPodByNode}); err != nil {
		return err
	}
	if err := epsInformer.AddIndexers(cache.Indexers{IndexEPSByService: indexEPSByService}); err != nil {
		return err
	}
	if err := ipInformer.AddIndexers(cache.Indexers{IndexIPBySubnet: indexIPBySubnet}); err != nil {
		return err
	}
	c.podIndexer = podInformer.GetIndexer()
	c.epsIndexer = epsInformer.GetIndexer()
	c.ipIndexer = ipInformer.GetIndexer()
	return nil
}
