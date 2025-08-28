package controller

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type updateVerGobgpConfigObject struct {
	key    string
	oldVer *kubeovnv1.GobgpConfig
	newVer *kubeovnv1.GobgpConfig
}

func (c *Controller) enqueueAddGobgpConfig(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.GobgpConfig)).String()
	klog.V(3).Infof("enqueue add gobgp-configuration %s", key)
	c.addGobgpConfigQueue.Add(key)
}

func (c *Controller) enqueueUpdateGobgpConfig(oldObj, newObj any) {
	key := cache.MetaObjectToName(newObj.(*kubeovnv1.GobgpConfig)).String()
	klog.V(3).Infof("enqueue update gobgp-configuration %s", key)

	if oldObj == nil || newObj == nil {
		klog.Warningf("enqueue update gobgp-configuration %s, but old object is nil", key)
		return
	}

	oldGobgpConfig := oldObj.(*kubeovnv1.GobgpConfig)
	newGobgpConfig := newObj.(*kubeovnv1.GobgpConfig)

	updateConfigVer := &updateVerGobgpConfigObject{
		key:    key,
		oldVer: oldGobgpConfig,
		newVer: newGobgpConfig,
	}

	if !newGobgpConfig.DeletionTimestamp.IsZero() {
		c.deleteGobgpConfigQueue.Add(key)
		return
	}

	if !reflect.DeepEqual(oldGobgpConfig.Spec, newGobgpConfig.Spec) {
		klog.Infof("enqueue update gobgp-config %s", key)
		c.updateGobgpConfigQueue.Add(updateConfigVer)
	}
}

func (c *Controller) enqueueDeleteGobgpConfig(obj any) {
	var gobgpConfig *kubeovnv1.GobgpConfig

	switch t := obj.(type) {
	case *kubeovnv1.GobgpConfig:
		gobgpConfig = t
	case cache.DeletedFinalStateUnknown:
		if v, ok := t.Obj.(*kubeovnv1.GobgpConfig); ok {
			gobgpConfig = v
		}
	}

	if gobgpConfig == nil {
		klog.Warning("enqueueDeleteGobgpConfig: object is not GobgpConfig")
		return
	}

	key := cache.MetaObjectToName(obj.(*kubeovnv1.GobgpConfig)).String()

	klog.V(3).Infof("enqueue delete gobgp-config %s", key)
	c.deleteGobgpConfigQueue.Add(key)
}

func (c *Controller) handleAddGobgpConfig(key string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.gobgpConfigKeyMutex.LockKey(key)
	defer func() { _ = c.gobgpConfigKeyMutex.UnlockKey(key) }()

	cachedGobgpConfig, err := c.gobgpConfigLister.GobgpConfigs(ns).Get(name)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Error(err)
			return err
		}
		return nil
	}

	if !cachedGobgpConfig.DeletionTimestamp.IsZero() {
		c.deleteGobgpConfigQueue.Add(key)
		return nil
	}

	klog.V(3).Infof("debug gobgp-config %s", cachedGobgpConfig.Name)
	gobgpConfig := cachedGobgpConfig.DeepCopy()
	if gobgpConfig, err = c.initGobgpConfigStatus(gobgpConfig); err != nil {
		klog.Error(err)
		return err
	}

	klog.Infof("reconciling gobgp-configuration %s for add", key)

	if controllerutil.AddFinalizer(gobgpConfig, util.KubeOVNControllerFinalizer) {
		updatedGobgpConfig, err := c.config.KubeOvnClient.KubeovnV1().GobgpConfigs(gobgpConfig.Namespace).
			Update(context.Background(),
				gobgpConfig, metav1.UpdateOptions{})
		if err != nil {
			err = fmt.Errorf("failed to add finalizer for gobgp-configuration %s/%s: %w", gobgpConfig.Namespace, gobgpConfig.Name, err)
			klog.Error(err)
			return err
		}
		gobgpConfig = updatedGobgpConfig
	}

	pods, err := c.validateGobgpConfig(gobgpConfig)
	if err != nil || pods == nil {
		klog.Error(err)
		return err
	}

	for _, pod := range pods {
		if len(pod.Status.PodIPs) == 0 {
			continue
		}
		klog.Infof("handle adding gobgp-config to pod %s", pod.Name)
		if err = c.execUpdateBgpPolicy(key, pod, nil, gobgpConfig); err != nil {
			klog.Error(err)
			return err
		}
	}

	gobgpConfig.Status.Conditions.SetReady("ReconcileSuccess", gobgpConfig.Generation)
	if _, err = c.updateGobgpConfigStatus(gobgpConfig); err != nil {
		return err
	}
	klog.Infof("finished reconciling gobgp-config %s", key)

	return nil
}

