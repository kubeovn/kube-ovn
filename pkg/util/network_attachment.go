package util

import (
	"encoding/json"
	"errors"
	"fmt"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"
	"k8s.io/klog/v2"
)

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

func IsDefaultNet(defaultNetAnnotation string, attach *nadv1.NetworkSelectionElement) bool {
	if defaultNetAnnotation == attach.Name || defaultNetAnnotation == fmt.Sprintf("%s/%s", attach.Namespace, attach.Name) {
		return true
	}
	return false
}

func GetNadInterfaceFromNetworkStatusAnnotation(networkStatus, nadName string) (string, error) {
	var interfaceName string
	if networkStatus == "" {
		return "", errors.New("no network status annotation found")
	}

	var status []map[string]any
	if err := json.Unmarshal([]byte(networkStatus), &status); err != nil {
		klog.Errorf("failed to unmarshal network status annotation: %v", err)
		return interfaceName, err
	}

	for _, s := range status {
		if name, ok := s["name"].(string); ok && name == nadName {
			if iface, ok := s["interface"].(string); ok {
				interfaceName = iface
			}
			break
		}
	}
	if interfaceName == "" {
		return "", fmt.Errorf("no interface name found for secondary network %s", nadName)
	}

	return interfaceName, nil
}
