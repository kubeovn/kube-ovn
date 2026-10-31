package controller

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/puzpuzpuz/xsync/v4"
	"github.com/scylladb/go-set/strset"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/utils/set"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	NetworkPolicyEnforcementStandard = "standard"
	NetworkPolicyEnforcementLax      = "lax"
	networkPolicyNamedPortKey        = "named-port"
)

type networkPolicyNamedPortVariant struct {
	name     string
	protocol corev1.Protocol
	port     int32
	ports    []string
	pgName   string
}

func networkPolicyNamedPortGroupName(baseName, name string, protocol corev1.Protocol, port int32) string {
	key := fmt.Sprintf("%s\x00%s\x00%d", name, protocol, port)
	sum := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%s.named.%x", baseName, sum[:6])
}

func networkPolicyPortVariants(ports []netv1.NetworkPolicyPort) (numeric, named []netv1.NetworkPolicyPort) {
	for _, port := range ports {
		if port.Port == nil || port.Port.Type == intstr.Int {
			numeric = append(numeric, port)
		} else if port.EndPort == nil {
			named = append(named, port)
		}
	}
	return numeric, named
}

func networkPolicyNamedPortVariantKey(name string, protocol corev1.Protocol) string {
	return fmt.Sprintf("%s\x00%s", name, protocol)
}

func networkPolicyResourceName(name string) string {
	first, _ := utf8.DecodeRuneInString(name)
	if !unicode.IsLetter(first) {
		return "np" + name
	}
	return name
}

func networkPolicyPortGroupName(namespace, name string) string {
	return strings.ReplaceAll(fmt.Sprintf("%s.%s", networkPolicyResourceName(name), namespace), "-", ".")
}

func (c *Controller) networkPolicyPortGroupInUse(namespace, portGroupName string) (bool, error) {
	policies, err := c.npsLister.NetworkPolicies(namespace).List(labels.Everything())
	if err != nil {
		return false, err
	}
	for _, policy := range policies {
		if networkPolicyPortGroupName(policy.Namespace, policy.Name) == portGroupName {
			return true, nil
		}
	}
	return false, nil
}

func (c *Controller) enqueueAddNp(obj any) {
	np := obj.(*netv1.NetworkPolicy)
	c.indexNetworkPolicy(np)
	key := cache.MetaObjectToName(np).String()
	klog.V(3).Infof("enqueue add network policy %s", key)
	c.updateNpQueue.Add(key)
}

func (c *Controller) enqueueDeleteNp(obj any) {
	var np *netv1.NetworkPolicy
	switch t := obj.(type) {
	case *netv1.NetworkPolicy:
		np = t
	case cache.DeletedFinalStateUnknown:
		n, ok := t.Obj.(*netv1.NetworkPolicy)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		np = n
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}
	c.policyEventIndex.remove(policyEventRef{kind: policyEventNetworkPolicy, key: np.Namespace + "/" + np.Name})

	key := cache.MetaObjectToName(np).String()
	klog.V(3).Infof("enqueue delete network policy %s", key)
	c.deleteNpQueue.Add(networkPolicyDeleteRequest{
		key:           key,
		policyUID:     np.UID,
		portGroupName: networkPolicyPortGroupName(np.Namespace, np.Name),
	})
}

func (c *Controller) enqueueUpdateNp(oldObj, newObj any) {
	oldNp := oldObj.(*netv1.NetworkPolicy)
	newNp := newObj.(*netv1.NetworkPolicy)
	c.indexNetworkPolicy(newNp)
	if !reflect.DeepEqual(oldNp.Spec, newNp.Spec) ||
		kubeOvnAnnotationsChanged(oldNp.Annotations, newNp.Annotations) {
		key := cache.MetaObjectToName(newNp).String()
		klog.V(3).Infof("enqueue update np %s", key)
		c.updateNpQueue.Add(key)
	}
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

type networkPolicyReconcileContext struct {
	key                       string
	npName                    string
	pgName                    string
	ingressAllowAsNamePrefix  string
	ingressExceptAsNamePrefix string
	egressAllowAsNamePrefix   string
	egressExceptAsNamePrefix  string
	providers                 set.Set[string]
	protocolSet               *strset.Set
	namedPortMap              map[string]*util.NamedPortInfo
	namedVariantsByRef        map[string][]networkPolicyNamedPortVariant
	desiredEgressAddrSets     map[string]struct{}
}

func (c *Controller) handleUpdateNp(key string) (err error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	lockKey := networkPolicyPortGroupName(namespace, name)
	c.npKeyMutex.LockKey(lockKey)
	defer func() { _ = c.npKeyMutex.UnlockKey(lockKey) }()
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
			c.recorder.Eventf(np, corev1.EventTypeWarning, "CreateACLFailed", "%s", err.Error())
		}
	}()
	return c.reconcileNetworkPolicy(key, np)
}

func (c *Controller) reconcileNetworkPolicy(key string, np *netv1.NetworkPolicy) (err error) {
	logEnable := np.Annotations[util.NetworkPolicyLogAnnotation] == "true"

	var logActions []string
	if np.Annotations[util.ACLActionsLogAnnotation] != "" {
		logActions = strings.Split(np.Annotations[util.ACLActionsLogAnnotation], ",")
	} else {
		logActions = []string{ovnnb.ACLActionDrop}
	}
	logRate := parseACLLogRate(np.Annotations)
	providers := parsePolicyFor(np)
	ctx, err := c.prepareNetworkPolicyContext(key, np, providers)
	if err != nil {
		return err
	}

	samplingState := c.prepareNetworkPolicyACLSampling(key, ctx.pgName, np)
	ingress, err := c.reconcileNetworkPolicyIngress(ctx, np, logEnable, logActions, logRate)
	if err != nil {
		return err
	}
	if ingress && !c.setNetworkPolicyACLLog(ctx.pgName, key, logEnable, true) {
		samplingState = nil
	}
	egress, err := c.reconcileNetworkPolicyEgress(ctx, np, logEnable, logActions, logRate)
	if err != nil {
		return err
	}
	if egress && !c.setNetworkPolicyACLLog(ctx.pgName, key, logEnable, false) {
		samplingState = nil
	}

	enforcementLax := c.isNetworkPolicyEnforcementLax(np)
	if !enforcementLax && ctx.protocolSet.Has(kubeovnv1.ProtocolIPv6) {
		if err = c.OVNNbClient.CreateGatewayACL("", ctx.pgName); err != nil {
			return fmt.Errorf("create gateway acl: %w", err)
		}
	}
	c.queueNetworkPolicyACLSampling(key, samplingState)
	return nil
}

