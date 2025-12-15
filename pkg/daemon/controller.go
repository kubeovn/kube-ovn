package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	ovsutil "github.com/digitalocean/go-openvswitch/ovs"
	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	nadutils "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/utils"
	"github.com/kubeovn/felix/ipsets"
	"github.com/kubeovn/go-iptables/iptables"
	"github.com/scylladb/go-set/strset"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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
	k8sipset "k8s.io/kubernetes/pkg/proxy/ipvs/ipset"
	k8siptables "k8s.io/kubernetes/pkg/util/iptables"
	k8sexec "k8s.io/utils/exec"

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
	addOrUpdateProviderNetworkQueue workqueue.TypedRateLimitingInterface[string]
	deleteProviderNetworkQueue      workqueue.TypedRateLimitingInterface[*kubeovnv1.ProviderNetwork]

	vlansLister kubeovnlister.VlanLister
	vlansSynced cache.InformerSynced

	subnetsLister kubeovnlister.SubnetLister
	subnetsSynced cache.InformerSynced
	subnetQueue   workqueue.TypedRateLimitingInterface[*subnetEvent]

	ovnEipsLister kubeovnlister.OvnEipLister
	ovnEipsSynced cache.InformerSynced

	podsLister     listerv1.PodLister
	podsSynced     cache.InformerSynced
	updatePodQueue workqueue.TypedRateLimitingInterface[string]
	deletePodQueue workqueue.TypedRateLimitingInterface[*podEvent]

	nodesLister     listerv1.NodeLister
	nodesSynced     cache.InformerSynced
	updateNodeQueue workqueue.TypedRateLimitingInterface[string]

	servicesLister listerv1.ServiceLister
	servicesSynced cache.InformerSynced
	serviceQueue   workqueue.TypedRateLimitingInterface[*serviceEvent]

	caSecretLister listerv1.SecretLister
	caSecretSynced cache.InformerSynced
	ipsecQueue     workqueue.TypedRateLimitingInterface[string]

	recorder record.EventRecorder

	protocol string

	ControllerRuntime
	localPodName   string
	localNamespace string

	k8sExec k8sexec.Interface
}

const (
	kernelModuleIPTables  = "ip_tables"
	kernelModuleIP6Tables = "ip6_tables"
)

type ControllerRuntime struct {
	iptables         map[string]*iptables.IPTables
	iptablesObsolete map[string]*iptables.IPTables
	k8siptables      map[string]k8siptables.Interface
	k8sipsets        k8sipset.Interface
	ipsets           map[string]*ipsets.IPSets
	gwCounters       map[string]*util.GwIPtableCounters

	nmSyncer  *networkManagerSyncer
	ovsClient *ovsutil.Client
}

type LbServiceRules struct {
	IP          string
	Port        uint16
	Protocol    string
	BridgeName  string
	DstMac      string
	UnderlayNic string
}

func newTypedRateLimitingQueue[T comparable](name string, rateLimiter workqueue.TypedRateLimiter[T]) workqueue.TypedRateLimitingInterface[T] {
	if rateLimiter == nil {
		rateLimiter = workqueue.DefaultTypedControllerRateLimiter[T]()
	}
	return workqueue.NewTypedRateLimitingQueueWithConfig(rateLimiter, workqueue.TypedRateLimitingQueueConfig[T]{Name: name})
}

// NewController init a daemon controller
func NewController(config *Configuration,
	stopCh <-chan struct{},
	podInformerFactory, nodeInformerFactory, caSecretInformerFactory informers.SharedInformerFactory,
	kubeovnInformerFactory kubeovninformer.SharedInformerFactory,
) (*Controller, error) {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: config.KubeClient.CoreV1().Events(v1.NamespaceAll)})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: config.NodeName})
	providerNetworkInformer := kubeovnInformerFactory.Kubeovn().V1().ProviderNetworks()
	vlanInformer := kubeovnInformerFactory.Kubeovn().V1().Vlans()
	subnetInformer := kubeovnInformerFactory.Kubeovn().V1().Subnets()
	ovnEipInformer := kubeovnInformerFactory.Kubeovn().V1().OvnEips()
	podInformer := podInformerFactory.Core().V1().Pods()
	nodeInformer := nodeInformerFactory.Core().V1().Nodes()
	servicesInformer := nodeInformerFactory.Core().V1().Services()
	caSecretInformer := caSecretInformerFactory.Core().V1().Secrets()

	controller := &Controller{
		config: config,

		providerNetworksLister:          providerNetworkInformer.Lister(),
		providerNetworksSynced:          providerNetworkInformer.Informer().HasSynced,
		addOrUpdateProviderNetworkQueue: newTypedRateLimitingQueue[string]("AddOrUpdateProviderNetwork", nil),
		deleteProviderNetworkQueue:      newTypedRateLimitingQueue[*kubeovnv1.ProviderNetwork]("DeleteProviderNetwork", nil),

		vlansLister: vlanInformer.Lister(),
		vlansSynced: vlanInformer.Informer().HasSynced,

		subnetsLister: subnetInformer.Lister(),
		subnetsSynced: subnetInformer.Informer().HasSynced,
		subnetQueue:   newTypedRateLimitingQueue[*subnetEvent]("Subnet", nil),

		ovnEipsLister: ovnEipInformer.Lister(),
		ovnEipsSynced: ovnEipInformer.Informer().HasSynced,

		podsLister:     podInformer.Lister(),
		podsSynced:     podInformer.Informer().HasSynced,
		updatePodQueue: newTypedRateLimitingQueue[string]("UpdatePod", nil),
		deletePodQueue: newTypedRateLimitingQueue[*podEvent]("DeletePod", nil),

		nodesLister:     nodeInformer.Lister(),
		nodesSynced:     nodeInformer.Informer().HasSynced,
		updateNodeQueue: newTypedRateLimitingQueue[string]("UpdateNode", nil),

		servicesLister: servicesInformer.Lister(),
		servicesSynced: servicesInformer.Informer().HasSynced,
		serviceQueue:   newTypedRateLimitingQueue[*serviceEvent]("Service", nil),

		caSecretLister: caSecretInformer.Lister(),
		caSecretSynced: caSecretInformer.Informer().HasSynced,
		ipsecQueue:     newTypedRateLimitingQueue[string]("IPSecCA", nil),

		recorder: recorder,
		k8sExec:  k8sexec.New(),
	}

	node, err := config.KubeClient.CoreV1().Nodes().Get(context.Background(), config.NodeName, metav1.GetOptions{})
	if err != nil {
		util.LogFatalAndExit(err, "failed to get node %s info", config.NodeName)
	}
	controller.protocol = util.CheckProtocol(node.Annotations[util.IPAddressAnnotation])

	if err = controller.initRuntime(); err != nil {
		return nil, err
	}

	podInformerFactory.Start(stopCh)
	nodeInformerFactory.Start(stopCh)
	kubeovnInformerFactory.Start(stopCh)
	caSecretInformerFactory.Start(stopCh)

	if !cache.WaitForCacheSync(stopCh,
		controller.providerNetworksSynced, controller.vlansSynced, controller.subnetsSynced,
		controller.podsSynced, controller.nodesSynced, controller.servicesSynced, controller.caSecretSynced) {
		util.LogFatalAndExit(nil, "failed to wait for caches to sync")
	}

	if _, err = providerNetworkInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddProviderNetwork,
		UpdateFunc: controller.enqueueUpdateProviderNetwork,
		DeleteFunc: controller.enqueueDeleteProviderNetwork,
	}); err != nil {
		return nil, err
	}
	if _, err = vlanInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: controller.enqueueUpdateVlan,
	}); err != nil {
		return nil, err
	}
	if _, err = subnetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddSubnet,
		UpdateFunc: controller.enqueueUpdateSubnet,
		DeleteFunc: controller.enqueueDeleteSubnet,
	}); err != nil {
		return nil, err
	}
	if _, err = servicesInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddService,
		DeleteFunc: controller.enqueueDeleteService,
		UpdateFunc: controller.enqueueUpdateService,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add service event handler")
	}

	if _, err = podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: controller.enqueueUpdatePod,
		DeleteFunc: controller.enqueueDeletePod,
	}); err != nil {
		return nil, err
	}
	if _, err = caSecretInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddIPSecCA,
		UpdateFunc: controller.enqueueUpdateIPSecCA,
	}); err != nil {
		return nil, err
	}
	if _, err = nodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: controller.enqueueUpdateNode,
	}); err != nil {
		return nil, err
	}

	return controller, nil
}

