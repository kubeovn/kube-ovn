package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alauda/felix/ipsets"
	ovsutil "github.com/digitalocean/go-openvswitch/ovs"
	"github.com/kubeovn/go-iptables/iptables"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovninformer "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// Controller watch pod and namespace changes to update iptables, ipset and ovs qos
type Controller struct {
	config *Configuration

	providerNetworksLister          kubeovnlister.ProviderNetworkLister
	providerNetworksSynced          cache.InformerSynced
	addOrUpdateProviderNetworkQueue workqueue.RateLimitingInterface
	deleteProviderNetworkQueue      workqueue.RateLimitingInterface

	subnetsLister kubeovnlister.SubnetLister
	subnetsSynced cache.InformerSynced
	subnetQueue   workqueue.RateLimitingInterface

	vlansLister kubeovnlister.VlanLister
	vlanSynced  cache.InformerSynced

	podsLister listerv1.PodLister
	podsSynced cache.InformerSynced
	podQueue   workqueue.RateLimitingInterface

	nodesLister listerv1.NodeLister
	nodesSynced cache.InformerSynced

	htbQosLister kubeovnlister.HtbQosLister
	htbQosSynced cache.InformerSynced

	recorder record.EventRecorder

	iptables         map[string]*iptables.IPTables
	iptablesObsolete map[string]*iptables.IPTables
	ipsets           map[string]*ipsets.IPSets
	ipsetLock        sync.Mutex

	protocol       string
	localPodName   string
	localNamespace string

	nmSyncer  *networkManagerSyncer
	ovsClient *ovsutil.Client
}

// NewController init a daemon controller
func NewController(config *Configuration, podInformerFactory, nodeInformerFactory informers.SharedInformerFactory, kubeovnInformerFactory kubeovninformer.SharedInformerFactory) (*Controller, error) {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: config.KubeClient.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: config.NodeName})

	providerNetworkInformer := kubeovnInformerFactory.Kubeovn().V1().ProviderNetworks()
	subnetInformer := kubeovnInformerFactory.Kubeovn().V1().Subnets()
	vlanInformer := kubeovnInformerFactory.Kubeovn().V1().Vlans()
	podInformer := podInformerFactory.Core().V1().Pods()
	nodeInformer := nodeInformerFactory.Core().V1().Nodes()
	htbQosInformer := kubeovnInformerFactory.Kubeovn().V1().HtbQoses()

	controller := &Controller{
		config: config,

		providerNetworksLister:          providerNetworkInformer.Lister(),
		providerNetworksSynced:          providerNetworkInformer.Informer().HasSynced,
		addOrUpdateProviderNetworkQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddOrUpdateProviderNetwork"),
		deleteProviderNetworkQueue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteProviderNetwork"),

		subnetsLister: subnetInformer.Lister(),
		subnetsSynced: subnetInformer.Informer().HasSynced,
		subnetQueue:   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Subnet"),

		vlansLister: vlanInformer.Lister(),
		vlanSynced:  vlanInformer.Informer().HasSynced,

		podsLister: podInformer.Lister(),
		podsSynced: podInformer.Informer().HasSynced,
		podQueue:   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Pod"),

		nodesLister: nodeInformer.Lister(),
		nodesSynced: nodeInformer.Informer().HasSynced,

		htbQosLister: htbQosInformer.Lister(),
		htbQosSynced: htbQosInformer.Informer().HasSynced,

		recorder: recorder,
	}

	node, err := config.KubeClient.CoreV1().Nodes().Get(context.Background(), config.NodeName, metav1.GetOptions{})
	if err != nil {
		util.LogFatalAndExit(err, "failed to get node %s info", config.NodeName)
	}
	controller.protocol = util.CheckProtocol(node.Annotations[util.IpAddressAnnotation])

	ok, err := isLegacyIptablesMode()
	if err != nil {
		klog.Errorf("failed to check iptables mode: %v", err)
		return nil, err
	}
	if !ok {
		// iptables works in nft mode, we should migrate iptables rules
		controller.iptablesObsolete = make(map[string]*iptables.IPTables, 2)
	}

	controller.iptables = make(map[string]*iptables.IPTables)
	controller.ipsets = make(map[string]*ipsets.IPSets)
	controller.ovsClient = ovsutil.New()
	if controller.protocol == kubeovnv1.ProtocolIPv4 || controller.protocol == kubeovnv1.ProtocolDual {
		ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
		if err != nil {
			return nil, err
		}
		controller.iptables[kubeovnv1.ProtocolIPv4] = ipt
		if controller.iptablesObsolete != nil {
			if ipt, err = iptables.NewWithProtocolAndMode(iptables.ProtocolIPv4, "legacy"); err != nil {
				return nil, err
			}
			controller.iptablesObsolete[kubeovnv1.ProtocolIPv4] = ipt
		}
		controller.ipsets[kubeovnv1.ProtocolIPv4] = ipsets.NewIPSets(ipsets.NewIPVersionConfig(ipsets.IPFamilyV4, IPSetPrefix, nil, nil))
	}
	if controller.protocol == kubeovnv1.ProtocolIPv6 || controller.protocol == kubeovnv1.ProtocolDual {
		ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv6)
		if err != nil {
			return nil, err
		}
		controller.iptables[kubeovnv1.ProtocolIPv6] = ipt
		if controller.iptablesObsolete != nil {
			if ipt, err = iptables.NewWithProtocolAndMode(iptables.ProtocolIPv6, "legacy"); err != nil {
				return nil, err
			}
			controller.iptablesObsolete[kubeovnv1.ProtocolIPv6] = ipt
		}
		controller.ipsets[kubeovnv1.ProtocolIPv6] = ipsets.NewIPSets(ipsets.NewIPVersionConfig(ipsets.IPFamilyV6, IPSetPrefix, nil, nil))
	}

	controller.nmSyncer = newNetworkManagerSyncer()
	controller.nmSyncer.Run(controller.transferAddrsAndRoutes)

	providerNetworkInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddProviderNetwork,
		UpdateFunc: controller.enqueueUpdateProviderNetwork,
		DeleteFunc: controller.enqueueDeleteProviderNetwork,
	})
	subnetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddSubnet,
		UpdateFunc: controller.enqueueUpdateSubnet,
		DeleteFunc: controller.enqueueDeleteSubnet,
	})
	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: controller.enqueuePod,
	})

	return controller, nil
}

func (c *Controller) enqueueAddProviderNetwork(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}

	klog.V(3).Infof("enqueue add provider network %s", key)
	c.addOrUpdateProviderNetworkQueue.Add(key)
}

