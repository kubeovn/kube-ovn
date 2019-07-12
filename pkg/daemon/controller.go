package daemon

import (
	"fmt"
	"net"
	"time"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"

	"github.com/alauda/kube-ovn/pkg/ovs"
	"k8s.io/apimachinery/pkg/api/errors"

	kubeovninformer "github.com/alauda/kube-ovn/pkg/client/informers/externalversions"
	kubeovnlister "github.com/alauda/kube-ovn/pkg/client/listers/kube-ovn/v1"
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

	ipSetsMgr   *ipsets.IPSets
	iptablesMgr *iptables.IPTables
}

// NewController init a daemon controller
func NewController(config *Configuration, informerFactory informers.SharedInformerFactory, kubeovnInformerFactory kubeovninformer.SharedInformerFactory) (*Controller, error) {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: config.KubeClient.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: config.NodeName})

	subnetInformer := kubeovnInformerFactory.Kubeovn().V1().Subnets()
	podInformer := informerFactory.Core().V1().Pods()
	iptablesMgr, err := iptables.New()
	if err != nil {
		return nil, err
	}
	ipsetConf := ipsets.NewIPVersionConfig(ipsets.IPFamilyV4, IPSetPrefix, nil, nil)
	ipsetsMgr := ipsets.NewIPSets(ipsetConf)
	controller := &Controller{
		config:        config,
		subnetsLister: subnetInformer.Lister(),
		subnetsSynced: subnetInformer.Informer().HasSynced,
		subnetQueue:   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Subnet"),

		podsLister: podInformer.Lister(),
		podsSynced: podInformer.Informer().HasSynced,
		podQueue:   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Pod"),

		recorder: recorder,

		ipSetsMgr:   ipsetsMgr,
		iptablesMgr: iptablesMgr,
	}

	subnetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueSubnet,
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
	c.subnetQueue.AddRateLimited(key)
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

func (c *Controller) handleNamespace(key string) error {
	return c.reconcileRouters()
}

func (c *Controller) reconcileRouters() error {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list namespace %v", err)
		return err
	}
	cidrs := []string{}
	for _, subnet := range subnets {
		cidrs = append(cidrs, subnet.Spec.CIDRBlock)
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
	routers, err := netlink.RouteList(nic, netlink.FAMILY_V4)
	if err != nil {
		return err
	}

	toDel := []string{}
	for _, route := range routers {

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

	toAdd := []string{}
	for _, c := range cidrs {
		found := false
		for _, r := range routers {
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

	for _, r := range toDel {
		_, cidr, _ := net.ParseCIDR(r)
		err := netlink.RouteDel(&netlink.Route{Dst: cidr})
		if err != nil {
			klog.Errorf("failed to del route %v", err)
		}
		return err
	}

	for _, r := range toAdd {
		_, cidr, _ := net.ParseCIDR(r)
		gw := net.ParseIP(gateway)
		err := netlink.RouteAdd(&netlink.Route{Dst: cidr, LinkIndex: nic.Attrs().Index, Scope: netlink.SCOPE_UNIVERSE, Gw: gw})
		if err != nil {
			klog.Errorf("failed to add route %v", err)
			return err
		}
	}
	return nil
}

func (c *Controller) enqueuePod(old, new interface{}) {
	oldPod := old.(*v1.Pod)
	newPod := new.(*v1.Pod)
	if newPod.Spec.NodeName != c.config.NodeName {
		return
	}
	if oldPod.Annotations[util.IngressRateAnnotation] != newPod.Annotations[util.IngressRateAnnotation] ||
		oldPod.Annotations[util.EgressRateAnnotation] != newPod.Annotations[util.EgressRateAnnotation] {
		var key string
		var err error
		if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
			utilruntime.HandleError(err)
			return
		}
		c.podQueue.AddRateLimited(key)
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
func (c *Controller) Run(stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()

	defer c.subnetQueue.ShutDown()
	defer c.podQueue.ShutDown()

	klog.Info("start watching namespace changes")
	if ok := cache.WaitForCacheSync(stopCh, c.subnetsSynced, c.podsSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	go wait.Until(c.runSubnetWorker, time.Second, stopCh)
	go wait.Until(c.runPodWorker, time.Second, stopCh)
	go c.runGateway(stopCh)

	klog.Info("Started workers")
	<-stopCh
	klog.Info("Shutting down workers")

	return nil
}
