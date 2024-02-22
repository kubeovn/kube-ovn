package controller

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode"

	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddNp(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add np %s", key)
	c.updateNpQueue.Add(key)
}

func (c *Controller) enqueueDeleteNp(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue delete np %s", key)
	c.deleteNpQueue.Add(key)
}

func (c *Controller) enqueueUpdateNp(oldObj, newObj interface{}) {
	oldNp := oldObj.(*netv1.NetworkPolicy)
	newNp := newObj.(*netv1.NetworkPolicy)
	if !reflect.DeepEqual(oldNp.Spec, newNp.Spec) ||
		!reflect.DeepEqual(oldNp.Annotations, newNp.Annotations) {
		var key string
		var err error
		if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
			utilruntime.HandleError(err)
			return
		}
		klog.V(3).Infof("enqueue update np %s", key)
		c.updateNpQueue.Add(key)
	}
}

func (c *Controller) runUpdateNpWorker() {
	for c.processNextUpdateNpWorkItem() {
	}
}

func (c *Controller) runDeleteNpWorker() {
	for c.processNextDeleteNpWorkItem() {
	}
}

func (c *Controller) processNextUpdateNpWorkItem() bool {
	obj, shutdown := c.updateNpQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.updateNpQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateNpQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateNp(key); err != nil {
			c.updateNpQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing network policy %s: %v, requeuing", key, err)
		}
		c.updateNpQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteNpWorkItem() bool {
	obj, shutdown := c.deleteNpQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.deleteNpQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.deleteNpQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDeleteNp(key); err != nil {
			c.deleteNpQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.deleteNpQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) createAsForNetpol(ns, name, direction, asName string, addresses []string) error {
	if err := c.OVNNbClient.CreateAddressSet(asName, map[string]string{
		networkPolicyKey: fmt.Sprintf("%s/%s/%s", ns, name, direction),
	}); err != nil {
		klog.Errorf("failed to create ovn address set %s for np %s/%s: %v", asName, ns, name, err)
		return err
	}

	if err := c.OVNNbClient.AddressSetUpdateAddress(asName, addresses...); err != nil {
		klog.Errorf("failed to set addresses %q to address set %s: %v", strings.Join(addresses, ","), asName, err)
		return err
	}

	return nil
}

func (c *Controller) handleUpdateNp(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.npKeyMutex.LockKey(key)
	defer func() { _ = c.npKeyMutex.UnlockKey(key) }()
	klog.Infof("handle add/update network policy %s", key)

	np, err := c.npsLister.NetworkPolicies(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	defer func() {
		if err != nil {
			c.recorder.Eventf(np, corev1.EventTypeWarning, "CreateACLFailed", err.Error())
		}
	}()

	logEnable := false
	if np.Annotations[util.NetworkPolicyLogAnnotation] == "true" {
		logEnable = true
	}

	npName := np.Name
	nameArray := []rune(np.Name)
	if !unicode.IsLetter(nameArray[0]) {
		npName = "np" + np.Name
	}

	// TODO: ovn acl doesn't support address_set name with '-', now we replace '-' by '.'.
	// This may cause conflict if two np with name test-np and test.np. Maybe hash is a better solution,
	// but we do not want to lost the readability now.
	pgName := strings.ReplaceAll(fmt.Sprintf("%s.%s", npName, np.Namespace), "-", ".")
	ingressAllowAsNamePrefix := strings.ReplaceAll(fmt.Sprintf("%s.%s.ingress.allow", npName, np.Namespace), "-", ".")
	ingressExceptAsNamePrefix := strings.ReplaceAll(fmt.Sprintf("%s.%s.ingress.except", npName, np.Namespace), "-", ".")
	egressAllowAsNamePrefix := strings.ReplaceAll(fmt.Sprintf("%s.%s.egress.allow", npName, np.Namespace), "-", ".")
	egressExceptAsNamePrefix := strings.ReplaceAll(fmt.Sprintf("%s.%s.egress.except", npName, np.Namespace), "-", ".")

	if err = c.OVNNbClient.CreatePortGroup(pgName, map[string]string{networkPolicyKey: np.Namespace + "/" + npName}); err != nil {
		klog.Errorf("create port group for np %s: %v", key, err)
		return err
	}

	namedPortMap := c.namedPort.GetNamedPortByNs(np.Namespace)
	ports, subnetNames, err := c.fetchSelectedPorts(np.Namespace, &np.Spec.PodSelector)
	if err != nil {
		klog.Errorf("fetch ports belongs to np %s: %v", key, err)
		return err
	}

	var subnets []*kubeovnv1.Subnet
	for _, subnetName := range subnetNames {
		subnet, err := c.subnetsLister.Get(subnetName)
		if err != nil {
			klog.Errorf("failed to get pod's subnet %s, %v", subnetName, err)
			return err
		}
		subnets = append(subnets, subnet)
	}

	if err = c.OVNNbClient.PortGroupSetPorts(pgName, ports); err != nil {
		klog.Errorf("failed to set ports of port group %s to %v: %v", pgName, ports, err)
		return err
	}

	ingressACLOps, err := c.OVNNbClient.DeleteAclsOps(pgName, portGroupKey, "to-lport", nil)
	if err != nil {
		klog.Errorf("generate operations that clear np %s ingress acls: %v", key, err)
		return err
	}

	if hasIngressRule(np) {
		for _, subnet := range subnets {
			for _, cidrBlock := range strings.Split(subnet.Spec.CIDRBlock, ",") {
				protocol := util.CheckProtocol(cidrBlock)

				for idx, npr := range np.Spec.Ingress {
					// A single address set must contain addresses of the same type and the name must be unique within table, so IPv4 and IPv6 address set should be different
					ingressAllowAsName := fmt.Sprintf("%s.%s.%d", ingressAllowAsNamePrefix, protocol, idx)
					ingressExceptAsName := fmt.Sprintf("%s.%s.%d", ingressExceptAsNamePrefix, protocol, idx)

					var allows, excepts []string
					if len(npr.From) == 0 {
						if protocol == kubeovnv1.ProtocolIPv4 {
							allows = []string{"0.0.0.0/0"}
						} else {
							allows = []string{"::/0"}
						}
					} else {
						var allow, except []string
						for _, npp := range npr.From {
							if allow, except, err = c.fetchPolicySelectedAddresses(np.Namespace, protocol, npp); err != nil {
								klog.Errorf("failed to fetch policy selected addresses, %v", err)
								return err
							}
							allows = append(allows, allow...)
							excepts = append(excepts, except...)
						}
					}
					klog.Infof("UpdateNp Ingress, allows is %v, excepts is %v, log %v, protocol %v", allows, excepts, logEnable, protocol)

					if err = c.createAsForNetpol(np.Namespace, npName, "ingress", ingressAllowAsName, allows); err != nil {
						klog.Error(err)
						return err
					}
					if err = c.createAsForNetpol(np.Namespace, npName, "ingress", ingressExceptAsName, excepts); err != nil {
						klog.Error(err)
						return err
					}

					npp := []netv1.NetworkPolicyPort{}
					if len(allows) != 0 || len(excepts) != 0 {
						npp = npr.Ports
					}

					ops, err := c.OVNNbClient.UpdateIngressACLOps(pgName, ingressAllowAsName, ingressExceptAsName, protocol, npp, logEnable, namedPortMap)
					if err != nil {
						klog.Errorf("generate operations that add ingress acls to np %s: %v", key, err)
						return err
					}

					ingressACLOps = append(ingressACLOps, ops...)
				}
				if len(np.Spec.Ingress) == 0 {
					ingressAllowAsName := fmt.Sprintf("%s.%s.all", ingressAllowAsNamePrefix, protocol)
					ingressExceptAsName := fmt.Sprintf("%s.%s.all", ingressExceptAsNamePrefix, protocol)

					if err = c.createAsForNetpol(np.Namespace, npName, "ingress", ingressAllowAsName, nil); err != nil {
						klog.Error(err)
						return err
					}
					if err = c.createAsForNetpol(np.Namespace, npName, "ingress", ingressExceptAsName, nil); err != nil {
						klog.Error(err)
						return err
					}

					ops, err := c.OVNNbClient.UpdateIngressACLOps(pgName, ingressAllowAsName, ingressExceptAsName, protocol, nil, logEnable, namedPortMap)
					if err != nil {
						klog.Errorf("generate operations that add ingress acls to np %s: %v", key, err)
						return err
					}

					ingressACLOps = append(ingressACLOps, ops...)
				}

				if err = c.OVNNbClient.Transact("add-ingress-acls", ingressACLOps); err != nil {
					return fmt.Errorf("add ingress acls to %s: %v", pgName, err)
				}

				if err = c.OVNNbClient.SetACLLog(pgName, protocol, logEnable, true); err != nil {
					// just log and do not return err here
					klog.Errorf("failed to set ingress acl log for np %s, %v", key, err)
				}
			}
		}

		ass, err := c.OVNNbClient.ListAddressSets(map[string]string{
			networkPolicyKey: fmt.Sprintf("%s/%s/%s", np.Namespace, npName, "ingress"),
		})
		if err != nil {
			klog.Errorf("list np %s address sets: %v", key, err)
			return err
		}

		// The format of asName is like "test.network.policy.test.ingress.except.0" or "test.network.policy.test.ingress.allow.0" for ingress
		for _, as := range ass {
			values := strings.Split(as.Name, ".")
			if len(values) <= 1 {
				continue
			}
			idxStr := values[len(values)-1]
			if idxStr == "all" {
				continue
			}
			idx, _ := strconv.Atoi(idxStr)
			if idx >= len(np.Spec.Ingress) {
				if err = c.OVNNbClient.DeleteAddressSet(as.Name); err != nil {
					klog.Errorf("failed to delete np %s address set, %v", key, err)
					return err
				}
			}
		}
	} else {
		if err = c.OVNNbClient.DeleteAcls(pgName, portGroupKey, "to-lport", nil); err != nil {
			klog.Errorf("delete np %s ingress acls: %v", key, err)
			return err
		}

		if err := c.OVNNbClient.DeleteAddressSets(map[string]string{
			networkPolicyKey: fmt.Sprintf("%s/%s/%s", np.Namespace, npName, "ingress"),
		}); err != nil {
			klog.Errorf("delete np %s ingress address set: %v", key, err)
			return err
		}
	}

	egressACLOps, err := c.OVNNbClient.DeleteAclsOps(pgName, portGroupKey, "from-lport", nil)
	if err != nil {
		klog.Errorf("generate operations that clear np %s egress acls: %v", key, err)
		return err
	}

	if hasEgressRule(np) {
		for _, subnet := range subnets {
			for _, cidrBlock := range strings.Split(subnet.Spec.CIDRBlock, ",") {
				protocol := util.CheckProtocol(cidrBlock)

				for idx, npr := range np.Spec.Egress {
					// A single address set must contain addresses of the same type and the name must be unique within table, so IPv4 and IPv6 address set should be different
					egressAllowAsName := fmt.Sprintf("%s.%s.%d", egressAllowAsNamePrefix, protocol, idx)
					egressExceptAsName := fmt.Sprintf("%s.%s.%d", egressExceptAsNamePrefix, protocol, idx)

					var allows, excepts []string
					if len(npr.To) == 0 {
						if protocol == kubeovnv1.ProtocolIPv4 {
							allows = []string{"0.0.0.0/0"}
						} else {
							allows = []string{"::/0"}
						}
					} else {
						var allow, except []string
						for _, npp := range npr.To {
							if allow, except, err = c.fetchPolicySelectedAddresses(np.Namespace, protocol, npp); err != nil {
								klog.Errorf("failed to fetch policy selected addresses, %v", err)
								return err
							}
							allows = append(allows, allow...)
							excepts = append(excepts, except...)
						}
					}
					klog.Infof("UpdateNp Egress, allows is %v, excepts is %v, log %v", allows, excepts, logEnable)

					if err = c.createAsForNetpol(np.Namespace, npName, "egress", egressAllowAsName, allows); err != nil {
						klog.Error(err)
						return err
					}
					if err = c.createAsForNetpol(np.Namespace, npName, "egress", egressExceptAsName, excepts); err != nil {
						klog.Error(err)
						return err
					}

					if len(allows) != 0 || len(excepts) != 0 {
						ops, err := c.OVNNbClient.UpdateEgressACLOps(pgName, egressAllowAsName, egressExceptAsName, protocol, npr.Ports, logEnable, namedPortMap)
						if err != nil {
							klog.Errorf("generate operations that add egress acls to np %s: %v", key, err)
							return err
						}

						egressACLOps = append(egressACLOps, ops...)
					}
				}
				if len(np.Spec.Egress) == 0 {
					egressAllowAsName := fmt.Sprintf("%s.%s.all", egressAllowAsNamePrefix, protocol)
					egressExceptAsName := fmt.Sprintf("%s.%s.all", egressExceptAsNamePrefix, protocol)

					if err = c.createAsForNetpol(np.Namespace, npName, "egress", egressAllowAsName, nil); err != nil {
						klog.Error(err)
						return err
					}
					if err = c.createAsForNetpol(np.Namespace, npName, "egress", egressExceptAsName, nil); err != nil {
						klog.Error(err)
						return err
					}

					ops, err := c.OVNNbClient.UpdateEgressACLOps(pgName, egressAllowAsName, egressExceptAsName, protocol, nil, logEnable, namedPortMap)
					if err != nil {
						klog.Errorf("generate operations that add egress acls to np %s: %v", key, err)
						return err
					}

					egressACLOps = append(egressACLOps, ops...)
				}

				if err = c.OVNNbClient.Transact("add-egress-acls", egressACLOps); err != nil {
					return fmt.Errorf("add egress acls to %s: %v", pgName, err)
				}

				if err = c.OVNNbClient.SetACLLog(pgName, protocol, logEnable, false); err != nil {
					// just log and do not return err here
					klog.Errorf("failed to set egress acl log for np %s, %v", key, err)
				}
			}
		}

		ass, err := c.OVNNbClient.ListAddressSets(map[string]string{
			networkPolicyKey: fmt.Sprintf("%s/%s/%s", np.Namespace, npName, "egress"),
		})
		if err != nil {
			klog.Errorf("list np %s address sets: %v", key, err)
			return err
		}

		// The format of asName is like "test.network.policy.test.egress.except.0" or "test.network.policy.test.egress.allow.0" for egress
		for _, as := range ass {
			values := strings.Split(as.Name, ".")
			if len(values) <= 1 {
				continue
			}
			idxStr := values[len(values)-1]
			if idxStr == "all" {
				continue
			}

			idx, _ := strconv.Atoi(idxStr)
			if idx >= len(np.Spec.Egress) {
				if err = c.OVNNbClient.DeleteAddressSet(as.Name); err != nil {
					klog.Errorf("delete np %s address set: %v", key, err)
					return err
				}
			}
		}
	} else {
		if err = c.OVNNbClient.DeleteAcls(pgName, portGroupKey, "from-lport", nil); err != nil {
			klog.Errorf("delete np %s egress acls: %v", key, err)
			return err
		}

		if err := c.OVNNbClient.DeleteAddressSets(map[string]string{
			networkPolicyKey: fmt.Sprintf("%s/%s/%s", np.Namespace, npName, "egress"),
		}); err != nil {
			klog.Errorf("delete np %s egress address set: %v", key, err)
			return err
		}
	}

	for _, subnet := range subnets {
		if err = c.OVNNbClient.CreateGatewayACL("", pgName, subnet.Spec.Gateway); err != nil {
			klog.Errorf("create gateway acl: %v", err)
			return err
		}
	}
	return nil
}

func (c *Controller) handleDeleteNp(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.npKeyMutex.LockKey(key)
	defer func() { _ = c.npKeyMutex.UnlockKey(key) }()
	klog.Infof("handle delete network policy %s", key)

	npName := name
	nameArray := []rune(name)
	if !unicode.IsLetter(nameArray[0]) {
		npName = "np" + name
	}

	pgName := strings.ReplaceAll(fmt.Sprintf("%s.%s", npName, namespace), "-", ".")
	if err = c.OVNNbClient.DeletePortGroup(pgName); err != nil {
		klog.Errorf("delete np %s port group: %v", key, err)
	}

	if err := c.OVNNbClient.DeleteAddressSets(map[string]string{
		networkPolicyKey: fmt.Sprintf("%s/%s/%s", namespace, npName, "service"),
	}); err != nil {
		klog.Errorf("delete np %s service address set: %v", key, err)
		return err
	}

	if err := c.OVNNbClient.DeleteAddressSets(map[string]string{
		networkPolicyKey: fmt.Sprintf("%s/%s/%s", namespace, npName, "ingress"),
	}); err != nil {
		klog.Errorf("delete np %s ingress address set: %v", key, err)
		return err
	}

	if err := c.OVNNbClient.DeleteAddressSets(map[string]string{
		networkPolicyKey: fmt.Sprintf("%s/%s/%s", namespace, npName, "egress"),
	}); err != nil {
		klog.Errorf("delete np %s egress address set: %v", key, err)
		return err
	}

	return nil
}

func (c *Controller) fetchSelectedPorts(namespace string, selector *metav1.LabelSelector) ([]string, []string, error) {
	var subnets []string
	sel, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating label selector, %v", err)
	}
	pods, err := c.podsLister.Pods(namespace).List(sel)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list pods, %v", err)
	}

	ports := make([]string, 0, len(pods))
	for _, pod := range pods {
		if pod.Spec.HostNetwork {
			continue
		}
		podName := c.getNameByPod(pod)
		podNets, err := c.getPodKubeovnNets(pod)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get pod networks, %v", err)
		}

		for _, podNet := range podNets {
			if !isOvnSubnet(podNet.Subnet) {
				continue
			}

			if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)] == "true" {
				ports = append(ports, ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName))
				// Pod selected by networkpolicy has its own subnet which is not the default subnet
				subnets = append(subnets, podNet.Subnet.Name)
			}
		}
	}
	subnets = slices.Compact(subnets)
	return ports, subnets, nil
}

