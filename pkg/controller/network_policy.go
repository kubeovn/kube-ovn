package controller

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddNp(obj interface{}) {
	if !c.isLeader() {
		return
	}
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
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue delete np %s", key)
	c.deleteNpQueue.Add(key)
}

func (c *Controller) enqueueUpdateNp(old, new interface{}) {
	if !c.isLeader() {
		return
	}
	oldNp := old.(*netv1.NetworkPolicy)
	newNp := new.(*netv1.NetworkPolicy)
	if !reflect.DeepEqual(oldNp.Spec, newNp.Spec) {
		var key string
		var err error
		if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
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
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
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

func (c *Controller) handleUpdateNp(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	np, err := c.npsLister.NetworkPolicies(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	subnet, err := c.subnetsLister.Get(c.config.DefaultLogicalSwitch)
	if err != nil {
		klog.Errorf("failed to get default subnet %v", err)
		return err
	}
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets %v", err)
		return err
	}

	for _, s := range subnets {
		for _, ns := range s.Spec.Namespaces {
			if ns == np.Namespace {
				subnet = s
				break
			}
		}
	}

	defer func() {
		if err != nil {
			c.recorder.Eventf(np, corev1.EventTypeWarning, "CreateACLFailed", err.Error())
		}
	}()

	// TODO: ovn acl doesn't support address_set name with '-', now we replace '-' by '.'.
	// This may cause conflict if two np with name test-np and test.np. Maybe hash is a better solution,
	// but we do not want to lost the readability now.
	pgName := strings.Replace(fmt.Sprintf("%s.%s", np.Name, np.Namespace), "-", ".", -1)
	ingressAllowAsNamePrefix := strings.Replace(fmt.Sprintf("%s.%s.ingress.allow", np.Name, np.Namespace), "-", ".", -1)
	ingressExceptAsNamePrefix := strings.Replace(fmt.Sprintf("%s.%s.ingress.except", np.Name, np.Namespace), "-", ".", -1)
	egressAllowAsNamePrefix := strings.Replace(fmt.Sprintf("%s.%s.egress.allow", np.Name, np.Namespace), "-", ".", -1)
	egressExceptAsNamePrefix := strings.Replace(fmt.Sprintf("%s.%s.egress.except", np.Name, np.Namespace), "-", ".", -1)

	// delete existed pg to update acl
	if err := c.ovnClient.DeletePortGroup(pgName); err != nil {
		klog.Errorf("failed to delete port group %s before networkpolicy update process, %v", pgName, err)
	}

	if err := c.ovnClient.CreateNpPortGroup(pgName, np.Namespace, np.Name); err != nil {
		klog.Errorf("failed to create port group for np %s, %v", key, err)
		return err
	}

	ports, err := c.fetchSelectedPorts(np.Namespace, &np.Spec.PodSelector)
	if err != nil {
		klog.Errorf("failed to fetch ports, %v", err)
		return err
	}

	err = c.ovnClient.SetPortsToPortGroup(pgName, ports)
	if err != nil && !strings.Contains(err.Error(), "not found") {
		klog.Errorf("failed to set port group, %v", err)
		return err
	}

	// set svc address_set
	svcAsNameIPv4 := strings.Replace(fmt.Sprintf("%s.%s.service.%s", np.Name, np.Namespace, kubeovnv1.ProtocolIPv4), "-", ".", -1)
	svcAsNameIPv6 := strings.Replace(fmt.Sprintf("%s.%s.service.%s", np.Name, np.Namespace, kubeovnv1.ProtocolIPv6), "-", ".", -1)
	svcIpv4s, svcIpv6s, err := c.fetchSelectedSvc(np.Namespace, &np.Spec.PodSelector)
	if err != nil {
		klog.Errorf("failed to fetchSelectedSvc svcIPs result  %v", err)
		return err
	}
	for _, cidrBlock := range strings.Split(subnet.Spec.CIDRBlock, ",") {
		protocol := util.CheckProtocol(cidrBlock)
		svcAsName := svcAsNameIPv4
		svcIPs := svcIpv4s
		if protocol == kubeovnv1.ProtocolIPv6 {
			svcAsName = svcAsNameIPv6
			svcIPs = svcIpv6s
		}
		if err := c.ovnClient.CreateAddressSet(svcAsName, np.Namespace, np.Name, "service"); err != nil {
			klog.Errorf("failed to create address_set %s, %v", svcAsNameIPv4, err)
			return err
		}
		if err := c.ovnClient.SetAddressesToAddressSet(svcIPs, svcAsName); err != nil {
			klog.Errorf("failed to set netpol svc, %v", err)
			return err
		}
	}

	// before update or add ingress info,we should first delete acl and address_set
	if err := c.ovnClient.DeleteACL(pgName, "to-lport"); err != nil {
		klog.Errorf("failed to delete np %s ingress acls, %v", key, err)
		return err
	}

	ingressAsNames, err := c.ovnClient.ListAddressSet(np.Namespace, np.Name, "ingress")
	if err != nil {
		klog.Errorf("failed to list ingress address_set, %v", err)
		return err
	}
	for _, ingressAsName := range ingressAsNames {
		if err := c.ovnClient.DeleteAddressSet(ingressAsName); err != nil {
			klog.Errorf("failed to delete np %s address set, %v", key, err)
			return err
		}
	}

	if hasIngressRule(np) {
		for _, cidrBlock := range strings.Split(subnet.Spec.CIDRBlock, ",") {
			protocol := util.CheckProtocol(cidrBlock)
			svcAsName := svcAsNameIPv4
			if protocol == kubeovnv1.ProtocolIPv6 {
				svcAsName = svcAsNameIPv6
			}

			for idx, npr := range np.Spec.Ingress {
				// A single address set must contain addresses of the same type and the name must be unique within table, so IPv4 and IPv6 address set should be different
				ingressAllowAsName := fmt.Sprintf("%s.%s.%d", ingressAllowAsNamePrefix, protocol, idx)
				ingressExceptAsName := fmt.Sprintf("%s.%s.%d", ingressExceptAsNamePrefix, protocol, idx)

				allows := []string{}
				excepts := []string{}
				if len(npr.From) == 0 {
					if protocol == kubeovnv1.ProtocolIPv4 {
						allows = []string{"0.0.0.0/0"}
					} else if protocol == kubeovnv1.ProtocolIPv6 {
						allows = []string{"::/0"}
					}
					excepts = []string{}
				} else {
					for _, npp := range npr.From {
						allow, except, err := c.fetchPolicySelectedAddresses(np.Namespace, protocol, npp)
						if err != nil {
							klog.Errorf("failed to fetch policy selected addresses, %v", err)
							return err
						}
						allows = append(allows, allow...)
						excepts = append(excepts, except...)
					}
				}
				klog.Infof("UpdateNp Ingress, allows is %v, excepts is %v", allows, excepts)
				if err := c.ovnClient.CreateAddressSet(ingressAllowAsName, np.Namespace, np.Name, "ingress"); err != nil {
					klog.Errorf("failed to create address_set %s, %v", ingressAllowAsName, err)
					return err
				}
				if err := c.ovnClient.SetAddressesToAddressSet(allows, ingressAllowAsName); err != nil {
					klog.Errorf("failed to set ingress allow address_set, %v", err)
					return err
				}

				if err := c.ovnClient.CreateAddressSet(ingressExceptAsName, np.Namespace, np.Name, "ingress"); err != nil {
					klog.Errorf("failed to create address_set %s, %v", ingressExceptAsName, err)
					return err
				}
				if err := c.ovnClient.SetAddressesToAddressSet(excepts, ingressExceptAsName); err != nil {
					klog.Errorf("failed to set ingress except address_set, %v", err)
					return err
				}

				if len(allows) != 0 || len(excepts) != 0 {
					if err := c.ovnClient.CreateIngressACL(fmt.Sprintf("%s/%s", np.Namespace, np.Name), pgName, ingressAllowAsName, ingressExceptAsName, svcAsName, protocol, npr.Ports); err != nil {
						klog.Errorf("failed to create ingress acls for np %s, %v", key, err)
						return err
					}
				}
			}
			if len(np.Spec.Ingress) == 0 {
				ingressAllowAsName := fmt.Sprintf("%s.%s.all", ingressAllowAsNamePrefix, protocol)
				ingressExceptAsName := fmt.Sprintf("%s.%s.all", ingressExceptAsNamePrefix, protocol)
				if err := c.ovnClient.CreateAddressSet(ingressAllowAsName, np.Namespace, np.Name, "ingress"); err != nil {
					klog.Errorf("failed to create address_set %s, %v", ingressAllowAsName, err)
					return err
				}

				if err := c.ovnClient.CreateAddressSet(ingressExceptAsName, np.Namespace, np.Name, "ingress"); err != nil {
					klog.Errorf("failed to create address_set %s, %v", ingressExceptAsName, err)
					return err
				}
				ingressPorts := []netv1.NetworkPolicyPort{}
				if err := c.ovnClient.CreateIngressACL(fmt.Sprintf("%s/%s", np.Namespace, np.Name), pgName, ingressAllowAsName, ingressExceptAsName, svcAsName, protocol, ingressPorts); err != nil {
					klog.Errorf("failed to create ingress acls for np %s, %v", key, err)
					return err
				}
			}
		}

		asNames, err := c.ovnClient.ListAddressSet(np.Namespace, np.Name, "ingress")
		if err != nil {
			klog.Errorf("failed to list address_set, %v", err)
			return err
		}
		// The format of asName is like "test.network.policy.test.ingress.except.0" or "test.network.policy.test.ingress.allow.0" for ingress
		for _, asName := range asNames {
			values := strings.Split(asName, ".")
			if len(values) <= 1 {
				continue
			}
			idxStr := values[len(values)-1]
			if idxStr == "all" {
				continue
			}
			idx, _ := strconv.Atoi(idxStr)
			if idx >= len(np.Spec.Ingress) {
				if err := c.ovnClient.DeleteAddressSet(asName); err != nil {
					klog.Errorf("failed to delete np %s address set, %v", key, err)
					return err
				}
			}
		}
	} else {
		if err := c.ovnClient.DeleteACL(pgName, "to-lport"); err != nil {
			klog.Errorf("failed to delete np %s ingress acls, %v", key, err)
			return err
		}

		asNames, err := c.ovnClient.ListAddressSet(np.Namespace, np.Name, "ingress")
		if err != nil {
			klog.Errorf("failed to list address_set, %v", err)
			return err
		}
		for _, asName := range asNames {
			if err := c.ovnClient.DeleteAddressSet(asName); err != nil {
				klog.Errorf("failed to delete np %s address set, %v", key, err)
				return err
			}
		}
	}

	// before update or add egress info, we should first delete acl and address_set
	if err := c.ovnClient.DeleteACL(pgName, "from-lport"); err != nil {
		klog.Errorf("failed to delete np %s egress acls, %v", key, err)
		return err
	}

	egressAsNames, err := c.ovnClient.ListAddressSet(np.Namespace, np.Name, "egress")
	if err != nil {
		klog.Errorf("failed to list egress address_set, %v", err)
		return err
	}
	for _, egressAsName := range egressAsNames {
		if err := c.ovnClient.DeleteAddressSet(egressAsName); err != nil {
			klog.Errorf("failed to delete np %s address set, %v", key, err)
			return err
		}
	}
	if hasEgressRule(np) {
		for _, cidrBlock := range strings.Split(subnet.Spec.CIDRBlock, ",") {
			protocol := util.CheckProtocol(cidrBlock)
			svcAsName := svcAsNameIPv4
			if protocol == kubeovnv1.ProtocolIPv6 {
				svcAsName = svcAsNameIPv6
			}

			for idx, npr := range np.Spec.Egress {
				// A single address set must contain addresses of the same type and the name must be unique within table, so IPv4 and IPv6 address set should be different
				egressAllowAsName := fmt.Sprintf("%s.%s.%d", egressAllowAsNamePrefix, protocol, idx)
				egressExceptAsName := fmt.Sprintf("%s.%s.%d", egressExceptAsNamePrefix, protocol, idx)

				allows := []string{}
				excepts := []string{}
				if len(npr.To) == 0 {
					if protocol == kubeovnv1.ProtocolIPv4 {
						allows = []string{"0.0.0.0/0"}
					} else if protocol == kubeovnv1.ProtocolIPv6 {
						allows = []string{"::/0"}
					}
					excepts = []string{}
				} else {
					for _, npp := range npr.To {
						allow, except, err := c.fetchPolicySelectedAddresses(np.Namespace, protocol, npp)
						if err != nil {
							klog.Errorf("failed to fetch policy selected addresses, %v", err)
							return err
						}
						allows = append(allows, allow...)
						excepts = append(excepts, except...)
					}
				}
				klog.Infof("UpdateNp Egress, allows is %v, excepts is %v", allows, excepts)
				if err := c.ovnClient.CreateAddressSet(egressAllowAsName, np.Namespace, np.Name, "egress"); err != nil {
					klog.Errorf("failed to create address_set %s, %v", egressAllowAsName, err)
					return err
				}
				if err = c.ovnClient.SetAddressesToAddressSet(allows, egressAllowAsName); err != nil {
					klog.Errorf("failed to set egress allow address_set, %v", err)
					return err
				}

				if err := c.ovnClient.CreateAddressSet(egressExceptAsName, np.Namespace, np.Name, "egress"); err != nil {
					klog.Errorf("failed to create address_set %s, %v", egressExceptAsName, err)
					return err
				}
				if err = c.ovnClient.SetAddressesToAddressSet(excepts, egressExceptAsName); err != nil {
					klog.Errorf("failed to set egress except address_set, %v", err)
					return err
				}

				if len(allows) != 0 || len(excepts) != 0 {
					if err := c.ovnClient.CreateEgressACL(fmt.Sprintf("%s/%s", np.Namespace, np.Name), pgName, egressAllowAsName, egressExceptAsName, protocol, npr.Ports, svcAsName); err != nil {
						klog.Errorf("failed to create egress acls for np %s, %v", key, err)
						return err
					}
				}
			}
			if len(np.Spec.Egress) == 0 {
				egressAllowAsName := fmt.Sprintf("%s.%s.all", egressAllowAsNamePrefix, protocol)
				egressExceptAsName := fmt.Sprintf("%s.%s.all", egressExceptAsNamePrefix, protocol)
				if err := c.ovnClient.CreateAddressSet(egressAllowAsName, np.Namespace, np.Name, "egress"); err != nil {
					klog.Errorf("failed to create address_set %s, %v", egressAllowAsName, err)
					return err
				}

				if err := c.ovnClient.CreateAddressSet(egressExceptAsName, np.Namespace, np.Name, "egress"); err != nil {
					klog.Errorf("failed to create address_set %s, %v", egressExceptAsName, err)
					return err
				}
				egressPorts := []netv1.NetworkPolicyPort{}
				if err := c.ovnClient.CreateEgressACL(fmt.Sprintf("%s/%s", np.Namespace, np.Name), pgName, egressAllowAsName, egressExceptAsName, protocol, egressPorts, svcAsName); err != nil {
					klog.Errorf("failed to create egress acls for np %s, %v", key, err)
					return err
				}
			}
		}

		asNames, err := c.ovnClient.ListAddressSet(np.Namespace, np.Name, "egress")
		if err != nil {
			klog.Errorf("failed to list address_set, %v", err)
			return err
		}
		// The format of asName is like "test.network.policy.test.egress.except.0" or "test.network.policy.test.egress.allow.0" for egress
		for _, asName := range asNames {
			values := strings.Split(asName, ".")
			if len(values) <= 1 {
				continue
			}
			idxStr := values[len(values)-1]
			if idxStr == "all" {
				continue
			}

			idx, _ := strconv.Atoi(idxStr)
			if idx >= len(np.Spec.Egress) {
				if err := c.ovnClient.DeleteAddressSet(asName); err != nil {
					klog.Errorf("failed to delete np %s address set, %v", key, err)
					return err
				}
			}
		}
	}

	if err := c.ovnClient.CreateGatewayACL(pgName, subnet.Spec.Gateway, subnet.Spec.CIDRBlock); err != nil {
		klog.Errorf("failed to create gateway acl, %v", err)
		return err
	}
	return nil
}

func (c *Controller) handleDeleteNp(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	pgName := strings.Replace(fmt.Sprintf("%s.%s", name, namespace), "-", ".", -1)
	if err := c.ovnClient.DeletePortGroup(pgName); err != nil {
		klog.Errorf("failed to delete np %s port group, %v", key, err)
	}

	svcAsNames, err := c.ovnClient.ListAddressSet(namespace, name, "service")
	if err != nil {
		klog.Errorf("failed to list svc address_set, %v", err)
		return err
	}
	for _, asName := range svcAsNames {
		if err := c.ovnClient.DeleteAddressSet(asName); err != nil {
			klog.Errorf("failed to delete np %s address set, %v", key, err)
			return err
		}
	}

	ingressAsNames, err := c.ovnClient.ListAddressSet(namespace, name, "ingress")
	if err != nil {
		klog.Errorf("failed to list address_set, %v", err)
		return err
	}
	for _, asName := range ingressAsNames {
		if err := c.ovnClient.DeleteAddressSet(asName); err != nil {
			klog.Errorf("failed to delete np %s address set, %v", key, err)
			return err
		}
	}

	egressAsNames, err := c.ovnClient.ListAddressSet(namespace, name, "egress")
	if err != nil {
		klog.Errorf("failed to list address_set, %v", err)
		return err
	}
	for _, asName := range egressAsNames {
		if err := c.ovnClient.DeleteAddressSet(asName); err != nil {
			klog.Errorf("failed to delete np %s address set, %v", key, err)
			return err
		}
	}
	return nil
}

func (c *Controller) fetchSelectedPorts(namespace string, selector *metav1.LabelSelector) ([]string, error) {
	sel, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, fmt.Errorf("error createing label selector, %v", err)
	}
	pods, err := c.podsLister.Pods(namespace).List(sel)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods, %v", err)
	}

	ports := make([]string, 0, len(pods))
	for _, pod := range pods {
		if !isPodAlive(pod) {
			continue
		}
		podName := c.getNameByPod(pod)
		if !pod.Spec.HostNetwork && pod.Annotations[util.AllocatedAnnotation] == "true" {
			ports = append(ports, fmt.Sprintf("%s.%s", podName, pod.Namespace))
		}
	}
	return ports, nil
}

