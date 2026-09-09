package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	kubevirtv1 "kubevirt.io/api/core/v1"

	"github.com/kubeovn/kube-ovn/pkg/informer"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// value kubevirt sets on kubevirtv1.AppLabel for the pods a VM runs in
const virtLauncherAppLabel = "virt-launcher"

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

	vmi, err := c.config.KubevirtClient.VirtualMachineInstance(namespace).Get(context.TODO(), vmiMigration.Spec.VMIName, metav1.GetOptions{})
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("failed to get VMI by name %s: %w", vmiMigration.Spec.VMIName, err))
		return err
	}

	// use VirtualMachineInstance's MigrationState because VirtualMachineInstanceMigration's MigrationState is not updated until migration finished
	var srcNodeName, targetNodeName string
	if vmi.Status.MigrationState != nil && vmi.Status.MigrationState.MigrationUID == vmiMigration.UID {
		klog.Infof("current vmiMigration %s status %s, target Node %s, source Node %s, target Pod %s, source Pod %s", key,
			vmiMigration.Status.Phase,
			vmi.Status.MigrationState.TargetNode,
			vmi.Status.MigrationState.SourceNode,
			vmi.Status.MigrationState.TargetPod,
			vmi.Status.MigrationState.SourcePod)
		srcNodeName = vmi.Status.MigrationState.SourceNode
		targetNodeName = vmi.Status.MigrationState.TargetNode
	} else {
		if vmi.Status.MigrationState != nil {
			klog.Infof("current vmiMigration %s status %s, vmi MigrationState is stale", key, vmiMigration.Status.Phase)
		} else {
			klog.Infof("current vmiMigration %s status %s, vmi MigrationState is nil", key, vmiMigration.Status.Phase)
		}
		// A migration that fails before its target pod is ready never reaches the VMI
		// migration state, so the nodes it used have to be recovered from the target pod.
		if vmiMigration.Status.Phase == kubevirtv1.MigrationFailed {
			if srcNodeName, targetNodeName, err = c.recoverVMIMigrationNodes(vmiMigration, vmi); err != nil {
				return err
			}
		}
		// A succeeded migration with a stale state was taken over by a newer one, which pinned
		// the ports itself; a failed one still has options of its own to release below
		if (srcNodeName == "" || targetNodeName == "") && vmiMigration.Status.Phase == kubevirtv1.MigrationSucceeded {
			klog.V(3).Infof("VirtualMachineInstanceMigration %s succeeded but VMI migration state is stale or nil, skipping", key)
			return nil
		}
	}

	lsps, err := c.OVNNbClient.ListNormalLogicalSwitchPorts(c.config.EnableExternalVpc, map[string]string{"pod": fmt.Sprintf("%s/%s", vmi.Namespace, vmi.Name)})
	if err != nil {
		klog.Errorf("failed to list logical switch ports for vmi %s/%s, %v", vmi.Namespace, vmi.Name, err)
		return err
	}

	portNames := make([]string, 0, len(lsps))
	for _, lsp := range lsps {
		portNames = append(portNames, lsp.Name)
	}

	klog.Infof("collected port names of vmi %s, port names are %v", vmi.Name, strings.Join(portNames, ", "))

	switch vmiMigration.Status.Phase {
	// Re-asserted on every phase before the migration, a no-op while the options are intact.
	// Not MigrationRunning: re-adding rarp after the guest RARP would block the target port.
	case kubevirtv1.MigrationScheduling, kubevirtv1.MigrationScheduled,
		kubevirtv1.MigrationPreparingTarget, kubevirtv1.MigrationTargetReady:
		targetPod, err := c.vmiMigrationTargetPod(vmiMigration)
		if err != nil {
			return err
		}
		if targetPod == nil {
			klog.Warningf("target pod not yet created for migration job UID %s in phase %s, waiting for pod creation",
				vmiMigration.UID, vmiMigration.Status.Phase)
			return nil
		}

		// Use vmi.Status.NodeName if SourceNode is empty because vmi.Status.MigrationState
		// only becomes authoritative for this migration once kubevirt hands off to the target
		sourceNode := srcNodeName
		if sourceNode == "" {
			sourceNode = vmi.Status.NodeName
		}

		if sourceNode == "" || targetPod.Spec.NodeName == "" || sourceNode == targetPod.Spec.NodeName {
			klog.Warningf("VM pod %s/%s migration setup skipped, source node: %s, target node: %s (migration job UID: %s)",
				targetPod.Namespace, targetPod.Name, sourceNode, targetPod.Spec.NodeName, vmiMigration.UID)
			// Scheduling produces no further event, so requeue instead of dropping it
			// https://github.com/kubeovn/kube-ovn/issues/6823
			if sourceNode == "" || targetPod.Spec.NodeName == "" {
				return fmt.Errorf("VM pod %s/%s migration setup deferred, source node %q, target node %q not ready yet (migration job UID %s)",
					targetPod.Namespace, targetPod.Name, sourceNode, targetPod.Spec.NodeName, vmiMigration.UID)
			}
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
	case kubevirtv1.MigrationSucceeded, kubevirtv1.MigrationFailed:
		// A controller restart replays terminal migrations: resetting ports a newer migration
		// already took over would strip its options and strand the new target port
		hasNewerMigration, err := c.hasNewerActiveVMIMigration(vmiMigration)
		if err != nil {
			return err
		}
		if hasNewerMigration {
			klog.Infof("skip resetting migrate options of vmi %s/%s for the %s migration %s, a newer migration is in progress",
				vmi.Namespace, vmi.Name, vmiMigration.Status.Phase, key)
			return nil
		}
		migrateFailed := vmiMigration.Status.Phase == kubevirtv1.MigrationFailed
		// a failed migration whose target pod is already gone has no node pair to roll back to,
		// so the options it wrote are dropped instead of being reset
		if migrateFailed && (srcNodeName == "" || targetNodeName == "") {
			for _, portName := range portNames {
				klog.Infof("migrate end clean options for lsp %s, migration failed with unknown nodes", portName)
				if err := c.OVNNbClient.CleanLogicalSwitchPortMigrateOptions(portName); err != nil {
					err = fmt.Errorf("failed to clean migrate options for lsp %s, %w", portName, err)
					klog.Error(err)
					return err
				}
			}
			return nil
		}
		for _, portName := range portNames {
			klog.Infof("migrate end reset options for lsp %s from %s to %s, migration %s", portName, srcNodeName, targetNodeName, vmiMigration.Status.Phase)
			if err := c.OVNNbClient.ResetLogicalSwitchPortMigrateOptions(portName, srcNodeName, targetNodeName, migrateFailed); err != nil {
				err = fmt.Errorf("failed to clean migrate options for lsp %s, %w", portName, err)
				klog.Error(err)
				return err
			}
		}
	}
	return nil
}