func (c *Controller) enqueueUpdateProviderNetwork(old, new interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(new)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}

	klog.V(3).Infof("enqueue update provider network %s", key)
	c.addOrUpdateProviderNetworkQueue.Add(key)
}

func (c *Controller) enqueueDeleteProviderNetwork(obj interface{}) {
	klog.V(3).Infof("enqueue delete provider network %s", obj.(*kubeovnv1.ProviderNetwork).Name)
	c.deleteProviderNetworkQueue.Add(obj)
}

func (c *Controller) runAddOrUpdateProviderNetworkWorker() {
	for c.processNextAddOrUpdateProviderNetworkWorkItem() {
	}
}

func (c *Controller) runDeleteProviderNetworkWorker() {
	for c.processNextDeleteProviderNetworkWorkItem() {
	}
}

func (c *Controller) processNextAddOrUpdateProviderNetworkWorkItem() bool {
	obj, shutdown := c.addOrUpdateProviderNetworkQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addOrUpdateProviderNetworkQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addOrUpdateProviderNetworkQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddOrUpdateProviderNetwork(key); err != nil {
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addOrUpdateProviderNetworkQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		c.addOrUpdateProviderNetworkQueue.AddRateLimited(obj)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteProviderNetworkWorkItem() bool {
	obj, shutdown := c.deleteProviderNetworkQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.deleteProviderNetworkQueue.Done(obj)
		var pn *kubeovnv1.ProviderNetwork
		var ok bool
		if pn, ok = obj.(*kubeovnv1.ProviderNetwork); !ok {
			c.deleteProviderNetworkQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected ProviderNetwork in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDeleteProviderNetwork(pn); err != nil {
			return fmt.Errorf("error syncing '%s': %v, requeuing", pn.Name, err)
		}
		c.deleteProviderNetworkQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		c.deleteProviderNetworkQueue.AddRateLimited(obj)
		return true
	}
	return true
}

func (c *Controller) handleAddOrUpdateProviderNetwork(key string) error {
	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		return err
	}
	pn, err := c.providerNetworksLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if util.ContainsString(pn.Spec.ExcludeNodes, node.Name) {
		c.recordProviderNetworkErr(pn.Name, "")
		return c.cleanProviderNetwork(pn.DeepCopy(), node.DeepCopy())
	}
	return c.initProviderNetwork(pn.DeepCopy(), node.DeepCopy())
}

func (c *Controller) initProviderNetwork(pn *kubeovnv1.ProviderNetwork, node *v1.Node) error {
	nic := pn.Spec.DefaultInterface
	for _, item := range pn.Spec.CustomInterfaces {
		if util.ContainsString(item.Nodes, node.Name) {
			nic = item.Interface
			break
		}
	}

	var mtu int
	var err error
	if mtu, err = c.ovsInitProviderNetwork(pn.Name, nic, pn.Spec.ExchangeLinkName, c.config.MacLearningFallback); err != nil {
		if oldLen := len(node.Labels); oldLen != 0 {
			delete(node.Labels, fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name))
			delete(node.Labels, fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, pn.Name))
			delete(node.Labels, fmt.Sprintf(util.ProviderNetworkMtuTemplate, pn.Name))
			if len(node.Labels) != oldLen {
				raw, _ := json.Marshal(node.Labels)
				patchPayload := fmt.Sprintf(`[{ "op": "replace", "path": "/metadata/labels", "value": %s }]`, raw)
				_, err1 := c.config.KubeClient.CoreV1().Nodes().Patch(context.Background(), node.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
				if err1 != nil {
					klog.Errorf("failed to patch node %s: %v", node.Name, err1)
				}
			}
		}
		c.recordProviderNetworkErr(pn.Name, err.Error())
		return err
	}

	delete(node.Labels, fmt.Sprintf(util.ProviderNetworkExcludeTemplate, pn.Name))
	node.Labels[fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name)] = "true"
	node.Labels[fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, pn.Name)] = nic
	node.Labels[fmt.Sprintf(util.ProviderNetworkMtuTemplate, pn.Name)] = strconv.Itoa(mtu)

	patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
	op := "replace"
	if len(node.Labels) == 0 {
		op = "add"
	}

	raw, _ := json.Marshal(node.Labels)
	patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
	_, err = c.config.KubeClient.CoreV1().Nodes().Patch(context.Background(), node.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
	if err != nil {
		klog.Errorf("failed to patch node %s: %v", node.Name, err)
		return err
	}
	c.recordProviderNetworkErr(pn.Name, "")
	return nil
}

func (c *Controller) recordProviderNetworkErr(providerNetwork, errMsg string) {
	var currentPod *v1.Pod
	var err error
	if c.localPodName == "" {
		pods, err := c.config.KubeClient.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{
			LabelSelector: "app=kube-ovn-cni",
			FieldSelector: fmt.Sprintf("spec.nodeName=%s", c.config.NodeName),
		})
		if err != nil {
			klog.Errorf("failed to list pod: %v", err)
			return
		}
		for _, pod := range pods.Items {
			if pod.Spec.NodeName == c.config.NodeName && pod.Status.Phase == v1.PodRunning {
				c.localPodName = pod.Name
				c.localNamespace = pod.Namespace
				currentPod = &pod
				break
			}
		}
		if currentPod == nil {
			klog.Warning("failed to get self pod")
			return
		}
	} else {
		if currentPod, err = c.config.KubeClient.CoreV1().Pods(c.localNamespace).Get(context.Background(), c.localPodName, metav1.GetOptions{}); err != nil {
			klog.Errorf("failed to get pod %s, %v", c.localPodName, err)
			return
		}
	}

	newPod := currentPod.DeepCopy()
	if newPod.Annotations == nil {
		newPod.Annotations = make(map[string]string)
	}
	if newPod.Annotations[fmt.Sprintf(util.ProviderNetworkErrMessageTemplate, providerNetwork)] != errMsg {
		if errMsg == "" {
			delete(newPod.Annotations, fmt.Sprintf(util.ProviderNetworkErrMessageTemplate, providerNetwork))
		} else {
			newPod.Annotations[fmt.Sprintf(util.ProviderNetworkErrMessageTemplate, providerNetwork)] = errMsg
		}
		patch, err := util.GenerateStrategicMergePatchPayload(currentPod, newPod)
		if err != nil {
			klog.Errorf("failed to gen patch payload pod %s: %v", c.localPodName, err)
			return
		}
		if _, err = c.config.KubeClient.CoreV1().Pods(c.localNamespace).Patch(context.Background(), c.localPodName,
			types.StrategicMergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
			klog.Errorf("failed to patch pod %s: %v", c.localPodName, err)
			return
		}
	}
}