func (c *Controller) fetchSelectedSvc(namespace string, selector *metav1.LabelSelector) ([]string, []string, error) {
	sel, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating label selector, %v", err)
	}
	pods, err := c.podsLister.Pods(namespace).List(sel)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list pods, %v", err)
	}

	svcIpv4s := make([]string, 0)
	svcIpv6s := make([]string, 0)
	svcs, err := c.servicesLister.Services(namespace).List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list svc, %v", err)
		return nil, nil, err
	}

	for _, pod := range pods {
		if !isPodAlive(pod) {
			continue
		}
		if !pod.Spec.HostNetwork && pod.Annotations[util.AllocatedAnnotation] == "true" {
			svcIpv4, err := svcMatchPods(svcs, pod, kubeovnv1.ProtocolIPv4)
			if err != nil {
				return nil, nil, err
			}
			svcIpv4s = append(svcIpv4s, svcIpv4...)

			svcIpv6, err := svcMatchPods(svcs, pod, kubeovnv1.ProtocolIPv6)
			if err != nil {
				return nil, nil, err
			}
			svcIpv6s = append(svcIpv6s, svcIpv6...)
		}
	}
	return svcIpv4s, svcIpv6s, nil
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
			return nil, nil, fmt.Errorf("error createing label selector, %v", err)
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
			for _, podIP := range pod.Status.PodIPs {
				if podIP.IP != "" && util.CheckProtocol(podIP.IP) == protocol {
					selectedAddresses = append(selectedAddresses, podIP.IP)
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
			clusterIPs := svc.Spec.ClusterIPs
			if len(clusterIPs) == 0 && svc.Spec.ClusterIP != "" && svc.Spec.ClusterIP != corev1.ClusterIPNone {
				clusterIPs = []string{svc.Spec.ClusterIP}
			}
			protocolClusterIPs := getProtocolSvcIp(clusterIPs, protocol)
			if len(protocolClusterIPs) != 0 {
				matchSvcs = append(matchSvcs, protocolClusterIPs...)
			}
		}
	}
	return matchSvcs, nil
}
func getProtocolSvcIp(clusterIPs []string, protocol string) []string {
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
	podNs, _ := c.namespacesLister.Get(pod.Namespace)
	nps, _ := c.npsLister.NetworkPolicies(corev1.NamespaceAll).List(labels.Everything())
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
