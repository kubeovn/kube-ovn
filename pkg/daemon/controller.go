package daemon

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"time"

	"github.com/scylladb/go-set/strset"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	k8sexec "k8s.io/utils/exec"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovninformer "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// Controller watch pod and namespace changes to update iptables, ipset and ovs qos
type Controller struct {
	config *Configuration

	providerNetworksLister          kubeovnlister.ProviderNetworkLister
	providerNetworksSynced          cache.InformerSynced
	addOrUpdateProviderNetworkQueue workqueue.TypedRateLimitingInterface[string]
	deleteProviderNetworkQueue      workqueue.TypedRateLimitingInterface[*kubeovnv1.ProviderNetwork]

	vlansLister kubeovnlister.VlanLister
	vlansSynced cache.InformerSynced

	subnetsLister kubeovnlister.SubnetLister
	subnetsSynced cache.InformerSynced
	subnetQueue   workqueue.TypedRateLimitingInterface[*subnetEvent]

	ovnEipsLister kubeovnlister.OvnEipLister
	ovnEipsSynced cache.InformerSynced

	podsLister listerv1.PodLister
	podsSynced cache.InformerSynced
	podQueue   workqueue.TypedRateLimitingInterface[string]

	nodesLister listerv1.NodeLister
	nodesSynced cache.InformerSynced

	recorder record.EventRecorder

	protocol string

	ControllerRuntime
	localPodName   string
	localNamespace string

	k8sExec k8sexec.Interface
}

func newTypedRateLimitingQueue[T comparable](name string, rateLimiter workqueue.TypedRateLimiter[T]) workqueue.TypedRateLimitingInterface[T] {
	if rateLimiter == nil {
		rateLimiter = workqueue.DefaultTypedControllerRateLimiter[T]()
	}
	return workqueue.NewTypedRateLimitingQueueWithConfig(rateLimiter, workqueue.TypedRateLimitingQueueConfig[T]{Name: name})
}

// NewController init a daemon controller
func NewController(config *Configuration, stopCh <-chan struct{}, podInformerFactory, nodeInformerFactory informers.SharedInformerFactory, kubeovnInformerFactory kubeovninformer.SharedInformerFactory) (*Controller, error) {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: config.KubeClient.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: config.NodeName})

	providerNetworkInformer := kubeovnInformerFactory.Kubeovn().V1().ProviderNetworks()
	vlanInformer := kubeovnInformerFactory.Kubeovn().V1().Vlans()
	subnetInformer := kubeovnInformerFactory.Kubeovn().V1().Subnets()
	ovnEipInformer := kubeovnInformerFactory.Kubeovn().V1().OvnEips()
	podInformer := podInformerFactory.Core().V1().Pods()
	nodeInformer := nodeInformerFactory.Core().V1().Nodes()

	controller := &Controller{
		config: config,

		providerNetworksLister:          providerNetworkInformer.Lister(),
		providerNetworksSynced:          providerNetworkInformer.Informer().HasSynced,
		addOrUpdateProviderNetworkQueue: newTypedRateLimitingQueue[string]("AddOrUpdateProviderNetwork", nil),
		deleteProviderNetworkQueue:      newTypedRateLimitingQueue[*kubeovnv1.ProviderNetwork]("DeleteProviderNetwork", nil),

		vlansLister: vlanInformer.Lister(),
		vlansSynced: vlanInformer.Informer().HasSynced,

		subnetsLister: subnetInformer.Lister(),
		subnetsSynced: subnetInformer.Informer().HasSynced,
		subnetQueue:   newTypedRateLimitingQueue[*subnetEvent]("Subnet", nil),

		ovnEipsLister: ovnEipInformer.Lister(),
		ovnEipsSynced: ovnEipInformer.Informer().HasSynced,

		podsLister: podInformer.Lister(),
		podsSynced: podInformer.Informer().HasSynced,
		podQueue:   newTypedRateLimitingQueue[string]("Pod", nil),

		nodesLister: nodeInformer.Lister(),
		nodesSynced: nodeInformer.Informer().HasSynced,

		recorder: recorder,
		k8sExec:  k8sexec.New(),
	}

	node, err := config.KubeClient.CoreV1().Nodes().Get(context.Background(), config.NodeName, metav1.GetOptions{})
	if err != nil {
		util.LogFatalAndExit(err, "failed to get node %s info", config.NodeName)
	}
	controller.protocol = util.CheckProtocol(node.Annotations[util.IPAddressAnnotation])

	if err = controller.initRuntime(); err != nil {
		return nil, err
	}

	podInformerFactory.Start(stopCh)
	nodeInformerFactory.Start(stopCh)
	kubeovnInformerFactory.Start(stopCh)

	if !cache.WaitForCacheSync(stopCh,
		controller.providerNetworksSynced, controller.vlansSynced, controller.subnetsSynced,
		controller.podsSynced, controller.nodesSynced) {
		util.LogFatalAndExit(nil, "failed to wait for caches to sync")
	}

	if _, err = providerNetworkInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddProviderNetwork,
		UpdateFunc: controller.enqueueUpdateProviderNetwork,
		DeleteFunc: controller.enqueueDeleteProviderNetwork,
	}); err != nil {
		return nil, err
	}
	if _, err = vlanInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: controller.enqueueUpdateVlan,
	}); err != nil {
		return nil, err
	}
	if _, err = subnetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddSubnet,
		UpdateFunc: controller.enqueueUpdateSubnet,
		DeleteFunc: controller.enqueueDeleteSubnet,
	}); err != nil {
		return nil, err
	}
	if _, err = podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: controller.enqueuePod,
	}); err != nil {
		return nil, err
	}

	return controller, nil
}