func (c *Controller) cleanProviderNetwork(pn *kubeovnv1.ProviderNetwork, node *v1.Node) error {
	patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
	op := "replace"
	if len(node.Labels) == 0 {
		op = "add"
	}

	var err error
	delete(node.Labels, fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name))
	delete(node.Labels, fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, pn.Name))
	delete(node.Labels, fmt.Sprintf(util.ProviderNetworkMtuTemplate, pn.Name))
	node.Labels[fmt.Sprintf(util.ProviderNetworkExcludeTemplate, pn.Name)] = "true"
	raw, _ := json.Marshal(node.Labels)
	patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
	_, err = c.config.KubeClient.CoreV1().Nodes().Patch(context.Background(), node.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
	if err != nil {
		klog.Errorf("failed to patch node %s: %v", node.Name, err)
		return err
	}

	if err = c.ovsCleanProviderNetwork(pn.Name); err != nil {
		return err
	}

	return nil
}

func (c *Controller) handleDeleteProviderNetwork(pn *kubeovnv1.ProviderNetwork) error {
	if err := c.ovsCleanProviderNetwork(pn.Name); err != nil {
		return err
	}

	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		return err
	}
	if len(node.Labels) == 0 {
		return nil
	}

	newNode := node.DeepCopy()
	delete(newNode.Labels, fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name))
	delete(newNode.Labels, fmt.Sprintf(util.ProviderNetworkExcludeTemplate, pn.Name))
	delete(newNode.Labels, fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, pn.Name))
	delete(newNode.Labels, fmt.Sprintf(util.ProviderNetworkMtuTemplate, pn.Name))
	raw, _ := json.Marshal(newNode.Labels)
	patchPayloadTemplate := `[{ "op": "replace", "path": "/metadata/labels", "value": %s }]`
	patchPayload := fmt.Sprintf(patchPayloadTemplate, raw)
	_, err = c.config.KubeClient.CoreV1().Nodes().Patch(context.Background(), node.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
	if err != nil {
		klog.Errorf("failed to patch node %s: %v", node.Name, err)
		return err
	}

	return nil
}

type subnetEvent struct {
	old, new interface{}
}

func (c *Controller) enqueueAddSubnet(obj interface{}) {
	c.subnetQueue.Add(subnetEvent{new: obj})
}

func (c *Controller) enqueueUpdateSubnet(old, new interface{}) {
	c.subnetQueue.Add(subnetEvent{old: old, new: new})
}

func (c *Controller) enqueueDeleteSubnet(obj interface{}) {
	c.subnetQueue.Add(subnetEvent{old: obj})
}

func (c *Controller) runSubnetWorker() {
	for c.processNextSubnetWorkItem() {
	}
}

func (c *Controller) processNextSubnetWorkItem() bool {
	obj, shutdown := c.subnetQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.subnetQueue.Done(obj)
		event, ok := obj.(subnetEvent)
		if !ok {
			c.subnetQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected subnetEvent in workqueue but got %#v", obj))
			return nil
		}
		if err := c.reconcileRouters(&event); err != nil {
			c.subnetQueue.AddRateLimited(event)
			return fmt.Errorf("error syncing '%s': %s, requeuing", event, err.Error())
		}
		c.subnetQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func evalCommandSymlinks(cmd string) (string, error) {
	path, err := exec.LookPath(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to search for command %q: %v", cmd, err)
	}
	file, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("failed to read evaluate symbolic links for file %q: %v", path, err)
	}

	return file, nil
}

func isLegacyIptablesMode() (bool, error) {
	path, err := evalCommandSymlinks("iptables")
	if err != nil {
		return false, err
	}
	pathLegacy, err := evalCommandSymlinks("iptables-legacy")
	if err != nil {
		return false, err
	}
	return path == pathLegacy, nil
}

