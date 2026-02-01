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
	vmiMigration := obj.(*kubevirtv1.VirtualMachineInstanceMigration)
	key := cache.MetaObjectToName(vmiMigration).String()
	klog.Infof("enqueue add VMI migration %s", key)
	c.addOrUpdateVMIMigrationQueue.Add(key)
}

func (c *Controller) enqueueUpdateVMIMigration(oldObj, newObj any) {
	oldVmiMigration := oldObj.(*kubevirtv1.VirtualMachineInstanceMigration)
	newVmiMigration := newObj.(*kubevirtv1.VirtualMachineInstanceMigration)

	if !newVmiMigration.DeletionTimestamp.IsZero() ||
		oldVmiMigration.Status.Phase != newVmiMigration.Status.Phase {
		key := cache.MetaObjectToName(newVmiMigration).String()
		klog.Infof("enqueue update VMI migration %s (phase: %s -> %s)",
			key, oldVmiMigration.Status.Phase, newVmiMigration.Status.Phase)
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

// handleAddOrUpdateVMIMigration handles VirtualMachineInstanceMigration events.
//
// Design Decision: This handler uses VMIMigration as both trigger and data source.
// - Trigger: VMIMigration.Status.Phase changes
// - Data: VMIMigration.Status.MigrationState (source/target node info)
//
// Why VMIMigration instead of VMI?
//   - Consistency: trigger and data come from the same resource, avoiding sync issues
//   - Snapshot: VMIMigration.Status.MigrationState is a snapshot of the migration,
//     won't be overwritten when a new migration starts (unlike VMI.Status.MigrationState)
//   - Simpler: no need for UID validation to check if VMI state matches current migration
//
// Trade-off: VMIMigration.Status.MigrationState is synced from VMI with slight delay.
// In MigrationScheduling phase, it may not be populated yet, requiring retry.
//
// Future improvement: If real-time responsiveness is critical, consider refactoring
// to watch VMI directly (like the release-1.12-mc branch does with Pod events).
// This would eliminate the sync delay but require more complex state management.
//
// KubeVirt ensures only ONE active migration per VMI at any time.
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

	migrationUID := vmiMigration.UID
	vmiName := vmiMigration.Spec.VMIName
	phase := vmiMigration.Status.Phase

	// Log migration lifecycle events for debugging
	switch phase {
	case kubevirtv1.MigrationPending:
		klog.Infof(">>> [MIGRATION START] Migration %s (UID: %s) for VMI %s/%s - Phase: Pending",
			key, migrationUID, namespace, vmiName)
	case kubevirtv1.MigrationSucceeded:
		klog.Infof("<<< [MIGRATION END] Migration %s (UID: %s) for VMI %s/%s - SUCCEEDED",
			key, migrationUID, namespace, vmiName)
	case kubevirtv1.MigrationFailed:
		klog.Infof("<<< [MIGRATION END] Migration %s (UID: %s) for VMI %s/%s - FAILED",
			key, migrationUID, namespace, vmiName)
	default:
		klog.V(3).Infof("--- [MIGRATION PROGRESS] Migration %s (UID: %s) for VMI %s/%s - Phase: %s",
			key, migrationUID, namespace, vmiName, phase)
	}

	// Use VMIMigration.Status.MigrationState as the data source
	// This is populated by KubeVirt controller, may have slight delay in early phases
	migrationState := vmiMigration.Status.MigrationState
	if migrationState == nil {
		klog.V(3).Infof("Migration %s (UID: %s) - MigrationState not yet populated, waiting for KubeVirt",
			key, migrationUID)
		return nil
	}

	srcNodeName := migrationState.SourceNode
	targetNodeName := migrationState.TargetNode
	if srcNodeName == "" || targetNodeName == "" {
		klog.V(3).Infof("Migration %s (UID: %s) - MigrationState incomplete (source: %q, target: %q), waiting",
			key, migrationUID, srcNodeName, targetNodeName)
		return nil
	}

	portName := ovs.PodNameToPortName(vmiName, namespace, util.OvnProvider)
	klog.Infof("Migration %s (UID: %s) - source: %s, target: %s, port: %s",
		key, migrationUID, srcNodeName, targetNodeName, portName)

	switch phase {
	case kubevirtv1.MigrationScheduling:
		if srcNodeName == targetNodeName {
			klog.Warningf("Migration %s (UID: %s) - Source and target are same node %s, skipping",
				key, migrationUID, srcNodeName)
			return nil
		}

		// Check for residual migration options on LSP. If activation-strategy exists,
		// it means a previous migration's cleanup was not completed. Possible causes:
		// 1. Controller restarted and missed the MigrationSucceeded/Failed event
		// 2. Previous ResetLogicalSwitchPortMigrateOptions call failed
		// Clean the residual options before setting new ones.
		lsp, err := c.OVNNbClient.GetLogicalSwitchPort(portName, false)
		if err != nil {
			klog.Errorf("Migration %s (UID: %s) - Failed to get LSP %s: %v",
				key, migrationUID, portName, err)
			return err
		}
		if lsp != nil && lsp.Options != nil {
			if _, hasActivationStrategy := lsp.Options["activation-strategy"]; hasActivationStrategy {
				klog.Warningf("Migration %s (UID: %s) - LSP %s has residual activation-strategy from incomplete cleanup, cleaning before setting new options",
					key, migrationUID, portName)
				if err := c.OVNNbClient.CleanLogicalSwitchPortMigrateOptions(portName); err != nil {
					klog.Errorf("Migration %s (UID: %s) - Failed to clean residual LSP %s options: %v",
						key, migrationUID, portName, err)
					return err
				}
				klog.Infof("Migration %s (UID: %s) - Cleaned residual LSP %s options, proceeding with new migration",
					key, migrationUID, portName)
				// Continue to set new options instead of returning error to avoid unnecessary retry
			}
		}

		klog.Infof(">>> [LSP SET] Migration %s (UID: %s) - Setting LSP %s: %s -> %s",
			key, migrationUID, portName, srcNodeName, targetNodeName)
		if err := c.OVNNbClient.SetLogicalSwitchPortMigrateOptions(portName, srcNodeName, targetNodeName); err != nil {
			klog.Errorf("Migration %s (UID: %s) - Failed to set LSP %s migrate options: %v",
				key, migrationUID, portName, err)
			return err
		}
		klog.Infof(">>> [LSP SET OK] Migration %s (UID: %s) - LSP %s configured", key, migrationUID, portName)

	case kubevirtv1.MigrationSucceeded:
		klog.Infof("<<< [LSP RESET] Migration %s (UID: %s) - Resetting LSP %s to target %s",
			key, migrationUID, portName, targetNodeName)
		if err := c.OVNNbClient.ResetLogicalSwitchPortMigrateOptions(portName, srcNodeName, targetNodeName, false); err != nil {
			klog.Errorf("Migration %s (UID: %s) - Failed to reset LSP %s: %v",
				key, migrationUID, portName, err)
			return err
		}
		klog.Infof("<<< [LSP RESET OK] Migration %s (UID: %s) - LSP %s reset to target", key, migrationUID, portName)

	case kubevirtv1.MigrationFailed:
		klog.Infof("<<< [LSP RESET] Migration %s (UID: %s) - Resetting LSP %s to source %s (rollback)",
			key, migrationUID, portName, srcNodeName)
		if err := c.OVNNbClient.ResetLogicalSwitchPortMigrateOptions(portName, srcNodeName, targetNodeName, true); err != nil {
			klog.Errorf("Migration %s (UID: %s) - Failed to reset LSP %s: %v",
				key, migrationUID, portName, err)
			return err
		}
		klog.Infof("<<< [LSP RESET OK] Migration %s (UID: %s) - LSP %s rolled back to source", key, migrationUID, portName)
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