func (c *Controller) enqueueAddProviderNetwork(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}

	klog.V(3).Infof("enqueue add provider network %s", key)
	c.addOrUpdateProviderNetworkQueue.Add(key)
}

func (c *Controller) enqueueUpdateProviderNetwork(_, newObj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(newObj)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}

	klog.V(3).Infof("enqueue update provider network %s", key)
	c.addOrUpdateProviderNetworkQueue.Add(key)
}

func (c *Controller) enqueueDeleteProviderNetwork(obj interface{}) {
	pn := obj.(*kubeovnv1.ProviderNetwork)
	klog.V(3).Infof("enqueue delete provider network %s", pn.Name)
	c.deleteProviderNetworkQueue.Add(pn)
}

func (c *Controller) runAddOrUpdateProviderNetworkWorker() {
	for c.processNextAddOrUpdateProviderNetworkWorkItem() {
	}
}

func (c *Controller) runDeleteProviderNetworkWorker() {
	for c.processNextDeleteProviderNetworkWorkItem() {
	}
}

func (c *Controller) processNextAddOrUpdateProviderNetworkWorkItem() bool {
	key, shutdown := c.addOrUpdateProviderNetworkQueue.Get()
	if shutdown {
		return false
	}

	err := func(key string) error {
		defer c.addOrUpdateProviderNetworkQueue.Done(key)
		if err := c.handleAddOrUpdateProviderNetwork(key); err != nil {
			return fmt.Errorf("error syncing %q: %w, requeuing", key, err)
		}
		c.addOrUpdateProviderNetworkQueue.Forget(key)
		return nil
	}(key)
	if err != nil {
		utilruntime.HandleError(err)
		c.addOrUpdateProviderNetworkQueue.AddRateLimited(key)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteProviderNetworkWorkItem() bool {
	obj, shutdown := c.deleteProviderNetworkQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj *kubeovnv1.ProviderNetwork) error {
		defer c.deleteProviderNetworkQueue.Done(obj)
		if err := c.handleDeleteProviderNetwork(obj); err != nil {
			return fmt.Errorf("error syncing %q: %w, requeuing", obj.Name, err)
		}
		c.deleteProviderNetworkQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		c.deleteProviderNetworkQueue.AddRateLimited(obj)
		return true
	}
	return true
}

func (c *Controller) handleAddOrUpdateProviderNetwork(key string) error {
	klog.V(3).Infof("handle update provider network %s", key)
	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Error(err)
		return err
	}
	pn, err := c.providerNetworksLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	if slices.Contains(pn.Spec.ExcludeNodes, node.Name) {
		c.recordProviderNetworkErr(pn.Name, "")
		return c.cleanProviderNetwork(pn.DeepCopy(), node.DeepCopy())
	}
	return c.initProviderNetwork(pn.DeepCopy(), node.DeepCopy())
}