func (c *Controller) reconcileRouters(event *subnetEvent) error {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets %v", err)
		return err
	}

	var oldSubnet, newSubnet *kubeovnv1.Subnet
	if event != nil {
		var ok bool
		if event.old != nil {
			if oldSubnet, ok = event.old.(*kubeovnv1.Subnet); !ok {
				klog.Errorf("expected old subnet in subnetEvent but got %#v", event.old)
				return nil
			}
		}
		if event.new != nil {
			if newSubnet, ok = event.new.(*kubeovnv1.Subnet); !ok {
				klog.Errorf("expected new subnet in subnetEvent but got %#v", event.new)
				return nil
			}
		}

		// handle policy routing
		rulesToAdd, rulesToDel, routesToAdd, routesToDel, err := c.diffPolicyRouting(oldSubnet, newSubnet)
		if err != nil {
			klog.Errorf("failed to get policy routing difference: %v", err)
			return err
		}
		// add new routes first
		for _, r := range routesToAdd {
			if err = netlink.RouteReplace(&r); err != nil && !errors.Is(err, syscall.EEXIST) {
				klog.Errorf("failed to replace route for subnet %s: %v", newSubnet.Name, err)
				return err
			}
		}
		// next, add new rules
		for _, r := range rulesToAdd {
			if err = netlink.RuleAdd(&r); err != nil && !errors.Is(err, syscall.EEXIST) {
				klog.Errorf("failed to add network rule for subnet %s: %v", newSubnet.Name, err)
				return err
			}
		}
		// then delete old network rules
		for _, r := range rulesToDel {
			// loop to delete all matched rules
			for {
				if err = netlink.RuleDel(&r); err != nil {
					if !errors.Is(err, syscall.ENOENT) {
						klog.Errorf("failed to delete network rule for subnet %s: %v", oldSubnet.Name, err)
						return err
					}
					break
				}
			}
		}
		// last, delete old network routes
		for _, r := range routesToDel {
			if err = netlink.RouteDel(&r); err != nil && !errors.Is(err, syscall.ENOENT) {
				klog.Errorf("failed to delete route for subnet %s: %v", oldSubnet.Name, err)
				return err
			}
		}

		// u2o arp filter in underlay subnet bridge to avoid arp request to u2o IP
		if newSubnet != nil && newSubnet.Spec.Vlan != "" && !newSubnet.Spec.LogicalGateway {
			vlanName := newSubnet.Spec.Vlan
			vlan, err := c.vlansLister.Get(vlanName)
			if err != nil {
				klog.Errorf("failed to get vlan %s %v", vlanName, err)
				return err
			}
			pn, err := c.providerNetworksLister.Get(vlan.Spec.Provider)
			if err != nil {
				klog.Errorf("failed to get provider network %s %v", vlan.Spec.Provider, err)
				return err
			}

			if pn.Status.Ready {
				bridgeName := util.ExternalBridgeName(pn.Name)
				underlayNic := pn.Spec.DefaultInterface
				for _, item := range pn.Spec.CustomInterfaces {
					if slices.Contains(item.Nodes, c.config.NodeName) {
						underlayNic = item.Interface
						break
					}
				}

				if newSubnet.Status.U2OInterconnectionIP != "" {
					u2oIPs := strings.Split(newSubnet.Status.U2OInterconnectionIP, ",")
					gatewayIPs := strings.Split(newSubnet.Spec.Gateway, ",")

					if len(u2oIPs) != len(gatewayIPs) {
						err := fmt.Errorf("u2o interconnection IPs %v and gateway IPs %v are not matched", u2oIPs, gatewayIPs)
						klog.Error(err)
						return err

					}
					for index, u2oIP := range u2oIPs {
						err := ovs.AddOrUpdateU2OFilterOpenFlow(c.ovsClient, bridgeName, gatewayIPs[index], u2oIP, underlayNic)
						if err != nil {
							return err
						}
					}
				} else {
					err := ovs.DeleteAllU2OFilterOpenFlow(c.ovsClient, bridgeName, newSubnet.Spec.Protocol)
					if err != nil {
						return err
					}
				}
			}
		}

		// remove subnet need remove u2o filter openflow
		if newSubnet == nil && oldSubnet.Spec.Vlan != "" && !oldSubnet.Spec.LogicalGateway {
			vlanName := oldSubnet.Spec.Vlan
			vlan, err := c.vlansLister.Get(vlanName)
			if err != nil {
				klog.Errorf("failed to get vlan %s %v", vlanName, err)
				return err
			}
			pn, err := c.providerNetworksLister.Get(vlan.Spec.Provider)
			if err != nil {
				klog.Errorf("failed to get provider network %s %v", vlan.Spec.Provider, err)
				return err
			}

			if pn.Status.Ready {
				bridgeName := util.ExternalBridgeName(pn.Name)
				err = ovs.DeleteAllU2OFilterOpenFlow(c.ovsClient, bridgeName, oldSubnet.Spec.Protocol)
				if err != nil {
					return err
				}
			}
		}
	}

	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get node %s %v", c.config.NodeName, err)
		return err
	}
	nodeIPv4, nodeIPv6 := util.GetNodeInternalIP(*node)
	var joinIPv4, joinIPv6 string
	if len(node.Annotations) != 0 {
		joinIPv4, joinIPv6 = util.SplitStringIP(node.Annotations[util.IpAddressAnnotation])
	}

	joinCIDR := make([]string, 0, 2)
	cidrs := make([]string, 0, len(subnets)*2)
	for _, subnet := range subnets {
		// The route for overlay subnet cidr via ovn0 should not be deleted even though subnet.Status has changed to not ready
		if subnet.Spec.Vlan != "" || subnet.Spec.Vpc != c.config.ClusterRouter ||
			(subnet.Name != c.config.NodeSwitch && !subnet.Status.IsReady()) {
			continue
		}

		for _, cidrBlock := range strings.Split(subnet.Spec.CIDRBlock, ",") {
			if _, ipNet, err := net.ParseCIDR(cidrBlock); err != nil {
				klog.Errorf("%s is not a valid cidr block", cidrBlock)
			} else {
				if nodeIPv4 != "" && util.CIDRContainIP(cidrBlock, nodeIPv4) {
					continue
				}
				if nodeIPv6 != "" && util.CIDRContainIP(cidrBlock, nodeIPv6) {
					continue
				}
				cidrs = append(cidrs, ipNet.String())
				if subnet.Name == c.config.NodeSwitch {
					joinCIDR = append(joinCIDR, ipNet.String())
				}
			}
		}
	}

	gateway, ok := node.Annotations[util.GatewayAnnotation]
	if !ok {
		klog.Errorf("annotation for node %s ovn.kubernetes.io/gateway not exists", node.Name)
		return fmt.Errorf("annotation for node ovn.kubernetes.io/gateway not exists")
	}
	nic, err := netlink.LinkByName(util.NodeNic)
	if err != nil {
		klog.Errorf("failed to get nic %s", util.NodeNic)
		return fmt.Errorf("failed to get nic %s", util.NodeNic)
	}

	nodeNicRoutes, err := getNicExistRoutes(nic, gateway)
	if err != nil {
		klog.Error(err)
		return err
	}
	toAdd, toDel := routeDiff(nodeNicRoutes, cidrs, joinCIDR, joinIPv4, joinIPv6, gateway)
	for _, r := range toDel {
		if err = netlink.RouteDel(&netlink.Route{Dst: r.Dst}); err != nil {
			klog.Errorf("failed to del route %v", err)
		}
	}

	for _, r := range toAdd {
		r.LinkIndex = nic.Attrs().Index
		if err = netlink.RouteReplace(&r); err != nil {
			klog.Errorf("failed to replace route %v: %v", r, err)
		}
	}

	if oldSubnet != nil && newSubnet != nil && oldSubnet.Spec.HtbQos != "" && newSubnet.Spec.HtbQos == "" {
		if err := c.deleteSubnetQos(newSubnet); err != nil {
			klog.Errorf("failed to delete htb qos for subnet %s: %v", newSubnet.Name, err)
			return err
		}
	} else if newSubnet != nil && newSubnet.Spec.HtbQos != "" {
		if err := c.setSubnetQosPriority(newSubnet); err != nil {
			klog.Errorf("failed to set htb qos priority for subnet %s: %v", newSubnet.Name, err)
			return err
		}
	}
	return nil
}

func getNicExistRoutes(nic netlink.Link, gateway string) ([]netlink.Route, error) {
	var routes, existRoutes []netlink.Route
	var err error
	for _, gw := range strings.Split(gateway, ",") {
		if util.CheckProtocol(gw) == kubeovnv1.ProtocolIPv4 {
			routes, err = netlink.RouteList(nic, netlink.FAMILY_V4)
		} else {
			routes, err = netlink.RouteList(nic, netlink.FAMILY_V6)
		}
		if err != nil {
			return nil, err
		}
		existRoutes = append(existRoutes, routes...)
	}
	return existRoutes, nil
}

