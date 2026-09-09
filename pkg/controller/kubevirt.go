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

// enqueueVMIMigrationForBoundLauncher requeues a migration once its target virt-launcher is bound.
// With hotplug volumes the VMIM stays Pending until that launcher is ready, so no phase event follows.
func (c *Controller) enqueueVMIMigrationForBoundLauncher(oldPod, newPod *corev1.Pod) {
	if !c.config.EnableLiveMigrationOptimize || oldPod.Spec.NodeName != "" || newPod.Spec.NodeName == "" ||
		newPod.Labels[kubevirtv1.AppLabel] != "virt-launcher" {
		return
	}
	if name := newPod.Annotations[kubevirtv1.MigrationJobNameAnnotation]; name != "" {
		c.addOrUpdateVMIMigrationQueue.Add(newPod.Namespace + "/" + name)
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
	// MigrationState may still be nil before target virt-launcher pod handoff. Pending and
	// Scheduling can derive both nodes without it, so allow network setup to proceed. Failed
	// migrations also need to proceed so options configured while Pending can be cleaned up.
	if vmiMigration.Status.MigrationState == nil &&
		vmiMigration.Status.Phase != kubevirtv1.MigrationPending &&
		vmiMigration.Status.Phase != kubevirtv1.MigrationScheduling &&
		vmiMigration.Status.Phase != kubevirtv1.MigrationFailed {
		klog.V(3).Infof("VirtualMachineInstanceMigration %s migration state is nil, skipping", key)
		return nil
	}
	if vmiMigration.Status.Phase == kubevirtv1.MigrationFailed {
		// Only this handler installs migration options, and its queue has a single worker, so a
		// migration that is not yet visible here cannot install options before the reset below.
		hasUnfinishedMigration, err := c.hasUnfinishedVMIMigration(vmiMigration)
		if err != nil {
			return err
		}
		if hasUnfinishedMigration {
			klog.Infof("VirtualMachineInstanceMigration %s is stale because another migration for VMI %s is unfinished, skipping cleanup",
				key, vmiMigration.Spec.VMIName)
			return nil
		}
	}

	vmi, err := c.config.KubevirtClient.VirtualMachineInstance(namespace).Get(context.TODO(), vmiMigration.Spec.VMIName, metav1.GetOptions{})
	if err != nil {
		// KubeVirt fills status.migrationState.sourcePod while Pending, so cleanup must not depend on it.
		if k8serrors.IsNotFound(err) && vmiMigration.Status.Phase == kubevirtv1.MigrationFailed {
			return c.cleanupFailedVMIMigrationWithoutVMI(vmiMigration)
		}
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
		// A failed migration may have configured options while Pending, before the VMI
		// migration state was populated. Recover its target from the launcher pod so
		// cleanup is scoped to the migration that wrote the options.
		if vmiMigration.Status.Phase == kubevirtv1.MigrationFailed {
			if srcNodeName, targetNodeName, err = c.recoverVMIMigrationNodes(vmiMigration, vmi); err != nil {
				return err
			}
		}
		// A succeeded migration with stale state was taken over by a newer one, which owns
		// the ports. A failed migration still has its own options to release below.
		if (srcNodeName == "" || targetNodeName == "") && vmiMigration.Status.Phase == kubevirtv1.MigrationSucceeded {
			klog.V(3).Infof("VirtualMachineInstanceMigration %s migration state is Succeeded but VMI migration state is stale or nil, skipping", key)
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
	case kubevirtv1.MigrationPending, kubevirtv1.MigrationScheduling:
		return c.setVMIMigrationOptions(vmiMigration, vmi, srcNodeName, portNames)
	case kubevirtv1.MigrationScheduled, kubevirtv1.MigrationPreparingTarget, kubevirtv1.MigrationTargetReady:
		// Re-assert options while the migration is preparing. A target pod may not exist
		// yet; unlike Pending/Scheduling this phase will produce another migration event.
		targetPod, err := c.vmiMigrationTargetPod(vmiMigration)
		if err != nil {
			return err
		}
		if targetPod == nil {
			klog.Warningf("target pod not yet created for migration job UID %s in phase %s, waiting for pod creation",
				vmiMigration.UID, vmiMigration.Status.Phase)
			return nil
		}
		sourceNode := srcNodeName
		if sourceNode == "" {
			sourceNode = vmi.Status.NodeName
		}
		if sourceNode == "" || targetPod.Spec.NodeName == "" || sourceNode == targetPod.Spec.NodeName {
			klog.Warningf("VM pod %s/%s migration setup skipped, source node: %s, target node: %s (migration job UID: %s)",
				targetPod.Namespace, targetPod.Name, sourceNode, targetPod.Spec.NodeName, vmiMigration.UID)
			return nil
		}
		for _, portName := range portNames {
			if err := c.OVNNbClient.SetLogicalSwitchPortMigrateOptions(portName, sourceNode, targetPod.Spec.NodeName); err != nil {
				return fmt.Errorf("failed to set migrate options for VM pod lsp %s: %w", portName, err)
			}
		}
	case kubevirtv1.MigrationSucceeded, kubevirtv1.MigrationFailed:
		// A terminal event can arrive after a newer migration has taken ownership.
		hasNewerMigration, err := c.hasNewerActiveVMIMigration(vmiMigration)
		if err != nil {
			return err
		}
		if hasNewerMigration {
			klog.Infof("skip resetting migrate options of vmi %s/%s for migration %s, a newer migration is in progress",
				vmi.Namespace, vmi.Name, key)
			return nil
		}
		migrationFailed := vmiMigration.Status.Phase == kubevirtv1.MigrationFailed
		if migrationFailed && (srcNodeName == "" || targetNodeName == "") {
			for _, portName := range portNames {
				if err := c.OVNNbClient.CleanLogicalSwitchPortMigrateOptions(portName); err != nil {
					return fmt.Errorf("failed to clean migrate options for lsp %s: %w", portName, err)
				}
			}
			return nil
		}
		return c.resetVMIMigrationOptions(portNames, srcNodeName, targetNodeName, migrationFailed)
	}
	return nil
}

// setVMIMigrationOptions requests both the source and the target launcher chassis for the VMI ports.
func (c *Controller) setVMIMigrationOptions(vmiMigration *kubevirtv1.VirtualMachineInstanceMigration, vmi *kubevirtv1.VirtualMachineInstance, sourceNode string, portNames []string) error {
	// Before MigrationState is synchronized, use the VMI's current node as the source.
	if sourceNode == "" {
		sourceNode = vmi.Status.NodeName
	}

	// KubeVirt creates the target virt-launcher in Pending and, for hotplug volumes,
	// creates its attachment pod only after the launcher is ready. An attachment pod
	// then gates Pending -> Scheduling, and both pods gate Scheduling -> Scheduled.
	// Configure Pending to unblock launcher CNI and keep Scheduling for retries.
	// Hotplug attachment pods share MigrationJobLabel, so AppLabel must distinguish
	// the target virt-launcher whose NodeName identifies the migration target.
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			kubevirtv1.MigrationJobLabel: string(vmiMigration.UID),
			kubevirtv1.AppLabel:          "virt-launcher",
		},
	})
	if err != nil {
		err = fmt.Errorf("failed to create label selector for migration job UID %s: %w", vmiMigration.UID, err)
		klog.Error(err)
		return err
	}

	launcherPods, err := c.podsLister.Pods(vmiMigration.Namespace).List(selector)
	if err != nil {
		err = fmt.Errorf("failed to list target launcher pods with migration job UID %s: %w", vmiMigration.UID, err)
		klog.Error(err)
		return err
	}
	if len(launcherPods) == 0 {
		klog.Warningf("target launcher pod not yet created for migration job UID %s in phase %s, waiting for pod creation",
			vmiMigration.UID, vmiMigration.Status.Phase)
		// Target launcher pod creation does not necessarily change the VMIM phase.
		// Requeue so the same Pending migration is retried after the pod appears.
		return fmt.Errorf("target launcher pod not yet created for migration job UID %s in phase %s", vmiMigration.UID, vmiMigration.Status.Phase)
	}

	targetLauncherPod := launcherPods[0]
	targetNode := targetLauncherPod.Spec.NodeName
	if sourceNode == "" || targetNode == "" {
		klog.Warningf("target launcher pod %s/%s migration setup skipped, source node: %s, target node: %s (migration job UID: %s)",
			targetLauncherPod.Namespace, targetLauncherPod.Name, sourceNode, targetNode, vmiMigration.UID)
		// Pod scheduling may complete without another VMIM phase update. Requeue until
		// both node assignments are visible instead of dropping the pending setup.
		return fmt.Errorf("target launcher pod %s/%s migration setup deferred, source node %q, target node %q not ready yet (migration job UID %s)",
			targetLauncherPod.Namespace, targetLauncherPod.Name, sourceNode, targetNode, vmiMigration.UID)
	}
	if sourceNode == targetNode {
		klog.Warningf("target launcher pod %s/%s migration setup skipped, source and target node are both %s (migration job UID: %s)",
			targetLauncherPod.Namespace, targetLauncherPod.Name, sourceNode, vmiMigration.UID)
		return nil
	}

	klog.Infof("target launcher pod %s/%s is migrating from %s to %s (migration job UID: %s)",
		targetLauncherPod.Namespace, targetLauncherPod.Name, sourceNode, targetNode, vmiMigration.UID)
	for _, portName := range portNames {
		if err := c.OVNNbClient.SetLogicalSwitchPortMigrateOptions(portName, sourceNode, targetNode); err != nil {
			err = fmt.Errorf("failed to set migrate options for VM pod lsp %s: %w", portName, err)
			klog.Error(err)
			return err
		}
		klog.Infof("successfully set migrate options for lsp %s from %s to %s", portName, sourceNode, targetNode)
	}
	return nil
}