func (c *Controller) enqueueAddIPSecCA(obj any) {
	key := cache.MetaObjectToName(obj.(*v1.Secret)).String()
	klog.V(3).Infof("enqueue add CA %s", key)
	c.ipsecQueue.Add(key)
}

func (c *Controller) enqueueUpdateIPSecCA(oldObj, newObj any) {
	oldSecret := oldObj.(*v1.Secret)
	newSecret := newObj.(*v1.Secret)
	if maps.EqualFunc(oldSecret.Data, newSecret.Data, bytes.Equal) {
		// No changes in CA data, no need to enqueue
		return
	}

	key := cache.MetaObjectToName(newSecret).String()
	klog.V(3).Infof("enqueue update CA %s", key)
	c.ipsecQueue.Add(key)
}

func (c *Controller) enqueueUpdateNode(oldObj, newObj any) {
	oldNode := oldObj.(*v1.Node)
	newNode := newObj.(*v1.Node)
	if newNode.Name != c.config.NodeName {
		return
	}
	if oldNode.Annotations[util.NodeNetworksAnnotation] != newNode.Annotations[util.NodeNetworksAnnotation] {
		klog.V(3).Infof("enqueue update node %s for node networks change", newNode.Name)
		c.updateNodeQueue.Add(newNode.Name)
	}
}

func (c *Controller) enqueueAddProviderNetwork(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.ProviderNetwork)).String()
	klog.V(3).Infof("enqueue add provider network %s", key)
	c.addOrUpdateProviderNetworkQueue.Add(key)
}

func (c *Controller) enqueueUpdateProviderNetwork(_, newObj any) {
	key := cache.MetaObjectToName(newObj.(*kubeovnv1.ProviderNetwork)).String()
	klog.V(3).Infof("enqueue update provider network %s", key)
	c.addOrUpdateProviderNetworkQueue.Add(key)
}

func (c *Controller) enqueueDeleteProviderNetwork(obj any) {
	var pn *kubeovnv1.ProviderNetwork
	switch t := obj.(type) {
	case *kubeovnv1.ProviderNetwork:
		pn = t
	case cache.DeletedFinalStateUnknown:
		p, ok := t.Obj.(*kubeovnv1.ProviderNetwork)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		pn = p
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	key := cache.MetaObjectToName(pn).String()
	klog.V(3).Infof("enqueue delete provider network %s", key)
	c.deleteProviderNetworkQueue.Add(pn)
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
	key, shutdown := c.addOrUpdateProviderNetworkQueue.Get()
	if shutdown {
		return false
	}

	err := func(key string) error {
		defer c.addOrUpdateProviderNetworkQueue.Done(key)
		if err := c.handleAddOrUpdateProviderNetwork(key); err != nil {
			return fmt.Errorf("error syncing %q: %w, requeuing", key, err)
		}
		c.addOrUpdateProviderNetworkQueue.Forget(key)
		return nil
	}(key)
	if err != nil {
		utilruntime.HandleError(err)
		c.addOrUpdateProviderNetworkQueue.AddRateLimited(key)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteProviderNetworkWorkItem() bool {
	obj, shutdown := c.deleteProviderNetworkQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj *kubeovnv1.ProviderNetwork) error {
		defer c.deleteProviderNetworkQueue.Done(obj)
		if err := c.handleDeleteProviderNetwork(obj); err != nil {
			return fmt.Errorf("error syncing %q: %w, requeuing", obj.Name, err)
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
	klog.V(3).Infof("handle update provider network %s", key)
	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Error(err)
		return err
	}
	pn, err := c.providerNetworksLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	excluded, err := util.IsNodeExcludedFromProviderNetwork(node, pn)
	if err != nil {
		klog.Error(err)
		return err
	}

	if excluded {
		c.recordProviderNetworkErr(pn.Name, "")
		return c.cleanProviderNetwork(pn.DeepCopy(), node.DeepCopy())
	}
	return c.initProviderNetwork(pn.DeepCopy(), node.DeepCopy())
}

func (c *Controller) initProviderNetwork(pn *kubeovnv1.ProviderNetwork, node *v1.Node) error {
	nic := pn.Spec.DefaultInterface
	for _, item := range pn.Spec.CustomInterfaces {
		if slices.Contains(item.Nodes, node.Name) {
			nic = item.Interface
			break
		}
	}

	patch := util.KVPatch{
		fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name):     nil,
		fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, pn.Name): nil,
		fmt.Sprintf(util.ProviderNetworkMtuTemplate, pn.Name):       nil,
		fmt.Sprintf(util.ProviderNetworkExcludeTemplate, pn.Name):   nil,
	}

	vlans := strset.NewWithSize(len(pn.Status.Vlans) + 1)
	for _, vlanName := range pn.Status.Vlans {
		vlan, err := c.vlansLister.Get(vlanName)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				klog.Infof("vlan %s not found", vlanName)
				continue
			}
			klog.Errorf("failed to get vlan %q: %v", vlanName, err)
			return err
		}
		vlans.Add(strconv.Itoa(vlan.Spec.ID))
	}
	// always add trunk 0 so that the ovs bridge can communicate with the external network
	vlans.Add("0")

	if pn.Spec.AutoCreateVlanSubinterfaces && strings.Contains(nic, ".") {
		parts := strings.SplitN(nic, ".", 2)
		parentIf := parts[0]
		if !util.CheckInterfaceExists(nic) {
			klog.Infof("Auto-create enabled: creating default VLAN subinterface %s on %s", nic, parentIf)
			if err := c.createVlanSubinterfaces([]string{nic}, parentIf, pn.Name); err != nil {
				klog.Errorf("Failed to create default VLAN subinterface %s: %v", nic, err)
				return err
			}
		} else {
			klog.V(3).Infof("Default VLAN subinterface %s already exists, skipping creation", nic)
		}
	}

	var mtu int
	var err error
	klog.V(3).Infof("ovs init provider network %s", pn.Name)
	if mtu, err = c.ovsInitProviderNetwork(pn.Name, nic, vlans.List(), pn.Spec.ExchangeLinkName, c.config.MacLearningFallback); err != nil {
		delete(patch, fmt.Sprintf(util.ProviderNetworkExcludeTemplate, pn.Name))
		if err1 := util.PatchLabels(c.config.KubeClient.CoreV1().Nodes(), node.Name, patch); err1 != nil {
			klog.Errorf("failed to patch annotations of node %s: %v", node.Name, err1)
		}
		c.recordProviderNetworkErr(pn.Name, err.Error())
		return err
	}

	patch[fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name)] = "true"
	patch[fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, pn.Name)] = nic
	patch[fmt.Sprintf(util.ProviderNetworkMtuTemplate, pn.Name)] = strconv.Itoa(mtu)
	if err = util.PatchLabels(c.config.KubeClient.CoreV1().Nodes(), node.Name, patch); err != nil {
		klog.Errorf("failed to patch labels of node %s: %v", node.Name, err)
		return err
	}
	c.recordProviderNetworkErr(pn.Name, "")
	return nil
}

