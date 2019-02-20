package daemon

import (
	"fmt"
	"github.com/vishvananda/netlink"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	"net"
	"time"
)

type Controller struct {
	config        *Configuration
	kubeclientset kubernetes.Interface

	namespacesLister     listerv1.NamespaceLister
	namespacesSynced     cache.InformerSynced
	addNamespaceQueue    workqueue.RateLimitingInterface
	deleteNamespaceQueue workqueue.RateLimitingInterface
}

func NewController(config *Configuration, informerFactory informers.SharedInformerFactory) *Controller {
	namespaceInformer := informerFactory.Core().V1().Namespaces()
	controller := &Controller{
		config:               config,
		kubeclientset:        config.KubeClient,
		namespacesLister:     namespaceInformer.Lister(),
		namespacesSynced:     namespaceInformer.Informer().HasSynced,
		addNamespaceQueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddNamespace"),
		deleteNamespaceQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteNamespace"),
	}

	namespaceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddNamespace,
		DeleteFunc: controller.enqueueDeleteNamespace,
	})

	return controller
}

func (c *Controller) enqueueAddNamespace(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.addNamespaceQueue.AddRateLimited(key)
}

func (c *Controller) enqueueDeleteNamespace(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.deleteNamespaceQueue.AddRateLimited(key)
}

func (c *Controller) runAddNamespaceWorker() {
	for c.processNextAddNamespaceWorkItem() {
	}
}

func (c *Controller) runDeleteNamespaceWorker() {
	for c.processNextDeleteNamespaceWorkItem() {
	}
}

func (c *Controller) processNextAddNamespaceWorkItem() bool {
	obj, shutdown := c.addNamespaceQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addNamespaceQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addNamespaceQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddNamespace(key); err != nil {
			c.addNamespaceQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addNamespaceQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteNamespaceWorkItem() bool {
	obj, shutdown := c.deleteNamespaceQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.deleteNamespaceQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.deleteNamespaceQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDeleteNamespace(key); err != nil {
			c.deleteNamespaceQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.deleteNamespaceQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) handleAddNamespace(key string) error {
	return c.reconcileRouters()
}

func (c *Controller) handleDeleteNamespace(key string) error {
	return c.reconcileRouters()
}

func (c *Controller) reconcileRouters() error {
	namespaces, err := c.namespacesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list namespace %v", err)
		return err
	}
	cidrs := []string{}
	for _, ns := range namespaces {
		if ns.Status.Phase == v1.NamespaceTerminating {
			continue
		}
		if cidr, ok := ns.Annotations["ovn.kubernetes.io/cidr"]; ok {
			found := false
			for _, c := range cidrs {
				if c == cidr {
					found = true
					break
				}
			}
			if !found {
				cidrs = append(cidrs, cidr)
			}
		}
	}
	node, err := c.kubeclientset.CoreV1().Nodes().Get(c.config.NodeName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("failed to get node %s %v", c.config.NodeName, err)
		return err
	}
	nicName, ok := node.Annotations["ovn.kubernetes.io/port_name"]
	if !ok {
		klog.Errorf("annotation for node %s ovn.kubernetes.io/port_name not exists", node.Name)
		return fmt.Errorf("annotation for node ovn.kubernetes.io/port_name not exists")
	}
	gateway, ok := node.Annotations["ovn.kubernetes.io/gateway"]
	if !ok {
		klog.Errorf("annotation for node %s ovn.kubernetes.io/gateway not exists", node.Name)
		return fmt.Errorf("annotation for node ovn.kubernetes.io/gateway not exists")
	}
	nic, err := netlink.LinkByName(nicName)
	if err != nil {
		klog.Errorf("failed to get nic %s", nicName)
		return fmt.Errorf("failed to get nic %s", nicName)
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
	klog.Infof("route to del %v", toDel)

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
	klog.Infof("route to add %v", toAdd)

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

func (c *Controller) Run(stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.addNamespaceQueue.ShutDown()
	defer c.deleteNamespaceQueue.ShutDown()

	klog.Info("start watching namespace changes")
	if ok := cache.WaitForCacheSync(stopCh, c.namespacesSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	go wait.Until(c.runAddNamespaceWorker, time.Second, stopCh)
	go wait.Until(c.runDeleteNamespaceWorker, time.Second, stopCh)

	klog.Info("Started workers")
	<-stopCh
	klog.Info("Shutting down workers")

	return nil
}
