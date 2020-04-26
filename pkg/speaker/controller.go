package speaker

import (
	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"time"

	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
)

const controllerAgentName = "ovn-speaker"

type Controller struct {
	config *Configuration

	podsLister listerv1.PodLister
	podsSynced cache.InformerSynced

	addPodQueue    workqueue.RateLimitingInterface
	deletePodQueue workqueue.RateLimitingInterface

	informerFactory kubeinformers.SharedInformerFactory
	recorder        record.EventRecorder
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
	podInformer := informerFactory.Core().V1().Pods()
	controller := &Controller{
		config: config,

		podsLister:     podInformer.Lister(),
		podsSynced:     podInformer.Informer().HasSynced,
		addPodQueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddPod"),
		deletePodQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeletePod"),

		informerFactory: informerFactory,
		recorder:        recorder,
	}
	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddPod,
		DeleteFunc: controller.enqueueDeletePod,
		UpdateFunc: controller.enqueueUpdatePod,
	})

	return controller
}

func (c *Controller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.addPodQueue.ShutDown()
	defer c.deletePodQueue.ShutDown()
	c.informerFactory.Start(stopCh)

	if ok := cache.WaitForCacheSync(stopCh, c.podsSynced); !ok {
		klog.Fatalf("failed to wait for caches to sync")
		return
	}

	klog.Info("Started workers")
	go wait.Until(c.runAddPodWorker, time.Second, stopCh)
	go wait.Until(c.runDeletePodWorker, 3*time.Second, stopCh)

	<-stopCh
	klog.Info("Shutting down workers")
}