func (c *Controller) initProviderNetwork(pn *kubeovnv1.ProviderNetwork, node *v1.Node) error {
	nic := pn.Spec.DefaultInterface
	for _, item := range pn.Spec.CustomInterfaces {
		if slices.Contains(item.Nodes, node.Name) {
			nic = item.Interface
			break
		}
	}

	labels := map[string]any{
		fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name):     nil,
		fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, pn.Name): nil,
		fmt.Sprintf(util.ProviderNetworkMtuTemplate, pn.Name):       nil,
		fmt.Sprintf(util.ProviderNetworkExcludeTemplate, pn.Name):   nil,
	}

	vlans := strset.NewWithSize(len(pn.Status.Vlans) + 1)
	for _, vlanName := range pn.Status.Vlans {
		vlan, err := c.vlansLister.Get(vlanName)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				klog.Infof("vlan %s not found", vlanName)
				continue
			}
			klog.Errorf("failed to get vlan %q: %v", vlanName, err)
			return err
		}
		vlans.Add(strconv.Itoa(vlan.Spec.ID))
	}
	// always add trunk 0 so that the ovs bridge can communicate with the external network
	vlans.Add("0")

	var mtu int
	var err error
	klog.V(3).Infof("ovs init provider network %s", pn.Name)
	if mtu, err = c.ovsInitProviderNetwork(pn.Name, nic, vlans.List(), pn.Spec.ExchangeLinkName, c.config.MacLearningFallback); err != nil {
		delete(labels, fmt.Sprintf(util.ProviderNetworkExcludeTemplate, pn.Name))
		if err1 := util.UpdateNodeLabels(c.config.KubeClient.CoreV1().Nodes(), node.Name, labels); err1 != nil {
			klog.Errorf("failed to update annotations of node %s: %v", node.Name, err1)
		}
		c.recordProviderNetworkErr(pn.Name, err.Error())
		return err
	}

	labels[fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name)] = "true"
	labels[fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, pn.Name)] = nic
	labels[fmt.Sprintf(util.ProviderNetworkMtuTemplate, pn.Name)] = strconv.Itoa(mtu)
	if err = util.UpdateNodeLabels(c.config.KubeClient.CoreV1().Nodes(), node.Name, labels); err != nil {
		klog.Errorf("failed to update labels of node %s: %v", node.Name, err)
		return err
	}
	c.recordProviderNetworkErr(pn.Name, "")
	return nil
}

func (c *Controller) recordProviderNetworkErr(providerNetwork, errMsg string) {
	var currentPod *v1.Pod
	var err error
	if c.localPodName == "" {
		pods, err := c.config.KubeClient.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{
			LabelSelector: "app=kube-ovn-cni",
			FieldSelector: fmt.Sprintf("spec.nodeName=%s", c.config.NodeName),
		})
		if err != nil {
			klog.Errorf("failed to list pod: %v", err)
			return
		}
		for _, pod := range pods.Items {
			if pod.Spec.NodeName == c.config.NodeName && pod.Status.Phase == v1.PodRunning {
				c.localPodName = pod.Name
				c.localNamespace = pod.Namespace
				currentPod = &pod
				break
			}
		}
		if currentPod == nil {
			klog.Warning("failed to get self pod")
			return
		}
	} else {
		if currentPod, err = c.podsLister.Pods(c.localNamespace).Get(c.localPodName); err != nil {
			klog.Errorf("failed to get pod %s, %v", c.localPodName, err)
			return
		}
	}

	newPod := currentPod.DeepCopy()
	if newPod.Annotations == nil {
		newPod.Annotations = make(map[string]string)
	}
	if newPod.Annotations[fmt.Sprintf(util.ProviderNetworkErrMessageTemplate, providerNetwork)] != errMsg {
		if errMsg == "" {
			delete(newPod.Annotations, fmt.Sprintf(util.ProviderNetworkErrMessageTemplate, providerNetwork))
		} else {
			newPod.Annotations[fmt.Sprintf(util.ProviderNetworkErrMessageTemplate, providerNetwork)] = errMsg
		}
		patch, err := util.GenerateStrategicMergePatchPayload(currentPod, newPod)
		if err != nil {
			klog.Errorf("failed to gen patch payload pod %s: %v", c.localPodName, err)
			return
		}
		if _, err = c.config.KubeClient.CoreV1().Pods(c.localNamespace).Patch(context.Background(), c.localPodName,
			types.StrategicMergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
			klog.Errorf("failed to patch pod %s: %v", c.localPodName, err)
			return
		}
	}
}

