package controller

import (
	"context"
	"fmt"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	kubevirtv1 "kubevirt.io/api/core/v1"

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

	if vmi.Status.MigrationState == nil {
		klog.Infof("VMI instance %s migration state is nil, skipping", key)
		return nil
	}

	if vmi.Status.MigrationState.SourcePod == "" {
		klog.Infof("VMI instance %s source pod is nil, skipping", key)
		return nil
	}

	// use VirtualMachineInsance's MigrationState because VirtualMachineInsanceMigration's MigrationState is not updated util migration finished
	klog.Infof("current vmiMigration %s status %s, target Node %s, source Node %s, target Pod %s, source Pod %s", key,
		vmiMigration.Status.Phase,
		vmi.Status.MigrationState.TargetNode,
		vmi.Status.MigrationState.SourceNode,
		vmi.Status.MigrationState.TargetPod,
		vmi.Status.MigrationState.SourcePod)

	sourcePodName := vmi.Status.MigrationState.SourcePod
	sourcePod, err := c.config.KubeClient.CoreV1().Pods(namespace).Get(context.TODO(), sourcePodName, metav1.GetOptions{})
	if err != nil {
		err = fmt.Errorf("failed to get source pod %s, %w", sourcePodName, err)
		klog.Error(err)
		return err
	}

	podNets, err := c.getPodKubeovnNets(sourcePod)
	if err != nil {
		err = fmt.Errorf("failed to get pod nets %w", err)
		klog.Error(err)
		return err
	}

	for _, podNet := range podNets {
		// Skip non-OVN subnets that don't create OVN logical switch ports
		if !isOvnSubnet(podNet.Subnet) {
			continue
		}

		portName := ovs.PodNameToPortName(vmiMigration.Spec.VMIName, vmiMigration.Namespace, podNet.ProviderName)
		srcNodeName := vmi.Status.MigrationState.SourceNode
		targetNodeName := vmi.Status.MigrationState.TargetNode
		switch vmiMigration.Status.Phase {
		case kubevirtv1.MigrationSucceeded:
			klog.Infof("migrate end reset options for lsp %s from %s to %s, migrated succeed", portName, srcNodeName, targetNodeName)
			if err := c.OVNNbClient.ResetLogicalSwitchPortMigrateOptions(portName, srcNodeName, targetNodeName, false); err != nil {
				err = fmt.Errorf("failed to clean migrate options for lsp %s, %w", portName, err)
				klog.Error(err)
				return err
			}
		case kubevirtv1.MigrationFailed:
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

func (c *Controller) isKubevirtCRDInstalled() bool {
	for _, crd := range util.KubeVirtCRD {
		_, err := c.config.ExtClient.ApiextensionsV1().CustomResourceDefinitions().Get(context.TODO(), crd, metav1.GetOptions{})
		if err != nil {
			return false
		}
	}
	klog.Info("Found KubeVirt CRDs")
	return true
}

func (c *Controller) StartKubevirtInformerFactory(ctx context.Context, kubevirtInformerFactory informer.KubeVirtInformerFactory) {
	ticker := time.NewTicker(10 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if c.isKubevirtCRDInstalled() {
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
