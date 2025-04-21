package util

import (
	"fmt"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"
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