func (c *Controller) handleUpdateGobgpConfig(updatedObj *updateVerGobgpConfigObject) error {
	key := updatedObj.key

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.gobgpConfigKeyMutex.LockKey(key)
	defer func() { _ = c.gobgpConfigKeyMutex.UnlockKey(key) }()

	cachedGobgpConfig, err := c.gobgpConfigLister.GobgpConfigs(ns).Get(name)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Error(err)
			return err
		}
		return nil
	}

	if !cachedGobgpConfig.DeletionTimestamp.IsZero() {
		c.deleteGobgpConfigQueue.Add(key)
		return nil
	}

	klog.Infof("reconciling gobgp-configs %s for update", key)
	gobgpConfig := cachedGobgpConfig.DeepCopy()

	pods, err := c.validateGobgpConfig(gobgpConfig)
	if err != nil || pods == nil {
		klog.Error(err)
		return err
	}

	for _, pod := range pods {
		if len(pod.Status.PodIPs) == 0 {
			continue
		}
		klog.Infof("handle adding gobgp-configs to pod %s", pod.Name)
		if err = c.execUpdateBgpPolicy(key, pod, updatedObj.oldVer, updatedObj.newVer); err != nil {
			klog.Error(err)
			return err
		}
	}

	gobgpConfig.Status.Conditions.SetReady("ReconcileSuccess", gobgpConfig.Generation)
	if _, err = c.updateGobgpConfigStatus(gobgpConfig); err != nil {
		return err
	}
	klog.Infof("finished reconciling gobgp-configs %s", key)

	return nil
}

func (c *Controller) handleDelGobgpConfig(key string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.gobgpConfigKeyMutex.LockKey(key)
	defer func() { _ = c.gobgpConfigKeyMutex.UnlockKey(key) }()

	cachedGobgpConfig, err := c.gobgpConfigLister.GobgpConfigs(ns).Get(name)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Error(err)
			return err
		}
		return nil
	}

	klog.Infof("reconciling gobgp-configs %s", key)
	gobgpConfig := cachedGobgpConfig.DeepCopy()

	pods, err := c.validateGobgpConfig(gobgpConfig)
	if err != nil || pods == nil {
		klog.Error(err)
		return err
	}

	for _, pod := range pods {
		if len(pod.Status.PodIPs) == 0 {
			continue
		}
		klog.Infof("handle deleting gobgp-configs %s", key)
		if err = c.execUpdateBgpPolicy(key, pod, gobgpConfig, nil); err != nil {
			klog.Error(err)
			return err
		}
	}

	gobgpConfig = cachedGobgpConfig.DeepCopy()
	if controllerutil.RemoveFinalizer(gobgpConfig, util.KubeOVNControllerFinalizer) {
		if _, err = c.config.KubeOvnClient.KubeovnV1().GobgpConfigs(gobgpConfig.Namespace).
			Update(context.Background(), gobgpConfig, metav1.UpdateOptions{}); err != nil {
			err = fmt.Errorf("failed to remove finalizer from gobgp-configs %s: %w", key, err)
			klog.Error(err)
		}
	}

	klog.Infof("finished reconciling gobgp-config %s", key)

	return nil
}

