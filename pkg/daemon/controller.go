package daemon

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"

	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/kubeovn/kube-ovn/pkg/ovs"

	"github.com/alauda/felix/ipsets"
	"github.com/coreos/go-iptables/iptables"
	"github.com/vishvananda/netlink"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovninformer "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// Controller watch pod and namespace changes to update iptables, ipset and ovs qos
type Controller struct {
	config *Configuration

	subnetsLister kubeovnlister.SubnetLister
	subnetsSynced cache.InformerSynced
	subnetQueue   workqueue.RateLimitingInterface

	podsLister listerv1.PodLister
	podsSynced cache.InformerSynced
	podQueue   workqueue.RateLimitingInterface

	nodesLister listerv1.NodeLister
	nodesSynced cache.InformerSynced

	recorder record.EventRecorder

	iptable map[string]*iptables.IPTables
	ipset   map[string]*ipsets.IPSets

	protocol   string
	internalIP string
}

// NewController init a daemon controller
func NewController(config *Configuration, podInformerFactory informers.SharedInformerFactory, nodeInformerFactory informers.SharedInformerFactory, kubeovnInformerFactory kubeovninformer.SharedInformerFactory) (*Controller, error) {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: config.KubeClient.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: config.NodeName})

	subnetInformer := kubeovnInformerFactory.Kubeovn().V1().Subnets()
	podInformer := podInformerFactory.Core().V1().Pods()
	nodeInformer := nodeInformerFactory.Core().V1().Nodes()

	controller := &Controller{
		config:        config,
		subnetsLister: subnetInformer.Lister(),
		subnetsSynced: subnetInformer.Informer().HasSynced,
		subnetQueue:   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Subnet"),

		podsLister: podInformer.Lister(),
		podsSynced: podInformer.Informer().HasSynced,
		podQueue:   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Pod"),

		nodesLister: nodeInformer.Lister(),
		nodesSynced: nodeInformer.Informer().HasSynced,

		recorder: recorder,
	}

	node, err := config.KubeClient.CoreV1().Nodes().Get(context.Background(), config.NodeName, metav1.GetOptions{})
	if err != nil {
		klog.Fatalf("failed to get node %s info %v", config.NodeName, err)
		return nil, err
	}
	controller.protocol = util.CheckProtocol(node.Annotations[util.IpAddressAnnotation])
	controller.internalIP = util.GetNodeInternalIP(*node)

	controller.iptable = make(map[string]*iptables.IPTables)
	controller.ipset = make(map[string]*ipsets.IPSets)
	if controller.protocol == kubeovnv1.ProtocolIPv4 || controller.protocol == kubeovnv1.ProtocolDual {
		iptable, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
		if err != nil {
			return nil, err
		}
		controller.iptable[kubeovnv1.ProtocolIPv4] = iptable
		controller.ipset[kubeovnv1.ProtocolIPv4] = ipsets.NewIPSets(ipsets.NewIPVersionConfig(ipsets.IPFamilyV4, IPSetPrefix, nil, nil))
	}
	if controller.protocol == kubeovnv1.ProtocolIPv6 || controller.protocol == kubeovnv1.ProtocolDual {
		iptable, err := iptables.NewWithProtocol(iptables.ProtocolIPv6)
		if err != nil {
			return nil, err
		}
		controller.iptable[kubeovnv1.ProtocolIPv6] = iptable
		controller.ipset[kubeovnv1.ProtocolIPv6] = ipsets.NewIPSets(ipsets.NewIPVersionConfig(ipsets.IPFamilyV6, IPSetPrefix, nil, nil))
	}

	subnetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueSubnet,
		UpdateFunc: controller.enqueueUpdateSubnet,
		DeleteFunc: controller.enqueueSubnet,
	})
	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: controller.enqueuePod,
	})

	return controller, nil
}

func (c *Controller) enqueueSubnet(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.subnetQueue.Add(key)
}

