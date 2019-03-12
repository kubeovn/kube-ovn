package daemon

import (
	"fmt"
	"net"
	"time"

	"bitbucket.org/mathildetech/kube-ovn/pkg/util"
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
)

type Controller struct {
	config        *Configuration
	kubeclientset kubernetes.Interface

	namespacesLister listerv1.NamespaceLister
	namespacesSynced cache.InformerSynced
	namespaceQueue   workqueue.RateLimitingInterface
}

func NewController(config *Configuration, informerFactory informers.SharedInformerFactory) *Controller {
	namespaceInformer := informerFactory.Core().V1().Namespaces()
	controller := &Controller{
		config:           config,
		kubeclientset:    config.KubeClient,
		namespacesLister: namespaceInformer.Lister(),
		namespacesSynced: namespaceInformer.Informer().HasSynced,
		namespaceQueue:   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Namespace"),
	}

	namespaceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueNamespace,
		DeleteFunc: controller.enqueueNamespace,
	})

	return controller
}

func (c *Controller) enqueueNamespace(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.namespaceQueue.AddRateLimited(key)
}

func (c *Controller) runNamespaceWorker() {
	for c.processNextNamespaceWorkItem() {
	}
}

func (c *Controller) processNextNamespaceWorkItem() bool {
	obj, shutdown := c.namespaceQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.namespaceQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.namespaceQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleNamespace(key); err != nil {
			c.namespaceQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.namespaceQueue.Forget(obj)
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
		if cidr, ok := ns.Annotations[util.CidrAnnotation]; ok {
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
	defer c.namespaceQueue.ShutDown()

	klog.Info("start watching namespace changes")
	if ok := cache.WaitForCacheSync(stopCh, c.namespacesSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	go wait.Until(c.runNamespaceWorker, time.Second, stopCh)

	klog.Info("Started workers")
	<-stopCh
	klog.Info("Shutting down workers")

	return nil
}