// resetVMIMigrationOptions pins the VMI ports to the target node after success, or back to the
// source node after failure.
func (c *Controller) resetVMIMigrationOptions(portNames []string, srcNodeName, targetNodeName string, migrationFailed bool) error {
	for _, portName := range portNames {
		klog.Infof("migrate end reset options for lsp %s from %s to %s, migration failed: %t", portName, srcNodeName, targetNodeName, migrationFailed)
		if err := c.OVNNbClient.ResetLogicalSwitchPortMigrateOptions(portName, srcNodeName, targetNodeName, migrationFailed); err != nil {
			err = fmt.Errorf("failed to clean migrate options for lsp %s, %w", portName, err)
			klog.Error(err)
			return err
		}
	}
	return nil
}

// cleanupFailedVMIMigrationWithoutVMI removes options left by a failed migration when the VMI
// is gone. There is no node to pin the ports to, and CreateLogicalSwitchPort keeps existing
// options, so a leftover requested-chassis would block the next VMI on any other node.
func (c *Controller) cleanupFailedVMIMigrationWithoutVMI(vmiMigration *kubevirtv1.VirtualMachineInstanceMigration) error {
	vmKey := fmt.Sprintf("%s/%s", vmiMigration.Namespace, vmiMigration.Spec.VMIName)
	lsps, err := c.OVNNbClient.ListNormalLogicalSwitchPorts(c.config.EnableExternalVpc, map[string]string{"pod": vmKey})
	if err != nil {
		return fmt.Errorf("failed to list logical switch ports for VMI %s: %w", vmKey, err)
	}

	for _, lsp := range lsps {
		if lsp.Options["activation-strategy"] != "rarp" {
			continue
		}
		nodes := strings.Split(lsp.Options["requested-chassis"], ",")
		if len(nodes) != 2 || nodes[0] == "" || nodes[1] == "" || nodes[0] == nodes[1] {
			continue
		}
		if err := c.OVNNbClient.CleanLogicalSwitchPortMigrateOptions(lsp.Name); err != nil {
			return fmt.Errorf("failed to clean migrate options for LSP %s: %w", lsp.Name, err)
		}
	}
	return nil
}