func hasIngressRule(np *netv1.NetworkPolicy) bool {
	for _, pt := range np.Spec.PolicyTypes {
		if strings.Contains(string(pt), string(netv1.PolicyTypeIngress)) {
			return true
		}
	}
	return np.Spec.Ingress != nil
}

func hasEgressRule(np *netv1.NetworkPolicy) bool {
	for _, pt := range np.Spec.PolicyTypes {
		if strings.Contains(string(pt), string(netv1.PolicyTypeEgress)) {
			return true
		}
	}
	return np.Spec.Egress != nil
}

func (c *Controller) fetchPolicySelectedAddresses(namespace, protocol string, npp netv1.NetworkPolicyPeer) ([]string, []string, error) {
	selectedAddresses := []string{}
	exceptAddresses := []string{}

	// ingress.from.ipblock or egress.to.ipblock
	if npp.IPBlock != nil && util.CheckProtocol(npp.IPBlock.CIDR) == protocol {
		selectedAddresses = append(selectedAddresses, npp.IPBlock.CIDR)
		if npp.IPBlock.Except != nil {
			exceptAddresses = append(exceptAddresses, npp.IPBlock.Except...)
		}
	}
	if npp.NamespaceSelector == nil && npp.PodSelector == nil {
		return selectedAddresses, exceptAddresses, nil
	}

	selectedNs := []string{}
	if npp.NamespaceSelector == nil {
		selectedNs = append(selectedNs, namespace)
	} else {
		sel, err := metav1.LabelSelectorAsSelector(npp.NamespaceSelector)
		if err != nil {
			return nil, nil, fmt.Errorf("error creating label selector, %v", err)
		}
		nss, err := c.namespacesLister.List(sel)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to list ns, %v", err)
		}
		for _, ns := range nss {
			selectedNs = append(selectedNs, ns.Name)
		}
	}

	var sel labels.Selector
	if npp.PodSelector == nil {
		sel = labels.Everything()
	} else {
		sel, _ = metav1.LabelSelectorAsSelector(npp.PodSelector)
	}

	for _, ns := range selectedNs {
		pods, err := c.podsLister.Pods(ns).List(sel)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to list pod, %v", err)
		}
		svcs, err := c.servicesLister.Services(ns).List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list svc, %v", err)
			return nil, nil, fmt.Errorf("failed to list svc, %v", err)
		}

		for _, pod := range pods {
			podNets, err := c.getPodKubeovnNets(pod)
			if err != nil {
				klog.Errorf("failed to get pod nets %v", err)
				return nil, nil, err
			}
			for _, podNet := range podNets {
				podIPAnnotation := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, podNet.ProviderName)]
				podIPs := strings.Split(podIPAnnotation, ",")
				for _, podIP := range podIPs {
					if podIP != "" && util.CheckProtocol(podIP) == protocol {
						selectedAddresses = append(selectedAddresses, podIP)
					}
				}
				if len(svcs) == 0 {
					continue
				}

				svcIPs, err := svcMatchPods(svcs, pod, protocol)
				if err != nil {
					return nil, nil, err
				}
				selectedAddresses = append(selectedAddresses, svcIPs...)
			}
		}
	}
	return selectedAddresses, exceptAddresses, nil
}