func (c *Controller) prepareNetworkPolicyContext(key string, np *netv1.NetworkPolicy, providers set.Set[string]) (*networkPolicyReconcileContext, error) {
	npName := networkPolicyResourceName(np.Name)
	ctx := &networkPolicyReconcileContext{
		key: key, npName: npName, providers: providers,
		pgName:                    networkPolicyPortGroupName(np.Namespace, np.Name),
		ingressAllowAsNamePrefix:  strings.ReplaceAll(fmt.Sprintf("%s.%s.ingress.allow", npName, np.Namespace), "-", "."),
		ingressExceptAsNamePrefix: strings.ReplaceAll(fmt.Sprintf("%s.%s.ingress.except", npName, np.Namespace), "-", "."),
		egressAllowAsNamePrefix:   strings.ReplaceAll(fmt.Sprintf("%s.%s.egress.allow", npName, np.Namespace), "-", "."),
		egressExceptAsNamePrefix:  strings.ReplaceAll(fmt.Sprintf("%s.%s.egress.except", npName, np.Namespace), "-", "."),
	}
	if err := c.OVNNbClient.CreatePortGroup(ctx.pgName, map[string]string{networkPolicyKey: key}); err != nil {
		return nil, err
	}
	ports, subnetNames, err := c.fetchSelectedPorts(np.Namespace, &np.Spec.PodSelector, providers)
	if err != nil {
		return nil, err
	}
	ctx.protocolSet = strset.NewWithSize(2)
	for _, subnetName := range subnetNames {
		subnet, err := c.subnetsLister.Get(subnetName)
		if err != nil {
			return nil, err
		}
		if subnet.Spec.Protocol == kubeovnv1.ProtocolDual {
			ctx.protocolSet.Add(kubeovnv1.ProtocolIPv4, kubeovnv1.ProtocolIPv6)
		} else {
			ctx.protocolSet.Add(subnet.Spec.Protocol)
		}
	}
	if err := c.OVNNbClient.PortGroupSetPorts(ctx.pgName, ports); err != nil {
		return nil, err
	}
	if err := c.prepareNetworkPolicyNamedPortGroups(ctx, np); err != nil {
		return nil, err
	}
	ctx.namedPortMap = c.namedPort.GetNamedPortByNs(np.Namespace)
	return ctx, nil
}