// hasUnfinishedVMIMigration reports whether another migration for the same VMI is still active.
// Terminal cleanup must yield to any such migration because its OVN options may already own the ports.
func (c *Controller) hasUnfinishedVMIMigration(vmiMigration *kubevirtv1.VirtualMachineInstanceMigration) (bool, error) {
	migrations, err := c.config.KubevirtClient.VirtualMachineInstanceMigration(vmiMigration.Namespace).
		List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to list migrations in namespace %s: %w", vmiMigration.Namespace, err)
	}

	for i := range migrations.Items {
		migration := &migrations.Items[i]
		if migration.Spec.VMIName == vmiMigration.Spec.VMIName && migration.UID != vmiMigration.UID && !migration.IsFinal() {
			return true, nil
		}
	}

	return false, nil
}

// vmiMigrationTargetPod returns the target virt-launcher pod created for a migration job.
func (c *Controller) vmiMigrationTargetPod(vmiMigration *kubevirtv1.VirtualMachineInstanceMigration) (*corev1.Pod, error) {
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{MatchLabels: map[string]string{
		kubevirtv1.MigrationJobLabel: string(vmiMigration.UID),
		kubevirtv1.AppLabel:          virtLauncherAppLabel,
	}})
	if err != nil {
		return nil, fmt.Errorf("failed to create label selector for migration job UID %s: %w", vmiMigration.UID, err)
	}
	pods, err := c.podsLister.Pods(vmiMigration.Namespace).List(selector)
	if err != nil {
		return nil, fmt.Errorf("failed to list target launcher pods with migration job UID %s: %w", vmiMigration.UID, err)
	}
	if len(pods) == 0 {
		return nil, nil
	}
	return pods[0], nil
}