func svcMatchPods(svcs []*corev1.Service, pod *corev1.Pod, protocol string) ([]string, error) {
	matchSvcs := []string{}
	// find svc ip by pod's info
	for _, svc := range svcs {
		isMatch, err := isSvcMatchPod(svc, pod)
		if err != nil {
			return nil, err
		}
		if isMatch {
			clusterIPs := util.ServiceClusterIPs(*svc)
			protocolClusterIPs := getProtocolSvcIP(clusterIPs, protocol)
			if len(protocolClusterIPs) != 0 {
				matchSvcs = append(matchSvcs, protocolClusterIPs...)
			}
		}
	}
	return matchSvcs, nil
}

func getProtocolSvcIP(clusterIPs []string, protocol string) []string {
	protocolClusterIPs := []string{}
	for _, clusterIP := range clusterIPs {
		if clusterIP != "" && clusterIP != corev1.ClusterIPNone && util.CheckProtocol(clusterIP) == protocol {
			protocolClusterIPs = append(protocolClusterIPs, clusterIP)
		}
	}
	return protocolClusterIPs
}

func isSvcMatchPod(svc *corev1.Service, pod *corev1.Pod) (bool, error) {
	ss := metav1.SetAsLabelSelector(svc.Spec.Selector)
	sel, err := metav1.LabelSelectorAsSelector(ss)
	if err != nil {
		return false, fmt.Errorf("error fetch label selector, %v", err)
	}
	if pod.Labels == nil {
		return false, nil
	}
	if sel.Matches(labels.Set(pod.Labels)) {
		return true, nil
	}
	return false, nil
}

