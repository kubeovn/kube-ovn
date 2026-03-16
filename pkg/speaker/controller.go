package speaker

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
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
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const controllerAgentName = "ovn-speaker"

type Controller struct {
	config *Configuration

	podsLister listerv1.PodLister
	podsSynced cache.InformerSynced

	// gwPodsLister watches pods in VpcNatGwNamespace for NodeRouteEIPMode
	// to find vpc-nat-gw pods and check their node placement
	gwPodsLister listerv1.PodLister
	gwPodsSynced cache.InformerSynced

	subnetsLister kubeovnlister.SubnetLister
	subnetSynced  cache.InformerSynced

	servicesLister listerv1.ServiceLister
	servicesSynced cache.InformerSynced

	eipLister kubeovnlister.IptablesEIPLister
	eipSynced cache.InformerSynced

	natgatewayLister kubeovnlister.VpcNatGatewayLister
	natgatewaySynced cache.InformerSynced

	// eipQueue is used in node-route-eip-mode for processing EIP events
	eipQueue workqueue.TypedRateLimitingInterface[string]

	informerFactory        kubeinformers.SharedInformerFactory
	podInformerFactory     kubeinformers.SharedInformerFactory
	gwPodsInformerFactory  kubeinformers.SharedInformerFactory
	kubeovnInformerFactory kubeovninformer.SharedInformerFactory
	recorder               record.EventRecorder
}

func NewController(config *Configuration) *Controller {
	utilruntime.Must(kubeovnv1.AddToScheme(scheme.Scheme))
	klog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: config.KubeClient.CoreV1().Events(corev1.NamespaceAll)})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	informerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(config.KubeClient, 0,
		kubeinformers.WithTweakListOptions(func(listOption *metav1.ListOptions) {
			listOption.AllowWatchBookmarks = true
		}))
	podInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(config.KubeClient, 0,
		kubeinformers.WithTweakListOptions(func(listOption *metav1.ListOptions) {
			listOption.FieldSelector = "spec.hostNetwork=false"
			listOption.AllowWatchBookmarks = true
		}))

	// gwPodsInformerFactory watches pods in VpcNatGwNamespace for NodeRouteEIPMode
	// to locate vpc-nat-gw pods (no hostNetwork filter applied)
	var gwPodsInformerFactory kubeinformers.SharedInformerFactory
	if config.NodeRouteEIPMode {
		gwPodsInformerFactory = kubeinformers.NewSharedInformerFactoryWithOptions(config.KubeClient, 0,
			kubeinformers.WithNamespace(config.VpcNatGwNamespace),
			kubeinformers.WithTransform(util.TrimManagedFields),
			kubeinformers.WithTweakListOptions(func(listOption *metav1.ListOptions) {
				listOption.AllowWatchBookmarks = true
			}))
	}

	kubeovnInformerFactory := kubeovninformer.NewSharedInformerFactoryWithOptions(config.KubeOvnClient, 0,
		kubeovninformer.WithTweakListOptions(func(listOption *metav1.ListOptions) {
			listOption.AllowWatchBookmarks = true
		}))

	podInformer := podInformerFactory.Core().V1().Pods()
	subnetInformer := kubeovnInformerFactory.Kubeovn().V1().Subnets()
	serviceInformer := informerFactory.Core().V1().Services()
	eipInformer := kubeovnInformerFactory.Kubeovn().V1().IptablesEIPs()
	natgatewayInformer := kubeovnInformerFactory.Kubeovn().V1().VpcNatGateways()

	controller := &Controller{
		config: config,

		podsLister:       podInformer.Lister(),
		podsSynced:       podInformer.Informer().HasSynced,
		subnetsLister:    subnetInformer.Lister(),
		subnetSynced:     subnetInformer.Informer().HasSynced,
		servicesLister:   serviceInformer.Lister(),
		servicesSynced:   serviceInformer.Informer().HasSynced,
		eipLister:        eipInformer.Lister(),
		eipSynced:        eipInformer.Informer().HasSynced,
		natgatewayLister: natgatewayInformer.Lister(),
		natgatewaySynced: natgatewayInformer.Informer().HasSynced,

		informerFactory:        informerFactory,
		podInformerFactory:     podInformerFactory,
		gwPodsInformerFactory:  gwPodsInformerFactory,
		kubeovnInformerFactory: kubeovnInformerFactory,
		recorder:               recorder,
	}

	// Initialize gwPodsLister for NodeRouteEIPMode
	if config.NodeRouteEIPMode && gwPodsInformerFactory != nil {
		gwPodsInformer := gwPodsInformerFactory.Core().V1().Pods()
		controller.gwPodsLister = gwPodsInformer.Lister()
		controller.gwPodsSynced = gwPodsInformer.Informer().HasSynced
		controller.initNodeRouteEIPMode()
	}

	return controller
}

func (c *Controller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.shutdownNodeRouteEIPWorkers()

	c.informerFactory.Start(stopCh)
	c.podInformerFactory.Start(stopCh)
	c.kubeovnInformerFactory.Start(stopCh)

	// Start gwPodsInformerFactory for NodeRouteEIPMode
	if c.gwPodsInformerFactory != nil {
		c.gwPodsInformerFactory.Start(stopCh)
	}

	cacheSyncs := []cache.InformerSynced{c.podsSynced, c.subnetSynced, c.servicesSynced, c.eipSynced}
	if c.gwPodsSynced != nil {
		cacheSyncs = append(cacheSyncs, c.gwPodsSynced)
	}

	if !cache.WaitForCacheSync(stopCh, cacheSyncs...) {
		util.LogFatalAndExit(nil, "failed to wait for caches to sync")
		return
	}

	klog.Info("Started workers")

	// Start node-route-eip workers if in that mode
	if c.config.NodeRouteEIPMode {
		c.startNodeRouteEIPWorkers(stopCh, 1)
		// Enqueue all ready EIPs on startup for route recovery
		if err := c.enqueueAllReadyEIPs(); err != nil {
			klog.Errorf("failed to enqueue ready EIPs on startup: %v", err)
		}
	}

	go wait.Until(c.Reconcile, 5*time.Second, stopCh)

	<-stopCh
	klog.Info("Shutting down workers")
}

func (c *Controller) Reconcile() {
	switch {
	case c.config.NatGwMode:
		if err := c.syncEIPRoutes(); err != nil {
			klog.Errorf("failed to reconcile EIPs: %s", err.Error())
		}
	case c.config.NodeRouteEIPMode:
		// Node route EIP mode: use the periodic reconcile for consistency check
		c.ReconcileNodeRouteEIPs()
	default:
		c.syncSubnetRoutes()
	}
}