func routeDiff(existRoutes []netlink.Route, cidrs, joinCIDR []string, joinIPv4, joinIPv6, gateway string) (toAdd, toDel []netlink.Route) {
	// joinIPv6 is not used for now
	_ = joinIPv6

	for _, route := range existRoutes {
		if route.Scope == netlink.SCOPE_LINK || route.Dst == nil || route.Dst.IP.IsLinkLocalUnicast() {
			continue
		}

		found := false
		for _, c := range cidrs {
			if route.Dst.String() == c {
				found = true
				break
			}
		}
		if !found {
			toDel = append(toDel, route)
		}
	}
	if len(toDel) > 0 {
		klog.Infof("routes to delete: %v", toDel)
	}

	ipv4, ipv6 := util.SplitStringIP(gateway)
	gwV4, gwV6 := net.ParseIP(ipv4), net.ParseIP(ipv6)
	for _, c := range cidrs {
		found := false
		for _, r := range existRoutes {
			if r.Dst.String() == c {
				found = true
				break
			}
		}
		if !found {
			var priority int
			var src, gw net.IP
			scope := netlink.SCOPE_UNIVERSE
			proto := netlink.RouteProtocol(syscall.RTPROT_STATIC)
			if slices.Contains(joinCIDR, c) {
				if util.CheckProtocol(c) == kubeovnv1.ProtocolIPv4 {
					src = net.ParseIP(joinIPv4)
				} else {
					priority = 256
				}
				scope = netlink.SCOPE_LINK
				proto = netlink.RouteProtocol(unix.RTPROT_KERNEL)
			} else {
				switch util.CheckProtocol(c) {
				case kubeovnv1.ProtocolIPv4:
					gw = gwV4
				case kubeovnv1.ProtocolIPv6:
					gw = gwV6
				}
			}
			_, cidr, _ := net.ParseCIDR(c)
			toAdd = append(toAdd, netlink.Route{
				Dst:      cidr,
				Src:      src,
				Gw:       gw,
				Protocol: proto,
				Scope:    scope,
				Priority: priority,
			})
		}
	}
	if len(toAdd) > 0 {
		klog.Infof("routes to add: %v", toAdd)
	}
	return
}

func getRulesToAdd(oldRules, newRules []netlink.Rule) []netlink.Rule {
	var toAdd []netlink.Rule

	for _, rule := range newRules {
		var found bool
		for _, r := range oldRules {
			if r.Family == rule.Family && r.Priority == rule.Priority && r.Table == rule.Table && reflect.DeepEqual(r.Src, rule.Src) {
				found = true
				break
			}
		}
		if !found {
			toAdd = append(toAdd, rule)
		}
	}

	return toAdd
}

func getRoutesToAdd(oldRoutes, newRoutes []netlink.Route) []netlink.Route {
	var toAdd []netlink.Route

	for _, route := range newRoutes {
		var found bool
		for _, r := range oldRoutes {
			if r.Equal(route) {
				found = true
				break
			}
		}
		if !found {
			toAdd = append(toAdd, route)
		}
	}

	return toAdd
}

func (c *Controller) diffPolicyRouting(oldSubnet, newSubnet *kubeovnv1.Subnet) (rulesToAdd, rulesToDel []netlink.Rule, routesToAdd, routesToDel []netlink.Route, err error) {
	oldRules, oldRoutes, err := c.getPolicyRouting(oldSubnet)
	if err != nil {
		klog.Error(err)
		return
	}
	newRules, newRoutes, err := c.getPolicyRouting(newSubnet)
	if err != nil {
		klog.Error(err)
		return
	}

	rulesToAdd = getRulesToAdd(oldRules, newRules)
	rulesToDel = getRulesToAdd(newRules, oldRules)
	routesToAdd = getRoutesToAdd(oldRoutes, newRoutes)
	routesToDel = getRoutesToAdd(newRoutes, oldRoutes)

	return
}

func (c *Controller) getPolicyRouting(subnet *kubeovnv1.Subnet) ([]netlink.Rule, []netlink.Route, error) {
	if subnet == nil || subnet.Spec.ExternalEgressGateway == "" || subnet.Spec.Vpc != c.config.ClusterRouter {
		return nil, nil, nil
	}
	if subnet.Spec.GatewayType == kubeovnv1.GWCentralizedType && !util.GatewayContains(subnet.Spec.GatewayNode, c.config.NodeName) {
		return nil, nil, nil
	}

	protocols := make([]string, 1, 2)
	if protocol := util.CheckProtocol(subnet.Spec.ExternalEgressGateway); protocol == kubeovnv1.ProtocolDual {
		protocols[0] = kubeovnv1.ProtocolIPv4
		protocols = append(protocols, kubeovnv1.ProtocolIPv6)
	} else {
		protocols[0] = protocol
	}

	cidr := strings.Split(subnet.Spec.CIDRBlock, ",")
	egw := strings.Split(subnet.Spec.ExternalEgressGateway, ",")

	// rules
	var rules []netlink.Rule
	rule := netlink.NewRule()
	rule.Table = int(subnet.Spec.PolicyRoutingTableID)
	rule.Priority = int(subnet.Spec.PolicyRoutingPriority)
	if subnet.Spec.GatewayType == kubeovnv1.GWDistributedType {
		pods, err := c.podsLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("list pods failed, %+v", err)
			return nil, nil, err
		}

		hostname := os.Getenv(util.HostnameEnv)
		for _, pod := range pods {
			if pod.Spec.HostNetwork ||
				pod.Status.PodIP == "" ||
				pod.Annotations[util.LogicalSwitchAnnotation] != subnet.Name ||
				pod.Spec.NodeName != hostname {
				continue
			}

			for i := range protocols {
				rule.Family, _ = util.ProtocolToFamily(protocols[i])

				var ip net.IP
				var maskBits int
				if len(pod.Status.PodIPs) == 2 && protocols[i] == kubeovnv1.ProtocolIPv6 {
					ip = net.ParseIP(pod.Status.PodIPs[1].IP)
					maskBits = 128
				} else if util.CheckProtocol(pod.Status.PodIP) == protocols[i] {
					ip = net.ParseIP(pod.Status.PodIP)
					maskBits = 32
					if rule.Family == netlink.FAMILY_V6 {
						maskBits = 128
					}
				}

				rule.Src = &net.IPNet{IP: ip, Mask: net.CIDRMask(maskBits, maskBits)}
				rules = append(rules, *rule)
			}
		}
	} else {
		for i := range protocols {
			rule.Family, _ = util.ProtocolToFamily(protocols[i])
			if len(cidr) == len(protocols) {
				_, rule.Src, _ = net.ParseCIDR(cidr[i])
			}
			rules = append(rules, *rule)
		}
	}

	// routes
	var routes []netlink.Route
	for i := range protocols {
		routes = append(routes, netlink.Route{
			Protocol: netlink.RouteProtocol(syscall.RTPROT_STATIC),
			Table:    int(subnet.Spec.PolicyRoutingTableID),
			Gw:       net.ParseIP(egw[i]),
		})
	}

	return rules, routes, nil
}