func (c *Controller) prepareNetworkPolicyNamedPortGroups(ctx *networkPolicyReconcileContext, np *netv1.NetworkPolicy) error {
	namedVariants, err := c.networkPolicyNamedPortVariants(np, ctx.providers)
	if err != nil {
		return err
	}
	desired := make(map[string]struct{}, len(namedVariants))
	ctx.namedVariantsByRef = make(map[string][]networkPolicyNamedPortVariant, len(namedVariants))
	for _, variant := range namedVariants {
		desired[variant.pgName] = struct{}{}
		ref := networkPolicyNamedPortVariantKey(variant.name, variant.protocol)
		ctx.namedVariantsByRef[ref] = append(ctx.namedVariantsByRef[ref], variant)
		if err := c.OVNNbClient.CreatePortGroup(variant.pgName, map[string]string{networkPolicyKey: ctx.key, networkPolicyNamedPortKey: "true"}); err != nil {
			return err
		}
		if err := c.OVNNbClient.PortGroupSetPorts(variant.pgName, variant.ports); err != nil {
			return err
		}
		if err := c.OVNNbClient.DeleteAcls(variant.pgName, portGroupKey, ovnnb.ACLDirectionToLport, nil); err != nil {
			return err
		}
	}
	namedPortGroups, err := c.OVNNbClient.ListPortGroups(map[string]string{networkPolicyKey: ctx.key, networkPolicyNamedPortKey: "true"})
	if err != nil {
		return err
	}
	for _, group := range namedPortGroups {
		if _, ok := desired[group.Name]; ok {
			continue
		}
		if err := c.OVNNbClient.DeleteAcls(group.Name, portGroupKey, ovnnb.ACLDirectionToLport, nil); err != nil {
			return err
		}
		for _, meterName := range networkPolicyMeterNames(group.Name) {
			if err := c.OVNNbClient.DeleteMeter(meterName); err != nil {
				return err
			}
		}
		if err := c.OVNNbClient.DeletePortGroup(group.Name); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) reconcileNetworkPolicyIngress(ctx *networkPolicyReconcileContext, np *netv1.NetworkPolicy, logEnable bool, logActions []string, logRate int) (bool, error) {
	ops, err := c.OVNNbClient.DeleteAclsOps(ctx.pgName, portGroupKey, ovnnb.ACLDirectionToLport, nil)
	if err != nil {
		return false, err
	}
	if !hasIngressRule(np) {
		if err := c.OVNNbClient.DeleteAcls(ctx.pgName, portGroupKey, ovnnb.ACLDirectionToLport, nil); err != nil {
			return false, err
		}
		if err := c.OVNNbClient.DeleteAddressSets(map[string]string{networkPolicyKey: fmt.Sprintf("%s/%s/ingress", np.Namespace, ctx.npName)}); err != nil {
			return false, err
		}
		return false, nil
	}
	enforcementLax := c.isNetworkPolicyEnforcementLax(np)
	if ctx.protocolSet.Size() > 0 {
		blockOps, err := c.OVNNbClient.UpdateDefaultBlockACLOps(ctx.key, ctx.pgName, ovnnb.ACLDirectionToLport, logEnable, enforcementLax, logRate)
		if err != nil {
			return false, err
		}
		ops = append(ops, blockOps...)
		if enforcementLax {
			exceptionOps, err := c.OVNNbClient.UpdateDefaultBlockExceptionsACLOps(ctx.key, ctx.pgName, np.Namespace, ovnnb.ACLDirectionToLport)
			if err != nil {
				return false, err
			}
			ops = append(ops, exceptionOps...)
		}
	}
	for _, protocol := range ctx.protocolSet.List() {
		protocolOps, err := c.networkPolicyIngressProtocol(ctx, np, protocol, logEnable, logActions, logRate)
		if err != nil {
			return false, err
		}
		ops = append(ops, protocolOps...)
	}
	if err := c.OVNNbClient.Transact("add-ingress-acls", ops); err != nil {
		return false, err
	}
	return true, c.cleanupNetworkPolicyIngressAddressSets(ctx, np)
}

func (c *Controller) networkPolicyIngressProtocol(ctx *networkPolicyReconcileContext, np *netv1.NetworkPolicy, protocol string, logEnable bool, logActions []string, logRate int) ([]ovsdb.Operation, error) {
	ops := make([]ovsdb.Operation, 0)
	for idx, rule := range np.Spec.Ingress {
		ruleOps, err := c.networkPolicyIngressRule(ctx, np, protocol, idx, rule, logEnable, logActions, logRate)
		if err != nil {
			return nil, err
		}
		ops = append(ops, ruleOps...)
	}
	if len(np.Spec.Ingress) == 0 {
		allow, except := fmt.Sprintf("%s.%s.all", ctx.ingressAllowAsNamePrefix, protocol), fmt.Sprintf("%s.%s.all", ctx.ingressExceptAsNamePrefix, protocol)
		if err := c.createAsForNetpol(np.Namespace, ctx.npName, "ingress", allow, nil); err != nil {
			return nil, err
		}
		if err := c.createAsForNetpol(np.Namespace, ctx.npName, "ingress", except, nil); err != nil {
			return nil, err
		}
		acl := fmt.Sprintf("np/%s.%s/ingress/%s/all", ctx.npName, np.Namespace, protocol)
		return c.OVNNbClient.UpdateIngressACLOps(ctx.pgName, allow, except, protocol, acl, nil, logEnable, logActions, logRate, ctx.namedPortMap)
	}
	return ops, nil
}

func (c *Controller) networkPolicyIngressRule(ctx *networkPolicyReconcileContext, np *netv1.NetworkPolicy, protocol string, idx int, rule netv1.NetworkPolicyIngressRule, logEnable bool, logActions []string, logRate int) ([]ovsdb.Operation, error) {
	allow, except := fmt.Sprintf("%s.%s.%d", ctx.ingressAllowAsNamePrefix, protocol, idx), fmt.Sprintf("%s.%s.%d", ctx.ingressExceptAsNamePrefix, protocol, idx)
	acl := fmt.Sprintf("np/%s.%s/ingress/%s/%d", ctx.npName, np.Namespace, protocol, idx)
	selectorAllows, ipBlocks, err := c.networkPolicyIngressPeers(np.Namespace, protocol, rule.From, ctx.providers)
	if err != nil {
		return nil, err
	}
	if err := c.createAsForNetpol(np.Namespace, ctx.npName, "ingress", allow, selectorAllows); err != nil {
		return nil, err
	}
	if err := c.createAsForNetpol(np.Namespace, ctx.npName, "ingress", except, nil); err != nil {
		return nil, err
	}
	numeric, named := networkPolicyPortVariants(rule.Ports)
	ops := make([]ovsdb.Operation, 0)
	if len(selectorAllows) != 0 && (len(rule.Ports) == 0 || len(numeric) != 0) {
		aclOps, err := c.OVNNbClient.UpdateIngressACLOps(ctx.pgName, allow, except, protocol, acl, numeric, logEnable, logActions, logRate, nil)
		if err != nil {
			return nil, err
		}
		ops = append(ops, aclOps...)
	}
	for _, namedPort := range named {
		transport := corev1.ProtocolTCP
		if namedPort.Protocol != nil {
			transport = *namedPort.Protocol
		} else {
			namedPort.Protocol = &transport
		}
		for _, variant := range ctx.namedVariantsByRef[networkPolicyNamedPortVariantKey(namedPort.Port.StrVal, transport)] {
			variantRule := rule
			variantRule.Ports = []netv1.NetworkPolicyPort{namedPort}
			portMap := map[string]*util.NamedPortInfo{namedPort.Port.StrVal: {PortID: variant.port, Protocol: string(variant.protocol)}}
			variantACL := fmt.Sprintf("%s/named/%s/%d", acl, variant.name, variant.port)
			if len(selectorAllows) != 0 {
				aclOps, err := c.OVNNbClient.UpdateIngressACLOps(variant.pgName, allow, except, protocol, variantACL, variantRule.Ports, logEnable, logActions, logRate, portMap)
				if err != nil {
					return nil, err
				}
				ops = append(ops, aclOps...)
			}
			if len(ipBlocks) != 0 {
				ipACL := fmt.Sprintf("%s/named/%s/%d", fmt.Sprintf("np/%s.%s/ingress/%s/%d/ipBlock", ctx.npName, np.Namespace, protocol, idx), variant.name, variant.port)
				aclOps, err := c.OVNNbClient.UpdateIngressIPBlockACLOps(variant.pgName, protocol, ipACL, ipBlocks, variantRule.Ports, logEnable, logActions, logRate, portMap)
				if err != nil {
					return nil, err
				}
				ops = append(ops, aclOps...)
			}
		}
	}
	if len(ipBlocks) != 0 && (len(rule.Ports) == 0 || len(numeric) != 0) {
		ipACL := fmt.Sprintf("np/%s.%s/ingress/%s/%d/ipBlock", ctx.npName, np.Namespace, protocol, idx)
		aclOps, err := c.OVNNbClient.UpdateIngressIPBlockACLOps(ctx.pgName, protocol, ipACL, ipBlocks, numeric, logEnable, logActions, logRate, nil)
		if err != nil {
			return nil, err
		}
		ops = append(ops, aclOps...)
	}
	return ops, nil
}

func (c *Controller) networkPolicyIngressPeers(namespace, protocol string, peers []netv1.NetworkPolicyPeer, providers set.Set[string]) ([]string, []netv1.IPBlock, error) {
	if len(peers) == 0 {
		if protocol == kubeovnv1.ProtocolIPv4 {
			return []string{"0.0.0.0/0"}, nil, nil
		}
		return []string{"::/0"}, nil, nil
	}
	var allows []string
	var blocks []netv1.IPBlock
	for _, peer := range peers {
		if peer.IPBlock != nil {
			blocks = append(blocks, *peer.IPBlock)
			continue
		}
		allow, _, err := c.fetchPolicySelectedAddresses(namespace, protocol, peer, providers)
		if err != nil {
			return nil, nil, err
		}
		allows = append(allows, allow...)
	}
	return allows, blocks, nil
}

func (c *Controller) cleanupNetworkPolicyIngressAddressSets(ctx *networkPolicyReconcileContext, np *netv1.NetworkPolicy) error {
	sets, err := c.OVNNbClient.ListAddressSets(map[string]string{networkPolicyKey: fmt.Sprintf("%s/%s/ingress", np.Namespace, ctx.npName)})
	if err != nil {
		return err
	}
	for _, addressSet := range sets {
		values := strings.Split(addressSet.Name, ".")
		if len(values) <= 1 || values[len(values)-1] == "all" {
			continue
		}
		idx, _ := strconv.Atoi(values[len(values)-1])
		if idx >= len(np.Spec.Ingress) {
			if err := c.OVNNbClient.DeleteAddressSet(addressSet.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Controller) reconcileNetworkPolicyEgress(ctx *networkPolicyReconcileContext, np *netv1.NetworkPolicy, logEnable bool, logActions []string, logRate int) (bool, error) {
	ops, err := c.OVNNbClient.DeleteAclsOps(ctx.pgName, portGroupKey, ovnnb.ACLDirectionFromLport, nil)
	if err != nil {
		return false, err
	}
	if !hasEgressRule(np) {
		if err := c.OVNNbClient.DeleteAcls(ctx.pgName, portGroupKey, ovnnb.ACLDirectionFromLport, nil); err != nil {
			return false, err
		}
		if err := c.OVNNbClient.DeleteAddressSets(map[string]string{networkPolicyKey: fmt.Sprintf("%s/%s/egress", np.Namespace, ctx.npName)}); err != nil {
			return false, err
		}
		return false, nil
	}
	ctx.desiredEgressAddrSets = make(map[string]struct{}, len(np.Spec.Egress)*2)
	enforcementLax := c.isNetworkPolicyEnforcementLax(np)
	if ctx.protocolSet.Size() > 0 {
		blockOps, err := c.OVNNbClient.UpdateDefaultBlockACLOps(ctx.key, ctx.pgName, ovnnb.ACLDirectionFromLport, logEnable, enforcementLax, logRate)
		if err != nil {
			return false, err
		}
		ops = append(ops, blockOps...)
		if enforcementLax {
			exceptionOps, err := c.OVNNbClient.UpdateDefaultBlockExceptionsACLOps(ctx.key, ctx.pgName, np.Namespace, ovnnb.ACLDirectionFromLport)
			if err != nil {
				return false, err
			}
			ops = append(ops, exceptionOps...)
		}
	}
	for _, protocol := range ctx.protocolSet.List() {
		protocolOps, err := c.networkPolicyEgressProtocol(ctx, np, protocol, logEnable, logActions, logRate)
		if err != nil {
			return false, err
		}
		ops = append(ops, protocolOps...)
	}
	if err := c.OVNNbClient.Transact("add-egress-acls", ops); err != nil {
		return false, err
	}
	return true, c.cleanupNetworkPolicyEgressAddressSets(ctx, np)
}

func (c *Controller) networkPolicyEgressProtocol(ctx *networkPolicyReconcileContext, np *netv1.NetworkPolicy, protocol string, logEnable bool, logActions []string, logRate int) ([]ovsdb.Operation, error) {
	ops := make([]ovsdb.Operation, 0)
	for idx, rule := range np.Spec.Egress {
		ruleOps, err := c.networkPolicyEgressRule(ctx, np, protocol, idx, rule, logEnable, logActions, logRate)
		if err != nil {
			return nil, err
		}
		ops = append(ops, ruleOps...)
	}
	if len(np.Spec.Egress) == 0 {
		allow, except := fmt.Sprintf("%s.%s.all", ctx.egressAllowAsNamePrefix, protocol), fmt.Sprintf("%s.%s.all", ctx.egressExceptAsNamePrefix, protocol)
		if err := c.createAsForNetpol(np.Namespace, ctx.npName, "egress", allow, nil); err != nil {
			return nil, err
		}
		if err := c.createAsForNetpol(np.Namespace, ctx.npName, "egress", except, nil); err != nil {
			return nil, err
		}
		ctx.desiredEgressAddrSets[allow] = struct{}{}
		ctx.desiredEgressAddrSets[except] = struct{}{}
		acl := fmt.Sprintf("np/%s.%s/egress/%s/all", ctx.npName, np.Namespace, protocol)
		return c.OVNNbClient.UpdateEgressACLOps(ctx.pgName, allow, except, protocol, acl, nil, logEnable, logActions, logRate, nil)
	}
	return ops, nil
}

func (c *Controller) networkPolicyEgressRule(ctx *networkPolicyReconcileContext, np *netv1.NetworkPolicy, protocol string, idx int, rule netv1.NetworkPolicyEgressRule, logEnable bool, logActions []string, logRate int) ([]ovsdb.Operation, error) {
	allow, except := fmt.Sprintf("%s.%s.%d", ctx.egressAllowAsNamePrefix, protocol, idx), fmt.Sprintf("%s.%s.%d", ctx.egressExceptAsNamePrefix, protocol, idx)
	acl := fmt.Sprintf("np/%s.%s/egress/%s/%d", ctx.npName, np.Namespace, protocol, idx)
	selectorAllows, ipBlocks, err := c.networkPolicyEgressPeers(np.Namespace, protocol, rule.To, ctx.providers)
	if err != nil {
		return nil, err
	}
	if err := c.createAsForNetpol(np.Namespace, ctx.npName, "egress", allow, selectorAllows); err != nil {
		return nil, err
	}
	if err := c.createAsForNetpol(np.Namespace, ctx.npName, "egress", except, nil); err != nil {
		return nil, err
	}
	ctx.desiredEgressAddrSets[allow] = struct{}{}
	ctx.desiredEgressAddrSets[except] = struct{}{}
	numeric, named := networkPolicyPortVariants(rule.Ports)
	ops := make([]ovsdb.Operation, 0)
	if len(selectorAllows) != 0 && (len(rule.Ports) == 0 || len(numeric) != 0) {
		aclOps, err := c.OVNNbClient.UpdateEgressACLOps(ctx.pgName, allow, except, protocol, acl, numeric, logEnable, logActions, logRate, nil)
		if err != nil {
			return nil, err
		}
		ops = append(ops, aclOps...)
	}
	for _, namedPort := range named {
		transport := corev1.ProtocolTCP
		if namedPort.Protocol != nil {
			transport = *namedPort.Protocol
		} else {
			namedPort.Protocol = &transport
		}
		addresses, err := c.fetchPolicySelectedNamedPortAddressesForPeers(np.Namespace, protocol, transport, rule.To, ctx.providers, namedPort.Port.StrVal)
		if err != nil {
			return nil, err
		}
		for variant, values := range addresses {
			if len(values) == 0 {
				continue
			}
			variantAllow := networkPolicyNamedPortAddressSetName(ctx.egressAllowAsNamePrefix, protocol, idx, namedPort.Port.StrVal, variant)
			if err := c.createAsForNetpol(np.Namespace, ctx.npName, "egress", variantAllow, values); err != nil {
				return nil, err
			}
			ctx.desiredEgressAddrSets[variantAllow] = struct{}{}
			portMap := map[string]*util.NamedPortInfo{namedPort.Port.StrVal: {PortID: variant.port, Protocol: string(variant.protocol)}}
			variantACL := fmt.Sprintf("%s/named/%s/%d", acl, namedPort.Port.StrVal, variant.port)
			aclOps, err := c.OVNNbClient.UpdateEgressACLOps(ctx.pgName, variantAllow, except, protocol, variantACL, []netv1.NetworkPolicyPort{namedPort}, logEnable, logActions, logRate, portMap)
			if err != nil {
				return nil, err
			}
			ops = append(ops, aclOps...)
		}
	}
	if len(ipBlocks) != 0 && (len(rule.Ports) == 0 || len(numeric) != 0) {
		ipACL := fmt.Sprintf("np/%s.%s/egress/%s/%d/ipBlock", ctx.npName, np.Namespace, protocol, idx)
		aclOps, err := c.OVNNbClient.UpdateEgressIPBlockACLOps(ctx.pgName, protocol, ipACL, ipBlocks, numeric, logEnable, logActions, logRate, nil)
		if err != nil {
			return nil, err
		}
		ops = append(ops, aclOps...)
	}
	return ops, nil
}

func (c *Controller) networkPolicyEgressPeers(namespace, protocol string, peers []netv1.NetworkPolicyPeer, providers set.Set[string]) ([]string, []netv1.IPBlock, error) {
	if len(peers) == 0 {
		if protocol == kubeovnv1.ProtocolIPv4 {
			return []string{"0.0.0.0/0"}, nil, nil
		}
		return []string{"::/0"}, nil, nil
	}
	var allows []string
	var blocks []netv1.IPBlock
	for _, peer := range peers {
		if peer.IPBlock != nil {
			blocks = append(blocks, *peer.IPBlock)
			continue
		}
		allow, _, err := c.fetchPolicySelectedAddresses(namespace, protocol, peer, providers)
		if err != nil {
			return nil, nil, err
		}
		allows = append(allows, allow...)
	}
	return allows, blocks, nil
}

func (c *Controller) cleanupNetworkPolicyEgressAddressSets(ctx *networkPolicyReconcileContext, np *netv1.NetworkPolicy) error {
	sets, err := c.OVNNbClient.ListAddressSets(map[string]string{networkPolicyKey: fmt.Sprintf("%s/%s/egress", np.Namespace, ctx.npName)})
	if err != nil {
		return err
	}
	for _, addressSet := range sets {
		if _, ok := ctx.desiredEgressAddrSets[addressSet.Name]; ok {
			continue
		}
		if err := c.OVNNbClient.DeleteAddressSet(addressSet.Name); err != nil {
			return err
		}
	}
	return nil
}

type networkPolicySamplingState struct {
	// Access is serialized with the policy's npKeyMutex lock.
	request       *ovs.NetworkPolicySamplingRequest
	policyUID     types.UID
	portGroupName string
	ready         bool
}

type networkPolicyDeleteRequest struct {
	key           string
	policyUID     types.UID
	portGroupName string
}

func (c *Controller) prepareNetworkPolicyACLSampling(key, pgName string, np *netv1.NetworkPolicy) *networkPolicySamplingState {
	if !c.config.ACLSampling.Enabled {
		return nil
	}
	if state, ok := c.npSamplingStates.Load(key); ok {
		if state.policyUID == np.UID {
			state.ready = false
			return state
		}
		c.npSamplingStates.Delete(key)
	}

	request, err := c.OVNNbClient.PrepareNetworkPolicyACLSampling(pgName, np.Namespace, np.Name, string(np.UID))
	if err != nil {
		// Sampling is best-effort and must never block NetworkPolicy enforcement.
		recordACLSamplingFailure(aclSamplingOperationPrepare)
		warnACLSamplingPrepareFailure(key, err)
		return nil
	}
	state := &networkPolicySamplingState{request: request, policyUID: np.UID, portGroupName: pgName}
	c.npSamplingStates.Store(key, state)
	return state
}

func (c *Controller) queueNetworkPolicyACLSampling(key string, state *networkPolicySamplingState) {
	if state == nil {
		return
	}
	state.ready = true
	c.npSamplingQueue.Add(key)
}

func (c *Controller) setNetworkPolicyACLLog(pgName, key string, logEnable, isIngress bool) bool {
	if err := c.OVNNbClient.SetNetPolACLLog(pgName, logEnable, isIngress); err != nil {
		// Logging is best-effort for enforcement, but sampling waits for the
		// complete enforcement path to succeed.
		klog.Errorf("failed to set network policy %s ACL log: %v", key, err)
		return false
	}
	return true
}

func (c *Controller) deleteNetworkPolicySamplingState(key, portGroupName string, policyUID types.UID) {
	if c.npSamplingStates == nil {
		return
	}
	if policyUID == "" {
		c.npSamplingStates.Range(func(candidateKey string, state *networkPolicySamplingState) bool {
			if state.portGroupName == portGroupName {
				c.npSamplingStates.Delete(candidateKey)
			}
			return true
		})
		return
	}
	c.npSamplingStates.Compute(key, func(state *networkPolicySamplingState, loaded bool) (*networkPolicySamplingState, xsync.ComputeOp) {
		if loaded && state.policyUID == policyUID {
			return nil, xsync.DeleteOp
		}
		return state, xsync.CancelOp
	})
}

func (c *Controller) handleNetworkPolicyACLSampling(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	lockKey := networkPolicyPortGroupName(namespace, name)
	c.npKeyMutex.LockKey(lockKey)
	defer func() {
		if err := c.npKeyMutex.UnlockKey(lockKey); err != nil {
			klog.Errorf("failed to unlock network policy %s after ACL sampling: %v", key, err)
		}
	}()

	state, ok := c.npSamplingStates.Load(key)
	if !ok || !state.ready {
		return nil
	}
	if err := c.OVNNbClient.ApplyNetworkPolicyACLSampling(c.config.ACLSampling, state.request); err != nil {
		recordACLSamplingFailure(aclSamplingOperationAttach)
		return err
	}
	recordACLSamplingSuccess()
	c.npSamplingStates.Compute(key, func(current *networkPolicySamplingState, loaded bool) (*networkPolicySamplingState, xsync.ComputeOp) {
		if loaded && current == state {
			return nil, xsync.DeleteOp
		}
		return current, xsync.CancelOp
	})
	return nil
}

func (c *Controller) handleDeleteNp(request networkPolicyDeleteRequest) error {
	key := request.key
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	lockKey := request.portGroupName
	if lockKey == "" {
		lockKey = networkPolicyPortGroupName(namespace, name)
	}
	c.npKeyMutex.LockKey(lockKey)
	defer func() { _ = c.npKeyMutex.UnlockKey(lockKey) }()
	klog.Infof("handle delete network policy %s", key)
	inUse, err := c.networkPolicyPortGroupInUse(namespace, lockKey)
	if err != nil {
		return err
	}
	if inUse {
		klog.V(3).Infof("ignore stale delete for network policy %s", key)
		return nil
	}
	c.deleteNetworkPolicySamplingState(key, lockKey, request.policyUID)

	npName := networkPolicyResourceName(name)
	pgName := lockKey
	var firstErr error
	for _, meterName := range networkPolicyMeterNames(pgName) {
		if err := c.OVNNbClient.DeleteMeter(meterName); err != nil {
			klog.Errorf("delete meter %s for np %s: %v", meterName, key, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	if err := c.OVNNbClient.DeletePortGroup(pgName); err != nil {
		klog.Errorf("delete np %s port group: %v", key, err)
		if firstErr == nil {
			firstErr = err
		}
	}

	namedPortGroups, err := c.OVNNbClient.ListPortGroups(map[string]string{
		networkPolicyKey:          key,
		networkPolicyNamedPortKey: "true",
	})
	if err != nil {
		klog.Errorf("list named port groups for deleted np %s: %v", key, err)
		if firstErr == nil {
			firstErr = err
		}
	} else {
		for _, namedPortGroup := range namedPortGroups {
			if err = c.OVNNbClient.DeleteAcls(namedPortGroup.Name, portGroupKey, ovnnb.ACLDirectionToLport, nil); err != nil {
				klog.Errorf("delete named port group %s ACLs: %v", namedPortGroup.Name, err)
				if firstErr == nil {
					firstErr = err
				}
			}
			for _, meterName := range networkPolicyMeterNames(namedPortGroup.Name) {
				if err = c.OVNNbClient.DeleteMeter(meterName); err != nil {
					klog.Errorf("delete named port group %s meter: %v", namedPortGroup.Name, err)
					if firstErr == nil {
						firstErr = err
					}
				}
			}
			if err = c.OVNNbClient.DeletePortGroup(namedPortGroup.Name); err != nil {
				klog.Errorf("delete named port group %s: %v", namedPortGroup.Name, err)
				if firstErr == nil {
					firstErr = err
				}
			}
		}
	}

	if err := c.OVNNbClient.DeleteAddressSets(map[string]string{
		networkPolicyKey: fmt.Sprintf("%s/%s/%s", namespace, npName, "ingress"),
	}); err != nil {
		klog.Errorf("delete np %s ingress address set: %v", key, err)
		if firstErr == nil {
			firstErr = err
		}
	}

	if err := c.OVNNbClient.DeleteAddressSets(map[string]string{
		networkPolicyKey: fmt.Sprintf("%s/%s/%s", namespace, npName, "egress"),
	}); err != nil {
		klog.Errorf("delete np %s egress address set: %v", key, err)
		if firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func parsePolicyFor(np *netv1.NetworkPolicy) set.Set[string] {
	raw := strings.TrimSpace(np.Annotations[util.NetworkPolicyForAnnotation])
	if raw == "" {
		return nil
	}

	providers := set.New[string]()
	invalidMsg := `ignore invalid network_policy_for annotation %q for netpol %s/%s, expect "ovn" or "<namespace>/<net-attach-def>"`

	for token := range strings.SplitSeq(raw, ",") {
		t := strings.TrimSpace(token)
		if t == "" {
			continue
		}

		if strings.EqualFold(t, "ovn") {
			providers.Insert(util.OvnProvider)
			continue
		}
		if strings.Contains(t, "/") {
			parts := strings.SplitN(t, "/", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				klog.Warningf(invalidMsg, t, np.Namespace, np.Name)
				continue
			}
			provider := fmt.Sprintf("%s.%s.%s", parts[1], parts[0], util.OvnProvider)
			providers.Insert(provider)
			continue
		}
		klog.Warningf(invalidMsg, t, np.Namespace, np.Name)
	}

	if providers.Len() == 0 {
		klog.Warningf("network_policy_for annotation has no valid entries; policy %s/%s selects no pods", np.Namespace, np.Name)
		return providers
	}
	return providers
}

func netpolAppliesToProvider(provider string, providers set.Set[string]) bool {
	if providers == nil {
		return true
	}
	return providers.Has(provider)
}

func (c *Controller) fetchSelectedPorts(namespace string, selector *metav1.LabelSelector, providers set.Set[string]) ([]string, []string, error) {
	var subnets []string
	sel, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating label selector, %w", err)
	}
	pods, err := c.podsLister.Pods(namespace).List(sel)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list pods, %w", err)
	}

	ports := make([]string, 0, len(pods))
	for _, pod := range pods {
		if pod.Spec.HostNetwork {
			continue
		}
		podName := c.getNameByPod(pod)
		podNets, err := c.getPodKubeovnNets(pod)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get pod networks, %w", err)
		}

		matchedProvider := false
		for _, podNet := range podNets {
			if !isOvnSubnet(podNet.Subnet) {
				continue
			}
			if !netpolAppliesToProvider(podNet.ProviderName, providers) {
				continue
			}
			matchedProvider = true

			if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)] == "true" {
				ports = append(ports, ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName))
				// Pod selected by networkpolicy has its own subnet which is not the default subnet
				subnets = append(subnets, podNet.Subnet.Name)
			}
		}
		if providers != nil && !matchedProvider {
			klog.V(4).Infof("skip pod %s/%s: no network attachment matches network_policy_for", pod.Namespace, pod.Name)
		}
	}
	subnets = slices.Compact(subnets)
	return ports, subnets, nil
}

func (c *Controller) fetchSelectedNamedPortLSPs(namespace string, selector *metav1.LabelSelector, providers set.Set[string], podNames *strset.Set) ([]string, error) {
	sel, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, fmt.Errorf("error creating label selector, %w", err)
	}
	pods, err := c.podsLister.Pods(namespace).List(sel)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods, %w", err)
	}

	ports := make([]string, 0, podNames.Size())
	for _, pod := range pods {
		if pod.Spec.HostNetwork || !podNames.Has(pod.Name) {
			continue
		}
		podName := c.getNameByPod(pod)
		podNets, err := c.getPodKubeovnNets(pod)
		if err != nil {
			return nil, fmt.Errorf("failed to get pod networks, %w", err)
		}

		for _, podNet := range podNets {
			if !isOvnSubnet(podNet.Subnet) || !netpolAppliesToProvider(podNet.ProviderName, providers) {
				continue
			}
			if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, podNet.ProviderName)] == "true" {
				ports = append(ports, ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName))
			}
		}
	}
	return ports, nil
}

