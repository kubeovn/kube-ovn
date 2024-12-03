package util

import (
	"encoding/json"
	"fmt"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/request"
)

// PodProviderRoutes represents configured routes for a provider/interface
type PodProviderRoutes map[string][]string // gateway -> destinations

// PodRoutes represents configured routes for all providers/interfaces
// This type is used to generate annotations needed by kube-ovn-cni to configure routes in the pod
type PodRoutes map[string]PodProviderRoutes // provider -> PodProviderRoutes

func NewPodRoutes() PodRoutes {
	return make(PodRoutes)
}

func (r PodRoutes) Add(provider, destination, gateway string) {
	if gateway == "" || destination == "" {
		return
	}

	if r[provider] == nil {
		r[provider] = make(PodProviderRoutes)
	}
	r[provider][gateway] = append(r[provider][gateway], destination)
}

func (r PodRoutes) ToAnnotations() (map[string]string, error) {
	annotations := make(map[string]string, len(r))
	for provider, routesMap := range r {
		var routes []request.Route
		for gw := range routesMap {
			if gw == "" {
				continue
			}
			for _, dst := range routesMap[gw] {
				routes = append(routes, request.Route{
					Destination: dst,
					Gateway:     gw,
				})
			}
		}
		if len(routes) == 0 {
			continue
		}

		buf, err := json.Marshal(routes)
		if err != nil {
			klog.Error(err)
			return nil, err
		}
		annotations[fmt.Sprintf(RoutesAnnotationTemplate, provider)] = string(buf)
	}
	return annotations, nil
}
