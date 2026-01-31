package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	kubevirtv1 "kubevirt.io/api/core/v1"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	nadutils "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/utils"
	"github.com/scylladb/go-set/strset"

	"github.com/kubeovn/kube-ovn/pkg/informer"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddVMIMigration(obj any) {
	key := cache.MetaObjectToName(obj.(*kubevirtv1.VirtualMachineInstanceMigration)).String()
	klog.Infof("enqueue add VMI migration %s", key)
	c.addOrUpdateVMIMigrationQueue.Add(key)
}

func (c *Controller) enqueueUpdateVMIMigration(oldObj, newObj any) {
	oldVmi := oldObj.(*kubevirtv1.VirtualMachineInstanceMigration)
	newVmi := newObj.(*kubevirtv1.VirtualMachineInstanceMigration)

	if !newVmi.DeletionTimestamp.IsZero() ||
		oldVmi.Status.Phase != newVmi.Status.Phase {
		key := cache.MetaObjectToName(newVmi).String()
		klog.Infof("enqueue update VMI migration %s", key)
		c.addOrUpdateVMIMigrationQueue.Add(key)
	}
}

func (c *Controller) enqueueDeleteVM(obj any) {
	var vm *kubevirtv1.VirtualMachine
	switch t := obj.(type) {
	case *kubevirtv1.VirtualMachine:
		vm = t
	case cache.DeletedFinalStateUnknown:
		v, ok := t.Obj.(*kubevirtv1.VirtualMachine)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		vm = v
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	key := cache.MetaObjectToName(vm).String()
	klog.Infof("enqueue add VM %s", key)
	c.deleteVMQueue.Add(key)
}

func (c *Controller) handleDeleteVM(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid vm key: %s", key))
		return nil
	}
	vmKey := fmt.Sprintf("%s/%s", namespace, name)

	ports, err := c.OVNNbClient.ListNormalLogicalSwitchPorts(true, map[string]string{"pod": vmKey})
	if err != nil {
		klog.Errorf("failed to list lsps of vm %s: %v", vmKey, err)
		return err
	}

	for _, port := range ports {
		if err := c.config.KubeOvnClient.KubeovnV1().IPs().Delete(context.Background(), port.Name, metav1.DeleteOptions{}); err != nil {
			if !k8serrors.IsNotFound(err) {
				klog.Errorf("failed to delete ip %s, %v", port.Name, err)
				return err
			}
		}

		subnetName := port.ExternalIDs["ls"]
		if subnetName != "" {
			c.ipam.ReleaseAddressByNic(vmKey, port.Name, subnetName)
		}

		if err := c.OVNNbClient.DeleteLogicalSwitchPort(port.Name); err != nil {
			klog.Errorf("failed to delete lsp %s, %v", port.Name, err)
			return err
		}
	}

	return nil
}