func (c *Controller) enqueuePod(old, new interface{}) {
	oldPod := old.(*v1.Pod)
	newPod := new.(*v1.Pod)

	if oldPod.Annotations[util.IngressRateAnnotation] != newPod.Annotations[util.IngressRateAnnotation] ||
		oldPod.Annotations[util.EgressRateAnnotation] != newPod.Annotations[util.EgressRateAnnotation] ||
		oldPod.Annotations[util.PriorityAnnotation] != newPod.Annotations[util.PriorityAnnotation] ||
		oldPod.Annotations[util.NetemQosLatencyAnnotation] != newPod.Annotations[util.NetemQosLatencyAnnotation] ||
		oldPod.Annotations[util.NetemQosLimitAnnotation] != newPod.Annotations[util.NetemQosLimitAnnotation] ||
		oldPod.Annotations[util.NetemQosLossAnnotation] != newPod.Annotations[util.NetemQosLossAnnotation] ||
		oldPod.Annotations[util.MirrorControlAnnotation] != newPod.Annotations[util.MirrorControlAnnotation] {
		var key string
		var err error
		if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
			utilruntime.HandleError(err)
			return
		}
		c.podQueue.Add(key)
	}

	attachNets, err := util.ParsePodNetworkAnnotation(newPod.Annotations[util.AttachmentNetworkAnnotation], newPod.Namespace)
	if err != nil {
		return
	}
	for _, multiNet := range attachNets {
		provider := fmt.Sprintf("%s.%s.ovn", multiNet.Name, multiNet.Namespace)
		if newPod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, provider)] == "true" {
			if oldPod.Annotations[fmt.Sprintf(util.IngressRateAnnotationTemplate, provider)] != newPod.Annotations[fmt.Sprintf(util.IngressRateAnnotationTemplate, provider)] ||
				oldPod.Annotations[fmt.Sprintf(util.EgressRateAnnotationTemplate, provider)] != newPod.Annotations[fmt.Sprintf(util.EgressRateAnnotationTemplate, provider)] ||
				oldPod.Annotations[fmt.Sprintf(util.PriorityAnnotationTemplate, provider)] != newPod.Annotations[fmt.Sprintf(util.PriorityAnnotationTemplate, provider)] ||
				oldPod.Annotations[fmt.Sprintf(util.NetemQosLatencyAnnotationTemplate, provider)] != newPod.Annotations[fmt.Sprintf(util.NetemQosLatencyAnnotationTemplate, provider)] ||
				oldPod.Annotations[fmt.Sprintf(util.NetemQosLimitAnnotationTemplate, provider)] != newPod.Annotations[fmt.Sprintf(util.NetemQosLimitAnnotationTemplate, provider)] ||
				oldPod.Annotations[fmt.Sprintf(util.NetemQosLossAnnotationTemplate, provider)] != newPod.Annotations[fmt.Sprintf(util.NetemQosLossAnnotationTemplate, provider)] ||
				oldPod.Annotations[fmt.Sprintf(util.MirrorControlAnnotationTemplate, provider)] != newPod.Annotations[fmt.Sprintf(util.MirrorControlAnnotationTemplate, provider)] {
				var key string
				var err error
				if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
					utilruntime.HandleError(err)
					return
				}
				c.podQueue.Add(key)
			}
		}
	}
}

func (c *Controller) runPodWorker() {
	for c.processNextPodWorkItem() {
	}
}

