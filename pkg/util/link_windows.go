package util

import "k8s.io/klog/v2"

// SetLinkUp sets a link up
func SetLinkUp(name string) error {
	adapter, err := GetNetAdapter(name, false)
	if err != nil {
		klog.Errorf("failed to get network adapter %s: %v", name, err)
		return err
	}

	if adapter.InterfaceAdminStatus != 1 {
		if err = EnableAdapter(name); err != nil {
			klog.Error(err)
			return err
		}
	}

	return nil
}
