package speaker

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	v1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// syncEIPRoutes retrieves all the EIPs attached to our GWs and starts announcing their route
func (c *Controller) syncEIPRoutes() error {
	// Retrieve the name of our gateway
	gatewayName := getGatewayName()
	if gatewayName == "" {
		return errors.New("failed to retrieve the name of the gateway, might not be running in a gateway pod")
	}

	// Create label requirements to only get EIPs attached to our NAT GW
	requirements, err := labels.NewRequirement(util.VpcNatGatewayNameLabel, selection.Equals, []string{gatewayName})
	if err != nil {
		return fmt.Errorf("failed to create label selector requirement: %w", err)
	}

	// Filter all EIPs attached to our NAT GW
	eips, err := c.eipLister.List(labels.NewSelector().Add(*requirements))
	if err != nil {
		return fmt.Errorf("failed to list EIPs attached to our GW: %w", err)
	}

	return c.announceEIPs(eips)
}

// announceEIPs announce all the prefixes related to EIPs attached to a GW
func (c *Controller) announceEIPs(eips []*v1.IptablesEIP) error {
	expectedPrefixes := make(prefixMap)
	for _, eip := range eips {
		// Only announce EIPs marked as "ready" and with the BGP annotation set to true
		if eip.Annotations[util.BgpAnnotation] != "true" || !eip.Status.Ready {
			continue
		}

		if eip.Spec.V4ip != "" { // If we have an IPv4, add it to prefixes we should be announcing
			addExpectedPrefix(eip.Spec.V4ip, expectedPrefixes)
		}

		if eip.Spec.V6ip != "" { // If we have an IPv6, add it to prefixes we should be announcing
			addExpectedPrefix(eip.Spec.V6ip, expectedPrefixes)
		}
	}

	return c.reconcileRoutes(expectedPrefixes)
}