func (c *Controller) execUpdateBgpPolicy(key string, pod *corev1.Pod, oldGobgpConfig, newGobgpConfig *kubeovnv1.GobgpConfig) error {
	klog.Infof("execUpdateBgpPolicy %s", key)

	if pod.Name == "" {
		err := fmt.Errorf("failed to get pod name %s", pod.Name)
		klog.Error(err)
		return err
	}

	cmdArs := []string{}
	if oldGobgpConfig != nil {
		klog.Infof("execUpdateBgpPolicy %s", key)

		for _, neighbor := range oldGobgpConfig.Spec.Neighbors {
			nbrIP := neighbor.Address
			if len(nbrIP) == 0 {
				klog.Warningf("neighbor address is empty for gobgp-config %s", key)
				continue
			}
			// erase neighbor.
			cmdArs = append(cmdArs, "--", "flush-neighbor-policy", nbrIP)
		}
	} else {
		// if oldGobgpConfig is nil, it means this is the first time to update the bgp policy
		// so we need to set default action to reject
		cmdArs = append(cmdArs, "--", "set-default-action", "reject")
	}

	if newGobgpConfig != nil {
		for _, neighbor := range newGobgpConfig.Spec.Neighbors {
			klog.Infof("new bgp config neighbor %v", neighbor)
			nbrIP := neighbor.Address
			if len(nbrIP) == 0 {
				klog.Warningf("neighbor address is empty for gobgp-config %s", key)
				continue
			}
			cmdArs = append(cmdArs, "--", "set-neighbor-policy", nbrIP)

			// toAdvertise
			advMode := neighbor.ToAdvertise.Allowed.Mode
			var advPrefixes []string
			if advMode == "all" {
				advPrefixes = []string{"0.0.0.0/0 0..32"}
			} else {
				advPrefixes = neighbor.ToAdvertise.Allowed.Prefixes
			}
			quoted := make([]string, len(advPrefixes))

			for i, p := range advPrefixes {
				quoted[i] = fmt.Sprintf("\"%s\"", p)
			}
			cmdArs = append(cmdArs, "--", "add-prefix", "out", nbrIP, strings.Join(quoted, ","))

			// toReceive
			recvMode := neighbor.ToReceive.Allowed.Mode
			var recvPrefixes []string
			if recvMode == "all" {
				recvPrefixes = []string{"0.0.0.0/0 0..32"}
			} else {
				recvPrefixes = neighbor.ToReceive.Allowed.Prefixes
			}
			quoted = make([]string, len(recvPrefixes))
			for i, p := range recvPrefixes {
				quoted[i] = fmt.Sprintf("\"%s\"", p)
			}
			cmdArs = append(cmdArs, "--", "add-prefix", "in", nbrIP, strings.Join(quoted, ","))
		}
	} else {
		// if newGobgpConfig is nil, it means the bgp policy is deleted
		// so we need to set default action to accept
		cmdArs = append(cmdArs, "--", "set-default-action", "accept")
	}

	if err := c.execCmd(pod, cmdArs); err != nil {
		klog.Error(err)
		return err
	}

	return nil
}

func (c *Controller) validateGobgpConfig(gobgpConfig *kubeovnv1.GobgpConfig) ([]*corev1.Pod, error) {
	klog.Infof("gobgpConfignamespace: %s name: %s", gobgpConfig.Namespace, gobgpConfig.Name)
	ber, err := c.bgpEdgeRouterLister.BgpEdgeRouters(gobgpConfig.Namespace).Get(gobgpConfig.Spec.BgpEdgeRouter)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, fmt.Errorf("bgp edge router %s not found: %w", gobgpConfig.Spec.BgpEdgeRouter, err)
		}
	}
	berNeighbors := ber.Spec.BGP.Neighbors
	gobgpNeighbors := gobgpConfig.Spec.Neighbors
	neighborFlag := false
	for _, gNeighbor := range gobgpNeighbors {
		if containsNeighbor(berNeighbors, gNeighbor.Address) {
			neighborFlag = true
			break
		}
	}

	if !neighborFlag {
		err = fmt.Errorf("no matching neighbor found in BgpEdgeRouter %s for GobgpConfig %s", gobgpConfig.Spec.BgpEdgeRouter, gobgpConfig.Name)
		klog.Error(err)
		return nil, err
	}

	deploy, err := c.berDeploymentsLister.Deployments(gobgpConfig.Namespace).Get(gobgpConfig.Spec.BgpEdgeRouter)
	if err != nil {
		gobgpConfig.Status.Ready = false
		msg := fmt.Sprintf("Waiting for %s %s to be ready", deploy.Kind, deploy.Name)
		gobgpConfig.Status.Conditions.SetCondition(kubeovnv1.Validated, corev1.ConditionFalse, "BgpEdgeRouterDeployNotFound", msg, gobgpConfig.Generation)
		_, _ = c.updateGobgpConfigStatus(gobgpConfig)
		klog.Error(err)
		return nil, err
	}

	ready := util.DeploymentIsReady(deploy)
	if !ready {
		gobgpConfig.Status.Ready = false
		msg := fmt.Sprintf("Waiting for %s %s to be ready", deploy.Kind, deploy.Name)
		gobgpConfig.Status.Conditions.SetCondition(kubeovnv1.Validated, corev1.ConditionFalse, "BgpEdgeRouterNotEnabled", msg, gobgpConfig.Generation)
		_, _ = c.updateGobgpConfigStatus(gobgpConfig)
		readyErr := fmt.Sprintf("Kind %s, Deployment %s is not ready", deploy.Kind, deploy.Name)
		klog.Error(readyErr)
		return nil, fmt.Errorf("%s", readyErr)
	}
	// get the pods of the deployment to collect the pod IPs
	podSelector, err := metav1.LabelSelectorAsSelector(deploy.Spec.Selector)
	if err != nil {
		err = fmt.Errorf("failed to get pod selector of deployment %s/%s: %w", deploy.Namespace, deploy.Name, err)
		klog.Error(err)
		return nil, err
	}

	pods, err := c.podsLister.Pods(deploy.Namespace).List(podSelector)
	if err != nil {
		err = fmt.Errorf("failed to list pods of deployment %s/%s: %w", deploy.Namespace, deploy.Name, err)
		klog.Error(err)
		return nil, err
	}

	if ready {
		gobgpConfig.Status.Ready = true
	}

	return pods, nil
}

