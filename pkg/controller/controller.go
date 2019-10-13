package controller

import (
	"fmt"
	"time"

	kubeovninformer "github.com/alauda/kube-ovn/pkg/client/informers/externalversions"
	kubeovnlister "github.com/alauda/kube-ovn/pkg/client/listers/kube-ovn/v1"
	"github.com/alauda/kube-ovn/pkg/ovs"
	corev1 "k8s.io/api/core/v1"
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

	podsLister v1.PodLister
	podsSynced cache.InformerSynced

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	addPodQueue       workqueue.RateLimitingInterface
	addIpPoolPodQueue workqueue.RateLimitingInterface
	deletePodQueue    workqueue.RateLimitingInterface
	updatePodQueue    workqueue.RateLimitingInterface

	subnetsLister           kubeovnlister.SubnetLister
	subnetSynced            cache.InformerSynced
	addSubnetQueue          workqueue.RateLimitingInterface
	deleteSubnetQueue       workqueue.RateLimitingInterface
	deleteRouteQueue        workqueue.RateLimitingInterface
	updateSubnetQueue       workqueue.RateLimitingInterface
	updateSubnetStatusQueue workqueue.RateLimitingInterface

	ipSynced cache.InformerSynced

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
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder

	informerFactory        informers.SharedInformerFactory
	kubeovnInformerFactory kubeovninformer.SharedInformerFactory

	elector *leaderelection.LeaderElector
}

// NewController returns a new ovn controller
func NewController(config *Configuration) *Controller {
	// Create event broadcaster
	// Add ovn-controller types to the default Kubernetes Scheme so Events can be
	// logged for ovn-controller types.
	utilruntime.Must(scheme.AddToScheme(scheme.Scheme))
	klog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: config.KubeClient.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	informerFactory := kubeinformers.NewSharedInformerFactory(config.KubeClient, time.Second*30)
	kubeovnInformerFactory := kubeovninformer.NewSharedInformerFactoryWithOptions(config.KubeOvnClient, time.Second*30)

	subnetInformer := kubeovnInformerFactory.Kubeovn().V1().Subnets()
	ipInformer := kubeovnInformerFactory.Kubeovn().V1().IPs()
	podInformer := informerFactory.Core().V1().Pods()
	namespaceInformer := informerFactory.Core().V1().Namespaces()
	nodeInformer := informerFactory.Core().V1().Nodes()
	serviceInformer := informerFactory.Core().V1().Services()
	endpointInformer := informerFactory.Core().V1().Endpoints()
	npInformer := informerFactory.Networking().V1().NetworkPolicies()

	controller := &Controller{
		config:    config,
		ovnClient: ovs.NewClient(config.OvnNbHost, config.OvnNbPort, "", 0, config.ClusterRouter, config.ClusterTcpLoadBalancer, config.ClusterUdpLoadBalancer, config.NodeSwitch, config.NodeSwitchCIDR),

		subnetsLister:           subnetInformer.Lister(),
		subnetSynced:            subnetInformer.Informer().HasSynced,
		addSubnetQueue:          workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddSubnet"),
		deleteSubnetQueue:       workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteSubnet"),
		deleteRouteQueue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteRoute"),
		updateSubnetQueue:       workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateSubnet"),
		updateSubnetStatusQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateSubnetStatus"),

		ipSynced: ipInformer.Informer().HasSynced,

		podsLister:        podInformer.Lister(),
		podsSynced:        podInformer.Informer().HasSynced,
		addPodQueue:       workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddPod"),
		addIpPoolPodQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddIpPoolPod"),
		deletePodQueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeletePod"),
		updatePodQueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdatePod"),

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
		DeleteFunc: controller.enqueueAddOrDelIP,
	})

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()

	defer c.addPodQueue.ShutDown()
	defer c.addIpPoolPodQueue.ShutDown()
	defer c.deletePodQueue.ShutDown()
	defer c.updatePodQueue.ShutDown()

	defer c.addNamespaceQueue.ShutDown()

	defer c.addSubnetQueue.ShutDown()
	defer c.updateSubnetQueue.ShutDown()
	defer c.deleteSubnetQueue.ShutDown()
	defer c.deleteRouteQueue.ShutDown()
	defer c.updateSubnetStatusQueue.ShutDown()

	defer c.addNodeQueue.ShutDown()
	defer c.updateNodeQueue.ShutDown()
	defer c.deleteNodeQueue.ShutDown()

	defer c.deleteTcpServiceQueue.ShutDown()
	defer c.deleteUdpServiceQueue.ShutDown()
	defer c.updateServiceQueue.ShutDown()
	defer c.updateEndpointQueue.ShutDown()

	defer c.updateNpQueue.ShutDown()
	defer c.deleteNpQueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	klog.Info("Starting OVN controller")

	// leader election
	elector := setupLeaderElection(&leaderElectionConfig{
		Client:       c.config.KubeClient,
		ElectionID:   "ovn-config",
		PodName:      c.config.PodName,
		PodNamespace: c.config.PodNamespace,
	})
	c.elector = elector
	for {
		klog.Info("waiting for becoming a leader")
		if c.isLeader() {
			break
		}
		time.Sleep(1 * time.Second)
	}

	if err := InitClusterRouter(c.config); err != nil {
		klog.Fatalf("init cluster router failed %v", err)
	}

	if err := InitLoadBalancer(c.config); err != nil {
		klog.Fatalf("init load balancer failed %v", err)
	}

	if err := InitNodeSwitch(c.config); err != nil {
		klog.Fatalf("init node switch failed %v", err)
	}

	if err := InitDefaultLogicalSwitch(c.config); err != nil {
		klog.Fatalf("init default switch failed %v", err)
	}

	c.informerFactory.Start(stopCh)
	c.kubeovnInformerFactory.Start(stopCh)

	// Wait for the caches to be synced before starting workers
	klog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.subnetSynced, c.ipSynced, c.podsSynced, c.namespacesSynced, c.nodesSynced, c.serviceSynced, c.endpointsSynced, c.npsSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	klog.Info("Starting workers")

	// Launch workers to process resources
	go wait.Until(c.runAddSubnetWorker, time.Second, stopCh)
	// wait default/join subnet ready
	time.Sleep(3 * time.Second)

	go wait.Until(c.runAddIpPoolPodWorker, time.Second, stopCh)
	go wait.Until(c.runAddNamespaceWorker, time.Second, stopCh)
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
		go wait.Until(c.runDeleteTcpServiceWorker, time.Second, stopCh)
		go wait.Until(c.runDeleteUdpServiceWorker, time.Second, stopCh)

		go wait.Until(c.runUpdateEndpointWorker, time.Second, stopCh)

		go wait.Until(c.runUpdateNpWorker, time.Second, stopCh)
		go wait.Until(c.runDeleteNpWorker, time.Second, stopCh)
	}

	klog.Info("Started workers")
	<-stopCh
	klog.Info("Shutting down workers")

	return nil
}

func (c *Controller) isLeader() bool {
	return c.elector.IsLeader()
}

func (c *Controller) hasLeader() bool {
	return c.elector.GetLeader() != ""
}
