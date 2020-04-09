package controller

import (
	ovnipam "github.com/alauda/kube-ovn/pkg/ipam"
	"time"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovninformer "github.com/alauda/kube-ovn/pkg/client/informers/externalversions"
	kubeovnlister "github.com/alauda/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/ovs"
	"github.com/alauda/kube-ovn/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	v1 "k8s.io/client-go/listers/core/v1"
	netv1 "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
)

const controllerAgentName = "ovn-controller"

// Controller is kube-ovn main controller that watch ns/pod/node/svc/ep and operate ovn
type Controller struct {
	config    *Configuration
	ovnClient *ovs.Client
	ipam      *ovnipam.IPAM

	podsLister v1.PodLister
	podsSynced cache.InformerSynced

	addPodQueue    workqueue.RateLimitingInterface
	deletePodQueue workqueue.RateLimitingInterface
	updatePodQueue workqueue.RateLimitingInterface

	subnetsLister           kubeovnlister.SubnetLister
	subnetSynced            cache.InformerSynced
	addSubnetQueue          workqueue.RateLimitingInterface
	deleteSubnetQueue       workqueue.RateLimitingInterface
	deleteRouteQueue        workqueue.RateLimitingInterface
	updateSubnetQueue       workqueue.RateLimitingInterface
	updateSubnetStatusQueue workqueue.RateLimitingInterface

	ipsLister kubeovnlister.IPLister
	ipSynced  cache.InformerSynced

	vlansLister kubeovnlister.VlanLister
	vlanSynced  cache.InformerSynced

	addVlanQueue    workqueue.RateLimitingInterface
	delVlanQueue    workqueue.RateLimitingInterface
	updateVlanQueue workqueue.RateLimitingInterface

	namespacesLister  v1.NamespaceLister
	namespacesSynced  cache.InformerSynced
	addNamespaceQueue workqueue.RateLimitingInterface

	nodesLister     v1.NodeLister
	nodesSynced     cache.InformerSynced
	addNodeQueue    workqueue.RateLimitingInterface
	updateNodeQueue workqueue.RateLimitingInterface
	deleteNodeQueue workqueue.RateLimitingInterface

	servicesLister        v1.ServiceLister
	serviceSynced         cache.InformerSynced
	deleteTcpServiceQueue workqueue.RateLimitingInterface
	deleteUdpServiceQueue workqueue.RateLimitingInterface
	updateServiceQueue    workqueue.RateLimitingInterface

	endpointsLister     v1.EndpointsLister
	endpointsSynced     cache.InformerSynced
	updateEndpointQueue workqueue.RateLimitingInterface

	npsLister     netv1.NetworkPolicyLister
	npsSynced     cache.InformerSynced
	updateNpQueue workqueue.RateLimitingInterface
	deleteNpQueue workqueue.RateLimitingInterface

	recorder               record.EventRecorder
	informerFactory        informers.SharedInformerFactory
	kubeovnInformerFactory kubeovninformer.SharedInformerFactory
	elector                *leaderelection.LeaderElector
}