func (c *Controller) cleanProviderNetwork(pn *kubeovnv1.ProviderNetwork, node *v1.Node) error {
	labels := map[string]any{
		fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name):     nil,
		fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, pn.Name): nil,
		fmt.Sprintf(util.ProviderNetworkMtuTemplate, pn.Name):       nil,
		fmt.Sprintf(util.ProviderNetworkExcludeTemplate, pn.Name):   "true",
	}
	if err := util.UpdateNodeLabels(c.config.KubeClient.CoreV1().Nodes(), node.Name, labels); err != nil {
		klog.Errorf("failed to update labels of node %s: %v", node.Name, err)
		return err
	}

	return c.ovsCleanProviderNetwork(pn.Name)
}

func (c *Controller) handleDeleteProviderNetwork(pn *kubeovnv1.ProviderNetwork) error {
	if err := c.ovsCleanProviderNetwork(pn.Name); err != nil {
		klog.Error(err)
		return err
	}

	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Error(err)
		return err
	}
	if len(node.Labels) == 0 {
		return nil
	}

	labels := map[string]any{
		fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name):     nil,
		fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, pn.Name): nil,
		fmt.Sprintf(util.ProviderNetworkMtuTemplate, pn.Name):       nil,
		fmt.Sprintf(util.ProviderNetworkExcludeTemplate, pn.Name):   nil,
	}
	if err = util.UpdateNodeLabels(c.config.KubeClient.CoreV1().Nodes(), node.Name, labels); err != nil {
		klog.Errorf("failed to update labels of node %s: %v", node.Name, err)
		return err
	}

	return nil
}

func (c *Controller) enqueueUpdateVlan(oldObj, newObj interface{}) {
	oldVlan := oldObj.(*kubeovnv1.Vlan)
	newVlan := newObj.(*kubeovnv1.Vlan)
	if oldVlan.Spec.ID != newVlan.Spec.ID {
		klog.V(3).Infof("enqueue update provider network %q", newVlan.Spec.Provider)
		c.addOrUpdateProviderNetworkQueue.Add(newVlan.Spec.Provider)
	}
}

type subnetEvent struct {
	oldObj, newObj interface{}
}

func (c *Controller) enqueueAddSubnet(obj interface{}) {
	c.subnetQueue.Add(&subnetEvent{newObj: obj})
}

func (c *Controller) enqueueUpdateSubnet(oldObj, newObj interface{}) {
	c.subnetQueue.Add(&subnetEvent{oldObj: oldObj, newObj: newObj})
}

func (c *Controller) enqueueDeleteSubnet(obj interface{}) {
	c.subnetQueue.Add(&subnetEvent{oldObj: obj})
}

func (c *Controller) runSubnetWorker() {
	for c.processNextSubnetWorkItem() {
	}
}