func (c *Controller) networkPolicyNamedPortVariants(np *netv1.NetworkPolicy, providers set.Set[string]) ([]networkPolicyNamedPortVariant, error) {
	indexed := c.namedPort.getNamedPortVariantsByNs(np.Namespace)
	if len(indexed) == 0 {
		return nil, nil
	}

	requested := make(map[string]struct{})
	for _, rule := range np.Spec.Ingress {
		for _, port := range rule.Ports {
			if port.Port == nil || port.Port.Type != intstr.String || port.EndPort != nil {
				continue
			}
			protocol := corev1.ProtocolTCP
			if port.Protocol != nil {
				protocol = *port.Protocol
			}
			requested[fmt.Sprintf("%s\x00%s", port.Port.StrVal, protocol)] = struct{}{}
		}
	}

	variants := make([]networkPolicyNamedPortVariant, 0, len(requested))
	for name, nameVariants := range indexed {
		for key, pods := range nameVariants {
			if _, ok := requested[fmt.Sprintf("%s\x00%s", name, key.protocol)]; !ok {
				continue
			}
			lspNames, err := c.fetchSelectedNamedPortLSPs(np.Namespace, &np.Spec.PodSelector, providers, pods)
			if err != nil {
				return nil, err
			}
			if len(lspNames) == 0 {
				continue
			}
			variants = append(variants, networkPolicyNamedPortVariant{
				name:     name,
				protocol: key.protocol,
				port:     key.port,
				ports:    lspNames,
				pgName:   networkPolicyNamedPortGroupName(networkPolicyPortGroupName(np.Namespace, np.Name), name, key.protocol, key.port),
			})
		}
	}
	return variants, nil
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

func policyLabelSelectorCacheKey(selector *metav1.LabelSelector) (string, error) {
	if selector == nil {
		return "nil", nil
	}

	parsed, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return "", fmt.Errorf("invalid label selector: %w", err)
	}
	return "value:" + parsed.String(), nil
}