func (c *Controller) enqueueUpdateSubnet(_, new interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.subnetQueue.Add(key)
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
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.subnetQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.reconcileRouters(); err != nil {
			c.subnetQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
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

func (c *Controller) reconcileRouters() error {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list namespace %v", err)
		return err
	}
	cidrs := make([]string, 0, len(subnets)*2)
	for _, subnet := range subnets {
		if !subnet.Status.IsReady() || subnet.Spec.UnderlayGateway {
			continue
		}

		for _, cidrBlock := range strings.Split(subnet.Spec.CIDRBlock, ",") {
			if _, ipNet, err := net.ParseCIDR(cidrBlock); err != nil {
				klog.Errorf("%s is not a valid cidr block", cidrBlock)
			} else {
				cidrs = append(cidrs, ipNet.String())
			}
		}
	}

	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get node %s %v", c.config.NodeName, err)
		return err
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

	existRoutes, err := getNicExistRoutes(nic, gateway)
	if err != nil {
		return err
	}

	toAdd, toDel := routeDiff(existRoutes, cidrs)
	for _, r := range toDel {
		_, cidr, _ := net.ParseCIDR(r)
		if err = netlink.RouteDel(&netlink.Route{Dst: cidr}); err != nil {
			klog.Errorf("failed to del route %v", err)
		}
	}

	for _, r := range toAdd {
		_, cidr, _ := net.ParseCIDR(r)
		for _, gw := range strings.Split(gateway, ",") {
			if util.CheckProtocol(gw) != util.CheckProtocol(r) {
				continue
			}
			if err = netlink.RouteReplace(&netlink.Route{Dst: cidr, LinkIndex: nic.Attrs().Index, Scope: netlink.SCOPE_UNIVERSE, Gw: net.ParseIP(gw)}); err != nil {
				klog.Errorf("failed to add route %v", err)
			}
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

func routeDiff(existRoutes []netlink.Route, cidrs []string) (toAdd []string, toDel []string) {
	for _, route := range existRoutes {
		if route.Scope == netlink.SCOPE_LINK {
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
			toDel = append(toDel, route.Dst.String())
		}
	}
	if len(toDel) > 0 {
		klog.Infof("route to del %v", toDel)
	}

	for _, c := range cidrs {
		found := false
		for _, r := range existRoutes {
			if r.Dst.String() == c {
				found = true
				break
			}
		}
		if !found {
			toAdd = append(toAdd, c)
		}
	}
	if len(toAdd) > 0 {
		klog.Infof("route to add %v", toAdd)
	}
	return
}

func (c *Controller) enqueuePod(old, new interface{}) {
	oldPod := old.(*v1.Pod)
	newPod := new.(*v1.Pod)

	if oldPod.Annotations[util.IngressRateAnnotation] != newPod.Annotations[util.IngressRateAnnotation] ||
		oldPod.Annotations[util.EgressRateAnnotation] != newPod.Annotations[util.EgressRateAnnotation] {
		var key string
		var err error
		if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
			utilruntime.HandleError(err)
			return
		}
		c.podQueue.Add(key)
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
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if err := util.ValidatePodNetwork(pod.Annotations); err != nil {
		klog.Errorf("validate pod %s/%s failed, %v", namespace, name, err)
		c.recorder.Eventf(pod, v1.EventTypeWarning, "ValidatePodNetworkFailed", err.Error())
		return err
	}

	return ovs.SetInterfaceBandwidth(fmt.Sprintf("%s.%s", pod.Name, pod.Namespace), pod.Annotations[util.EgressRateAnnotation], pod.Annotations[util.IngressRateAnnotation])
}

// Run starts controller
func (c *Controller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.subnetQueue.ShutDown()
	defer c.podQueue.ShutDown()

	go wait.Until(ovs.CleanLostInterface, time.Minute, stopCh)
	go wait.Until(recompute, 10*time.Minute, stopCh)

	klog.Info("start watching namespace changes")
	if ok := cache.WaitForCacheSync(stopCh, c.subnetsSynced, c.podsSynced, c.nodesSynced); !ok {
		klog.Fatalf("failed to wait for caches to sync")
		return
	}

	klog.Info("Started workers")
	go wait.Until(c.loopOvn0Check, 5*time.Second, stopCh)
	go wait.Until(c.disableTunnelOffload, 5*time.Second, stopCh)

	go wait.Until(c.runSubnetWorker, time.Second, stopCh)
	go wait.Until(c.runPodWorker, time.Second, stopCh)
	go wait.Until(c.runGateway, 3*time.Second, stopCh)
	<-stopCh
	klog.Info("Shutting down workers")
}

func recompute() {
	output, err := exec.Command("ovn-appctl", "-t", "ovn-controller", "recompute").CombinedOutput()
	if err != nil {
		klog.Errorf("failed to recompute ovn-controller %q", output)
	}
}
