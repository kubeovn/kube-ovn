package controller

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	kubevirtv1 "kubevirt.io/api/core/v1"
	kubevirtController "kubevirt.io/kubevirt/pkg/controller"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddVMIMigration(obj interface{}) {
	key := cache.MetaObjectToName(obj.(*kubevirtv1.VirtualMachineInstanceMigration)).String()
	klog.Infof("enqueue add VMI migration %s", key)
	c.addOrUpdateVMIMigrationQueue.Add(key)
}

func (c *Controller) enqueueUpdateVMIMigration(oldObj, newObj interface{}) {
	oldVmi := oldObj.(*kubevirtv1.VirtualMachineInstanceMigration)
	newVmi := newObj.(*kubevirtv1.VirtualMachineInstanceMigration)

	if !newVmi.DeletionTimestamp.IsZero() ||
		oldVmi.Status.Phase != newVmi.Status.Phase {
		key := cache.MetaObjectToName(newVmi).String()
		klog.Infof("enqueue update VMI migration %s", key)
		c.addOrUpdateVMIMigrationQueue.Add(key)
	}
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

	needAllocatePodNets := needAllocateSubnets(sourcePod, podNets)
	for _, podNet := range needAllocatePodNets {
		portName := ovs.PodNameToPortName(vmiMigration.Spec.VMIName, vmiMigration.Namespace, podNet.ProviderName)
		srcNodeName := vmi.Status.MigrationState.SourceNode
		targetNodeName := vmi.Status.MigrationState.TargetNode
		switch vmiMigration.Status.Phase {
		case kubevirtv1.MigrationRunning:
			klog.Infof("migrate start set options for lsp %s from %s to %s", portName, srcNodeName, targetNodeName)
			if err := c.OVNNbClient.SetLogicalSwitchPortMigrateOptions(portName, srcNodeName, targetNodeName); err != nil {
				err = fmt.Errorf("failed to set migrate options for lsp %s, %w", portName, err)
				klog.Error(err)
				return err
			}
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

func (c *Controller) isVMIMigrationCRDInstalled() bool {
	_, err := c.config.ExtClient.ApiextensionsV1().CustomResourceDefinitions().Get(context.TODO(), "virtualmachineinstancemigrations.kubevirt.io", metav1.GetOptions{})
	if err != nil {
		return false
	}
	klog.Info("Found KubeVirt VMI Migration CRD")
	return true
}

func (c *Controller) StartMigrationInformerFactory(ctx context.Context, kubevirtInformerFactory kubevirtController.KubeInformerFactory) {
	ticker := time.NewTicker(10 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if c.isVMIMigrationCRDInstalled() {
					klog.Info("Start VMI migration informer")
					kubevirtInformerFactory.Start(ctx.Done())
					if !cache.WaitForCacheSync(ctx.Done(), c.vmiMigrationSynced) {
						util.LogFatalAndExit(nil, "failed to wait for vmi migration caches to sync")
					}
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}