func (c *Controller) initGobgpConfigStatus(gobgpConfig *kubeovnv1.GobgpConfig) (*kubeovnv1.GobgpConfig, error) {
	var err error
	gobgpConfig, err = c.updateGobgpConfigStatus(gobgpConfig)
	return gobgpConfig, err
}

func (c *Controller) updateGobgpConfigStatus(gobgpConfig *kubeovnv1.GobgpConfig) (*kubeovnv1.GobgpConfig, error) {
	if len(gobgpConfig.Status.Conditions) == 0 {
		gobgpConfig.Status.Conditions.SetCondition(kubeovnv1.Init, corev1.ConditionUnknown, "Processing", "", gobgpConfig.Generation)
	}

	updatedGobgpConfig, err := c.config.KubeOvnClient.KubeovnV1().GobgpConfigs(gobgpConfig.Namespace).
		UpdateStatus(context.Background(), gobgpConfig, metav1.UpdateOptions{})
	if err != nil {
		err = fmt.Errorf("failed to update status of gobgp-config %s/%s: %w", gobgpConfig.Namespace, gobgpConfig.Name, err)
		klog.Error(err)
		return nil, err
	}

	return updatedGobgpConfig, nil
}

func (c *Controller) execGetCmd(pod *corev1.Pod, cmdArs []string) (string, error) {
	cmd := fmt.Sprintf("bash /kube-ovn/update-bgp-policy.sh %s", strings.Join(cmdArs, " "))

	klog.Infof("exec command : %s", cmd)
	stdOutput, errOutput, err := util.ExecuteCommandInContainer(c.config.KubeClient, c.config.KubeRestConfig, pod.Namespace, pod.Name, "bgp-router-speaker", []string{"/bin/bash", "-c", cmd}...)
	if err != nil {
		if len(errOutput) > 0 {
			klog.Errorf("failed to ExecuteCommandInContainer, errOutput: %v", errOutput)
		}
		if len(stdOutput) > 0 {
			klog.Infof("failed to ExecuteCommandInContainer, stdOutput: %v", stdOutput)
		}
		klog.Error(err)
		return "", err
	}

	cmdSuccess := false
	if len(stdOutput) > 0 {
		klog.Infof("ExecuteCommandInContainer stdOutput: %v", stdOutput)
		if strings.Contains(stdOutput, "Update bgp policy completed successfully") {
			cmdSuccess = true
		}
	}

	if len(errOutput) > 0 && !cmdSuccess {
		klog.Errorf("failed to ExecuteCommandInContainer errOutput: %v", errOutput)
		return "", errors.New(errOutput)
	}

	return stdOutput, nil
}