// recoverVMIMigrationNodes recovers the node pair for a failed migration whose VMI state was
// never populated. The VMI's current node is the source and the target launcher node is the target.
func (c *Controller) recoverVMIMigrationNodes(vmiMigration *kubevirtv1.VirtualMachineInstanceMigration, vmi *kubevirtv1.VirtualMachineInstance) (string, string, error) {
	targetPod, err := c.vmiMigrationTargetPod(vmiMigration)
	if err != nil {
		return "", "", err
	}
	if targetPod == nil || targetPod.Spec.NodeName == "" || vmi.Status.NodeName == "" || vmi.Status.NodeName == targetPod.Spec.NodeName {
		return "", "", nil
	}
	return vmi.Status.NodeName, targetPod.Spec.NodeName, nil
}

// listVMIMigrations returns cached migrations for a VMI.
func (c *Controller) listVMIMigrations(vmiKey string) []*kubevirtv1.VirtualMachineInstanceMigration {
	indexer := c.kubevirtInformerFactory.VirtualMachineInstanceMigration().GetIndexer()
	objects, err := indexer.ByIndex(informer.ByVMINameIndex, vmiKey)
	if err != nil {
		klog.Errorf("failed to list migrations of vmi %s: %v", vmiKey, err)
		return nil
	}
	migrations := make([]*kubevirtv1.VirtualMachineInstanceMigration, 0, len(objects))
	for _, object := range objects {
		if migration, ok := object.(*kubevirtv1.VirtualMachineInstanceMigration); ok {
			migrations = append(migrations, migration)
		}
	}
	return migrations
}

// vmiMigrationHasTargetPod reports whether a migration has created its target launcher.
func (c *Controller) vmiMigrationHasTargetPod(vmiMigration *kubevirtv1.VirtualMachineInstanceMigration) (bool, error) {
	pod, err := c.vmiMigrationTargetPod(vmiMigration)
	return pod != nil, err
}