func (c *Controller) processNextPodWorkItem() bool {
	obj, shutdown := c.podQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.podQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.podQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handlePod(key); err != nil {
			c.podQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.podQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) handlePod(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	klog.Infof("handle qos update for pod %s/%s", namespace, name)

	pod, err := c.podsLister.Pods(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if err := util.ValidatePodNetwork(pod.Annotations); err != nil {
		klog.Errorf("validate pod %s/%s failed, %v", namespace, name, err)
		c.recorder.Eventf(pod, v1.EventTypeWarning, "ValidatePodNetworkFailed", err.Error())
		return err
	}

	podName := pod.Name
	if pod.Annotations[fmt.Sprintf(util.VmTemplate, util.OvnProvider)] != "" {
		podName = pod.Annotations[fmt.Sprintf(util.VmTemplate, util.OvnProvider)]
	}

	priority := pod.Annotations[util.PriorityAnnotation]
	subnetName := pod.Annotations[util.LogicalSwitchAnnotation]
	subnetPriority := c.getSubnetQosPriority(subnetName)
	if priority == "" && subnetPriority != "" {
		priority = subnetPriority
	}

	// set default nic bandwidth
	ifaceID := ovs.PodNameToPortName(podName, pod.Namespace, util.OvnProvider)
	err = ovs.SetInterfaceBandwidth(podName, pod.Namespace, ifaceID, pod.Annotations[util.EgressRateAnnotation], pod.Annotations[util.IngressRateAnnotation], priority)
	if err != nil {
		return err
	}
	err = ovs.ConfigInterfaceMirror(c.config.EnableMirror, pod.Annotations[util.MirrorControlAnnotation], ifaceID)
	if err != nil {
		return err
	}
	// set linux-netem qos
	err = ovs.SetNetemQos(podName, pod.Namespace, ifaceID, pod.Annotations[util.NetemQosLatencyAnnotation], pod.Annotations[util.NetemQosLimitAnnotation], pod.Annotations[util.NetemQosLossAnnotation])
	if err != nil {
		return err
	}

	// set multus-nic bandwidth
	attachNets, err := util.ParsePodNetworkAnnotation(pod.Annotations[util.AttachmentNetworkAnnotation], pod.Namespace)
	if err != nil {
		return err
	}
	for _, multiNet := range attachNets {
		provider := fmt.Sprintf("%s.%s.ovn", multiNet.Name, multiNet.Namespace)
		if pod.Annotations[fmt.Sprintf(util.VmTemplate, provider)] != "" {
			podName = pod.Annotations[fmt.Sprintf(util.VmTemplate, provider)]
		}
		if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, provider)] == "true" {
			ifaceID = ovs.PodNameToPortName(podName, pod.Namespace, provider)
			priority := pod.Annotations[fmt.Sprintf(util.PriorityAnnotationTemplate, provider)]
			subnetName := pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider)]
			subnetPriority := c.getSubnetQosPriority(subnetName)
			if priority == "" && subnetPriority != "" {
				priority = subnetPriority
			}

			err = ovs.SetInterfaceBandwidth(podName, pod.Namespace, ifaceID, pod.Annotations[fmt.Sprintf(util.EgressRateAnnotationTemplate, provider)], pod.Annotations[fmt.Sprintf(util.IngressRateAnnotationTemplate, provider)], priority)
			if err != nil {
				return err
			}
			err = ovs.ConfigInterfaceMirror(c.config.EnableMirror, pod.Annotations[fmt.Sprintf(util.MirrorControlAnnotationTemplate, provider)], ifaceID)
			if err != nil {
				return err
			}
			err = ovs.SetNetemQos(podName, pod.Namespace, ifaceID, pod.Annotations[fmt.Sprintf(util.NetemQosLatencyAnnotationTemplate, provider)], pod.Annotations[fmt.Sprintf(util.NetemQosLimitAnnotationTemplate, provider)], pod.Annotations[fmt.Sprintf(util.NetemQosLossAnnotationTemplate, provider)])
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Controller) loopEncapIpCheck() {
	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get node %s %v", c.config.NodeName, err)
		return
	}

	if nodeTunnelName := node.GetAnnotations()[util.TunnelInterfaceAnnotation]; nodeTunnelName != "" {
		iface, err := findInterface(nodeTunnelName)
		if err != nil {
			klog.Errorf("failed to find iface %s, %v", nodeTunnelName, err)
			return
		}
		if iface.Flags&net.FlagUp == 0 {
			klog.Errorf("iface %v is down", nodeTunnelName)
			return
		}
		addrs, err := iface.Addrs()
		if err != nil {
			klog.Errorf("failed to get iface addr. %v", err)
			return
		}
		if len(addrs) == 0 {
			klog.Errorf("iface %s has no ip address", nodeTunnelName)
			return
		}
		if iface.Name != c.config.tunnelIface {
			klog.Infof("use %s as tunnel interface", iface.Name)
			c.config.tunnelIface = iface.Name
		}

		// if assigned iface in node annotation is down or with no ip, the error msg should be printed periodically
		if c.config.Iface == nodeTunnelName {
			klog.V(3).Infof("node tunnel interface %s not changed", nodeTunnelName)
			return
		}
		c.config.Iface = nodeTunnelName
		klog.Infof("Update node tunnel interface %v", nodeTunnelName)

		encapIP := strings.Split(addrs[0].String(), "/")[0]
		if err = setEncapIP(encapIP); err != nil {
			klog.Errorf("failed to set encap ip %s for iface %s", encapIP, c.config.Iface)
			return
		}
	}
}

var lastNoPodOvsPort map[string]bool

func (c *Controller) markAndCleanInternalPort() error {
	klog.V(4).Infof("start to gc ovs internal ports")
	residualPorts := ovs.GetResidualInternalPorts()
	if len(residualPorts) == 0 {
		return nil
	}

	noPodOvsPort := map[string]bool{}
	for _, portName := range residualPorts {
		if !lastNoPodOvsPort[portName] {
			noPodOvsPort[portName] = true
		} else {
			klog.Infof("gc ovs internal port %s", portName)
			// Remove ovs port
			output, err := ovs.Exec(ovs.IfExists, "--with-iface", "del-port", "br-int", portName)
			if err != nil {
				return fmt.Errorf("failed to delete ovs port %v, %q", err, output)
			}
		}
	}
	lastNoPodOvsPort = noPodOvsPort

	return nil
}

func (c *Controller) setSubnetQosPriority(subnet *kubeovnv1.Subnet) error {
	htbQos, err := c.htbQosLister.Get(subnet.Spec.HtbQos)
	if err != nil {
		klog.Errorf("failed to get htbqos %s: %v", subnet.Spec.HtbQos, err)
		return err
	}

	pods, err := c.podsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list pods, %v", err)
		return err
	}

	qosIfaceUidMap, err := ovs.ListExternalIds("qos")
	if err != nil {
		return err
	}
	queueIfaceUidMap, err := ovs.ListExternalIds("queue")
	if err != nil {
		return err
	}

	for _, pod := range pods {
		if pod.Spec.HostNetwork ||
			pod.DeletionTimestamp != nil ||
			pod.Status.PodIP == "" {
			continue
		}
		podName := pod.Name
		if pod.Annotations[fmt.Sprintf(util.VmTemplate, util.OvnProvider)] != "" {
			podName = pod.Annotations[fmt.Sprintf(util.VmTemplate, util.OvnProvider)]
		}

		// set priority for eth0 interface in pod
		if pod.Annotations[util.LogicalSwitchAnnotation] == subnet.Name {
			ifaceID := ovs.PodNameToPortName(podName, pod.Namespace, util.OvnProvider)
			priority := htbQos.Spec.Priority
			if pod.Annotations[util.PriorityAnnotation] != "" {
				priority = pod.Annotations[util.PriorityAnnotation]
			}
			if err = ovs.SetPodQosPriority(podName, pod.Namespace, ifaceID, priority, qosIfaceUidMap, queueIfaceUidMap); err != nil {
				klog.Errorf("failed to set htbqos priority for pod %s/%s, iface %v: %v", pod.Namespace, pod.Name, ifaceID, err)
				return err
			}
		}

		// set priority for attach interface provided by kube-ovn in pod
		attachNets, err := util.ParsePodNetworkAnnotation(pod.Annotations[util.AttachmentNetworkAnnotation], pod.Namespace)
		if err != nil {
			return err
		}
		for _, multiNet := range attachNets {
			provider := fmt.Sprintf("%s.%s.ovn", multiNet.Name, multiNet.Namespace)
			if subnet.Spec.Provider != provider {
				continue
			}
			if pod.Annotations[fmt.Sprintf(util.VmTemplate, provider)] != "" {
				podName = pod.Annotations[fmt.Sprintf(util.VmTemplate, provider)]
			}

			if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, provider)] == "true" {
				ifaceID := ovs.PodNameToPortName(podName, pod.Namespace, provider)
				priority := htbQos.Spec.Priority
				if pod.Annotations[fmt.Sprintf(util.PriorityAnnotationTemplate, provider)] != "" {
					priority = pod.Annotations[fmt.Sprintf(util.PriorityAnnotationTemplate, provider)]
				}

				if err = ovs.SetPodQosPriority(podName, pod.Namespace, ifaceID, priority, qosIfaceUidMap, queueIfaceUidMap); err != nil {
					klog.Errorf("failed to set htbqos priority for pod %s/%s, iface %v: %v", pod.Namespace, pod.Name, ifaceID, err)
					return err
				}
			}
		}
	}
	return nil
}