func (c *Controller) recordProviderNetworkErr(providerNetwork, errMsg string) {
	var currentPod *v1.Pod
	var err error
	if c.localPodName == "" {
		pods, err := c.config.KubeClient.CoreV1().Pods(v1.NamespaceAll).List(context.Background(), metav1.ListOptions{
			LabelSelector: "app=kube-ovn-cni",
			FieldSelector: "spec.nodeName=" + c.config.NodeName,
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
		if currentPod, err = c.podsLister.Pods(c.localNamespace).Get(c.localPodName); err != nil {
			klog.Errorf("failed to get pod %s, %v", c.localPodName, err)
			return
		}
	}

	patch := util.KVPatch{}
	if currentPod.Annotations[fmt.Sprintf(util.ProviderNetworkErrMessageTemplate, providerNetwork)] != errMsg {
		if errMsg == "" {
			patch[fmt.Sprintf(util.ProviderNetworkErrMessageTemplate, providerNetwork)] = nil
		} else {
			patch[fmt.Sprintf(util.ProviderNetworkErrMessageTemplate, providerNetwork)] = errMsg
		}
		if err = util.PatchAnnotations(c.config.KubeClient.CoreV1().Pods(c.localNamespace), c.localPodName, patch); err != nil {
			klog.Errorf("failed to patch pod %s: %v", c.localPodName, err)
			return
		}
	}
}

func (c *Controller) cleanProviderNetwork(pn *kubeovnv1.ProviderNetwork, node *v1.Node) error {
	patch := util.KVPatch{
		fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name):     nil,
		fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, pn.Name): nil,
		fmt.Sprintf(util.ProviderNetworkMtuTemplate, pn.Name):       nil,
		fmt.Sprintf(util.ProviderNetworkExcludeTemplate, pn.Name):   "true",
	}
	if err := util.PatchLabels(c.config.KubeClient.CoreV1().Nodes(), node.Name, patch); err != nil {
		klog.Errorf("failed to patch labels of node %s: %v", node.Name, err)
		return err
	}

	return c.ovsCleanProviderNetwork(pn.Name)
}

func (c *Controller) handleDeleteProviderNetwork(pn *kubeovnv1.ProviderNetwork) error {
	if err := c.ovsCleanProviderNetwork(pn.Name); err != nil {
		klog.Error(err)
		return err
	}

	if err := c.cleanupAutoCreatedVlanInterfaces(pn.Name); err != nil {
		klog.Errorf("Failed to cleanup auto-created VLAN interfaces for provider %s: %v", pn.Name, err)
		return err
	}

	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Error(err)
		return err
	}
	if len(node.Labels) == 0 {
		return nil
	}

	patch := util.KVPatch{
		fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name):     nil,
		fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, pn.Name): nil,
		fmt.Sprintf(util.ProviderNetworkMtuTemplate, pn.Name):       nil,
		fmt.Sprintf(util.ProviderNetworkExcludeTemplate, pn.Name):   nil,
	}
	if err = util.PatchLabels(c.config.KubeClient.CoreV1().Nodes(), node.Name, patch); err != nil {
		klog.Errorf("failed to patch labels of node %s: %v", node.Name, err)
		return err
	}

	return nil
}

func (c *Controller) enqueueUpdateVlan(oldObj, newObj any) {
	oldVlan := oldObj.(*kubeovnv1.Vlan)
	newVlan := newObj.(*kubeovnv1.Vlan)
	if oldVlan.Spec.ID != newVlan.Spec.ID {
		klog.V(3).Infof("enqueue update provider network %q", newVlan.Spec.Provider)
		c.addOrUpdateProviderNetworkQueue.Add(newVlan.Spec.Provider)
	}
}

type subnetEvent struct {
	oldObj, newObj any
}

type serviceEvent struct {
	oldObj, newObj any
}

type podEvent struct {
	oldObj any
}

func (c *Controller) enqueueAddSubnet(obj any) {
	c.subnetQueue.Add(&subnetEvent{newObj: obj})
}

func (c *Controller) enqueueUpdateSubnet(oldObj, newObj any) {
	c.subnetQueue.Add(&subnetEvent{oldObj: oldObj, newObj: newObj})
}

func (c *Controller) enqueueDeleteSubnet(obj any) {
	c.subnetQueue.Add(&subnetEvent{oldObj: obj})
}

func (c *Controller) runSubnetWorker() {
	for c.processNextSubnetWorkItem() {
	}
}

func (c *Controller) enqueueAddService(obj any) {
	c.serviceQueue.Add(&serviceEvent{newObj: obj})
}

func (c *Controller) enqueueUpdateService(oldObj, newObj any) {
	c.serviceQueue.Add(&serviceEvent{oldObj: oldObj, newObj: newObj})
}

func (c *Controller) enqueueDeleteService(obj any) {
	c.serviceQueue.Add(&serviceEvent{oldObj: obj})
}

func (c *Controller) runAddOrUpdateServicekWorker() {
	for c.processNextServiceWorkItem() {
	}
}

func (c *Controller) processNextSubnetWorkItem() bool {
	obj, shutdown := c.subnetQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj *subnetEvent) error {
		defer c.subnetQueue.Done(obj)
		if err := c.reconcileRouters(obj); err != nil {
			c.subnetQueue.AddRateLimited(obj)
			return fmt.Errorf("error syncing %v: %w, requeuing", obj, err)
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

func (c *Controller) processNextServiceWorkItem() bool {
	obj, shutdown := c.serviceQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj *serviceEvent) error {
		defer c.serviceQueue.Done(obj)
		if err := c.reconcileServices(obj); err != nil {
			c.serviceQueue.AddRateLimited(obj)
			return fmt.Errorf("error syncing %v: %w, requeuing", obj, err)
		}
		c.serviceQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) enqueueUpdatePod(oldObj, newObj any) {
	oldPod := oldObj.(*v1.Pod)
	newPod := newObj.(*v1.Pod)
	key := cache.MetaObjectToName(newPod).String()

	if oldPod.Annotations[util.IngressRateAnnotation] != newPod.Annotations[util.IngressRateAnnotation] ||
		oldPod.Annotations[util.EgressRateAnnotation] != newPod.Annotations[util.EgressRateAnnotation] ||
		oldPod.Annotations[util.NetemQosLatencyAnnotation] != newPod.Annotations[util.NetemQosLatencyAnnotation] ||
		oldPod.Annotations[util.NetemQosJitterAnnotation] != newPod.Annotations[util.NetemQosJitterAnnotation] ||
		oldPod.Annotations[util.NetemQosLimitAnnotation] != newPod.Annotations[util.NetemQosLimitAnnotation] ||
		oldPod.Annotations[util.NetemQosLossAnnotation] != newPod.Annotations[util.NetemQosLossAnnotation] ||
		oldPod.Annotations[util.MirrorControlAnnotation] != newPod.Annotations[util.MirrorControlAnnotation] ||
		oldPod.Annotations[util.IPAddressAnnotation] != newPod.Annotations[util.IPAddressAnnotation] {
		c.updatePodQueue.Add(key)
		return
	}

	attachNets, err := nadutils.ParsePodNetworkAnnotation(newPod)
	if err != nil {
		return
	}
	for _, multiNet := range attachNets {
		provider := fmt.Sprintf("%s.%s.%s", multiNet.Name, multiNet.Namespace, util.OvnProvider)
		if newPod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, provider)] == "true" {
			if oldPod.Annotations[fmt.Sprintf(util.IngressRateAnnotationTemplate, provider)] != newPod.Annotations[fmt.Sprintf(util.IngressRateAnnotationTemplate, provider)] ||
				oldPod.Annotations[fmt.Sprintf(util.EgressRateAnnotationTemplate, provider)] != newPod.Annotations[fmt.Sprintf(util.EgressRateAnnotationTemplate, provider)] ||
				oldPod.Annotations[fmt.Sprintf(util.NetemQosLatencyAnnotationTemplate, provider)] != newPod.Annotations[fmt.Sprintf(util.NetemQosLatencyAnnotationTemplate, provider)] ||
				oldPod.Annotations[fmt.Sprintf(util.NetemQosJitterAnnotationTemplate, provider)] != newPod.Annotations[fmt.Sprintf(util.NetemQosJitterAnnotationTemplate, provider)] ||
				oldPod.Annotations[fmt.Sprintf(util.NetemQosLimitAnnotationTemplate, provider)] != newPod.Annotations[fmt.Sprintf(util.NetemQosLimitAnnotationTemplate, provider)] ||
				oldPod.Annotations[fmt.Sprintf(util.NetemQosLossAnnotationTemplate, provider)] != newPod.Annotations[fmt.Sprintf(util.NetemQosLossAnnotationTemplate, provider)] ||
				oldPod.Annotations[fmt.Sprintf(util.MirrorControlAnnotationTemplate, provider)] != newPod.Annotations[fmt.Sprintf(util.MirrorControlAnnotationTemplate, provider)] {
				c.updatePodQueue.Add(key)
			}
		}
	}
}