// vmiMigrationTargetPod returns the target virt-launcher pod created for the given migration
// job, or nil when it does not exist (yet).
func (c *Controller) vmiMigrationTargetPod(vmiMigration *kubevirtv1.VirtualMachineInstanceMigration) (*corev1.Pod, error) {
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			kubevirtv1.MigrationJobLabel: string(vmiMigration.UID),
			kubevirtv1.AppLabel:          virtLauncherAppLabel,
		},
	})
	if err != nil {
		err = fmt.Errorf("failed to create label selector for migration job UID %s: %w", vmiMigration.UID, err)
		klog.Error(err)
		return nil, err
	}

	pods, err := c.podsLister.Pods(vmiMigration.Namespace).List(selector)
	if err != nil {
		err = fmt.Errorf("failed to list target launcher pods with migration job UID %s: %w", vmiMigration.UID, err)
		klog.Error(err)
		return nil, err
	}
	if len(pods) == 0 {
		return nil, nil
	}
	return pods[0], nil
}

// vmiMigrationHasTargetPod reports whether the migration's target launcher pod exists.
func (c *Controller) vmiMigrationHasTargetPod(vmiMigration *kubevirtv1.VirtualMachineInstanceMigration) (bool, error) {
	targetPod, err := c.vmiMigrationTargetPod(vmiMigration)
	if err != nil {
		return false, err
	}
	return targetPod != nil, nil
}

