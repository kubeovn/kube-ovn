package util

import (
	"k8s.io/klog"
	"strconv"
)

func IsNetworkVlan(networkType string, vlanID string, vlanRange string) bool {
	if networkType != NetworkTypeVlan {
		return false
	}

	if vlanID == "" {
		return false
	}

	tag, err := strconv.Atoi(vlanID)
	if err != nil {
		klog.Errorf("the vlan id is invalid, %v", err)
		return false
	}

	if err = ValidateVlan(tag, vlanRange); err != nil {
		klog.Errorf("validate vlan failed, %v", err)
		return false
	}

	return true
}

func IsProviderVlan(networkType, provider string) bool {
	if networkType != NetworkTypeVlan {
		return false
	}

	if provider == "" {
		return false
	}

	return true
}