func (c *Controller) enqueueDeletePod(obj any) {
	var pod *v1.Pod
	switch t := obj.(type) {
	case *v1.Pod:
		pod = t
	case cache.DeletedFinalStateUnknown:
		p, ok := t.Obj.(*v1.Pod)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		pod = p
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	klog.V(3).Infof("enqueue delete pod %s", pod.Name)
	c.deletePodQueue.Add(&podEvent{oldObj: pod})
}

func (c *Controller) runUpdatePodWorker() {
	for c.processNextUpdatePodWorkItem() {
	}
}

func (c *Controller) runDeletePodWorker() {
	for c.processNextDeletePodWorkItem() {
	}
}

func (c *Controller) processNextUpdatePodWorkItem() bool {
	key, shutdown := c.updatePodQueue.Get()
	if shutdown {
		return false
	}

	err := func(key string) error {
		defer c.updatePodQueue.Done(key)
		if err := c.handleUpdatePod(key); err != nil {
			c.updatePodQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing %q: %w, requeuing", key, err)
		}
		c.updatePodQueue.Forget(key)
		return nil
	}(key)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeletePodWorkItem() bool {
	event, shutdown := c.deletePodQueue.Get()
	if shutdown {
		return false
	}

	err := func(event *podEvent) error {
		defer c.deletePodQueue.Done(event)
		if err := c.handleDeletePod(event); err != nil {
			c.deletePodQueue.AddRateLimited(event)
			return fmt.Errorf("error syncing pod event: %w, requeuing", err)
		}
		c.deletePodQueue.Forget(event)
		return nil
	}(event)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) gcInterfaces() {
	interfacePodMap, err := ovs.ListInterfacePodMap()
	if err != nil {
		klog.Errorf("failed to list interface pod map: %v", err)
		return
	}
	for iface, pod := range interfacePodMap {
		parts := strings.Split(pod, "/")
		if len(parts) < 3 {
			klog.Errorf("malformed pod string %q for interface %s, expected format 'namespace/name/errText'", pod, iface)
			continue
		}

		podNamespace, podName, errText := parts[0], parts[1], parts[2]
		if strings.Contains(errText, "No such device") {
			klog.Infof("pod %s/%s not found, delete ovs interface %s", podNamespace, podName, iface)
			if err := ovs.CleanInterface(iface); err != nil {
				klog.Errorf("failed to clean ovs interface %s: %v", iface, err)
			}
			continue
		}

		if podEntity, err := c.podsLister.Pods(podNamespace).Get(podName); err != nil {
			// Pod not found by name. Check if this might be a KubeVirt VM.
			// For KubeVirt VMs, the pod_name in OVS external_ids is set to the VM name (not the launcher pod name).
			// The actual launcher pod has the label 'vm.kubevirt.io/name' with the VM name as value.
			// Try to find launcher pods by this label.
			if k8serrors.IsNotFound(err) {
				selector := labels.SelectorFromSet(map[string]string{util.KubeVirtVMNameLabel: podName})
				launcherPods, listErr := c.podsLister.Pods(podNamespace).List(selector)
				if listErr != nil {
					klog.Errorf("failed to list launcher pods for vm %s/%s: %v", podNamespace, podName, listErr)
					continue
				}

				// If we found launcher pod(s) for this VM, keep the interface
				if len(launcherPods) > 0 {
					klog.V(5).Infof("found %d launcher pod(s) for vm %s/%s, keeping ovs interface %s",
						len(launcherPods), podNamespace, podName, iface)
					continue
				}

				// No pod and no launcher pod found - safe to delete
				klog.Infof("pod %s/%s not found, delete ovs interface %s", podNamespace, podName, iface)
				if err := ovs.CleanInterface(iface); err != nil {
					klog.Errorf("failed to clean ovs interface %s: %v", iface, err)
				}
			}
		} else {
			// If the pod is found, compare the pod's node with the current cni node. If they differ, delete the interface.
			if podEntity.Spec.NodeName != c.config.NodeName {
				klog.Infof("pod %s/%s is on node %s, delete ovs interface %s on node %s ", podNamespace, podName, podEntity.Spec.NodeName, iface, c.config.NodeName)
				if err := ovs.CleanInterface(iface); err != nil {
					klog.Errorf("failed to clean ovs interface %s: %v", iface, err)
				}
			}
		}
	}
}

func (c *Controller) runIPSecWorker() {
	if err := c.StartIPSecService(); err != nil {
		klog.Errorf("starting ipsec service: %v", err)
	}

	for c.processNextIPSecWorkItem() {
	}
}

func (c *Controller) processNextIPSecWorkItem() bool {
	key, shutdown := c.ipsecQueue.Get()
	if shutdown {
		return false
	}
	defer c.ipsecQueue.Done(key)

	err := func(key string) error {
		if err := c.SyncIPSecKeys(key); err != nil {
			c.ipsecQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing %q: %w, requeuing", key, err)
		}
		c.ipsecQueue.Forget(key)
		return nil
	}(key)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) runUpdateNodeWorker() {
	for c.processNextUpdateNodeWorkItem() {
	}
}

func (c *Controller) processNextUpdateNodeWorkItem() bool {
	key, shutdown := c.updateNodeQueue.Get()
	if shutdown {
		return false
	}

	err := func(key string) error {
		defer c.updateNodeQueue.Done(key)
		if err := c.handleUpdateNode(key); err != nil {
			c.updateNodeQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing node %q: %w, requeuing", key, err)
		}
		c.updateNodeQueue.Forget(key)
		return nil
	}(key)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) handleUpdateNode(key string) error {
	node, err := c.nodesLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	klog.Infof("updating node networks for node %s", key)
	return c.config.UpdateNodeNetworks(node)
}

// Run starts controller
func (c *Controller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.addOrUpdateProviderNetworkQueue.ShutDown()
	defer c.deleteProviderNetworkQueue.ShutDown()
	defer c.subnetQueue.ShutDown()
	defer c.serviceQueue.ShutDown()
	defer c.updatePodQueue.ShutDown()
	defer c.deletePodQueue.ShutDown()
	defer c.ipsecQueue.ShutDown()
	defer c.updateNodeQueue.ShutDown()
	go wait.Until(c.gcInterfaces, time.Minute, stopCh)
	go wait.Until(recompute, 10*time.Minute, stopCh)
	go wait.Until(rotateLog, 1*time.Hour, stopCh)

	if err := c.setIPSet(); err != nil {
		util.LogFatalAndExit(err, "failed to set ipsets")
	}

	klog.Info("Started workers")
	go wait.Until(c.loopOvn0Check, 5*time.Second, stopCh)
	go wait.Until(c.loopOvnExt0Check, 5*time.Second, stopCh)
	go wait.Until(c.loopTunnelCheck, 5*time.Second, stopCh)
	go wait.Until(c.runAddOrUpdateProviderNetworkWorker, time.Second, stopCh)
	go wait.Until(c.runAddOrUpdateServicekWorker, time.Second, stopCh)
	go wait.Until(c.runDeleteProviderNetworkWorker, time.Second, stopCh)
	go wait.Until(c.runSubnetWorker, time.Second, stopCh)
	go wait.Until(c.runUpdatePodWorker, time.Second, stopCh)
	go wait.Until(c.runDeletePodWorker, time.Second, stopCh)
	go wait.Until(c.runUpdateNodeWorker, time.Second, stopCh)
	go wait.Until(c.runIPSecWorker, 3*time.Second, stopCh)
	go wait.Until(c.runGateway, 3*time.Second, stopCh)
	go wait.Until(c.loopEncapIPCheck, 3*time.Second, stopCh)
	go wait.Until(c.ovnMetricsUpdate, 3*time.Second, stopCh)
	go wait.Until(func() {
		if err := c.reconcileRouters(nil); err != nil {
			klog.Errorf("failed to reconcile ovn0 routes: %v", err)
		}
	}, 3*time.Second, stopCh)

	if c.config.EnableTProxy {
		go c.StartTProxyForwarding()
		go wait.Until(c.runTProxyConfigWorker, 3*time.Second, stopCh)
		// Using the tproxy method, kubelet's TCP probe packets cannot reach the namespace of the pod of the custom VPC,
		// so tproxy itself probes the pod of the custom VPC, if probe failed remove the iptable rules from
		// kubelet to tproxy, if probe success recover the iptable rules
		go wait.Until(c.StartTProxyTCPPortProbe, 1*time.Second, stopCh)
	} else {
		c.cleanTProxyConfig()
	}

	if !c.config.EnableOVNIPSec {
		if err := c.StopAndClearIPSecResource(); err != nil {
			klog.Errorf("stop and clear ipsec resource error: %v", err)
		}
	}

	<-stopCh
	klog.Info("Shutting down workers")
}

func recompute() {
	output, err := ovs.Appctl(ovs.OvnController, "inc-engine/recompute")
	if err != nil {
		klog.Errorf("failed to trigger force recompute for %s: %q", ovs.OvnController, output)
	}
}

func evalCommandSymlinks(cmd string) (string, error) {
	path, err := exec.LookPath(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to search for command %q: %w", cmd, err)
	}
	file, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("failed to read evaluate symbolic links for file %q: %w", path, err)
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

func (c *Controller) initRuntime() error {
	ok, err := isLegacyIptablesMode()
	if err != nil {
		klog.Errorf("failed to check iptables mode: %v", err)
		return err
	}
	if !ok {
		c.iptablesObsolete = make(map[string]*iptables.IPTables, 2)
	}

	c.iptables = make(map[string]*iptables.IPTables)
	c.ipsets = make(map[string]*ipsets.IPSets)
	c.gwCounters = make(map[string]*util.GwIPtableCounters)
	c.k8siptables = make(map[string]k8siptables.Interface)
	c.k8sipsets = k8sipset.New()
	c.ovsClient = ovsutil.New()

	if c.protocol == kubeovnv1.ProtocolIPv4 || c.protocol == kubeovnv1.ProtocolDual {
		ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
		if err != nil {
			klog.Error(err)
			return err
		}
		c.iptables[kubeovnv1.ProtocolIPv4] = ipt
		if c.iptablesObsolete != nil {
			ok, err := kernelModuleLoaded(kernelModuleIPTables)
			if err != nil {
				klog.Errorf("failed to check kernel module %s: %v", kernelModuleIPTables, err)
			}
			if ok {
				if ipt, err = iptables.NewWithProtocolAndMode(iptables.ProtocolIPv4, "legacy"); err != nil {
					klog.Error(err)
					return err
				}
				c.iptablesObsolete[kubeovnv1.ProtocolIPv4] = ipt
			}
		}
		c.ipsets[kubeovnv1.ProtocolIPv4] = ipsets.NewIPSets(ipsets.NewIPVersionConfig(ipsets.IPFamilyV4, IPSetPrefix, nil, nil))
		c.k8siptables[kubeovnv1.ProtocolIPv4] = k8siptables.New(k8siptables.ProtocolIPv4)
	}
	if c.protocol == kubeovnv1.ProtocolIPv6 || c.protocol == kubeovnv1.ProtocolDual {
		ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv6)
		if err != nil {
			klog.Error(err)
			return err
		}
		c.iptables[kubeovnv1.ProtocolIPv6] = ipt
		if c.iptablesObsolete != nil {
			ok, err := kernelModuleLoaded(kernelModuleIP6Tables)
			if err != nil {
				klog.Errorf("failed to check kernel module %s: %v", kernelModuleIP6Tables, err)
			}
			if ok {
				if ipt, err = iptables.NewWithProtocolAndMode(iptables.ProtocolIPv6, "legacy"); err != nil {
					klog.Error(err)
					return err
				}
				c.iptablesObsolete[kubeovnv1.ProtocolIPv6] = ipt
			}
		}
		c.ipsets[kubeovnv1.ProtocolIPv6] = ipsets.NewIPSets(ipsets.NewIPVersionConfig(ipsets.IPFamilyV6, IPSetPrefix, nil, nil))
		c.k8siptables[kubeovnv1.ProtocolIPv6] = k8siptables.New(k8siptables.ProtocolIPv6)
	}

	c.nmSyncer = newNetworkManagerSyncer()
	c.nmSyncer.Run(c.transferAddrsAndRoutes)

	return nil
}

func (c *Controller) handleEnableExternalLBAddressChange(oldSubnet, newSubnet *kubeovnv1.Subnet) error {
	var subnetName string
	var action string

	switch {
	case oldSubnet != nil && newSubnet != nil:
		subnetName = oldSubnet.Name
		if oldSubnet.Spec.EnableExternalLBAddress != newSubnet.Spec.EnableExternalLBAddress {
			klog.Infof("EnableExternalLBAddress changed for subnet %s", newSubnet.Name)
			if newSubnet.Spec.EnableExternalLBAddress {
				action = "add"
			} else {
				action = "remove"
			}
		}
	case oldSubnet != nil:
		subnetName = oldSubnet.Name
		if oldSubnet.Spec.EnableExternalLBAddress {
			klog.Infof("EnableExternalLBAddress removed for subnet %s", oldSubnet.Name)
			action = "remove"
		}
	case newSubnet != nil:
		subnetName = newSubnet.Name
		if newSubnet.Spec.EnableExternalLBAddress {
			klog.Infof("EnableExternalLBAddress added for subnet %s", newSubnet.Name)
			action = "add"
		}
	}

	if action != "" {
		services, err := c.servicesLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list services: %v", err)
			return err
		}

		for _, svc := range services {
			if svc.Annotations[util.ServiceExternalIPFromSubnetAnnotation] == subnetName {
				klog.Infof("Service %s/%s has external LB address pool annotation from subnet %s, action: %s", svc.Namespace, svc.Name, subnetName, action)
				switch action {
				case "add":
					c.serviceQueue.Add(&serviceEvent{newObj: svc})
				case "remove":
					c.serviceQueue.Add(&serviceEvent{oldObj: svc})
				}
			}
		}
	}
	return nil
}

func (c *Controller) reconcileRouters(event *subnetEvent) error {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets %v", err)
		return err
	}

	if event != nil {
		var ok bool
		var oldSubnet, newSubnet *kubeovnv1.Subnet
		if event.oldObj != nil {
			if oldSubnet, ok = event.oldObj.(*kubeovnv1.Subnet); !ok {
				klog.Errorf("expected old subnet in subnetEvent but got %#v", event.oldObj)
				return nil
			}
		}
		if event.newObj != nil {
			if newSubnet, ok = event.newObj.(*kubeovnv1.Subnet); !ok {
				klog.Errorf("expected new subnet in subnetEvent but got %#v", event.newObj)
				return nil
			}
		}

		isAdd, needAction := c.CheckSubnetU2OChangeAction(oldSubnet, newSubnet)
		if needAction {
			if err := c.HandleU2OForSubnet(newSubnet, isAdd); err != nil {
				return err
			}
		}

		if err = c.handleEnableExternalLBAddressChange(oldSubnet, newSubnet); err != nil {
			klog.Errorf("failed to handle enable external lb address change: %v", err)
			return err
		}
		rulesToAdd, rulesToDel, routesToAdd, routesToDel, err := c.diffPolicyRouting(oldSubnet, newSubnet)
		if err != nil {
			klog.Errorf("failed to get policy routing difference: %v", err)
			return err
		}
		for _, r := range routesToAdd {
			if err = netlink.RouteReplace(&r); err != nil && !errors.Is(err, syscall.EEXIST) {
				klog.Errorf("failed to replace route for subnet %s: %v", newSubnet.Name, err)
				return err
			}
		}
		for _, r := range rulesToAdd {
			if err = netlink.RuleAdd(&r); err != nil && !errors.Is(err, syscall.EEXIST) {
				klog.Errorf("failed to add network rule for subnet %s: %v", newSubnet.Name, err)
				return err
			}
		}
		for _, r := range rulesToDel {
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
		for _, r := range routesToDel {
			if err = netlink.RouteDel(&r); err != nil && !errors.Is(err, syscall.ENOENT) {
				klog.Errorf("failed to delete route for subnet %s: %v", oldSubnet.Name, err)
				return err
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
		joinIPv4, joinIPv6 = util.SplitStringIP(node.Annotations[util.IPAddressAnnotation])
	}

	joinCIDR := make([]string, 0, 2)
	cidrs := make([]string, 0, len(subnets)*2)
	for _, subnet := range subnets {
		if subnet.Spec.Vpc != c.config.ClusterRouter ||
			(subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway && (!subnet.Spec.U2OInterconnection || (subnet.Spec.EnableLb != nil && *subnet.Spec.EnableLb))) ||
			!subnet.Status.IsValidated() {
			continue
		}

		for cidrBlock := range strings.SplitSeq(subnet.Spec.CIDRBlock, ",") {
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
		return errors.New("annotation for node ovn.kubernetes.io/gateway not exists")
	}
	nic, err := netlink.LinkByName(util.NodeNic)
	if err != nil {
		klog.Errorf("failed to get nic %s", util.NodeNic)
		return fmt.Errorf("failed to get nic %s", util.NodeNic)
	}

	allRoutes, err := getNicExistRoutes(nil, gateway)
	if err != nil {
		klog.Error(err)
		return err
	}
	nodeNicRoutes, err := getNicExistRoutes(nic, gateway)
	if err != nil {
		klog.Error(err)
		return err
	}
	toAdd, toDel := routeDiff(nodeNicRoutes, allRoutes, cidrs, joinCIDR, joinIPv4, joinIPv6, gateway, net.ParseIP(nodeIPv4), net.ParseIP(nodeIPv6))
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

	return nil
}

func genLBServiceRules(service *v1.Service, bridgeName, underlayNic string) []LbServiceRules {
	var lbServiceRules []LbServiceRules
	for _, ingress := range service.Status.LoadBalancer.Ingress {
		for _, port := range service.Spec.Ports {
			lbServiceRules = append(lbServiceRules, LbServiceRules{
				IP:          ingress.IP,
				Port:        uint16(port.Port),
				Protocol:    string(port.Protocol),
				DstMac:      util.MasqueradeExternalLBAccessMac,
				UnderlayNic: underlayNic,
				BridgeName:  bridgeName,
			})
		}
	}
	return lbServiceRules
}

func (c *Controller) diffExternalLBServiceRules(oldService, newService *v1.Service, isSubnetExternalLBEnabled bool) (lbServiceRulesToAdd, lbServiceRulesToDel []LbServiceRules, err error) {
	var oldlbServiceRules, newlbServiceRules []LbServiceRules

	if oldService != nil && oldService.Annotations[util.ServiceExternalIPFromSubnetAnnotation] != "" {
		oldBridgeName, underlayNic, err := c.getExtInfoBySubnet(oldService.Annotations[util.ServiceExternalIPFromSubnetAnnotation])
		if err != nil {
			klog.Errorf("failed to get provider network by subnet %s: %v", oldService.Annotations[util.ServiceExternalIPFromSubnetAnnotation], err)
			return nil, nil, err
		}

		oldlbServiceRules = genLBServiceRules(oldService, oldBridgeName, underlayNic)
	}

	if isSubnetExternalLBEnabled && newService != nil && newService.Annotations[util.ServiceExternalIPFromSubnetAnnotation] != "" {
		newBridgeName, underlayNic, err := c.getExtInfoBySubnet(newService.Annotations[util.ServiceExternalIPFromSubnetAnnotation])
		if err != nil {
			klog.Errorf("failed to get provider network by subnet %s: %v", newService.Annotations[util.ServiceExternalIPFromSubnetAnnotation], err)
			return nil, nil, err
		}
		newlbServiceRules = genLBServiceRules(newService, newBridgeName, underlayNic)
	}

	for _, oldRule := range oldlbServiceRules {
		found := slices.Contains(newlbServiceRules, oldRule)
		if !found {
			lbServiceRulesToDel = append(lbServiceRulesToDel, oldRule)
		}
	}

	for _, newRule := range newlbServiceRules {
		found := slices.Contains(oldlbServiceRules, newRule)
		if !found {
			lbServiceRulesToAdd = append(lbServiceRulesToAdd, newRule)
		}
	}

	return lbServiceRulesToAdd, lbServiceRulesToDel, nil
}

func (c *Controller) getExtInfoBySubnet(subnetName string) (string, string, error) {
	subnet, err := c.subnetsLister.Get(subnetName)
	if err != nil {
		klog.Errorf("failed to get subnet %s: %v", subnetName, err)
		return "", "", err
	}

	vlanName := subnet.Spec.Vlan
	if vlanName == "" {
		return "", "", errors.New("vlan not specified in subnet")
	}

	vlan, err := c.vlansLister.Get(vlanName)
	if err != nil {
		klog.Errorf("failed to get vlan %s: %v", vlanName, err)
		return "", "", err
	}

	providerNetworkName := vlan.Spec.Provider
	if providerNetworkName == "" {
		return "", "", errors.New("provider network not specified in vlan")
	}

	pn, err := c.providerNetworksLister.Get(providerNetworkName)
	if err != nil {
		klog.Errorf("failed to get provider network %s: %v", providerNetworkName, err)
		return "", "", err
	}

	underlayNic := pn.Spec.DefaultInterface
	for _, item := range pn.Spec.CustomInterfaces {
		if slices.Contains(item.Nodes, c.config.NodeName) {
			underlayNic = item.Interface
			break
		}
	}
	klog.Infof("Provider network: %s, Underlay NIC: %s", providerNetworkName, underlayNic)
	return util.ExternalBridgeName(providerNetworkName), underlayNic, nil
}

func (c *Controller) reconcileServices(event *serviceEvent) error {
	if event == nil {
		return nil
	}
	var ok bool
	var oldService, newService *v1.Service
	if event.oldObj != nil {
		if oldService, ok = event.oldObj.(*v1.Service); !ok {
			klog.Errorf("expected old service in serviceEvent but got %#v", event.oldObj)
			return nil
		}
	}

	if event.newObj != nil {
		if newService, ok = event.newObj.(*v1.Service); !ok {
			klog.Errorf("expected new service in serviceEvent but got %#v", event.newObj)
			return nil
		}
	}

	isSubnetExternalLBEnabled := false
	if newService != nil && newService.Annotations[util.ServiceExternalIPFromSubnetAnnotation] != "" {
		subnet, err := c.subnetsLister.Get(newService.Annotations[util.ServiceExternalIPFromSubnetAnnotation])
		if err != nil {
			klog.Errorf("failed to get subnet %s: %v", newService.Annotations[util.ServiceExternalIPFromSubnetAnnotation], err)
			return err
		}
		isSubnetExternalLBEnabled = subnet.Spec.EnableExternalLBAddress
	}

	lbServiceRulesToAdd, lbServiceRulesToDel, err := c.diffExternalLBServiceRules(oldService, newService, isSubnetExternalLBEnabled)
	if err != nil {
		klog.Errorf("failed to get ip port difference: %v", err)
		return err
	}

	if len(lbServiceRulesToAdd) > 0 {
		for _, rule := range lbServiceRulesToAdd {
			klog.Infof("Adding LB service rule: %+v", rule)
			if err := ovs.AddOrUpdateUnderlaySubnetSvcLocalOpenFlow(c.ovsClient, rule.BridgeName, rule.IP, rule.Protocol, rule.DstMac, rule.UnderlayNic, rule.Port); err != nil {
				klog.Errorf("failed to add or update underlay subnet svc local openflow: %v", err)
			}
		}
	}

	if len(lbServiceRulesToDel) > 0 {
		for _, rule := range lbServiceRulesToDel {
			klog.Infof("Delete LB service rule: %+v", rule)
			if err := ovs.DeleteUnderlaySubnetSvcLocalOpenFlow(c.ovsClient, rule.BridgeName, rule.IP, rule.Protocol, rule.UnderlayNic, rule.Port); err != nil {
				klog.Errorf("failed to delete underlay subnet svc local openflow: %v", err)
			}
		}
	}

	return nil
}

func getNicExistRoutes(nic netlink.Link, gateway string) ([]netlink.Route, error) {
	var routes, existRoutes []netlink.Route
	var err error
	for gw := range strings.SplitSeq(gateway, ",") {
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

func routeDiff(nodeNicRoutes, allRoutes []netlink.Route, cidrs, joinCIDR []string, joinIPv4, joinIPv6, gateway string, srcIPv4, srcIPv6 net.IP) (toAdd, toDel []netlink.Route) {
	_ = joinIPv6

	for _, route := range nodeNicRoutes {
		if route.Scope == netlink.SCOPE_LINK || route.Dst == nil || route.Dst.IP.IsLinkLocalUnicast() {
			continue
		}

		found := slices.Contains(cidrs, route.Dst.String())
		if !found {
			toDel = append(toDel, route)
		}
		conflict := false
		for _, ar := range allRoutes {
			if ar.Dst != nil && ar.Dst.String() == route.Dst.String() && ar.LinkIndex != route.LinkIndex {
				conflict = true
				break
			}
		}
		if conflict {
			toDel = append(toDel, route)
		}
	}
	if len(toDel) > 0 {
		klog.Infof("routes to delete: %v", toDel)
	}

	ipv4, ipv6 := util.SplitStringIP(gateway)
	gwV4, gwV6 := net.ParseIP(ipv4), net.ParseIP(ipv6)
	for _, c := range cidrs {
		var src, gw net.IP
		switch util.CheckProtocol(c) {
		case kubeovnv1.ProtocolIPv4:
			src, gw = srcIPv4, gwV4
		case kubeovnv1.ProtocolIPv6:
			src, gw = srcIPv6, gwV6
		}

		found := false
		for _, ar := range allRoutes {
			if ar.Dst != nil && ar.Dst.String() == c {
				if slices.Contains(joinCIDR, c) {
					found = true
					klog.V(3).Infof("[routeDiff] joinCIDR route already exists in allRoutes: %v", ar)
					break
				} else if (ar.Src == nil && src == nil) || (ar.Src != nil && src != nil && ar.Src.Equal(src)) {
					found = true
					klog.V(3).Infof("[routeDiff] route already exists in allRoutes: %v", ar)
					break
				}
			}
		}
		if found {
			continue
		}
		for _, r := range nodeNicRoutes {
			if r.Dst == nil || r.Dst.String() != c {
				continue
			}
			if (src == nil && r.Src == nil) || (src != nil && r.Src != nil && src.Equal(r.Src)) {
				found = true
				break
			}
		}
		if !found {
			var priority int
			scope := netlink.SCOPE_UNIVERSE
			proto := netlink.RouteProtocol(syscall.RTPROT_STATIC)
			if slices.Contains(joinCIDR, c) {
				if util.CheckProtocol(c) == kubeovnv1.ProtocolIPv4 {
					src = net.ParseIP(joinIPv4)
				} else {
					src, priority = nil, 256
				}
				gw, scope = nil, netlink.SCOPE_LINK
				proto = netlink.RouteProtocol(unix.RTPROT_KERNEL)
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
	return toAdd, toDel
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
		return rulesToAdd, rulesToDel, routesToAdd, routesToDel, err
	}
	newRules, newRoutes, err := c.getPolicyRouting(newSubnet)
	if err != nil {
		klog.Error(err)
		return rulesToAdd, rulesToDel, routesToAdd, routesToDel, err
	}

	rulesToAdd = getRulesToAdd(oldRules, newRules)
	rulesToDel = getRulesToAdd(newRules, oldRules)
	routesToAdd = getRoutesToAdd(oldRoutes, newRoutes)
	routesToDel = getRoutesToAdd(newRoutes, oldRoutes)

	return rulesToAdd, rulesToDel, routesToAdd, routesToDel, err
}

func (c *Controller) getPolicyRouting(subnet *kubeovnv1.Subnet) ([]netlink.Rule, []netlink.Route, error) {
	if subnet == nil || subnet.Spec.ExternalEgressGateway == "" || subnet.Spec.Vpc != c.config.ClusterRouter {
		return nil, nil, nil
	}
	if subnet.Spec.GatewayType == kubeovnv1.GWCentralizedType {
		node, err := c.nodesLister.Get(c.config.NodeName)
		if err != nil {
			klog.Errorf("failed to get node %s: %v", c.config.NodeName, err)
			return nil, nil, err
		}
		isGatewayNode := util.GatewayContains(subnet.Spec.GatewayNode, c.config.NodeName) ||
			(subnet.Spec.GatewayNode == "" && util.MatchLabelSelectors(subnet.Spec.GatewayNodeSelectors, node.Labels))
		if !isGatewayNode {
			return nil, nil, nil
		}
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

func (c *Controller) GetProviderInfoFromSubnet(subnet *kubeovnv1.Subnet) (bridgeName, chassisMac string, err error) {
	if subnet == nil {
		return "", "", nil
	}
	if subnet.Spec.Vlan == "" {
		return "", "", nil
	}

	vlan, err := c.vlansLister.Get(subnet.Spec.Vlan)
	if err != nil {
		return "", "", fmt.Errorf("failed to get vlan %s: %w", subnet.Spec.Vlan, err)
	}
	providerName := vlan.Spec.Provider
	chassisMac, err = GetProviderChassisMac(providerName)
	if err != nil {
		return "", "", fmt.Errorf("failed to get chassis mac for provider %s: %w", providerName, err)
	}

	bridgeName = util.ExternalBridgeName(providerName)
	return bridgeName, chassisMac, nil
}

func HandleU2OForPod(ovsClient *ovsutil.Client, pod *v1.Pod, bridgeName, chassisMac, subnetName string, isAdd bool) error {
	if pod == nil {
		return errors.New("pod is nil")
	}

	podMac := pod.Annotations[util.MacAddressAnnotation]

	podIPs := []string{}
	if pod.Annotations != nil && pod.Annotations[util.IPAddressAnnotation] != "" {
		podIPs = append(podIPs, strings.Split(pod.Annotations[util.IPAddressAnnotation], ",")...)

		for _, podIP := range podIPs {
			var err error
			if isAdd {
				err = ovs.AddOrUpdateU2OKeepSrcMac(ovsClient, bridgeName, podIP, podMac, chassisMac, subnetName)
			} else {
				err = ovs.DeleteU2OKeepSrcMac(ovsClient, bridgeName, podIP, chassisMac, subnetName)
			}

			if err != nil {
				action := "add"
				if !isAdd {
					action = "delete"
				}
				return fmt.Errorf("failed to %s U2O rule for pod %s/%s: %w", action, pod.Namespace, pod.Name, err)
			}
		}
	}

	return nil
}

func (c *Controller) HandleU2OForSubnet(subnet *kubeovnv1.Subnet, isAdd bool) error {
	klog.Infof("U2O processing for subnet %s, action: %v", subnet.Name, isAdd)

	bridgeName, chassisMac, err := c.GetProviderInfoFromSubnet(subnet)
	if err != nil {
		return fmt.Errorf("failed to get provider info: %w", err)
	}

	pods, err := c.podsLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	for _, pod := range pods {
		if pod.Annotations[util.LogicalSwitchAnnotation] != subnet.Name {
			continue
		}
		if err := HandleU2OForPod(c.ovsClient, pod, bridgeName, chassisMac, subnet.Name, isAdd); err != nil {
			klog.Error(err)
			return err
		}
	}

	return nil
}

func (c *Controller) CheckSubnetU2OChangeAction(oldSubnet, newSubnet *kubeovnv1.Subnet) (bool, bool) {
	if newSubnet == nil ||
		(oldSubnet != nil && oldSubnet.Spec.U2OInterconnection == newSubnet.Spec.U2OInterconnection) {
		return false, false
	}

	if newSubnet.Spec.Vlan == "" || newSubnet.Spec.LogicalGateway {
		return false, false
	}

	return newSubnet.Spec.U2OInterconnection, true
}

func (c *Controller) handleUpdatePod(key string) error {
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
		klog.Error(err)
		return err
	}

	if err := util.ValidatePodNetwork(pod.Annotations); err != nil {
		klog.Errorf("validate pod %s/%s failed, %v", namespace, name, err)
		c.recorder.Eventf(pod, v1.EventTypeWarning, "ValidatePodNetworkFailed", err.Error())
		return err
	}

	if _, ok := pod.Annotations[util.LogicalSwitchAnnotation]; ok {
		subnet, err := c.subnetsLister.Get(pod.Annotations[util.LogicalSwitchAnnotation])
		if err != nil {
			klog.Error(err)
			return err
		}

		if subnet.Spec.U2OInterconnection {
			bridgeName, chassisMac, err := c.GetProviderInfoFromSubnet(subnet)
			if err != nil {
				klog.Error(err)
				return err
			}
			if err := HandleU2OForPod(c.ovsClient, pod, bridgeName, chassisMac, subnet.Name, true); err != nil {
				klog.Error(err)
				return err
			}
		}
	}

	podName := pod.Name
	if pod.Annotations[fmt.Sprintf(util.VMAnnotationTemplate, util.OvnProvider)] != "" {
		podName = pod.Annotations[fmt.Sprintf(util.VMAnnotationTemplate, util.OvnProvider)]
	}

	ifaceID := ovs.PodNameToPortName(podName, pod.Namespace, util.OvnProvider)
	ovsIngress := pod.Annotations[util.EgressRateAnnotation]
	ovsEgress := pod.Annotations[util.IngressRateAnnotation]
	err = ovs.SetInterfaceBandwidth(podName, pod.Namespace, ifaceID, ovsIngress, ovsEgress)
	if err != nil {
		klog.Error(err)
		return err
	}
	err = ovs.ConfigInterfaceMirror(c.config.EnableMirror, pod.Annotations[util.MirrorControlAnnotation], ifaceID)
	if err != nil {
		klog.Error(err)
		return err
	}
	err = ovs.SetNetemQos(podName, pod.Namespace, ifaceID, pod.Annotations[util.NetemQosLatencyAnnotation], pod.Annotations[util.NetemQosLimitAnnotation], pod.Annotations[util.NetemQosLossAnnotation], pod.Annotations[util.NetemQosJitterAnnotation])
	if err != nil {
		klog.Error(err)
		return err
	}

	attachNets, err := nadutils.ParsePodNetworkAnnotation(pod)
	if err != nil {
		if _, ok := err.(*nadv1.NoK8sNetworkError); ok {
			return nil
		}
		klog.Error(err)
		return err
	}
	for _, multiNet := range attachNets {
		provider := fmt.Sprintf("%s.%s.%s", multiNet.Name, multiNet.Namespace, util.OvnProvider)
		if pod.Annotations[fmt.Sprintf(util.VMAnnotationTemplate, provider)] != "" {
			podName = pod.Annotations[fmt.Sprintf(util.VMAnnotationTemplate, provider)]
		}
		if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, provider)] == "true" {
			ifaceID = ovs.PodNameToPortName(podName, pod.Namespace, provider)

			err = ovs.SetInterfaceBandwidth(podName, pod.Namespace, ifaceID, pod.Annotations[fmt.Sprintf(util.EgressRateAnnotationTemplate, provider)], pod.Annotations[fmt.Sprintf(util.IngressRateAnnotationTemplate, provider)])
			if err != nil {
				klog.Error(err)
				return err
			}
			err = ovs.ConfigInterfaceMirror(c.config.EnableMirror, pod.Annotations[fmt.Sprintf(util.MirrorControlAnnotationTemplate, provider)], ifaceID)
			if err != nil {
				klog.Error(err)
				return err
			}
			err = ovs.SetNetemQos(podName, pod.Namespace, ifaceID, pod.Annotations[fmt.Sprintf(util.NetemQosLatencyAnnotationTemplate, provider)], pod.Annotations[fmt.Sprintf(util.NetemQosLimitAnnotationTemplate, provider)], pod.Annotations[fmt.Sprintf(util.NetemQosLossAnnotationTemplate, provider)], pod.Annotations[fmt.Sprintf(util.NetemQosJitterAnnotationTemplate, provider)])
			if err != nil {
				klog.Error(err)
				return err
			}
		}
	}

	return nil
}

func (c *Controller) handleDeletePod(event *podEvent) error {
	var pod *v1.Pod
	if event.oldObj != nil {
		pod = event.oldObj.(*v1.Pod)
	} else {
		return nil
	}

	logicalSwitch, ok := pod.Annotations[util.LogicalSwitchAnnotation]
	if !ok {
		return nil
	}

	subnet, err := c.subnetsLister.Get(logicalSwitch)
	if err != nil {
		klog.Error(err)
		return err
	}

	if !subnet.Spec.U2OInterconnection {
		return nil
	}

	bridgeName, chassisMac, err := c.GetProviderInfoFromSubnet(subnet)
	if err != nil {
		klog.Error(err)
		return err
	}

	if err := HandleU2OForPod(c.ovsClient, pod, bridgeName, chassisMac, subnet.Name, false); err != nil {
		klog.Error(err)
		return err
	}

	return nil
}

func (c *Controller) loopEncapIPCheck() {
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

		if c.config.Iface == nodeTunnelName {
			klog.V(3).Infof("node tunnel interface %s not changed", nodeTunnelName)
			return
		}
		c.config.Iface = nodeTunnelName
		klog.Infof("Update node tunnel interface %v", nodeTunnelName)

		c.config.DefaultEncapIP = strings.Split(addrs[0].String(), "/")[0]
		if err = c.config.setEncapIPs(); err != nil {
			klog.Errorf("failed to set encap ip %s for iface %s", c.config.DefaultEncapIP, c.config.Iface)
			return
		}
	}
}

func (c *Controller) ovnMetricsUpdate() {
	c.setOvnSubnetGatewayMetric()

	resetSysParaMetrics()
	c.setIPLocalPortRangeMetric()
	c.setCheckSumErrMetric()
	c.setDNSSearchMetric()
	c.setTCPTwRecycleMetric()
	c.setTCPMtuProbingMetric()
	c.setConntrackTCPLiberalMetric()
	c.setBridgeNfCallIptablesMetric()
	c.setIPv6RouteMaxsizeMetric()
	c.setTCPMemMetric()
}

func resetSysParaMetrics() {
	metricIPLocalPortRange.Reset()
	metricCheckSumErr.Reset()
	metricDNSSearch.Reset()
	metricTCPTwRecycle.Reset()
	metricTCPMtuProbing.Reset()
	metricConntrackTCPLiberal.Reset()
	metricBridgeNfCallIptables.Reset()
	metricTCPMem.Reset()
	metricIPv6RouteMaxsize.Reset()
}

func rotateLog() {
	output, err := exec.Command("logrotate", "/etc/logrotate.d/openvswitch").CombinedOutput()
	if err != nil {
		klog.Errorf("failed to rotate openvswitch log %q", output)
	}
	output, err = exec.Command("logrotate", "/etc/logrotate.d/ovn").CombinedOutput()
	if err != nil {
		klog.Errorf("failed to rotate ovn log %q", output)
	}
	output, err = exec.Command("logrotate", "/etc/logrotate.d/kubeovn").CombinedOutput()
	if err != nil {
		klog.Errorf("failed to rotate kube-ovn log %q", output)
	}
}

func kernelModuleLoaded(module string) (bool, error) {
	data, err := os.ReadFile("/proc/modules")
	if err != nil {
		klog.Errorf("failed to read /proc/modules: %v", err)
		return false, err
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		if fields := strings.Fields(line); len(fields) != 0 && fields[0] == module {
			return true, nil
		}
	}

	return false, nil
}