// NewController returns a new ovn controller
func NewController(config *Configuration) *Controller {
	utilruntime.Must(kubeovnv1.AddToScheme(scheme.Scheme))
	klog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: config.KubeClient.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	informerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(config.KubeClient, 0,
		kubeinformers.WithTweakListOptions(func(listOption *metav1.ListOptions) {
			listOption.AllowWatchBookmarks = true
		}))
	kubeovnInformerFactory := kubeovninformer.NewSharedInformerFactoryWithOptions(config.KubeOvnClient, 0,
		kubeovninformer.WithTweakListOptions(func(listOption *metav1.ListOptions) {
			listOption.AllowWatchBookmarks = true
		}))

	subnetInformer := kubeovnInformerFactory.Kubeovn().V1().Subnets()
	ipInformer := kubeovnInformerFactory.Kubeovn().V1().IPs()
	vlanInformer := kubeovnInformerFactory.Kubeovn().V1().Vlans()
	podInformer := informerFactory.Core().V1().Pods()
	namespaceInformer := informerFactory.Core().V1().Namespaces()
	nodeInformer := informerFactory.Core().V1().Nodes()
	serviceInformer := informerFactory.Core().V1().Services()
	endpointInformer := informerFactory.Core().V1().Endpoints()
	npInformer := informerFactory.Networking().V1().NetworkPolicies()

	controller := &Controller{
		config:    config,
		ovnClient: ovs.NewClient(config.OvnNbHost, config.OvnNbPort, config.OvnTimeout, config.OvnSbHost, config.OvnSbPort, config.ClusterRouter, config.ClusterTcpLoadBalancer, config.ClusterUdpLoadBalancer, config.NodeSwitch, config.NodeSwitchCIDR),
		ipam:      ovnipam.NewIPAM(),

		subnetsLister:           subnetInformer.Lister(),
		subnetSynced:            subnetInformer.Informer().HasSynced,
		addSubnetQueue:          workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddSubnet"),
		deleteSubnetQueue:       workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteSubnet"),
		deleteRouteQueue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteRoute"),
		updateSubnetQueue:       workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateSubnet"),
		updateSubnetStatusQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateSubnetStatus"),

		ipsLister: ipInformer.Lister(),
		ipSynced:  ipInformer.Informer().HasSynced,

		vlansLister:     vlanInformer.Lister(),
		vlanSynced:      vlanInformer.Informer().HasSynced,
		addVlanQueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddVlan"),
		delVlanQueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DelVlan"),
		updateVlanQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateVlan"),

		podsLister:     podInformer.Lister(),
		podsSynced:     podInformer.Informer().HasSynced,
		addPodQueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddPod"),
		deletePodQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeletePod"),
		updatePodQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdatePod"),

		namespacesLister:  namespaceInformer.Lister(),
		namespacesSynced:  namespaceInformer.Informer().HasSynced,
		addNamespaceQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddNamespace"),

		nodesLister:     nodeInformer.Lister(),
		nodesSynced:     nodeInformer.Informer().HasSynced,
		addNodeQueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddNode"),
		updateNodeQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateNode"),
		deleteNodeQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteNode"),

		servicesLister:        serviceInformer.Lister(),
		serviceSynced:         serviceInformer.Informer().HasSynced,
		deleteTcpServiceQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteTcpService"),
		deleteUdpServiceQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteUdpService"),
		updateServiceQueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateService"),

		endpointsLister:     endpointInformer.Lister(),
		endpointsSynced:     endpointInformer.Informer().HasSynced,
		updateEndpointQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateEndpoint"),

		npsLister:     npInformer.Lister(),
		npsSynced:     npInformer.Informer().HasSynced,
		updateNpQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateNp"),
		deleteNpQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteNp"),

		recorder: recorder,

		informerFactory:        informerFactory,
		kubeovnInformerFactory: kubeovnInformerFactory,
	}

	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddPod,
		DeleteFunc: controller.enqueueDeletePod,
		UpdateFunc: controller.enqueueUpdatePod,
	})

	namespaceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddNamespace,
		UpdateFunc: controller.enqueueUpdateNamespace,
		DeleteFunc: controller.enqueueDeleteNamespace,
	})

	nodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddNode,
		UpdateFunc: controller.enqueueUpdateNode,
		DeleteFunc: controller.enqueueDeleteNode,
	})

	serviceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: controller.enqueueDeleteService,
		UpdateFunc: controller.enqueueUpdateService,
	})

	endpointInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddEndpoint,
		UpdateFunc: controller.enqueueUpdateEndpoint,
	})

	npInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddNp,
		UpdateFunc: controller.enqueueUpdateNp,
		DeleteFunc: controller.enqueueDeleteNp,
	})

	subnetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddSubnet,
		UpdateFunc: controller.enqueueUpdateSubnet,
		DeleteFunc: controller.enqueueDeleteSubnet,
	})

	ipInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddOrDelIP,
		UpdateFunc: controller.enqueueUpdateIP,
		DeleteFunc: controller.enqueueAddOrDelIP,
	})

	vlanInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddVlan,
		DeleteFunc: controller.enqueueDelVlan,
		UpdateFunc: controller.enqueueUpdateVlan,
	})

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(stopCh <-chan struct{}) {
	defer c.shutdown()
	klog.Info("Starting OVN controller")

	// wait for becoming a leader
	c.leaderElection()

	// Wait for the caches to be synced before starting workers
	c.informerFactory.Start(stopCh)
	c.kubeovnInformerFactory.Start(stopCh)

	klog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.subnetSynced, c.ipSynced, c.vlanSynced, c.podsSynced, c.namespacesSynced, c.nodesSynced, c.serviceSynced, c.endpointsSynced, c.npsSynced); !ok {
		klog.Fatalf("failed to wait for caches to sync")
	}

	if err := c.InitOVN(); err != nil {
		klog.Fatalf("failed to init ovn resource %v", err)
	}

	if err := c.InitIPAM(); err != nil {
		klog.Fatalf("failed to init ipam %v", err)
	}

	// remove resources in ovndb that not exist any more in kubernetes resources
	if err := c.gc(); err != nil {
		klog.Fatalf("gc failed %v", err)
	}

	// start workers to do all the network operations
	c.startWorkers(stopCh)
	<-stopCh
	klog.Info("Shutting down workers")
}