func (c *Controller) clearQos(podName, podNamespace, ifaceID string) error {
	if htbQos, _ := ovs.IsHtbQos(ifaceID); !htbQos {
		return nil
	}

	if err := ovs.ClearPortQosBinding(ifaceID); err != nil {
		klog.Errorf("failed to delete qos bingding info for interface %s: %v", ifaceID, err)
		return err
	}

	if err := ovs.ClearPodBandwidth(podName, podNamespace, ifaceID); err != nil {
		klog.Errorf("failed to delete htbqos record for pod %s/%s, iface %v: %v", podNamespace, podName, ifaceID, err)
		return err
	}

	if err := ovs.ClearHtbQosQueue(podName, podNamespace, ifaceID); err != nil {
		klog.Errorf("failed to delete htbqos queue for pod %s/%s, iface %v: %v", podNamespace, podName, ifaceID, err)
		return err
	}
	return nil
}

func (c *Controller) deleteSubnetQos(subnet *kubeovnv1.Subnet) error {
	pods, err := c.podsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list pods, %v", err)
		return err
	}

	for _, pod := range pods {
		if pod.Spec.HostNetwork ||
			pod.DeletionTimestamp != nil {
			continue
		}
		podName := pod.Name

		if pod.Annotations[util.LogicalSwitchAnnotation] == subnet.Name {
			// if pod's annotation for ingress-rate or priority exists, should keep qos for pod
			if pod.Annotations[util.IngressRateAnnotation] != "" ||
				pod.Annotations[util.PriorityAnnotation] != "" {
				continue
			}
			if pod.Annotations[fmt.Sprintf(util.VmTemplate, util.OvnProvider)] != "" {
				podName = pod.Annotations[fmt.Sprintf(util.VmTemplate, util.OvnProvider)]
			}
			ifaceID := ovs.PodNameToPortName(podName, pod.Namespace, util.OvnProvider)
			if err := c.clearQos(podName, pod.Namespace, ifaceID); err != nil {
				return err
			}
		}

		// clear priority for attach interface provided by kube-ovn in pod
		attachNets, err := util.ParsePodNetworkAnnotation(pod.Annotations[util.AttachmentNetworkAnnotation], pod.Namespace)
		if err != nil {
			return err
		}
		for _, multiNet := range attachNets {
			provider := fmt.Sprintf("%s.%s.ovn", multiNet.Name, multiNet.Namespace)
			if subnet.Spec.Provider != provider ||
				pod.Annotations[fmt.Sprintf(util.IngressRateAnnotationTemplate, provider)] != "" ||
				pod.Annotations[fmt.Sprintf(util.PriorityAnnotationTemplate, provider)] != "" {
				continue
			}
			if pod.Annotations[fmt.Sprintf(util.VmTemplate, provider)] != "" {
				podName = pod.Annotations[fmt.Sprintf(util.VmTemplate, provider)]
			}

			if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, provider)] == "true" {
				ifaceID := ovs.PodNameToPortName(podName, pod.Namespace, provider)
				if err := c.clearQos(podName, pod.Namespace, ifaceID); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (c *Controller) getSubnetQosPriority(subnetName string) string {
	var priority string
	subnet, err := c.subnetsLister.Get(subnetName)
	if err != nil {
		klog.Errorf("failed to get subnet %s: %v", subnetName, err)
	} else if subnet.Spec.HtbQos != "" {
		htbQos, err := c.htbQosLister.Get(subnet.Spec.HtbQos)
		if err != nil {
			klog.Errorf("failed to get htbqos %s: %v", subnet.Spec.HtbQos, err)
		} else {
			priority = htbQos.Spec.Priority
		}
	}
	return priority
}

func rotateLog() {
	if output, err := exec.Command("logrotate", "/etc/logrotate.d/kubeovn").CombinedOutput(); err != nil {
		klog.Errorf("failed to rotate kube-ovn log %q", output)
	}
}

// Run starts controller
func (c *Controller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.addOrUpdateProviderNetworkQueue.ShutDown()
	defer c.deleteProviderNetworkQueue.ShutDown()
	defer c.subnetQueue.ShutDown()
	defer c.podQueue.ShutDown()

	go wait.Until(ovs.CleanLostInterface, time.Minute, stopCh)
	go wait.Until(recompute, 10*time.Minute, stopCh)
	go wait.Until(rotateLog, 1*time.Hour, stopCh)

	if ok := cache.WaitForCacheSync(stopCh, c.providerNetworksSynced, c.subnetsSynced, c.podsSynced, c.nodesSynced, c.htbQosSynced); !ok {
		util.LogFatalAndExit(nil, "failed to wait for caches to sync")
	}

	if err := c.setIPSet(); err != nil {
		util.LogFatalAndExit(err, "failed to set ipsets")
	}

	klog.Info("Started workers")
	go wait.Until(c.loopOvn0Check, 5*time.Second, stopCh)
	go wait.Until(c.runAddOrUpdateProviderNetworkWorker, time.Second, stopCh)
	go wait.Until(c.runDeleteProviderNetworkWorker, time.Second, stopCh)
	go wait.Until(c.runSubnetWorker, time.Second, stopCh)
	go wait.Until(c.runPodWorker, time.Second, stopCh)
	go wait.Until(c.runGateway, 3*time.Second, stopCh)
	go wait.Until(c.loopEncapIpCheck, 3*time.Second, stopCh)
	go wait.Until(func() {
		if err := c.reconcileRouters(nil); err != nil {
			klog.Errorf("failed to reconcile ovn0 routes: %v", err)
		}
	}, 3*time.Second, stopCh)
	go wait.Until(func() {
		if err := c.markAndCleanInternalPort(); err != nil {
			klog.Errorf("gc ovs port error: %v", err)
		}
	}, 5*time.Minute, stopCh)

	<-stopCh
	klog.Info("Shutting down workers")
}

func recompute() {
	output, err := exec.Command("ovn-appctl", "-t", "ovn-controller", "recompute").CombinedOutput()
	if err != nil {
		klog.Errorf("failed to recompute ovn-controller %q", output)
	}
}