func (c *Controller) execCmd(pod *corev1.Pod, cmdArs []string) error {
	cmd := fmt.Sprintf("bash /kube-ovn/update-bgp-policy.sh --batch %s", strings.Join(cmdArs, " "))

	klog.Infof("exec command : %s", cmd)
	stdOutput, errOutput, err := util.ExecuteCommandInContainer(c.config.KubeClient, c.config.KubeRestConfig, pod.Namespace, pod.Name, "bgp-router-speaker", []string{"/bin/bash", "-c", cmd}...)
	if err != nil {
		if len(errOutput) > 0 {
			klog.Errorf("failed to ExecuteCommandInContainer, errOutput: %v", errOutput)
		}
		if len(stdOutput) > 0 {
			klog.Infof("failed to ExecuteCommandInContainer, stdOutput: %v", stdOutput)
		}
		klog.Error(err)
		return err
	}

	cmdSuccess := false
	if len(stdOutput) > 0 {
		klog.Infof("ExecuteCommandInContainer stdOutput: %v", stdOutput)
		if strings.Contains(stdOutput, "Update bgp policy completed successfully") {
			cmdSuccess = true
		}
	}

	if len(errOutput) > 0 && !cmdSuccess {
		klog.Errorf("failed to ExecuteCommandInContainer errOutput: %v", errOutput)
		return errors.New(errOutput)
	}

	return nil
}

func containsNeighbor(neighbors []string, address string) bool {
	return slices.Contains(neighbors, address)
}

func (c *Controller) resyncBgpPolicyRules() {
	klog.Info("resync bgp edge router")

	// resync all bgp edge routers
	gobgpConfigs, err := c.gobgpConfigLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list bgp edge routers: %v", err)
		return
	}

	for _, gobgpConfig := range gobgpConfigs {
		if err := c.syncGobgpPolicy(gobgpConfig); err != nil {
			klog.Errorf("failed to sync advertised routes for bgp edge router %s: %v", gobgpConfig.Name, err)
			continue
		}
		klog.Infof("resync bgp edge router %s", gobgpConfig.Name)
	}
}

func (c *Controller) syncGobgpPolicy(gobgpConfig *kubeovnv1.GobgpConfig) error {
	key := cache.MetaObjectToName(gobgpConfig).String()

	c.gobgpConfigKeyMutex.LockKey(key)
	defer func() { _ = c.gobgpConfigKeyMutex.UnlockKey(key) }()

	if !gobgpConfig.DeletionTimestamp.IsZero() {
		c.deleteGobgpConfigQueue.Add(key)
		return nil
	}
	klog.Infof("reconciling bgp-edge-router %s", key)
	// Deep copy because we might mutate Status below.
	cachedGobgpConfig := gobgpConfig.DeepCopy()

	pods, err := c.validateGobgpConfig(cachedGobgpConfig)
	if err != nil || pods == nil {
		klog.Error(err)
		return err
	}

	for _, pod := range pods {
		if len(pod.Status.PodIPs) == 0 {
			continue
		}

		// execGetBgpPolicy
		// if
		cmdArs := []string{"list-global-policy"}
		output, err := c.execGetCmd(pod, cmdArs)
		if err != nil {
			klog.Error(err)
			return err
		}

		validate, err := c.validateSyncGobgpConfig(output, cachedGobgpConfig)
		if !validate {
			err = c.execUpdateBgpPolicy(key, pod, cachedGobgpConfig, cachedGobgpConfig)
			if err != nil {
				return err
			}
		} else if err != nil {
			return err
		}

		// klog.Infof("router pod %s/%s policy: %v", pod.Namespace, pod.Name, gobgpConfig)
	}

	klog.Infof("finished sync bgp-edge-router %s advertised routes", key)
	return nil
}