func (c *Controller) processNextSubnetWorkItem() bool {
	obj, shutdown := c.subnetQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj *subnetEvent) error {
		defer c.subnetQueue.Done(obj)
		if err := c.reconcileRouters(obj); err != nil {
			c.subnetQueue.AddRateLimited(obj)
			return fmt.Errorf("error syncing %v: %w, requeuing", obj, err)
		}
		c.subnetQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) enqueuePod(oldObj, newObj interface{}) {
	oldPod := oldObj.(*v1.Pod)
	newPod := newObj.(*v1.Pod)

	if oldPod.Annotations[util.IngressRateAnnotation] != newPod.Annotations[util.IngressRateAnnotation] ||
		oldPod.Annotations[util.EgressRateAnnotation] != newPod.Annotations[util.EgressRateAnnotation] ||
		oldPod.Annotations[util.NetemQosLatencyAnnotation] != newPod.Annotations[util.NetemQosLatencyAnnotation] ||
		oldPod.Annotations[util.NetemQosJitterAnnotation] != newPod.Annotations[util.NetemQosJitterAnnotation] ||
		oldPod.Annotations[util.NetemQosLimitAnnotation] != newPod.Annotations[util.NetemQosLimitAnnotation] ||
		oldPod.Annotations[util.NetemQosLossAnnotation] != newPod.Annotations[util.NetemQosLossAnnotation] ||
		oldPod.Annotations[util.MirrorControlAnnotation] != newPod.Annotations[util.MirrorControlAnnotation] {
		var key string
		var err error
		if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
			utilruntime.HandleError(err)
			return
		}
		c.podQueue.Add(key)
	}

	attachNets, err := util.ParsePodNetworkAnnotation(newPod.Annotations[util.AttachmentNetworkAnnotation], newPod.Namespace)
	if err != nil {
		return
	}
	for _, multiNet := range attachNets {
		provider := fmt.Sprintf("%s.%s.%s", multiNet.Name, multiNet.Namespace, util.OvnProvider)
		if newPod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, provider)] == "true" {
			if oldPod.Annotations[fmt.Sprintf(util.IngressRateAnnotationTemplate, provider)] != newPod.Annotations[fmt.Sprintf(util.IngressRateAnnotationTemplate, provider)] ||
				oldPod.Annotations[fmt.Sprintf(util.EgressRateAnnotationTemplate, provider)] != newPod.Annotations[fmt.Sprintf(util.EgressRateAnnotationTemplate, provider)] ||
				oldPod.Annotations[fmt.Sprintf(util.NetemQosLatencyAnnotationTemplate, provider)] != newPod.Annotations[fmt.Sprintf(util.NetemQosLatencyAnnotationTemplate, provider)] ||
				oldPod.Annotations[fmt.Sprintf(util.NetemQosJitterAnnotationTemplate, provider)] != newPod.Annotations[fmt.Sprintf(util.NetemQosJitterAnnotationTemplate, provider)] ||
				oldPod.Annotations[fmt.Sprintf(util.NetemQosLimitAnnotationTemplate, provider)] != newPod.Annotations[fmt.Sprintf(util.NetemQosLimitAnnotationTemplate, provider)] ||
				oldPod.Annotations[fmt.Sprintf(util.NetemQosLossAnnotationTemplate, provider)] != newPod.Annotations[fmt.Sprintf(util.NetemQosLossAnnotationTemplate, provider)] ||
				oldPod.Annotations[fmt.Sprintf(util.MirrorControlAnnotationTemplate, provider)] != newPod.Annotations[fmt.Sprintf(util.MirrorControlAnnotationTemplate, provider)] {
				var key string
				var err error
				if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
					utilruntime.HandleError(err)
					return
				}
				c.podQueue.Add(key)
			}
		}
	}
}

func (c *Controller) runPodWorker() {
	for c.processNextPodWorkItem() {
	}
}

