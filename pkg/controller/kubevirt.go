package controller

import (
	"context"
	"fmt"
	"reflect"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	kubevirtv1 "kubevirt.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

func (c *Controller) enqueueAddVmiMigration(obj interface{}) {
	var (
		key string
		err error
	)

	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}

	klog.Info("enqueue add vmi migration %s ", key)
	c.addOrUpdateVmiMigrationQueue.Add(key)
}

func (c *Controller) enqueueUpdateVmiMigration(oldObj, newObj interface{}) {
	oldVmi := oldObj.(*kubevirtv1.VirtualMachineInstanceMigration)
	newVmi := newObj.(*kubevirtv1.VirtualMachineInstanceMigration)

	if !newVmi.DeletionTimestamp.IsZero() ||
		!reflect.DeepEqual(oldVmi.Status.Phase, newVmi.Status.Phase) {
		// TODO:// label VpcExternalLabel replace with spec enable external
		key, err := cache.MetaNamespaceKeyFunc(newObj)
		if err != nil {
			utilruntime.HandleError(err)
			return
		}
		klog.Infof("enqueue update vmi migration %s", key)

		c.addOrUpdateVmiMigrationQueue.Add(key)
	}
}

func (c *Controller) handleAddOrUpdateVmiMigration(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	vmiMigration, err := c.config.KubevirtClient.VirtualMachineInstanceMigration(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("failed to get VMI migration by key %s: %v", key, err))
		return err
	}

	if vmiMigration.Status.MigrationState == nil {
		klog.Infof("vmiMigration %s migration state is nil, skip", key)
		return nil
	}

	vmi, err := c.config.KubevirtClient.VirtualMachineInstance(namespace).Get(context.TODO(), vmiMigration.Spec.VMIName, metav1.GetOptions{})
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("failed to get VMI by name %s: %v", vmiMigration.Spec.VMIName, err))
		return err
	}

	if vmi.Status.MigrationState == nil {
		klog.Infof("vmi instance %s migration state is nil, skip", key)
		return nil
	}

	// use vmi's migration state because vmiMigration's migration state is not updated util migration finished
	klog.Infof("Current vmiMigration %s status %s, targetNode %s, sourceNode %s ", key,
		vmiMigration.Status.Phase,
		vmi.Status.MigrationState.TargetNode,
		vmi.Status.MigrationState.SourceNode)

	portName := ovs.PodNameToPortName(vmiMigration.Spec.VMIName, vmiMigration.Namespace, "ovn")
	srcNodeName := vmi.Status.MigrationState.SourceNode
	targetNodeName := vmi.Status.MigrationState.TargetNode
	switch vmiMigration.Status.Phase {
	// when migration is targetready or running, set migrate options for lsp
	case kubevirtv1.MigrationTargetReady:
		klog.Infof("migrate start set options for lsp %s from %s to %s", portName, srcNodeName, targetNodeName)
		if err := c.OVNNbClient.SetLogicalSwitchPortMigrateOptions(portName, srcNodeName, targetNodeName); err != nil {
			err = fmt.Errorf("failed to set migrate options for lsp %s, %w", portName, err)
			klog.Error(err)
			return err
		}
	case kubevirtv1.MigrationRunning:
		klog.Infof("migrate start set options for lsp %s from %s to %s", portName, srcNodeName, targetNodeName)
		if err := c.OVNNbClient.SetLogicalSwitchPortMigrateOptions(portName, srcNodeName, targetNodeName); err != nil {
			err = fmt.Errorf("failed to set migrate options for lsp %s, %w", portName, err)
			klog.Error(err)
			return err
		}
	case kubevirtv1.MigrationSucceeded:
		klog.Infof("migrate end reset options for lsp %s from %s to %s, migrated fail: %t", portName, srcNodeName, targetNodeName, false)
		if err := c.OVNNbClient.ResetLogicalSwitchPortMigrateOptions(portName, srcNodeName, targetNodeName, false); err != nil {
			err = fmt.Errorf("failed to clean migrate options for lsp %s, %w", portName, err)
			klog.Error(err)
			return err
		}
	case kubevirtv1.MigrationFailed:
		klog.Infof("migrate end reset options for lsp %s from %s to %s, migrated fail: %t", portName, srcNodeName, targetNodeName, true)
		if err := c.OVNNbClient.ResetLogicalSwitchPortMigrateOptions(portName, srcNodeName, targetNodeName, true); err != nil {
			err = fmt.Errorf("failed to clean migrate options for lsp %s, %w", portName, err)
			klog.Error(err)
			return err
		}
	}
	return nil
}
