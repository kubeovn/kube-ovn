package controller

import (
	"sync"
	"time"

	"github.com/neverlee/keymutex"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	v1 "k8s.io/client-go/listers/core/v1"
	netv1 "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovninformer "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	ovnipam "github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const controllerAgentName = "kube-ovn-controller"

// Controller is kube-ovn main controller that watch ns/pod/node/svc/ep and operate ovn
type Controller struct {
	config *Configuration
	vpcs   *sync.Map
	//subnetVpcMap *sync.Map
	podSubnetMap *sync.Map
	ipam         *ovnipam.IPAM

	ovnLegacyClient *ovs.LegacyClient
	ovnClient       *ovs.OvnClient
	ovnPgKeyMutex   *keymutex.KeyMutex

	podsLister             v1.PodLister
	podsSynced             cache.InformerSynced
	addPodQueue            workqueue.RateLimitingInterface
	deletePodQueue         workqueue.RateLimitingInterface
	updatePodQueue         workqueue.RateLimitingInterface
	updatePodSecurityQueue workqueue.RateLimitingInterface
	podKeyMutex            *keymutex.KeyMutex

	vpcsLister           kubeovnlister.VpcLister
	vpcSynced            cache.InformerSynced
	addOrUpdateVpcQueue  workqueue.RateLimitingInterface
	delVpcQueue          workqueue.RateLimitingInterface
	updateVpcStatusQueue workqueue.RateLimitingInterface

	vpcNatGatewayLister           kubeovnlister.VpcNatGatewayLister
	vpcNatGatewaySynced           cache.InformerSynced
	addOrUpdateVpcNatGatewayQueue workqueue.RateLimitingInterface
	delVpcNatGatewayQueue         workqueue.RateLimitingInterface
	initVpcNatGatewayQueue        workqueue.RateLimitingInterface
	updateVpcEipQueue             workqueue.RateLimitingInterface
	updateVpcFloatingIpQueue      workqueue.RateLimitingInterface
	updateVpcDnatQueue            workqueue.RateLimitingInterface
	updateVpcSnatQueue            workqueue.RateLimitingInterface
	updateVpcSubnetQueue          workqueue.RateLimitingInterface
	vpcNatGwKeyMutex              *keymutex.KeyMutex

	subnetsLister           kubeovnlister.SubnetLister
	subnetSynced            cache.InformerSynced
	addOrUpdateSubnetQueue  workqueue.RateLimitingInterface
	deleteSubnetQueue       workqueue.RateLimitingInterface
	deleteRouteQueue        workqueue.RateLimitingInterface
	updateSubnetStatusQueue workqueue.RateLimitingInterface

	ipsLister kubeovnlister.IPLister
	ipSynced  cache.InformerSynced

	vlansLister kubeovnlister.VlanLister
	vlanSynced  cache.InformerSynced

	providerNetworksLister     kubeovnlister.ProviderNetworkLister
	providerNetworkSynced      cache.InformerSynced
	updateProviderNetworkQueue workqueue.RateLimitingInterface

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

	servicesLister     v1.ServiceLister
	serviceSynced      cache.InformerSynced
	deleteServiceQueue workqueue.RateLimitingInterface
	updateServiceQueue workqueue.RateLimitingInterface

	endpointsLister     v1.EndpointsLister
	endpointsSynced     cache.InformerSynced
	updateEndpointQueue workqueue.RateLimitingInterface

	npsLister     netv1.NetworkPolicyLister
	npsSynced     cache.InformerSynced
	updateNpQueue workqueue.RateLimitingInterface
	deleteNpQueue workqueue.RateLimitingInterface

	sgsLister          kubeovnlister.SecurityGroupLister
	sgSynced           cache.InformerSynced
	addOrUpdateSgQueue workqueue.RateLimitingInterface
	delSgQueue         workqueue.RateLimitingInterface
	syncSgPortsQueue   workqueue.RateLimitingInterface
	sgKeyMutex         *keymutex.KeyMutex

	configMapsLister v1.ConfigMapLister
	configMapsSynced cache.InformerSynced

	recorder               record.EventRecorder
	informerFactory        kubeinformers.SharedInformerFactory
	cmInformerFactory      kubeinformers.SharedInformerFactory
	kubeovnInformerFactory kubeovninformer.SharedInformerFactory
	elector                *leaderelection.LeaderElector
}

// NewController returns a new ovn controller
func NewController(config *Configuration) *Controller {
	utilruntime.Must(kubeovnv1.AddToScheme(scheme.Scheme))
	klog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: config.KubeFactoryClient.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	informerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(config.KubeFactoryClient, 0,
		kubeinformers.WithTweakListOptions(func(listOption *metav1.ListOptions) {
			listOption.AllowWatchBookmarks = true
		}))
	cmInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(config.KubeFactoryClient, 0,
		kubeinformers.WithTweakListOptions(func(listOption *metav1.ListOptions) {
			listOption.AllowWatchBookmarks = true
		}), kubeinformers.WithNamespace(config.PodNamespace))
	kubeovnInformerFactory := kubeovninformer.NewSharedInformerFactoryWithOptions(config.KubeOvnFactoryClient, 0,
		kubeovninformer.WithTweakListOptions(func(listOption *metav1.ListOptions) {
			listOption.AllowWatchBookmarks = true
		}))

	vpcInformer := kubeovnInformerFactory.Kubeovn().V1().Vpcs()
	vpcNatGatewayInformer := kubeovnInformerFactory.Kubeovn().V1().VpcNatGateways()
	subnetInformer := kubeovnInformerFactory.Kubeovn().V1().Subnets()
	ipInformer := kubeovnInformerFactory.Kubeovn().V1().IPs()
	vlanInformer := kubeovnInformerFactory.Kubeovn().V1().Vlans()
	providerNetworkInformer := kubeovnInformerFactory.Kubeovn().V1().ProviderNetworks()
	sgInformer := kubeovnInformerFactory.Kubeovn().V1().SecurityGroups()
	podInformer := informerFactory.Core().V1().Pods()
	namespaceInformer := informerFactory.Core().V1().Namespaces()
	nodeInformer := informerFactory.Core().V1().Nodes()
	serviceInformer := informerFactory.Core().V1().Services()
	endpointInformer := informerFactory.Core().V1().Endpoints()
	configMapInformer := cmInformerFactory.Core().V1().ConfigMaps()

	controller := &Controller{
		config:          config,
		vpcs:            &sync.Map{},
		podSubnetMap:    &sync.Map{},
		ovnLegacyClient: ovs.NewLegacyClient(config.OvnNbAddr, config.OvnTimeout, config.OvnSbAddr, config.ClusterRouter, config.ClusterTcpLoadBalancer, config.ClusterUdpLoadBalancer, config.ClusterTcpSessionLoadBalancer, config.ClusterUdpSessionLoadBalancer, config.NodeSwitch, config.NodeSwitchCIDR),
		ovnPgKeyMutex:   keymutex.New(97),
		ipam:            ovnipam.NewIPAM(),

		vpcsLister:           vpcInformer.Lister(),
		vpcSynced:            vpcInformer.Informer().HasSynced,
		addOrUpdateVpcQueue:  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddOrUpdateVpc"),
		delVpcQueue:          workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteVpc"),
		updateVpcStatusQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateVpcStatus"),

		vpcNatGatewayLister:           vpcNatGatewayInformer.Lister(),
		vpcNatGatewaySynced:           vpcNatGatewayInformer.Informer().HasSynced,
		addOrUpdateVpcNatGatewayQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddOrUpdateVpcNatGw"),
		initVpcNatGatewayQueue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "InitVpcNatGw"),
		delVpcNatGatewayQueue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteVpcNatGw"),
		updateVpcEipQueue:             workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateVpcEip"),
		updateVpcFloatingIpQueue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateVpcFloatingIp"),
		updateVpcDnatQueue:            workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateVpcDnat"),
		updateVpcSnatQueue:            workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateVpcSnat"),
		updateVpcSubnetQueue:          workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateVpcSubnet"),
		vpcNatGwKeyMutex:              keymutex.New(97),

		subnetsLister:           subnetInformer.Lister(),
		subnetSynced:            subnetInformer.Informer().HasSynced,
		addOrUpdateSubnetQueue:  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddSubnet"),
		deleteSubnetQueue:       workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteSubnet"),
		deleteRouteQueue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteRoute"),
		updateSubnetStatusQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateSubnetStatus"),

		ipsLister: ipInformer.Lister(),
		ipSynced:  ipInformer.Informer().HasSynced,

		vlansLister:     vlanInformer.Lister(),
		vlanSynced:      vlanInformer.Informer().HasSynced,
		addVlanQueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddVlan"),
		delVlanQueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DelVlan"),
		updateVlanQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateVlan"),

		providerNetworksLister:     providerNetworkInformer.Lister(),
		providerNetworkSynced:      providerNetworkInformer.Informer().HasSynced,
		updateProviderNetworkQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateProviderNetwork"),

		podsLister:             podInformer.Lister(),
		podsSynced:             podInformer.Informer().HasSynced,
		addPodQueue:            workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddPod"),
		deletePodQueue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeletePod"),
		updatePodQueue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdatePod"),
		updatePodSecurityQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdatePodSecurity"),
		podKeyMutex:            keymutex.New(97),

		namespacesLister:  namespaceInformer.Lister(),
		namespacesSynced:  namespaceInformer.Informer().HasSynced,
		addNamespaceQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddNamespace"),

		nodesLister:     nodeInformer.Lister(),
		nodesSynced:     nodeInformer.Informer().HasSynced,
		addNodeQueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddNode"),
		updateNodeQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateNode"),
		deleteNodeQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteNode"),

		servicesLister:     serviceInformer.Lister(),
		serviceSynced:      serviceInformer.Informer().HasSynced,
		deleteServiceQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteService"),
		updateServiceQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateService"),

		endpointsLister:     endpointInformer.Lister(),
		endpointsSynced:     endpointInformer.Informer().HasSynced,
		updateEndpointQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateEndpoint"),

		configMapsLister: configMapInformer.Lister(),
		configMapsSynced: configMapInformer.Informer().HasSynced,

		recorder: recorder,

		sgsLister:          sgInformer.Lister(),
		sgSynced:           sgInformer.Informer().HasSynced,
		addOrUpdateSgQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateSg"),
		delSgQueue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteSg"),
		syncSgPortsQueue:   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "SyncSgPorts"),
		sgKeyMutex:         keymutex.New(97),

		informerFactory:        informerFactory,
		cmInformerFactory:      cmInformerFactory,
		kubeovnInformerFactory: kubeovnInformerFactory,
	}

	var err error
	if controller.ovnClient, err = ovs.NewOvnClient(config.OvnNbAddr, config.OvnTimeout); err != nil {
		klog.Fatal(err)
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
		AddFunc:    controller.enqueueAddService,
		DeleteFunc: controller.enqueueDeleteService,
		UpdateFunc: controller.enqueueUpdateService,
	})

	endpointInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddEndpoint,
		UpdateFunc: controller.enqueueUpdateEndpoint,
	})

	vpcInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddVpc,
		UpdateFunc: controller.enqueueUpdateVpc,
		DeleteFunc: controller.enqueueDelVpc,
	})

	vpcNatGatewayInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddVpcNatGw,
		UpdateFunc: controller.enqueueUpdateVpcNatGw,
		DeleteFunc: controller.enqueueDeleteVpcNatGw,
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

	providerNetworkInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: controller.enqueueUpdateProviderNetwork,
	})

	if config.EnableNP {
		npInformer := informerFactory.Networking().V1().NetworkPolicies()
		controller.npsLister = npInformer.Lister()
		controller.npsSynced = npInformer.Informer().HasSynced
		controller.updateNpQueue = workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateNp")
		controller.deleteNpQueue = workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteNp")
		npInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    controller.enqueueAddNp,
			UpdateFunc: controller.enqueueUpdateNp,
			DeleteFunc: controller.enqueueDeleteNp,
		})
	}
	sgInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddSg,
		DeleteFunc: controller.enqueueDeleteSg,
		UpdateFunc: controller.enqueueUpdateSg,
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
	c.cmInformerFactory.Start(stopCh)
	c.kubeovnInformerFactory.Start(stopCh)

	klog.Info("Waiting for informer caches to sync")
	cacheSyncs := []cache.InformerSynced{
		c.vpcNatGatewaySynced, c.vpcSynced, c.subnetSynced, c.ipSynced,
		c.vlanSynced, c.podsSynced, c.namespacesSynced, c.nodesSynced,
		c.serviceSynced, c.endpointsSynced, c.configMapsSynced,
	}
	if c.config.EnableNP {
		cacheSyncs = append(cacheSyncs, c.npsSynced)
	}
	if ok := cache.WaitForCacheSync(stopCh, cacheSyncs...); !ok {
		klog.Fatalf("failed to wait for caches to sync")
	}

	if err := c.ovnLegacyClient.SetUseCtInvMatch(); err != nil {
		klog.Fatalf("failed to set NB_Global option use_ct_inv_match to false: %v", err)
	}

	if err := c.InitDefaultVpc(); err != nil {
		klog.Fatalf("failed to init default vpc: %v", err)
	}

	if err := c.InitOVN(); err != nil {
		klog.Fatalf("failed to init ovn resource: %v", err)
	}

	if err := c.InitIPAM(); err != nil {
		klog.Fatalf("failed to init ipam: %v", err)
	}

	if err := c.initNodeRoutes(); err != nil {
		klog.Fatalf("failed to initialize node routes: %v", err)
	}

	if err := c.initDenyAllSecurityGroup(); err != nil {
		klog.Fatalf("failed to init 'deny_all' security group: %v", err)
	}

	// remove resources in ovndb that not exist any more in kubernetes resources
	if err := c.gc(); err != nil {
		klog.Fatalf("gc failed: %v", err)
	}

	c.registerSubnetMetrics()
	if err := c.initSyncCrdIPs(); err != nil {
		klog.Errorf("failed to sync crd ips: %v", err)
	}
	if err := c.initSyncCrdSubnets(); err != nil {
		klog.Errorf("failed to sync crd subnets: %v", err)
	}
	if err := c.initSyncCrdVlans(); err != nil {
		klog.Errorf("failed to sync crd vlans: %v", err)
	}
	// The static route for node gw can be deleted when gc static route, so add it after gc process
	dstIp := "0.0.0.0/0,::/0"
	if err := c.ovnLegacyClient.AddStaticRoute("", dstIp, c.config.NodeSwitchGateway, c.config.ClusterRouter, util.NormalRouteType, false); err != nil {
		klog.Errorf("failed to add static route for node gw: %v", err)
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
	c.updatePodSecurityQueue.ShutDown()

	c.addNamespaceQueue.ShutDown()

	c.addOrUpdateSubnetQueue.ShutDown()
	c.deleteSubnetQueue.ShutDown()
	c.deleteRouteQueue.ShutDown()
	c.updateSubnetStatusQueue.ShutDown()

	c.addNodeQueue.ShutDown()
	c.updateNodeQueue.ShutDown()
	c.deleteNodeQueue.ShutDown()

	c.deleteServiceQueue.ShutDown()
	c.updateServiceQueue.ShutDown()
	c.updateEndpointQueue.ShutDown()

	c.addVlanQueue.ShutDown()
	c.delVlanQueue.ShutDown()
	c.updateVlanQueue.ShutDown()

	c.updateProviderNetworkQueue.ShutDown()

	c.addOrUpdateVpcQueue.ShutDown()
	c.updateVpcStatusQueue.ShutDown()
	c.delVpcQueue.ShutDown()

	c.addOrUpdateVpcNatGatewayQueue.ShutDown()
	c.initVpcNatGatewayQueue.ShutDown()
	c.delVpcNatGatewayQueue.ShutDown()
	c.updateVpcEipQueue.ShutDown()
	c.updateVpcFloatingIpQueue.ShutDown()
	c.updateVpcDnatQueue.ShutDown()
	c.updateVpcSnatQueue.ShutDown()
	c.updateVpcSubnetQueue.ShutDown()

	if c.config.EnableNP {
		c.updateNpQueue.ShutDown()
		c.deleteNpQueue.ShutDown()
	}
	c.addOrUpdateSgQueue.ShutDown()
	c.delSgQueue.ShutDown()
	c.syncSgPortsQueue.ShutDown()
}