func (c *Controller) handleAddOrUpdateVMIMigration(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	vmiMigration, err := c.config.KubevirtClient.VirtualMachineInstanceMigration(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("failed to get VMI migration by key %s: %w", key, err))
		return err
	}
	if vmiMigration.Status.MigrationState == nil {
		klog.V(3).Infof("VirtualMachineInstanceMigration %s migration state is nil, skipping", key)
		return nil
	}

	if vmiMigration.Status.MigrationState.Completed {
		klog.V(3).Infof("VirtualMachineInstanceMigration %s migration state is completed, skipping", key)
		return nil
	}

	vmi, err := c.config.KubevirtClient.VirtualMachineInstance(namespace).Get(context.TODO(), vmiMigration.Spec.VMIName, metav1.GetOptions{})
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("failed to get VMI by name %s: %w", vmiMigration.Spec.VMIName, err))
		return err
	}

	// use VirtualMachineInstance's MigrationState because VirtualMachineInstanceMigration's MigrationState is not updated until migration finished
	var srcNodeName, targetNodeName string
	if vmi.Status.MigrationState != nil {
		klog.Infof("current vmiMigration %s status %s, target Node %s, source Node %s, target Pod %s, source Pod %s", key,
			vmiMigration.Status.Phase,
			vmi.Status.MigrationState.TargetNode,
			vmi.Status.MigrationState.SourceNode,
			vmi.Status.MigrationState.TargetPod,
			vmi.Status.MigrationState.SourcePod)
		srcNodeName = vmi.Status.MigrationState.SourceNode
		targetNodeName = vmi.Status.MigrationState.TargetNode
	} else {
		klog.Infof("current vmiMigration %s status %s, vmi MigrationState is nil", key, vmiMigration.Status.Phase)
	}

	// collect all port names related to the VMI (pod network/multus annotations/attached networks)
	portNamesSet := strset.New()
	// only consider ports which kube-ovn created (it may be that non kube-ovn NAD attachements are being used)
	lsps, err := c.OVNNbClient.ListNormalLogicalSwitchPorts(c.config.EnableExternalVpc, nil)
	if err != nil {
		klog.Errorf("failed to list logical switch port, %v", err)
		return err
	}

	allPortNames := strset.NewWithSize(len(lsps))
	for _, lsp := range lsps {
		allPortNames.Add(lsp.Name)
	}

	defaultMultus := false
	for _, network := range vmi.Spec.Networks {
		if network.Multus != nil && network.Multus.Default {
			defaultMultus = true
			break
		}
	}
	if !defaultMultus {
		portName := ovs.PodNameToPortName(vmiMigration.Spec.VMIName, vmiMigration.Namespace, util.OvnProvider)
		if allPortNames.Has(portName) {
			portNamesSet.Add(portName)
		}
	}

	nadAnnotation := vmi.Annotations[nadv1.NetworkAttachmentAnnot]
	if nadAnnotation != "" {
		attachNets, err := nadutils.ParseNetworkAnnotation(nadAnnotation, vmi.Namespace)
		if err != nil {
			klog.Errorf("failed to get attachment subnet of vmi %s, %v", vmi.Name, err)
		} else {
			for _, multiNet := range attachNets {
				provider := fmt.Sprintf("%s.%s.%s", multiNet.Name, multiNet.Namespace, util.OvnProvider)
				portName := ovs.PodNameToPortName(vmi.Name, vmi.Namespace, provider)
				if allPortNames.Has(portName) {
					portNamesSet.Add(portName)
				}
			}
		}
	}

	for _, network := range vmi.Spec.Networks {
		if network.Multus != nil && network.Multus.NetworkName != "" {
			items := strings.Split(network.Multus.NetworkName, "/")
			if len(items) != 2 {
				items = []string{vmi.Namespace, items[0]}
			}
			provider := fmt.Sprintf("%s.%s.%s", items[1], items[0], util.OvnProvider)
			portName := ovs.PodNameToPortName(vmi.Name, vmi.Namespace, provider)
			if allPortNames.Has(portName) {
				portNamesSet.Add(portName)
			}
		}
	}

	portNames := portNamesSet.List()
	klog.Infof("collected port names of vmi %s, port names are %v", vmi.Name, strings.Join(portNames, ", "))

	switch vmiMigration.Status.Phase {
	case kubevirtv1.MigrationScheduling:
		selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
			MatchLabels: map[string]string{
				kubevirtv1.MigrationJobLabel: string(vmiMigration.UID),
			},
		})
		if err != nil {
			err = fmt.Errorf("failed to create label selector for migration job UID %s: %w", vmiMigration.UID, err)
			klog.Error(err)
			return err
		}

		pods, err := c.podsLister.Pods(vmiMigration.Namespace).List(selector)
		if err != nil {
			err = fmt.Errorf("failed to list pods with migration job UID %s: %w", vmiMigration.UID, err)
			klog.Error(err)
			return err
		}

		if len(pods) > 0 {
			targetPod := pods[0]
			// During MigrationScheduling phase, use vmi.Status.NodeName if SourceNode is empty
			// because vmi.Status.MigrationState may not be fully synchronized yet
			sourceNode := srcNodeName
			if sourceNode == "" {
				sourceNode = vmi.Status.NodeName
			}

			if sourceNode == "" || targetPod.Spec.NodeName == "" || sourceNode == targetPod.Spec.NodeName {
				klog.Warningf("VM pod %s/%s migration setup skipped, source node: %s, target node: %s (migration job UID: %s)",
					targetPod.Namespace, targetPod.Name, sourceNode, targetPod.Spec.NodeName, vmiMigration.UID)
				return nil
			}

			klog.Infof("VM pod %s/%s is migrating from %s to %s (migration job UID: %s)",
				targetPod.Namespace, targetPod.Name, sourceNode, targetPod.Spec.NodeName, vmiMigration.UID)

			for _, portName := range portNames {
				if err := c.OVNNbClient.SetLogicalSwitchPortMigrateOptions(portName, sourceNode, targetPod.Spec.NodeName); err != nil {
					err = fmt.Errorf("failed to set migrate options for VM pod lsp %s: %w", portName, err)
					klog.Error(err)
					return err
				}
				klog.Infof("successfully set migrate options for lsp %s from %s to %s", portName, sourceNode, targetPod.Spec.NodeName)
			}
		} else {
			klog.Warningf("target pod not yet created for migration job UID %s in phase %s, waiting for pod creation",
				vmiMigration.UID, vmiMigration.Status.Phase)
			return nil
		}
	case kubevirtv1.MigrationSucceeded:
		for _, portName := range portNames {
			klog.Infof("migrate end reset options for lsp %s from %s to %s, migrated succeed", portName, srcNodeName, targetNodeName)
			if err := c.OVNNbClient.ResetLogicalSwitchPortMigrateOptions(portName, srcNodeName, targetNodeName, false); err != nil {
				err = fmt.Errorf("failed to clean migrate options for lsp %s, %w", portName, err)
				klog.Error(err)
				return err
			}
		}
	case kubevirtv1.MigrationFailed:
		for _, portName := range portNames {
			klog.Infof("migrate end reset options for lsp %s from %s to %s, migrated fail", portName, srcNodeName, targetNodeName)
			if err := c.OVNNbClient.ResetLogicalSwitchPortMigrateOptions(portName, srcNodeName, targetNodeName, true); err != nil {
				err = fmt.Errorf("failed to clean migrate options for lsp %s, %w", portName, err)
				klog.Error(err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) isKubevirtCRDInstalled() (bool, error) {
	return apiResourceExists(c.config.KubevirtClient.Discovery(),
		kubevirtv1.GroupVersion.String(),
		util.KindVirtualMachine,
		util.KindVirtualMachineInstance,
		util.KindVirtualMachineInstanceMigration,
	)
}

func (c *Controller) StartKubevirtInformerFactory(ctx context.Context, kubevirtInformerFactory informer.KubeVirtInformerFactory) {
	ticker := time.NewTicker(10 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ok, err := c.isKubevirtCRDInstalled()
				if err != nil {
					klog.Errorf("checking kubevirt CRD exists: %v", err)
					continue
				}
				if ok {
					klog.Info("Start kubevirt informer")
					vmiMigrationInformer := kubevirtInformerFactory.VirtualMachineInstanceMigration()
					vmInformer := kubevirtInformerFactory.VirtualMachine()

					kubevirtInformerFactory.Start(ctx.Done())
					if !cache.WaitForCacheSync(ctx.Done(), vmiMigrationInformer.HasSynced, vmInformer.HasSynced) {
						util.LogFatalAndExit(nil, "failed to wait for kubevirt caches to sync")
					}

					if c.config.EnableLiveMigrationOptimize {
						if _, err := vmiMigrationInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
							AddFunc:    c.enqueueAddVMIMigration,
							UpdateFunc: c.enqueueUpdateVMIMigration,
						}); err != nil {
							util.LogFatalAndExit(err, "failed to add VMI Migration event handler")
						}
					}

					if _, err := vmInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
						DeleteFunc: c.enqueueDeleteVM,
					}); err != nil {
						util.LogFatalAndExit(err, "failed to add vm event handler")
					}
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}
