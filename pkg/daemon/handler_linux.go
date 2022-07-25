package daemon

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (csh cniServerHandler) validatePodRequest(req *request.CniRequest) error {
	// nothing to do on linux
	return nil
}

func createShortSharedDir(pod *v1.Pod, volumeName string) (err error) {
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
	originSharedDir := fmt.Sprintf("/var/lib/kubelet/pods/%s/volumes/kubernetes.io~empty-dir/%s", pod.UID, volumeName)
	newSharedDir := getShortSharedDir(pod.UID, volumeName)
	// set vhostuser dir 777 for qemu has the permission to create sock
	mask := syscall.Umask(0)
	defer syscall.Umask(mask)
	if _, err = os.Stat(newSharedDir); os.IsNotExist(err) {
		err = os.MkdirAll(newSharedDir, 0777)
		if err != nil {
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
	return err
}

func removeShortSharedDir(pod *v1.Pod, volumeName string) (err error) {
	sharedDir := getShortSharedDir(pod.UID, volumeName)
	if _, err = os.Stat(sharedDir); os.IsNotExist(err) {
		klog.Errorf("shared directory %s does not exist to unmount, %s", sharedDir, err)
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