func policyIPBlockCacheKey(block *netv1.IPBlock) string {
	if block == nil {
		return "nil"
	}
	return fmt.Sprintf("value:%#v", *block)
}

func policyNetworkPolicyPeerCacheKey(peer netv1.NetworkPolicyPeer) (string, error) {
	podSelector, err := policyLabelSelectorCacheKey(peer.PodSelector)
	if err != nil {
		return "", fmt.Errorf("invalid pod selector: %w", err)
	}
	namespaceSelector, err := policyLabelSelectorCacheKey(peer.NamespaceSelector)
	if err != nil {
		return "", fmt.Errorf("invalid namespace selector: %w", err)
	}
	return strings.Join([]string{
		podSelector,
		namespaceSelector,
		policyIPBlockCacheKey(peer.IPBlock),
	}, "\x00"), nil
}

const policySelectorCacheScanAttempts = 2

func (c *Controller) fetchPolicySelectedAddresses(namespace, protocol string, npp netv1.NetworkPolicyPeer, providers set.Set[string]) ([]string, []string, error) {
	for range policySelectorCacheScanAttempts {
		generation := c.policySelectorCacheGeneration()
		selectedAddresses, exceptAddresses, err := c.fetchPolicySelectedAddressesOnce(namespace, protocol, npp, providers, generation)
		if err != nil {
			return nil, nil, err
		}
		if generation == c.policySelectorCacheGeneration() {
			return selectedAddresses, exceptAddresses, nil
		}
	}
	return nil, nil, errors.New("policy selector cache invalidated during address scan")
}

