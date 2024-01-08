package ovn_ic_client

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
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const controllerAgentName = "ovn-ic-client"

type Controller struct {
	config *Configuration

	subnetsLister    kubeovnlister.SubnetLister
	subnetSynced     cache.InformerSynced
	nodesLister      listerv1.NodeLister
	nodesSynced      cache.InformerSynced
	configMapsLister listerv1.ConfigMapLister
	configMapsSynced cache.InformerSynced

	informerFactory        kubeinformers.SharedInformerFactory
	kubeovnInformerFactory kubeovninformer.SharedInformerFactory
	recorder               record.EventRecorder

	ovnClient       *ovs.OvnClient
	ovnLegacyClient *ovs.LegacyClient
}

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

	nodeInformer := informerFactory.Core().V1().Nodes()
	subnetInformer := kubeovnInformerFactory.Kubeovn().V1().Subnets()
	configMapInformer := informerFactory.Core().V1().ConfigMaps()

	controller := &Controller{
		config:          config,
		ovnLegacyClient: ovs.NewLegacyClient(config.OvnNbAddr, config.OvnTimeout, config.OvnSbAddr, config.ClusterRouter, config.ClusterTcpLoadBalancer, config.ClusterUdpLoadBalancer, config.ClusterTcpSessionLoadBalancer, config.ClusterUdpSessionLoadBalancer, config.NodeSwitch, config.NodeSwitchCIDR),

		subnetsLister:    subnetInformer.Lister(),
		subnetSynced:     subnetInformer.Informer().HasSynced,
		nodesLister:      nodeInformer.Lister(),
		nodesSynced:      nodeInformer.Informer().HasSynced,
		configMapsLister: configMapInformer.Lister(),
		configMapsSynced: configMapInformer.Informer().HasSynced,

		informerFactory:        informerFactory,
		kubeovnInformerFactory: kubeovnInformerFactory,
		recorder:               recorder,
	}

	var err error
	if controller.ovnClient, err = ovs.NewOvnClient(config.OvnNbAddr, config.OvnTimeout); err != nil {
		util.LogFatalAndExit(err, "failed to create ovn client")
	}

	return controller
}

func (c *Controller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	c.informerFactory.Start(stopCh)
	c.kubeovnInformerFactory.Start(stopCh)

	if !cache.WaitForCacheSync(stopCh, c.subnetSynced, c.nodesSynced) {
		util.LogFatalAndExit(nil, "failed to wait for caches to sync")
		return
	}

	klog.Info("Started workers")
	go wait.Until(c.resyncInterConnection, time.Second, stopCh)
	go wait.Until(c.SynRouteToPolicy, 5*time.Second, stopCh)
	<-stopCh
	klog.Info("Shutting down workers")
}
