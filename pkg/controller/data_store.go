package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/pkg/errors"
)

// CheckpointFormatVersion is the version stamp used on stored checkpoints.
const CheckpointFormatVersion = "kube-ovn-ipam/1"

// CheckpointData is the format of stored checkpoints. Note this is
// deliberately a "dumb" format since efficiency is less important
// than version stability here.
type CheckpointData struct {
	Version     string            `json:"version"`
	Allocations []CheckpointEntry `json:"allocations"`
}

// CheckpointEntry is a "row" in the conceptual IPAM datastore, as stored
// in checkpoints.
type CheckpointEntry struct {
	IPv4            string `json:"ipv4,omitempty"`
	IPv6            string `json:"ipv6,omitempty"`
	K8SPodNamespace string `json:"k8sPodNamespace,omitempty"`
	K8SPodName      string `json:"k8sPodName,omitempty"`
}

// ReadBackingStore initializes the IP allocation state from the
// configured backing store. Should be called before using data store.
func (c *Controller) ReadBackingStore() error {
	var data CheckpointData

	// Read from checkpoint file
	klog.Infof("Begin ipam state recovery from backing store")

	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.OvnBackendStoreConfig)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.Infof("Backend store configmap does not exist, will create later")
			return nil
		}
		klog.Errorf("failed to get ovn-backend-store-config, %v", err)
		return err
	}
	if cm.Data == nil {
		klog.Infof("There's no data record in ovn-backend-store-config, ignore init restore")
		return nil
	}

	assignedData := cm.Data[util.OvnAssignedKey]
	if err := json.Unmarshal([]byte(assignedData), &data); err != nil {
		klog.Errorf("failed to read configmap data from ovn-backend-store-config, %v", err)
		return err
	}

	if data.Version != CheckpointFormatVersion {
		return errors.Errorf("failed ipam state recovery due to unexpected checkpointVersion: %v/%v", data.Version, CheckpointFormatVersion)
	}
	if normalizedData, err := c.normalizeCheckpointDataByPodExistence(data); err != nil {
		return errors.Wrap(err, "failed normalize checkpoint data with pod check")
	} else {
		data = normalizedData
	}

	for _, allocation := range data.Allocations {
		cachedPod, err := c.podsLister.Pods(allocation.K8SPodNamespace).Get(allocation.K8SPodName)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				continue
			}
			klog.Error(err)
		}
		podNets, err := c.getPodKubeovnNets(cachedPod)
		if err != nil {
			klog.Errorf("failed to get pod kubeovn nets %s.%s address %s: %v", cachedPod.Name, cachedPod.Namespace, cachedPod.Annotations[util.IpAddressAnnotation], err)
			continue
		}
		podName := c.getNameByPod(cachedPod)
		key := fmt.Sprintf("%s/%s", cachedPod.Namespace, podName)

		for _, podNet := range podNets {
			portName := ovs.PodNameToPortName(podName, cachedPod.Namespace, podNet.ProviderName)
			ip := cachedPod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)]
			mac := cachedPod.Annotations[fmt.Sprintf(util.MacAddressAnnotationTemplate, podNet.ProviderName)]
			_, _, _, err := c.ipam.GetStaticAddress(key, portName, ip, &mac, podNet.Subnet.Name, true)
			if err != nil {
				klog.Errorf("failed to init pod %s.%s address %s: %v", podName, cachedPod.Namespace, cachedPod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)], err)
			}
		}
	}

	bytes, _ := json.Marshal(data)
	cm.Data[util.OvnAssignedKey] = string(bytes)
	if _, err := c.config.KubeClient.CoreV1().ConfigMaps(c.config.PodNamespace).Update(context.Background(), cm, metav1.UpdateOptions{}); err != nil {
		klog.Errorf("failed to update ovn-backend-store-config configmap, %v", err)
		return err
	}

	klog.Infof("Completed ipam state recovery")
	return nil
}

func (c *Controller) normalizeCheckpointDataByPodExistence(checkpoint CheckpointData) (CheckpointData, error) {
	var validatedAllocations []CheckpointEntry

	for _, allocation := range checkpoint.Allocations {
		if err := c.validateAllocationByPodExistence(allocation); err != nil {
			klog.Errorf("failed to validate IP allocation for pod(%v): IPv4(%v), IPv6(%v), %v", allocation.K8SPodName, allocation.IPv4, allocation.IPv6, err)
		} else {
			validatedAllocations = append(validatedAllocations, allocation)
		}
	}
	checkpoint.Allocations = validatedAllocations

	return checkpoint, nil
}