func (c *Controller) podMatchNetworkPolicies(pod *corev1.Pod) []string {
	podNs, err := c.namespacesLister.Get(pod.Namespace)
	if err != nil {
		klog.Errorf("failed to get namespace %s: %v", pod.Namespace, err)
		utilruntime.HandleError(err)
		return nil
	}

	nps, err := c.npsLister.NetworkPolicies(corev1.NamespaceAll).List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list network policies: %v", err)
		utilruntime.HandleError(err)
		return nil
	}

	match := []string{}
	for _, np := range nps {
		if isPodMatchNetworkPolicy(pod, *podNs, np, np.Namespace) {
			match = append(match, fmt.Sprintf("%s/%s", np.Namespace, np.Name))
		}
	}
	return match
}

func (c *Controller) svcMatchNetworkPolicies(svc *corev1.Service) ([]string, error) {
	// find all match pod
	pods, err := c.podsLister.Pods(svc.Namespace).List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("failed to list pods, %v", err)
	}

	// find all match netpol
	nps, err := c.npsLister.NetworkPolicies(corev1.NamespaceAll).List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("failed to list netpols, %v", err)
	}
	match := []string{}
	for _, pod := range pods {
		podNs, _ := c.namespacesLister.Get(pod.Namespace)
		for _, np := range nps {
			if isPodMatchNetworkPolicy(pod, *podNs, np, np.Namespace) {
				match = append(match, fmt.Sprintf("%s/%s", np.Namespace, np.Name))
				klog.V(3).Infof("svc %s/%s match np %s/%s", svc.Namespace, svc.Name, np.Namespace, np.Name)
			}
		}
	}
	return match, nil
}