// isCurrentVMIPod reports whether pod is the newest VM pod on the node where the VMI runs.
func (c *Controller) isCurrentVMIPod(pod *corev1.Pod, vmiNodeName, vmName string) (bool, error) {
	onVMINode := func(candidate *corev1.Pod) bool {
		return vmiNodeName == "" || vmiNodeName == candidate.Spec.NodeName
	}
	if !onVMINode(pod) {
		return false, nil
	}
	siblings, err := c.podsLister.Pods(pod.Namespace).List(labels.Everything())
	if err != nil {
		return false, fmt.Errorf("failed to list pods in namespace %s: %w", pod.Namespace, err)
	}
	for _, sibling := range siblings {
		if sibling.Name == pod.Name || !onVMINode(sibling) {
			continue
		}
		if isVM, name := isVMPod(sibling); !isVM || name != vmName {
			continue
		}
		if pod.CreationTimestamp.Before(&sibling.CreationTimestamp) {
			return false, nil
		}
	}
	return true, nil
}

// podFromEarlierVMI reports whether pod belongs to an older VMI incarnation.
func podFromEarlierVMI(pod *corev1.Pod, vmi *kubevirtv1.VirtualMachineInstance) bool {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == util.KindVirtualMachineInstance && strings.HasPrefix(owner.APIVersion, kubevirtv1.SchemeGroupVersion.Group+"/") {
			return owner.UID != "" && vmi.UID != "" && owner.UID != vmi.UID
		}
	}
	return false
}

// hasActiveVMIMigrationWithTarget reports whether another active migration owns a target pod.
func (c *Controller) hasActiveVMIMigrationWithTarget(namespace, vmName string) (bool, error) {
	for _, migration := range c.listVMIMigrations(fmt.Sprintf("%s/%s", namespace, vmName)) {
		if migration.IsFinal() {
			continue
		}
		hasTarget, err := c.vmiMigrationHasTargetPod(migration)
		if err != nil {
			return false, err
		}
		if hasTarget {
			return true, nil
		}
	}
	return false, nil
}

// deletedPodOwnsMigrateOptions reports whether deleting pod may release the VM's shared LSPs.
func (c *Controller) deletedPodOwnsMigrateOptions(pod *corev1.Pod, vmName string) (bool, error) {
	vmi, err := c.config.KubevirtClient.VirtualMachineInstance(pod.Namespace).Get(context.Background(), vmName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return true, nil
		}
		return false, fmt.Errorf("failed to get vmi %s/%s: %w", pod.Namespace, vmName, err)
	}
	if podFromEarlierVMI(pod, vmi) {
		active, err := c.hasActiveVMIMigrationWithTarget(pod.Namespace, vmName)
		if err != nil {
			return false, err
		}
		return !active, nil
	}
	current, err := c.isCurrentVMIPod(pod, vmi.Status.NodeName, vmName)
	if err != nil {
		return false, err
	}
	if current {
		return true, nil
	}
	podMigrationUID := pod.Labels[kubevirtv1.MigrationJobLabel]
	if podMigrationUID == "" {
		return false, nil
	}
	for _, migration := range c.listVMIMigrations(fmt.Sprintf("%s/%s", pod.Namespace, vmName)) {
		if !migration.IsFinal() && podMigrationUID == string(migration.UID) {
			return true, nil
		}
	}
	return false, nil
}

// hasNewerActiveVMIMigration reports whether a newer migration has a target pod and owns ports.
func (c *Controller) hasNewerActiveVMIMigration(vmiMigration *kubevirtv1.VirtualMachineInstanceMigration) (bool, error) {
	vmiKey := fmt.Sprintf("%s/%s", vmiMigration.Namespace, vmiMigration.Spec.VMIName)
	for _, migration := range c.listVMIMigrations(vmiKey) {
		if migration.UID == vmiMigration.UID || migration.IsFinal() || migration.CreationTimestamp.Before(&vmiMigration.CreationTimestamp) {
			continue
		}
		hasTarget, err := c.vmiMigrationHasTargetPod(migration)
		if err != nil {
			return false, err
		}
		if hasTarget {
			return true, nil
		}
	}
	return false, nil
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