func (c *Controller) validateSyncGobgpConfig(output string, gobgpConfig *kubeovnv1.GobgpConfig) (bool, error) {
	klog.Infof("output: %s", output)
	// Parse the output to verify that all neighbors, statements, and prefixes in gobgpConfig.Spec.Neighbors exist in the output.
	for _, neighbor := range gobgpConfig.Spec.Neighbors {
		nbrIP := neighbor.Address
		if nbrIP == "" {
			klog.Warningf("neighbor address is empty for gobgpConfig %s", gobgpConfig.Name)
			return false, errors.New("neighbor address is empty")
		}

		// Check Import policy statement and prefix
		var inPrefixes, outPrefixes []string
		inPrefixName := fmt.Sprintf("prefix-%s-in", nbrIP)
		outPrefixName := fmt.Sprintf("prefix-%s-out", nbrIP)

		if neighbor.ToReceive.Allowed.Mode == "all" || neighbor.ToReceive.Allowed.Mode == "filtered" {
			importPolicy := fmt.Sprintf("policy-%s-in", nbrIP)
			importStmt := fmt.Sprintf("stmt-%s-in", nbrIP)
			importPrefix := fmt.Sprintf("prefix-%s-in", nbrIP)
			if !strings.Contains(output, importPolicy) ||
				!strings.Contains(output, importStmt) ||
				!strings.Contains(output, importPrefix) {
				klog.Warningf("missing import policy/statement/prefix for neighbor %s", nbrIP)
				return false, nil
			}
		}

		if neighbor.ToAdvertise.Allowed.Mode == "all" || neighbor.ToAdvertise.Allowed.Mode == "filtered" {
			// Check Export policy statement and prefix
			exportPolicy := fmt.Sprintf("policy-%s-out", nbrIP)
			exportStmt := fmt.Sprintf("stmt-%s-out", nbrIP)
			exportPrefix := fmt.Sprintf("prefix-%s-out", nbrIP)
			if !strings.Contains(output, exportPolicy) ||
				!strings.Contains(output, exportStmt) ||
				!strings.Contains(output, exportPrefix) {
				klog.Warningf("missing export policy/statement/prefix for neighbor %s", nbrIP)
				return false, nil
			}
		}

		// Parse global policy prefix lines for this neighbor
		// Parse only the lines after "=== Global Policy Prefix ==="

		lines := strings.Split(output, "\n")
		startIdx := 0
		for i, line := range lines {
			if strings.Contains(line, "=== Policy Prefix ===") {
				startIdx = i + 2
				break
			}
		}

		var inDir bool
		if strings.HasPrefix(lines[startIdx], inPrefixName) {
			inDir = true
		} else {
			inDir = false
		}

		for _, line := range lines[startIdx:] {
			line = strings.TrimSpace(line)
			// klog.Infof("line: %v, startIndex: %d", line, startIdx)

			if strings.Contains(line, "Update bgp policy completed successfully") {
				break
			}

			if strings.HasPrefix(line, inPrefixName) || inDir {
				// klog.Infof("line: %v", line)
				prefixPart := strings.TrimPrefix(line, inPrefixName+"  ")
				// klog.Infof("prefixPart: %v", prefixPart)
				inPrefixes = append(inPrefixes, prefixPart)
				if strings.HasPrefix(lines[startIdx+1], outPrefixName) {
					inDir = false
				}
			} else if strings.HasPrefix(line, outPrefixName) || !inDir {
				klog.Infof("line: %v", line)
				prefixPart := strings.TrimPrefix(line, outPrefixName+"  ")
				klog.Infof("prefixPart: %v", prefixPart)
				outPrefixes = append(outPrefixes, prefixPart)
				if strings.HasPrefix(lines[startIdx+1], inPrefixName) {
					inDir = true
				}
			}
		}
		klog.Warningf("inPrefixes: %v, outPrefixes: %v", inPrefixes, outPrefixes)

		// Check advertised prefixes (out)
		if neighbor.ToAdvertise.Allowed.Mode == "filtered" {
			for _, p := range neighbor.ToAdvertise.Allowed.Prefixes {
				found := false
				for _, out := range outPrefixes {
					if strings.Contains(out, p) {
						found = true
						break
					}
				}
				if !found {
					klog.Warningf("missing advertised prefix %s for neighbor %s", p, nbrIP)
					return false, nil
				}
			}
		} else {
			// Mode "all": should contain 0.0.0.0/0 0..32
			found := false
			for _, out := range outPrefixes {
				if strings.Contains(out, "0.0.0.0/0") && strings.Contains(out, "0..32") {
					found = true
					break
				}
			}
			if !found {
				klog.Warningf("missing advertised prefix 0.0.0.0/0 0..32 for neighbor %s", nbrIP)
				return false, nil
			}
		}

		// Check received prefixes (in)
		if neighbor.ToReceive.Allowed.Mode == "filtered" {
			for _, p := range neighbor.ToReceive.Allowed.Prefixes {
				found := false
				for _, in := range inPrefixes {
					if strings.Contains(in, p) {
						found = true
						break
					}
				}
				if !found {
					klog.Warningf("missing received prefix %s for neighbor %s", p, nbrIP)
					return false, nil
				}
			}
		} else {
			// Mode "all": should contain 0.0.0.0/0 0..32
			found := false
			for _, in := range inPrefixes {
				if strings.Contains(in, "0.0.0.0/0") && strings.Contains(in, "0..32") {
					found = true
					break
				}
			}
			if !found {
				klog.Warningf("missing received prefix 0.0.0.0/0 0..32 for neighbor %s", nbrIP)
				return false, nil
			}
		}
	}
	klog.Infof("Sync is not needed.")
	return true, nil
}
