package daemon

import (
	"fmt"
	"net"
	"os/exec"
	"time"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"

	"github.com/alauda/kube-ovn/pkg/ovs"
	"k8s.io/apimachinery/pkg/api/errors"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovninformer "github.com/alauda/kube-ovn/pkg/client/informers/externalversions"
	kubeovnlister "github.com/alauda/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/util"
	"github.com/coreos/go-iptables/iptables"
	"github.com/projectcalico/felix/ipsets"
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

	recorder record.EventRecorder

	iptable *iptables.IPTables
	ipset   *ipsets.IPSets

	protocol string
}

// NewController init a daemon controller
func NewController(config *Configuration, informerFactory informers.SharedInformerFactory, kubeovnInformerFactory kubeovninformer.SharedInformerFactory) (*Controller, error) {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: config.KubeClient.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: config.NodeName})

	subnetInformer := kubeovnInformerFactory.Kubeovn().V1().Subnets()
	podInformer := informerFactory.Core().V1().Pods()

	controller := &Controller{
		config:        config,
		subnetsLister: subnetInformer.Lister(),
		subnetsSynced: subnetInformer.Informer().HasSynced,
		subnetQueue:   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Subnet"),

		podsLister: podInformer.Lister(),
		podsSynced: podInformer.Informer().HasSynced,
		podQueue:   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Pod"),

		recorder: recorder,
	}

	node, err := config.KubeClient.CoreV1().Nodes().Get(config.NodeName, metav1.GetOptions{})
	if err != nil {
		klog.Fatalf("failed to get node %s info %v", config.NodeName, err)
		return nil, err
	}
	controller.protocol = util.CheckProtocol(node.Annotations[util.IpAddressAnnotation])

	if controller.protocol == kubeovnv1.ProtocolIPv4 {
		iptable, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
		if err != nil {
			return nil, err
		}
		controller.iptable = iptable
		controller.ipset = ipsets.NewIPSets(ipsets.NewIPVersionConfig(ipsets.IPFamilyV4, IPSetPrefix, nil, nil))
	} else {
		iptable, err := iptables.NewWithProtocol(iptables.ProtocolIPv6)
		if err != nil {
			return nil, err
		}
		controller.iptable = iptable
		controller.ipset = ipsets.NewIPSets(ipsets.NewIPVersionConfig(ipsets.IPFamilyV6, IPSetPrefix, nil, nil))
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

func (c *Controller) enqueueUpdateSubnet(old, new interface{}) {
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
	cidrs := make([]string, 0, len(subnets))
	for _, subnet := range subnets {
		if !subnet.Status.IsReady() {
			continue
		}
		if _, ipNet, err := net.ParseCIDR(subnet.Spec.CIDRBlock); err != nil {
			klog.Errorf("%s is not a valid cidr block", subnet.Spec.CIDRBlock)
		} else {
			cidrs = append(cidrs, ipNet.String())
		}
	}
	node, err := c.config.KubeClient.CoreV1().Nodes().Get(c.config.NodeName, metav1.GetOptions{})
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

	var existRoutes []netlink.Route
	if util.CheckProtocol(gateway) == kubeovnv1.ProtocolIPv4 {
		existRoutes, err = netlink.RouteList(nic, netlink.FAMILY_V4)
	} else {
		existRoutes, err = netlink.RouteList(nic, netlink.FAMILY_V6)
	}
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
		gw := net.ParseIP(gateway)
		if err = netlink.RouteReplace(&netlink.Route{Dst: cidr, LinkIndex: nic.Attrs().Index, Scope: netlink.SCOPE_UNIVERSE, Gw: gw}); err != nil {
			klog.Errorf("failed to add route %v", err)
		}
	}
	return err
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
		oldPod.Annotations[util.EgressRateAnnotation] != newPod.Annotations[util.EgressRateAnnotation] ||
		oldPod.Annotations[util.VlanIdAnnotation] != newPod.Annotations[util.VlanIdAnnotation] {
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

	return ovs.SetPodBandwidth(pod.Name, pod.Namespace, pod.Annotations[util.IngressRateAnnotation], pod.Annotations[util.EgressRateAnnotation])
}

// Run starts controller
func (c *Controller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.subnetQueue.ShutDown()
	defer c.podQueue.ShutDown()

	go wait.Until(ovs.CleanLostInterface, time.Minute, stopCh)
	go wait.Until(recompute, 10*time.Minute, stopCh)

	klog.Info("start watching namespace changes")
	if ok := cache.WaitForCacheSync(stopCh, c.subnetsSynced, c.podsSynced); !ok {
		klog.Fatalf("failed to wait for caches to sync")
		return
	}

	klog.Info("Started workers")
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
