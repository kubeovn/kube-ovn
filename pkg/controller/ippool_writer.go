package controller

import (
	"errors"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

// IPPoolWriter separates API persistence from the shared allocation backend.
// Implementations retain their leader context and advance the working object's
// resource version after their own writes, without replacing pending conditions.
type IPPoolWriter interface {
	PatchIPs(*kubeovnv1.IPPool, []string) error
	PatchStatus(*kubeovnv1.IPPool) error
	PatchStatistics(*kubeovnv1.IPPool) error
	AddFinalizer(*kubeovnv1.IPPool) error
	RemoveFinalizer(*kubeovnv1.IPPool) error
}

// SetIPPoolWriter installs persistence before the runtime controller starts.
// Startup restoration and every other migration mode retain their legacy client.
func (c *Controller) SetIPPoolWriter(writer IPPoolWriter) error {
	if !controllerRuntimeIPPoolEnabled(c.config) {
		return errors.New("IPPool writer requires ippool runtime mode")
	}
	if writer == nil || c.ipPoolWriter != nil {
		return errors.New("IPPool writer must be installed exactly once")
	}
	c.ipPoolWriter = writer
	return nil
}
