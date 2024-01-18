package ovn_ic_controller

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

const controllerAgentName = "ovn-ic-controller"

type Controller struct {
	config *Configuration

	subnetsLister    kubeovnlister.SubnetLister
	subnetSynced     cache.InformerSynced
	nodesLister      listerv1.NodeLister
	nodesSynced      cache.InformerSynced
	configMapsLister listerv1.ConfigMapLister
	configMapsSynced cache.InformerSynced
	vpcsLister       kubeovnlister.VpcLister
	vpcSynced        cache.InformerSynced

	informerFactory        kubeinformers.SharedInformerFactory
	kubeovnInformerFactory kubeovninformer.SharedInformerFactory
	recorder               record.EventRecorder

	ovnLegacyClient *ovs.LegacyClient
	OVNNbClient     ovs.NbClient
	OVNSbClient     ovs.SbClient
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

	vpcInformer := kubeovnInformerFactory.Kubeovn().V1().Vpcs()
	nodeInformer := informerFactory.Core().V1().Nodes()
	subnetInformer := kubeovnInformerFactory.Kubeovn().V1().Subnets()
	configMapInformer := informerFactory.Core().V1().ConfigMaps()

	controller := &Controller{
		config: config,

		vpcsLister:       vpcInformer.Lister(),
		vpcSynced:        vpcInformer.Informer().HasSynced,
		subnetsLister:    subnetInformer.Lister(),
		subnetSynced:     subnetInformer.Informer().HasSynced,
		nodesLister:      nodeInformer.Lister(),
		nodesSynced:      nodeInformer.Informer().HasSynced,
		configMapsLister: configMapInformer.Lister(),
		configMapsSynced: configMapInformer.Informer().HasSynced,

		informerFactory:        informerFactory,
		kubeovnInformerFactory: kubeovnInformerFactory,
		recorder:               recorder,

		ovnLegacyClient: ovs.NewLegacyClient(config.OvnTimeout),
	}

	var err error
	if controller.OVNNbClient, err = ovs.NewOvnNbClient(config.OvnNbAddr, config.OvnTimeout); err != nil {
		util.LogFatalAndExit(err, "failed to create ovn nb client")
	}
	if controller.OVNSbClient, err = ovs.NewOvnSbClient(config.OvnSbAddr, config.OvnTimeout); err != nil {
		util.LogFatalAndExit(err, "failed to create ovn sb client")
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