// recoverVMIMigrationNodes rebuilds the node pair of a failed migration that never made it
// into the VMI migration state.
func (c *Controller) recoverVMIMigrationNodes(vmiMigration *kubevirtv1.VirtualMachineInstanceMigration, vmi *kubevirtv1.VirtualMachineInstance) (string, string, error) {
	targetPod, err := c.vmiMigrationTargetPod(vmiMigration)
	if err != nil {
		return "", "", err
	}
	if targetPod == nil || targetPod.Spec.NodeName == "" ||
		vmi.Status.NodeName == "" || vmi.Status.NodeName == targetPod.Spec.NodeName {
		return "", "", nil
	}

	klog.Infof("recovered nodes %s -> %s of failed migration %s/%s from target pod %s",
		vmi.Status.NodeName, targetPod.Spec.NodeName, vmiMigration.Namespace, vmiMigration.Name, targetPod.Name)
	return vmi.Status.NodeName, targetPod.Spec.NodeName, nil
}

// listVMIMigrations returns the cached migrations of the given VM. The cache is empty while
// kubevirt is not installed, because its informer only starts once the CRDs exist.
func (c *Controller) listVMIMigrations(vmiKey string) []*kubevirtv1.VirtualMachineInstanceMigration {
	indexer := c.kubevirtInformerFactory.VirtualMachineInstanceMigration().GetIndexer()
	objs, err := indexer.ByIndex(informer.ByVMINameIndex, vmiKey)
	if err != nil {
		// the index is registered when the informer is created, so this should never happen
		klog.Errorf("failed to list migrations of vmi %s: %v", vmiKey, err)
		return nil
	}

	migrations := make([]*kubevirtv1.VirtualMachineInstanceMigration, 0, len(objs))
	for _, obj := range objs {
		if migration, ok := obj.(*kubevirtv1.VirtualMachineInstanceMigration); ok {
			migrations = append(migrations, migration)
		}
	}
	return migrations
}

// isCurrentVMIPod reports whether the pod is the one kubevirt runs the VM on: the most recently
// created pod of the VM on the node the VMI is on, mirroring kubevirt's own CurrentVMIPod.
func (c *Controller) isCurrentVMIPod(pod *corev1.Pod, vmiNodeName, vmName string) (bool, error) {
	onVMINode := func(p *corev1.Pod) bool {
		// a pod on another node belongs to a migration preparing a new target
		return vmiNodeName == "" || vmiNodeName == p.Spec.NodeName
	}
	if !onVMINode(pod) {
		return false, nil
	}

	siblings, err := c.podsLister.Pods(pod.Namespace).List(labels.Everything())
	if err != nil {
		return false, fmt.Errorf("failed to list pods in namespace %s: %w", pod.Namespace, err)
	}
	for _, other := range siblings {
		if other.Name == pod.Name || !onVMINode(other) {
			continue
		}
		if isVM, name := isVMPod(other); !isVM || name != vmName {
			continue
		}
		if pod.CreationTimestamp.Before(&other.CreationTimestamp) {
			return false, nil
		}
	}

	return true, nil
}

// podFromEarlierVMI reports whether the pod was created for a previous incarnation of the VM,
// which a deletion handled after the VM restarted refers to. Only a positive match counts.
func podFromEarlierVMI(pod *corev1.Pod, vmi *kubevirtv1.VirtualMachineInstance) bool {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == util.KindVirtualMachineInstance &&
			strings.HasPrefix(owner.APIVersion, kubevirtv1.SchemeGroupVersion.Group+"/") {
			return owner.UID != "" && vmi.UID != "" && owner.UID != vmi.UID
		}
	}
	return false
}

