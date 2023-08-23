package util

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strings"

	"gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"
	"k8s.io/klog/v2"
)

var attachmentRegexp = regexp.MustCompile("^[a-z0-9]([-a-z0-9]*[a-z0-9])?$")

func parsePodNetworkObjectName(podNetwork string) (string, string, string, error) {
	var netNsName string
	var netIfName string
	var networkName string

	klog.V(3).Infof("parsePodNetworkObjectName: %s", podNetwork)
	slashItems := strings.Split(podNetwork, "/")
	switch len(slashItems) {
	case 2:
		netNsName = strings.TrimSpace(slashItems[0])
		networkName = slashItems[1]
	case 1:
		networkName = slashItems[0]
	default:
		klog.Errorf("parsePodNetworkObjectName: Invalid network object (failed at '/')")
		return "", "", "", fmt.Errorf("parsePodNetworkObjectName: Invalid network object (failed at '/')")
	}

	atItems := strings.Split(networkName, "@")
	networkName = strings.TrimSpace(atItems[0])
	if len(atItems) == 2 {
		netIfName = strings.TrimSpace(atItems[1])
	} else if len(atItems) != 1 {
		klog.Errorf("parsePodNetworkObjectName: Invalid network object (failed at '@')")
		return "", "", "", fmt.Errorf("parsePodNetworkObjectName: Invalid network object (failed at '@')")
	}

	// Check and see if each item matches the specification for valid attachment name.
	// "Valid attachment names must be comprised of units of the InternalDNS-1123 label format"
	// [a-z0-9]([-a-z0-9]*[a-z0-9])?
	// And we allow at (@), and forward slash (/) (units separated by commas)
	// It must start and end alphanumerically.
	allItems := []string{netNsName, networkName, netIfName}
	for i := range allItems {
		if !attachmentRegexp.MatchString(allItems[i]) && len([]rune(allItems[i])) > 0 {
			klog.Errorf(fmt.Sprintf("parsePodNetworkObjectName: Failed to parse: "+
				"one or more items did not match comma-delimited format (must consist of lower case alphanumeric characters). "+
				"Must start and end with an alphanumeric character), mismatch @ '%v'", allItems[i]))
			return "", "", "", fmt.Errorf(fmt.Sprintf("parsePodNetworkObjectName: Failed to parse: "+
				"one or more items did not match comma-delimited format (must consist of lower case alphanumeric characters). "+
				"Must start and end with an alphanumeric character), mismatch @ '%v'", allItems[i]))
		}
	}

	klog.V(5).Infof("parsePodNetworkObjectName: parsed: %s, %s, %s", netNsName, networkName, netIfName)
	return netNsName, networkName, netIfName, nil
}

func ParsePodNetworkAnnotation(podNetworks, defaultNamespace string) ([]*types.NetworkSelectionElement, error) {
	var networks []*types.NetworkSelectionElement

	klog.V(3).Infof("parsePodNetworkAnnotation: %s, %s", podNetworks, defaultNamespace)
	if podNetworks == "" {
		return nil, nil
	}

	if strings.ContainsAny(podNetworks, "[{\"") {
		if err := json.Unmarshal([]byte(podNetworks), &networks); err != nil {
			klog.Errorf("parsePodNetworkAnnotation: failed to parse pod Network Attachment Selection Annotation JSON format: %v", err)
			return nil, fmt.Errorf("parsePodNetworkAnnotation: failed to parse pod Network Attachment Selection Annotation JSON format: %v", err)
		}
	} else {
		// Comma-delimited list of network attachment object names
		for _, item := range strings.Split(podNetworks, ",") {
			// Remove leading and trailing whitespace.
			item = strings.TrimSpace(item)

			// Parse network name (i.e. <namespace>/<network name>@<ifname>)
			netNsName, networkName, netIfName, err := parsePodNetworkObjectName(item)
			if err != nil {
				klog.Errorf("parsePodNetworkAnnotation: %v", err)
				return nil, fmt.Errorf("parsePodNetworkAnnotation: %v", err)
			}

			networks = append(networks, &types.NetworkSelectionElement{
				Name:             networkName,
				Namespace:        netNsName,
				InterfaceRequest: netIfName,
			})
		}
	}

	for _, n := range networks {
		if n.Namespace == "" {
			n.Namespace = defaultNamespace
		}
		if n.MacRequest != "" {
			// validate MAC address
			if _, err := net.ParseMAC(n.MacRequest); err != nil {
				klog.Errorf("parsePodNetworkAnnotation: failed to mac: %v", err)
				return nil, fmt.Errorf("parsePodNetworkAnnotation: failed to mac: %v", err)
			}
		}
		if n.IPRequest != nil {
			for _, ip := range n.IPRequest {
				// validate IP address
				if strings.Contains(ip, "/") {
					if _, _, err := net.ParseCIDR(ip); err != nil {
						klog.Errorf("failed to parse CIDR %q: %v", ip, err)
						return nil, fmt.Errorf("failed to parse CIDR %q: %v", ip, err)
					}
				} else if net.ParseIP(ip) == nil {
					klog.Errorf("failed to parse IP address %q", ip)
					return nil, fmt.Errorf("failed to parse IP address %q", ip)
				}
			}
		}
		// compatibility pre v3.2, will be removed in v4.0
		if n.DeprecatedInterfaceRequest != "" && n.InterfaceRequest == "" {
			n.InterfaceRequest = n.DeprecatedInterfaceRequest
		}
	}

	return networks, nil
}

func IsOvnNetwork(netCfg *types.DelegateNetConf) bool {
	if netCfg.Conf.Type == CniTypeName {
		return true
	}
	for _, item := range netCfg.ConfList.Plugins {
		if item.Type == CniTypeName {
			return true
		}
	}
	return false
}

func IsDefaultNet(defaultNetAnnotation string, attach *types.NetworkSelectionElement) bool {
	if defaultNetAnnotation == attach.Name || defaultNetAnnotation == fmt.Sprintf("%s/%s", attach.Namespace, attach.Name) {
		return true
	}
	return false
}