func (c *Controller) validateAllocationByPodExistence(allocation CheckpointEntry) error {
	if allocation.K8SPodNamespace == "" || allocation.K8SPodName == "" {
		return nil
	}

	cachedPod, err := c.podsLister.Pods(allocation.K8SPodNamespace).Get(allocation.K8SPodName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	podNets, err := c.getPodKubeovnNets(cachedPod)
	if err != nil {
		klog.Errorf("failed to get pod nets %v", err)
		return err
	}

	for _, podNet := range podNets {
		if _, ok := c.ipam.Subnets[podNet.Subnet.Name]; !ok {
			return fmt.Errorf("can not find subnet for pod %s/%s", allocation.K8SPodNamespace, allocation.K8SPodName)
		} else {
			// The ipam is being recreated now, so can not rely on ipam to check, just recover every pod record in cm
			return nil
		}
	}

	return errors.Errorf("ipam record not found for pod %v/%v", allocation.K8SPodNamespace, allocation.K8SPodName)
}

func (c *Controller) updateAssignedPodIPAddressRecord() error {
	var allocations []CheckpointEntry
	var podNs string

	for _, subnet := range c.ipam.Subnets {
		var allocation CheckpointEntry
		if subnet.Name == c.config.NodeSwitch {
			continue
		}

		switch subnet.Protocol {
		case kubeovnv1.ProtocolIPv4:
			for ip, podName := range subnet.V4IPToPod {
				podInfos := strings.Split(podName, "/")
				if len(podInfos) > 1 {
					podNs = podInfos[0]
					podName = podInfos[1]
				} else {
					podName = podInfos[0]
				}

				allocation.K8SPodName = podName
				allocation.K8SPodNamespace = podNs
				allocation.IPv4 = ip
				allocations = append(allocations, allocation)
			}

		case kubeovnv1.ProtocolIPv6:
			for ip, podName := range subnet.V6IPToPod {
				podInfos := strings.Split(podName, "/")
				if len(podInfos) > 1 {
					podNs = podInfos[0]
					podName = podInfos[1]
				} else {
					podName = podInfos[0]
				}

				allocation.K8SPodName = podName
				allocation.K8SPodNamespace = podNs
				allocation.IPv6 = ip
				allocations = append(allocations, allocation)
			}
		case kubeovnv1.ProtocolDual:
			for ip, podName := range subnet.V4IPToPod {
				podInfos := strings.Split(podName, "/")
				if len(podInfos) > 1 {
					podNs = podInfos[0]
					podName = podInfos[1]
				} else {
					podName = podInfos[0]
				}

				allocation.K8SPodName = podName
				allocation.K8SPodNamespace = podNs
				allocation.IPv4 = ip
				// it may be error with multus nic
				nic := subnet.PodToNicList[podName][0]
				allocation.IPv6 = subnet.V6NicToIP[nic].String()
				allocations = append(allocations, allocation)
			}
		}
	}

	cm, err := c.configMapsLister.ConfigMaps(c.config.PodNamespace).Get(util.OvnBackendStoreConfig)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			klog.Errorf("failed to get ovn-backend-store-config, should create cm first")

			var newCm corev1.ConfigMap
			newCm.Name = util.OvnBackendStoreConfig
			newCm.Namespace = c.config.PodNamespace
			if cm, err = c.config.KubeClient.CoreV1().ConfigMaps(c.config.PodNamespace).Create(context.Background(), &newCm, metav1.CreateOptions{}); err != nil {
				klog.Errorf("failed to create ovn-backend-store-config configmap, %v", err)
				return err
			}
		} else {
			klog.Errorf("failed to get ovn-backend-store-config, %v", err)
			return err
		}
	}
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}

	data := CheckpointData{
		Version:     CheckpointFormatVersion,
		Allocations: allocations,
	}
	bytes, _ := json.Marshal(data)
	cm.Data[util.OvnAssignedKey] = string(bytes)
	if _, err := c.config.KubeClient.CoreV1().ConfigMaps(c.config.PodNamespace).Update(context.Background(), cm, metav1.UpdateOptions{}); err != nil {
		klog.Errorf("failed to update ovn-backend-store-config configmap, %v", err)
		return err
	}

	return nil
}