func (c *Controller) fetchPolicySelectedAddressesOnce(namespace, protocol string, npp netv1.NetworkPolicyPeer, providers set.Set[string], generation uint64) ([]string, []string, error) {
	providerKey := "all"
	if providers != nil {
		providerKey = strings.Join(providers.SortedList(), ",")
	}
	peerKey, err := policyNetworkPolicyPeerCacheKey(npp)
	if err != nil {
		return nil, nil, err
	}
	cacheKey := fmt.Sprintf("%s\x00%s\x00%s\x00%s", namespace, protocol, peerKey, providerKey)
	if selected, except, ok := c.getPolicyAddressCache(cacheKey, generation); ok {
		return selected, except, nil
	}

	selectedAddresses := []string{}
	exceptAddresses := []string{}

	// ingress.from.ipblock or egress.to.ipblock
	if npp.IPBlock != nil {
		if util.CheckProtocol(npp.IPBlock.CIDR) == protocol {
			selectedAddresses = append(selectedAddresses, npp.IPBlock.CIDR)
			if npp.IPBlock.Except != nil {
				exceptAddresses = append(exceptAddresses, npp.IPBlock.Except...)
			}
		}
		return selectedAddresses, exceptAddresses, nil
	}

	selectedNs := []string{}
	if npp.NamespaceSelector == nil {
		selectedNs = append(selectedNs, namespace)
	} else {
		sel, err := metav1.LabelSelectorAsSelector(npp.NamespaceSelector)
		if err != nil {
			return nil, nil, fmt.Errorf("error creating label selector, %w", err)
		}
		nss, err := c.namespacesLister.List(sel)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to list ns, %w", err)
		}
		for _, ns := range nss {
			selectedNs = append(selectedNs, ns.Name)
		}
	}

	var sel labels.Selector
	if npp.PodSelector == nil {
		sel = labels.Everything()
	} else {
		sel, err = metav1.LabelSelectorAsSelector(npp.PodSelector)
		if err != nil {
			return nil, nil, fmt.Errorf("error creating pod label selector, %w", err)
		}
	}

	for _, ns := range selectedNs {
		pods, err := c.podsLister.Pods(ns).List(sel)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to list pod, %w", err)
		}
		for _, pod := range pods {
			podNets, err := c.getPodKubeovnNets(pod)
			if err != nil {
				klog.Errorf("failed to get pod nets %v", err)
				return nil, nil, err
			}
			matchedProvider := false
			for _, podNet := range podNets {
				provider := podNet.ProviderName
				if !netpolAppliesToProvider(provider, providers) {
					continue
				}
				matchedProvider = true

				podIPAnnotation := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, podNet.ProviderName)]
				podIPs := strings.SplitSeq(podIPAnnotation, ",")
				for podIP := range podIPs {
					if podIP != "" && util.CheckProtocol(podIP) == protocol {
						selectedAddresses = append(selectedAddresses, podIP)
					}
				}
			}
			if providers != nil && !matchedProvider {
				klog.V(4).Infof("skip pod %s/%s: no network attachment matches network_policy_for", pod.Namespace, pod.Name)
			}
		}
	}
	c.setPolicyAddressCache(cacheKey, selectedAddresses, exceptAddresses, generation)
	return selectedAddresses, exceptAddresses, nil
}