func (c *Controller) processNextPodWorkItem() bool {
	key, shutdown := c.podQueue.Get()
	if shutdown {
		return false
	}

	err := func(key string) error {
		defer c.podQueue.Done(key)
		if err := c.handlePod(key); err != nil {
			c.podQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing %q: %w, requeuing", key, err)
		}
		c.podQueue.Forget(key)
		return nil
	}(key)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

var lastNoPodOvsPort map[string]bool

func (c *Controller) markAndCleanInternalPort() error {
	klog.V(4).Infof("start to gc ovs internal ports")
	residualPorts := ovs.GetResidualInternalPorts()
	if len(residualPorts) == 0 {
		return nil
	}

	noPodOvsPort := map[string]bool{}
	for _, portName := range residualPorts {
		if !lastNoPodOvsPort[portName] {
			noPodOvsPort[portName] = true
		} else {
			klog.Infof("gc ovs internal port %s", portName)
			// Remove ovs port
			output, err := ovs.Exec(ovs.IfExists, "--with-iface", "del-port", "br-int", portName)
			if err != nil {
				return fmt.Errorf("failed to delete ovs port %w, %q", err, output)
			}
		}
	}
	lastNoPodOvsPort = noPodOvsPort

	return nil
}

// Run starts controller
func (c *Controller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.addOrUpdateProviderNetworkQueue.ShutDown()
	defer c.deleteProviderNetworkQueue.ShutDown()
	defer c.subnetQueue.ShutDown()
	defer c.podQueue.ShutDown()

	go wait.Until(ovs.CleanLostInterface, time.Minute, stopCh)
	go wait.Until(recompute, 10*time.Minute, stopCh)
	go wait.Until(rotateLog, 1*time.Hour, stopCh)

	if err := c.setIPSet(); err != nil {
		util.LogFatalAndExit(err, "failed to set ipsets")
	}

	klog.Info("Started workers")
	go wait.Until(c.loopOvn0Check, 5*time.Second, stopCh)
	go wait.Until(c.loopOvnExt0Check, 5*time.Second, stopCh)
	go wait.Until(c.loopTunnelCheck, 5*time.Second, stopCh)
	go wait.Until(c.runAddOrUpdateProviderNetworkWorker, time.Second, stopCh)
	go wait.Until(c.runDeleteProviderNetworkWorker, time.Second, stopCh)
	go wait.Until(c.runSubnetWorker, time.Second, stopCh)
	go wait.Until(c.runPodWorker, time.Second, stopCh)
	go wait.Until(c.runGateway, 3*time.Second, stopCh)
	go wait.Until(c.loopEncapIPCheck, 3*time.Second, stopCh)
	go wait.Until(c.ovnMetricsUpdate, 3*time.Second, stopCh)
	go wait.Until(func() {
		if err := c.reconcileRouters(nil); err != nil {
			klog.Errorf("failed to reconcile ovn0 routes: %v", err)
		}
	}, 3*time.Second, stopCh)
	go wait.Until(func() {
		if err := c.markAndCleanInternalPort(); err != nil {
			klog.Errorf("gc ovs port error: %v", err)
		}
	}, 5*time.Minute, stopCh)

	if c.config.EnableTProxy {
		go c.StartTProxyForwarding()
		go wait.Until(c.runTProxyConfigWorker, 3*time.Second, stopCh)
		// Using the tproxy method, kubelet's TCP probe packets cannot reach the namespace of the pod of the custom VPC,
		// so tproxy itself probes the pod of the custom VPC, if probe failed remove the iptable rules from
		// kubelet to tproxy, if probe success recover the iptable rules
		go wait.Until(c.StartTProxyTCPPortProbe, 1*time.Second, stopCh)
	} else {
		c.cleanTProxyConfig()
	}

	if c.config.EnableOVNIPSec {
		go wait.Until(func() {
			if err := c.ManageIPSecKeys(); err != nil {
				klog.Errorf("manage ipsec keys error: %v", err)
			}
		}, 24*time.Hour, stopCh)
	} else {
		if err := c.StopAndClearIPSecResouce(); err != nil {
			klog.Errorf("stop and clear ipsec resource error: %v", err)
		}
	}

	<-stopCh
	klog.Info("Shutting down workers")
}

func recompute() {
	output, err := exec.Command("ovn-appctl", "-t", "ovn-controller", "inc-engine/recompute").CombinedOutput()
	if err != nil {
		klog.Errorf("failed to recompute ovn-controller %q", output)
	}
}