// hasActiveVMIMigrationWithTarget reports whether a migration of the given VM already has a target
// pod, and therefore owns the migrate options of the VM ports.
func (c *Controller) hasActiveVMIMigrationWithTarget(namespace, vmName string) (bool, error) {
	for _, migration := range c.listVMIMigrations(fmt.Sprintf("%s/%s", namespace, vmName)) {
		if migration.IsFinal() {
			continue
		}
		hasTargetPod, err := c.vmiMigrationHasTargetPod(migration)
		if err != nil {
			return false, err
		}
		if hasTargetPod {
			klog.Infof("migration %s/%s is still in phase %s with a target pod", migration.Namespace, migration.Name, migration.Status.Phase)
			return true, nil
		}
	}
	return false, nil
}

// deletedPodOwnsMigrateOptions reports whether the deleted pod is the one the VM runs on or the
// target of a running migration, the only pods whose removal releases the port migrate options.
func (c *Controller) deletedPodOwnsMigrateOptions(pod *corev1.Pod, vmName string) (bool, error) {
	vmi, err := c.config.KubevirtClient.VirtualMachineInstance(pod.Namespace).Get(context.Background(), vmName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// a stopped or restarting VM has no vmi, so nothing can be migrating and the ports
			// have to be unpinned for it to come back on any node
			klog.Infof("vmi %s/%s is gone, the migrate options of its ports may be cleaned up", pod.Namespace, vmName)
			return true, nil
		}
		return false, fmt.Errorf("failed to get vmi %s/%s: %w", pod.Namespace, vmName, err)
	}

	// options that predate a restart are stale unless the new vmi is migrating and owns them
	if podFromEarlierVMI(pod, vmi) {
		hasActiveMigration, err := c.hasActiveVMIMigrationWithTarget(pod.Namespace, vmName)
		if err != nil {
			return false, err
		}
		if hasActiveMigration {
			return false, nil
		}
		klog.Infof("pod %s/%s belongs to an earlier vmi %s, its migrate options may be cleaned up", pod.Namespace, pod.Name, vmName)
		return true, nil
	}

	isCurrent, err := c.isCurrentVMIPod(pod, vmi.Status.NodeName, vmName)
	if err != nil {
		return false, err
	}
	if isCurrent {
		klog.Infof("pod %s/%s is the pod vmi %s runs on", pod.Namespace, pod.Name, vmName)
		return true, nil
	}

	// a migration target runs on another node until the VM moves, so it is matched by its job label
	podMigrationUID := pod.Labels[kubevirtv1.MigrationJobLabel]
	if podMigrationUID == "" {
		return false, nil
	}
	for _, migration := range c.listVMIMigrations(fmt.Sprintf("%s/%s", pod.Namespace, vmName)) {
		if !migration.IsFinal() && podMigrationUID == string(migration.UID) {
			klog.Infof("pod %s/%s is the target of migration %s", pod.Namespace, pod.Name, migration.Name)
			return true, nil
		}
	}

	return false, nil
}

// hasNewerActiveVMIMigration reports whether a migration of the same VMI created no earlier than
// the given one has a target pod, and therefore owns the migrate options of the VMI ports.
func (c *Controller) hasNewerActiveVMIMigration(vmiMigration *kubevirtv1.VirtualMachineInstanceMigration) (bool, error) {
	vmiKey := fmt.Sprintf("%s/%s", vmiMigration.Namespace, vmiMigration.Spec.VMIName)
	for _, migration := range c.listVMIMigrations(vmiKey) {
		if migration.UID == vmiMigration.UID || migration.IsFinal() {
			continue
		}
		// Timestamps have a one second granularity: a migration created in the same second
		// cannot be ruled out as the newer one, so assume it is and leave its options alone
		if migration.CreationTimestamp.Before(&vmiMigration.CreationTimestamp) {
			continue
		}
		// a migration that has no target pod yet cannot have written any options
		hasTargetPod, err := c.vmiMigrationHasTargetPod(migration)
		if err != nil {
			return false, err
		}
		if !hasTargetPod {
			continue
		}
		klog.Infof("migration %s/%s of vmi %s is still in phase %s with a target pod", migration.Namespace, migration.Name, vmiKey, migration.Status.Phase)
		return true, nil
	}

	return false, nil
}

func (c *Controller) isKubevirtCRDInstalled() (bool, error) {
	return util.APIResourceExists(c.config.KubevirtClient.Discovery(),
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