func (c *Controller) startWorkers(stopCh <-chan struct{}) {
	klog.Info("Starting workers")

	go wait.Until(c.runAddVpcWorker, time.Second, stopCh)

	go wait.Until(c.runAddOrUpdateVpcNatGwWorker, time.Second, stopCh)
	go wait.Until(c.runInitVpcNatGwWorker, time.Second, stopCh)
	go wait.Until(c.runDelVpcNatGwWorker, time.Second, stopCh)
	go wait.Until(c.runUpdateVpcEipWorker, time.Second, stopCh)
	go wait.Until(c.runUpdateVpcFloatingIpWorker, time.Second, stopCh)
	go wait.Until(c.runUpdateVpcDnatWorker, time.Second, stopCh)
	go wait.Until(c.runUpdateVpcSnatWorker, time.Second, stopCh)
	go wait.Until(c.runUpdateVpcSubnetWorker, time.Second, stopCh)

	// add default/join subnet and wait them ready
	go wait.Until(c.runAddSubnetWorker, time.Second, stopCh)
	go wait.Until(c.runAddVlanWorker, time.Second, stopCh)
	go wait.Until(c.runAddNamespaceWorker, time.Second, stopCh)
	for {
		klog.Infof("wait for %s and %s ready", c.config.DefaultLogicalSwitch, c.config.NodeSwitch)
		time.Sleep(3 * time.Second)
		lss, err := c.ovnLegacyClient.ListLogicalSwitch(c.config.EnableExternalVpc)
		if err != nil {
			klog.Fatalf("failed to list logical switch: %v", err)
		}

		if util.IsStringIn(c.config.DefaultLogicalSwitch, lss) && util.IsStringIn(c.config.NodeSwitch, lss) && c.addNamespaceQueue.Len() == 0 {
			break
		}
	}

	go wait.Until(c.runAddSgWorker, time.Second, stopCh)
	go wait.Until(c.runDelSgWorker, time.Second, stopCh)
	go wait.Until(c.runSyncSgPortsWorker, time.Second, stopCh)

	// run node worker before handle any pods
	for i := 0; i < c.config.WorkerNum; i++ {
		go wait.Until(c.runAddNodeWorker, time.Second, stopCh)
		go wait.Until(c.runUpdateNodeWorker, time.Second, stopCh)
		go wait.Until(c.runDeleteNodeWorker, time.Second, stopCh)
	}
	for {
		ready := true
		time.Sleep(3 * time.Second)
		nodes, err := c.nodesLister.List(labels.Everything())
		if err != nil {
			klog.Fatalf("failed to list nodes: %v", err)
		}
		for _, node := range nodes {
			if node.Annotations[util.AllocatedAnnotation] != "true" {
				klog.Infof("wait node %s annotation ready", node.Name)
				ready = false
				break
			}
		}
		if ready {
			break
		}
	}

	go wait.Until(c.runDelVpcWorker, time.Second, stopCh)
	go wait.Until(c.runUpdateVpcStatusWorker, time.Second, stopCh)
	go wait.Until(c.runUpdateProviderNetworkWorker, time.Second, stopCh)

	if c.config.EnableLb {
		// run in a single worker to avoid delete the last vip, which will lead ovn to delete the loadbalancer
		go wait.Until(c.runDeleteServiceWorker, time.Second, stopCh)
	}

	for i := 0; i < c.config.WorkerNum; i++ {
		go wait.Until(c.runAddPodWorker, time.Second, stopCh)
		go wait.Until(c.runDeletePodWorker, time.Second, stopCh)
		go wait.Until(c.runUpdatePodWorker, time.Second, stopCh)
		go wait.Until(c.runUpdatePodSecurityWorker, time.Second, stopCh)

		go wait.Until(c.runDeleteSubnetWorker, time.Second, stopCh)
		go wait.Until(c.runDeleteRouteWorker, time.Second, stopCh)
		go wait.Until(c.runUpdateSubnetStatusWorker, time.Second, stopCh)

		if c.config.EnableLb {
			go wait.Until(c.runUpdateServiceWorker, time.Second, stopCh)
			go wait.Until(c.runUpdateEndpointWorker, time.Second, stopCh)
		}

		if c.config.EnableNP {
			go wait.Until(c.runUpdateNpWorker, time.Second, stopCh)
			go wait.Until(c.runDeleteNpWorker, time.Second, stopCh)
		}

		go wait.Until(c.runDelVlanWorker, time.Second, stopCh)
		go wait.Until(c.runUpdateVlanWorker, time.Second, stopCh)
	}

	go wait.Until(func() {
		c.resyncInterConnection()
	}, time.Second, stopCh)

	go wait.Until(func() {
		c.resyncExternalGateway()
	}, time.Second, stopCh)

	go wait.Until(func() {
		c.resyncVpcNatGwConfig()
	}, time.Second, stopCh)

	go wait.Until(func() {
		if err := c.markAndCleanLSP(); err != nil {
			klog.Errorf("gc lsp error: %v", err)
		}
	}, 6*time.Minute, stopCh)

	go wait.Until(func() {
		if err := c.inspectPod(); err != nil {
			klog.Errorf("inspection error: %v", err)
		}
	}, 20*time.Second, stopCh)

	if c.config.EnableExternalVpc {
		go wait.Until(func() {
			c.syncExternalVpc()
		}, 5*time.Second, stopCh)
	}

	go wait.Until(c.resyncProviderNetworkStatus, 30*time.Second, stopCh)
	go wait.Until(c.resyncSubnetMetrics, 30*time.Second, stopCh)
	go wait.Until(c.CheckGatewayReady, 5*time.Second, stopCh)

	if c.config.EnableNP {
		go wait.Until(c.CheckNodePortGroup, time.Duration(c.config.NodePgProbeTime)*time.Minute, stopCh)
	}

	go wait.Until(c.syncVmLiveMigrationPort, 15*time.Second, stopCh)
}
