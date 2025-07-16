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

	subnetsLister kubeovnlister.SubnetLister
	subnetSynced  cache.InformerSynced

	servicesLister listerv1.ServiceLister
	servicesSynced cache.InformerSynced

	eipLister kubeovnlister.IptablesEIPLister
	eipSynced cache.InformerSynced

	natgatewayLister kubeovnlister.VpcNatGatewayLister
	natgatewaySynced cache.InformerSynced

	informerFactory        kubeinformers.SharedInformerFactory
	kubeovnInformerFactory kubeovninformer.SharedInformerFactory
	recorder               record.EventRecorder

	routerSyncer *RouteSyncer
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
	kubeovnInformerFactory := kubeovninformer.NewSharedInformerFactoryWithOptions(config.KubeOvnClient, 0,
		kubeovninformer.WithTweakListOptions(func(listOption *metav1.ListOptions) {
			listOption.AllowWatchBookmarks = true
		}))

	podInformer := informerFactory.Core().V1().Pods()
	subnetInformer := kubeovnInformerFactory.Kubeovn().V1().Subnets()
	serviceInformer := informerFactory.Core().V1().Services()
	eipInformer := kubeovnInformerFactory.Kubeovn().V1().IptablesEIPs()
	natgatewayInformer := kubeovnInformerFactory.Kubeovn().V1().VpcNatGateways()

	routerSyncer := NewRouteSyncer(time.Second*60, config)

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
		kubeovnInformerFactory: kubeovnInformerFactory,
		recorder:               recorder,

		routerSyncer: routerSyncer,
	}

	return controller
}

func (c *Controller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	c.informerFactory.Start(stopCh)
	c.kubeovnInformerFactory.Start(stopCh)

	if !cache.WaitForCacheSync(stopCh, c.podsSynced, c.subnetSynced, c.servicesSynced, c.eipSynced) {
		util.LogFatalAndExit(nil, "failed to wait for caches to sync")
		return
	}

	klog.Info("Started workers")
	if c.config.EdgeRouterMode {
		// Start route syncer work
		// run every c.routerSyncer.injectedRoutesSyncPeriod
		c.routerSyncer.Run(stopCh)
	} else {
		go wait.Until(c.Reconcile, 5*time.Second, stopCh)
	}

	<-stopCh
	klog.Info("Shutting down workers")
}

func (c *Controller) Reconcile() {
	if c.config.NatGwMode {
		err := c.syncEIPRoutes()
		if err != nil {
			klog.Errorf("failed to reconcile EIPs: %s", err.Error())
		}
	} else {
		c.syncSubnetRoutes()
	}
}
