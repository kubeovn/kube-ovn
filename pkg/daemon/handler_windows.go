package daemon

import (
	"errors"

	v1 "k8s.io/api/core/v1"

	"github.com/kubeovn/kube-ovn/pkg/request"
)

func (csh cniServerHandler) validatePodRequest(req *request.CniRequest) error {
	if req.DeviceID != "" {
		return errors.New("SR-IOV is not supported on Windows")
	}
	if req.VhostUserSocketVolumeName != "" {
		return errors.New("DPDK is not supported on Windows")
	}

	return nil
}

func createShortSharedDir(pod *v1.Pod, volumeName, socketConsumption, kubeletDir string) error {
	// nothing to do on Windows
	return nil
}

func removeShortSharedDir(pod *v1.Pod, volumeName, socketConsumption string) error {
	// nothing to do on Windows
	return nil
}