// fetchPolicySelectedNamedPortAddresses returns destination addresses grouped
// by the concrete protocol/port variant exposed by the selected Pods. Named
// ports are resolved on the destination side of an egress rule, so the lookup
// must retain namespace boundaries and cannot use the policy namespace's
// namespace-wide named-port map.
func (c *Controller) fetchPolicySelectedNamedPortAddresses(namespace, ipProtocol string, transportProtocol corev1.Protocol, npp netv1.NetworkPolicyPeer, providers set.Set[string], name string) (map[namedPortKey][]string, error) {
	if npp.IPBlock != nil {
		return nil, nil
	}

	selectedNamespaces, err := c.policySelectedNamespaces(namespace, npp.NamespaceSelector)
	if err != nil {
		return nil, err
	}

	podSelector := labels.Everything()
	if npp.PodSelector != nil {
		podSelector, err = metav1.LabelSelectorAsSelector(npp.PodSelector)
		if err != nil {
			return nil, fmt.Errorf("error creating pod label selector, %w", err)
		}
	}

	addresses := make(map[namedPortKey][]string)
	for _, selectedNamespace := range selectedNamespaces {
		variants := c.namedPort.getNamedPortVariantsByNs(selectedNamespace)[name]
		if len(variants) == 0 {
			continue
		}
		pods, err := c.podsLister.Pods(selectedNamespace).List(podSelector)
		if err != nil {
			return nil, fmt.Errorf("failed to list pod, %w", err)
		}

		for _, pod := range pods {
			if pod.Spec.HostNetwork {
				continue
			}
			podNets, err := c.getPodKubeovnNets(pod)
			if err != nil {
				return nil, fmt.Errorf("failed to get pod nets, %w", err)
			}

			for variant, podNames := range variants {
				if variant.protocol != transportProtocol || !podNames.Has(pod.Name) {
					continue
				}
				for _, podNet := range podNets {
					if !netpolAppliesToProvider(podNet.ProviderName, providers) {
						continue
					}
					podIPAnnotation := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, podNet.ProviderName)]
					for podIP := range strings.SplitSeq(podIPAnnotation, ",") {
						if podIP != "" && util.CheckProtocol(podIP) == ipProtocol {
							addresses[variant] = append(addresses[variant], podIP)
						}
					}
				}
			}
		}
	}
	return addresses, nil
}

func (c *Controller) fetchPolicySelectedNamedPortAddressesForPeers(namespace, ipProtocol string, transportProtocol corev1.Protocol, peers []netv1.NetworkPolicyPeer, providers set.Set[string], name string) (map[namedPortKey][]string, error) {
	if len(peers) == 0 {
		peers = []netv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{}}}
	}
	addresses := make(map[namedPortKey][]string)
	for _, peer := range peers {
		selected, err := c.fetchPolicySelectedNamedPortAddresses(namespace, ipProtocol, transportProtocol, peer, providers, name)
		if err != nil {
			return nil, err
		}
		for variant, values := range selected {
			addresses[variant] = append(addresses[variant], values...)
		}
	}
	return addresses, nil
}