func (c *Controller) shutdown() {
	utilruntime.HandleCrash()

	c.addPodQueue.ShutDown()
	c.deletePodQueue.ShutDown()
	c.updatePodQueue.ShutDown()

	c.addNamespaceQueue.ShutDown()

	c.addSubnetQueue.ShutDown()
	c.updateSubnetQueue.ShutDown()
	c.deleteSubnetQueue.ShutDown()
	c.deleteRouteQueue.ShutDown()
	c.updateSubnetStatusQueue.ShutDown()

	c.addNodeQueue.ShutDown()
	c.updateNodeQueue.ShutDown()
	c.deleteNodeQueue.ShutDown()

	c.deleteTcpServiceQueue.ShutDown()
	c.deleteUdpServiceQueue.ShutDown()
	c.updateServiceQueue.ShutDown()
	c.updateEndpointQueue.ShutDown()

	c.updateNpQueue.ShutDown()
	c.deleteNpQueue.ShutDown()

	c.addVlanQueue.ShutDown()
	c.delVlanQueue.ShutDown()
	c.updateVlanQueue.ShutDown()
}

func (c *Controller) startWorkers(stopCh <-chan struct{}) {
	klog.Info("Starting workers")

	// add default/join subnet and wait them ready
	go wait.Until(c.runAddSubnetWorker, time.Second, stopCh)
	for {
		klog.Infof("wait for %s and %s ready", c.config.DefaultLogicalSwitch, c.config.NodeSwitch)
		time.Sleep(3 * time.Second)
		lss, err := c.ovnClient.ListLogicalSwitch()
		if err != nil {
			klog.Fatal("failed to list logical switch")
		}

		if util.IsStringIn(c.config.DefaultLogicalSwitch, lss) && util.IsStringIn(c.config.NodeSwitch, lss) {
			break
		}
	}

	// run in a single worker to avoid subnet cidr conflict
	go wait.Until(c.runAddNamespaceWorker, time.Second, stopCh)

	// run in a single worker to avoid delete the last vip, which will lead ovn to delete the loadbalancer
	go wait.Until(c.runDeleteTcpServiceWorker, time.Second, stopCh)
	go wait.Until(c.runDeleteUdpServiceWorker, time.Second, stopCh)
	for i := 0; i < c.config.WorkerNum; i++ {
		go wait.Until(c.runAddPodWorker, time.Second, stopCh)
		go wait.Until(c.runDeletePodWorker, time.Second, stopCh)
		go wait.Until(c.runUpdatePodWorker, time.Second, stopCh)

		go wait.Until(c.runDeleteSubnetWorker, time.Second, stopCh)
		go wait.Until(c.runUpdateSubnetWorker, time.Second, stopCh)
		go wait.Until(c.runDeleteRouteWorker, time.Second, stopCh)
		go wait.Until(c.runUpdateSubnetStatusWorker, time.Second, stopCh)

		go wait.Until(c.runAddNodeWorker, time.Second, stopCh)
		go wait.Until(c.runUpdateNodeWorker, time.Second, stopCh)
		go wait.Until(c.runDeleteNodeWorker, time.Second, stopCh)

		go wait.Until(c.runUpdateServiceWorker, time.Second, stopCh)
		go wait.Until(c.runUpdateEndpointWorker, time.Second, stopCh)

		go wait.Until(c.runUpdateNpWorker, time.Second, stopCh)
		go wait.Until(c.runDeleteNpWorker, time.Second, stopCh)

		go wait.Until(c.runAddVlanWorker, time.Second, stopCh)
		go wait.Until(c.runDelVlanWorker, time.Second, stopCh)
		go wait.Until(c.runUpdateVlanWorker, time.Second, stopCh)
	}
}
