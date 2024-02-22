package daemon

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"syscall"

	"github.com/moby/sys/mountinfo"
	"golang.org/x/sys/unix"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (csh cniServerHandler) validatePodRequest(_ *request.CniRequest) error {
	// nothing to do on linux
	return nil
}

func createShortSharedDir(pod *v1.Pod, volumeName, socketConsumption, kubeletDir string) (err error) {
	var volume *v1.Volume
	for index, v := range pod.Spec.Volumes {
		if v.Name == volumeName {
			volume = &pod.Spec.Volumes[index]
			break
		}
	}
	if volume == nil {
		return fmt.Errorf("can not found volume %s in pod %s", volumeName, pod.Name)
	}
	if volume.EmptyDir == nil {
		return fmt.Errorf("volume %s is not empty dir", volume.Name)
	}
	originSharedDir := fmt.Sprintf("%s/pods/%s/volumes/kubernetes.io~empty-dir/%s", kubeletDir, pod.UID, volumeName)
	newSharedDir := getShortSharedDir(pod.UID, volumeName)
	// set vhostuser dir 777 for qemu has the permission to create sock
	mask := syscall.Umask(0)
	defer syscall.Umask(mask)
	if _, err = os.Stat(newSharedDir); err != nil {
		if os.IsNotExist(err) {
			err = os.MkdirAll(newSharedDir, 0o777)
			if err != nil {
				klog.Error(err)
				return fmt.Errorf("createSharedDir: Failed to create dir (%s): %v", newSharedDir, err)
			}

			if strings.Contains(newSharedDir, util.DefaultHostVhostuserBaseDir) {
				klog.Infof("createSharedDir: Mount from %s to %s", originSharedDir, newSharedDir)
				err = unix.Mount(originSharedDir, newSharedDir, "", unix.MS_BIND, "")
				if err != nil {
					return fmt.Errorf("createSharedDir: Failed to bind mount: %s", err)
				}
			}
			return nil
		}
		klog.Error(err)
		return err
	}

	if socketConsumption != util.ConsumptionKubevirt {
		return fmt.Errorf("createSharedDir: voume name %s is exists", volumeName)
	}

	return nil
}

func removeShortSharedDir(pod *v1.Pod, volumeName, socketConsumption string) (err error) {
	sharedDir := getShortSharedDir(pod.UID, volumeName)
	if _, err = os.Stat(sharedDir); os.IsNotExist(err) {
		klog.Infof("shared directory %s does not exist to unmount, %s", sharedDir, err)
		return nil
	}

	// keep mount util dpdk sock not used by kuebvirt
	if socketConsumption == util.ConsumptionKubevirt {
		files, err := os.ReadDir(sharedDir)
		if err != nil {
			return fmt.Errorf("read file from dpdk share dir error: %s", err)
		}
		if len(files) != 0 {
			return nil
		}
	}

	foundMount, err := mountinfo.Mounted(sharedDir)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !foundMount) {
		klog.Infof("volume: %s not mounted, no need to unmount", sharedDir)
		return nil
	}
	err = unix.Unmount(sharedDir, 0)
	if err != nil {
		klog.Errorf("Failed to unmount dir: %v", err)
		return err
	}
	err = os.Remove(sharedDir)
	if err != nil {
		klog.Errorf("Failed to remove dir: %v", err)
		return err
	}

	return nil
}