func (c *Controller) policySelectedNamespaces(namespace string, selector *metav1.LabelSelector) ([]string, error) {
	if selector == nil {
		return []string{namespace}, nil
	}

	nsSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, fmt.Errorf("error creating namespace label selector, %w", err)
	}
	namespaces, err := c.namespacesLister.List(nsSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to list ns, %w", err)
	}
	selected := make([]string, 0, len(namespaces))
	for _, selectedNamespace := range namespaces {
		selected = append(selected, selectedNamespace.Name)
	}
	return selected, nil
}

func networkPolicyNamedPortAddressSetName(prefix, protocol string, index int, name string, port namedPortKey) string {
	key := fmt.Sprintf("%s\x00%s\x00%d\x00%d", name, port.protocol, port.port, index)
	sum := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%s.%s.%d.named.%x", strings.ReplaceAll(prefix, "-", "."), protocol, index, sum[:6])
}

func (c *Controller) enqueueNamespaceNamedPortNetworkPolicies(namespace string) {
	policies, err := c.npsLister.NetworkPolicies(namespace).List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list network policies in namespace %s for named port update: %v", namespace, err)
		return
	}
	for _, policy := range policies {
		if networkPolicyHasNamedPort(policy) {
			c.updateNpQueue.Add(cache.MetaObjectToName(policy).String())
		}
	}
}

func (c *Controller) enqueueNamedPortNetworkPolicies() {
	policies, err := c.npsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list network policies for named port update: %v", err)
		return
	}
	for _, policy := range policies {
		if networkPolicyHasNamedPort(policy) {
			c.updateNpQueue.Add(cache.MetaObjectToName(policy).String())
		}
	}
}

func networkPolicyHasNamedPort(policy *netv1.NetworkPolicy) bool {
	for _, rule := range policy.Spec.Ingress {
		for _, port := range rule.Ports {
			if port.Port != nil && port.Port.Type == intstr.String {
				return true
			}
		}
	}
	for _, rule := range policy.Spec.Egress {
		for _, port := range rule.Ports {
			if port.Port != nil && port.Port.Type == intstr.String {
				return true
			}
		}
	}
	return false
}

func (c *Controller) podMatchNetworkPolicies(pod *corev1.Pod) []string {
	podNs, err := c.namespacesLister.Get(pod.Namespace)
	if err != nil {
		klog.Errorf("failed to get namespace %s: %v", pod.Namespace, err)
		utilruntime.HandleError(err)
		return nil
	}

	match := []string{}
	keys := c.policyEventIndexCandidatesForPod(pod, podNs)
	if !c.policyEventIndex.isReady() {
		policies, err := c.npsLister.NetworkPolicies(corev1.NamespaceAll).List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list network policies: %v", err)
			utilruntime.HandleError(err)
			return nil
		}
		keys = make([]string, 0, len(policies))
		for _, policy := range policies {
			keys = append(keys, cache.MetaObjectToName(policy).String())
		}
	}
	for _, key := range keys {
		namespace, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			klog.Errorf("failed to split network policy key %q: %v", key, err)
			continue
		}
		np, err := c.npsLister.NetworkPolicies(namespace).Get(name)
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				klog.Errorf("failed to get network policy %s: %v", key, err)
				utilruntime.HandleError(err)
			}
			continue
		}
		if isPodMatchNetworkPolicy(pod, podNs, np, np.Namespace) {
			match = append(match, key)
		}
	}
	return match
}

func isPodMatchNetworkPolicy(pod *corev1.Pod, podNs *corev1.Namespace, policy *netv1.NetworkPolicy, policyNs string) bool {
	sel, _ := metav1.LabelSelectorAsSelector(&policy.Spec.PodSelector)
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
		if len(npr.To) == 0 {
			for _, port := range npr.Ports {
				if port.Port == nil || port.Port.Type != intstr.String {
					continue
				}
				protocol := corev1.ProtocolTCP
				if port.Protocol != nil {
					protocol = *port.Protocol
				}
				for ref := range namedPortsForPod(pod) {
					if ref.name == port.Port.StrVal && ref.protocol == protocol {
						return true
					}
				}
			}
		}
		for _, npp := range npr.To {
			if isPodMatchPolicyPeer(pod, podNs, npp, policyNs) {
				return true
			}
		}
	}
	return false
}

func isPodMatchPolicyPeer(pod *corev1.Pod, podNs *corev1.Namespace, policyPeer netv1.NetworkPolicyPeer, policyNs string) bool {
	if policyPeer.IPBlock != nil {
		return false
	}
	if policyPeer.NamespaceSelector == nil {
		if policyNs != podNs.Name {
			return false
		}
	} else if !util.ObjectMatchesLabelSelector(podNs, policyPeer.NamespaceSelector) {
		return false
	}

	return policyPeer.PodSelector == nil || util.ObjectMatchesLabelSelector(pod, policyPeer.PodSelector)
}

func (c *Controller) namespaceMatchNetworkPolicies(ns *corev1.Namespace) []string {
	match := make([]string, 0)
	keys := c.policyEventIndex.candidatesForNamespace(ns.Labels)
	if !c.policyEventIndex.isReady() {
		policies, err := c.npsLister.NetworkPolicies(corev1.NamespaceAll).List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list network policies: %v", err)
			return nil
		}
		keys = make([]string, 0, len(policies))
		for _, policy := range policies {
			keys = append(keys, cache.MetaObjectToName(policy).String())
		}
	}
	for _, key := range keys {
		namespace, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			klog.Errorf("failed to split network policy key %q: %v", key, err)
			continue
		}
		np, err := c.npsLister.NetworkPolicies(namespace).Get(name)
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				klog.Errorf("failed to get network policy %s: %v", key, err)
			}
			continue
		}
		if isNamespaceMatchNetworkPolicy(ns, np) {
			match = append(match, key)
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

func (c *Controller) isNetworkPolicyEnforcementLax(policy *netv1.NetworkPolicy) bool {
	// User provided a custom enforcement through annotations
	if value, ok := policy.Annotations[util.NetworkPolicyEnforcementAnnotation]; ok {
		return value == NetworkPolicyEnforcementLax
	}

	// Fallback to the configuration of the controller
	return c.config.NetworkPolicyEnforcement == NetworkPolicyEnforcementLax
}

func parseACLLogRate(annotations map[string]string) int {
	val := strings.TrimSpace(annotations[util.ACLLogMeterAnnotation])
	if val == "" {
		return 0
	}
	rate, err := strconv.Atoi(val)
	if err != nil || rate <= 0 {
		if val != "" {
			klog.Warningf("invalid acl_log_meter value %q, should be positive integer", val)
		}
		return 0
	}
	return rate
}

func npPortGroupName(namespace, npName string) string {
	return strings.ReplaceAll(fmt.Sprintf("%s.%s", npName, namespace), "-", ".")
}

func networkPolicyMeterNames(pgName string) []string {
	return []string{
		fmt.Sprintf("%s_to-lport_meter", pgName),
		fmt.Sprintf("%s_from-lport_meter", pgName),
	}
}