func isPodMatchNetworkPolicy(pod *corev1.Pod, podNs corev1.Namespace, policy *netv1.NetworkPolicy, policyNs string) bool {
	sel, _ := metav1.LabelSelectorAsSelector(&policy.Spec.PodSelector)
	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}
	if podNs.Name == policyNs && sel.Matches(labels.Set(pod.Labels)) {
		return true
	}
	for _, npr := range policy.Spec.Ingress {
		for _, npp := range npr.From {
			if isPodMatchPolicyPeer(pod, podNs, npp, policyNs) {
				return true
			}
		}
	}
	for _, npr := range policy.Spec.Egress {
		for _, npp := range npr.To {
			if isPodMatchPolicyPeer(pod, podNs, npp, policyNs) {
				return true
			}
		}
	}
	return false
}

func isPodMatchPolicyPeer(pod *corev1.Pod, podNs corev1.Namespace, policyPeer netv1.NetworkPolicyPeer, policyNs string) bool {
	if policyPeer.IPBlock != nil {
		return false
	}
	if policyPeer.NamespaceSelector == nil {
		if policyNs != podNs.Name {
			return false
		}
	} else {
		nsSel, _ := metav1.LabelSelectorAsSelector(policyPeer.NamespaceSelector)
		if podNs.Labels == nil {
			podNs.Labels = map[string]string{}
		}
		if !nsSel.Matches(labels.Set(podNs.Labels)) {
			return false
		}
	}

	if policyPeer.PodSelector == nil {
		return true
	}

	sel, _ := metav1.LabelSelectorAsSelector(policyPeer.PodSelector)
	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}
	return sel.Matches(labels.Set(pod.Labels))
}

func (c *Controller) namespaceMatchNetworkPolicies(ns *corev1.Namespace) []string {
	nps, _ := c.npsLister.NetworkPolicies(corev1.NamespaceAll).List(labels.Everything())
	match := []string{}
	for _, np := range nps {
		if isNamespaceMatchNetworkPolicy(ns, np) {
			match = append(match, fmt.Sprintf("%s/%s", np.Namespace, np.Name))
		}
	}
	return match
}

func isNamespaceMatchNetworkPolicy(ns *corev1.Namespace, policy *netv1.NetworkPolicy) bool {
	for _, npr := range policy.Spec.Ingress {
		for _, npp := range npr.From {
			if npp.NamespaceSelector != nil {
				nsSel, _ := metav1.LabelSelectorAsSelector(npp.NamespaceSelector)
				if ns.Labels == nil {
					ns.Labels = map[string]string{}
				}
				if nsSel.Matches(labels.Set(ns.Labels)) {
					return true
				}
			}
		}
	}

	for _, npr := range policy.Spec.Egress {
		for _, npp := range npr.To {
			if npp.NamespaceSelector != nil {
				nsSel, _ := metav1.LabelSelectorAsSelector(npp.NamespaceSelector)
				if ns.Labels == nil {
					ns.Labels = map[string]string{}
				}
				if nsSel.Matches(labels.Set(ns.Labels)) {
					return true
				}
			}
		}
	}
	return false
}
