package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"time"

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
	addOrUpdateProviderNetworkQueue workqueue.RateLimitingInterface
	deleteProviderNetworkQueue      workqueue.RateLimitingInterface

	subnetsLister kubeovnlister.SubnetLister
	subnetsSynced cache.InformerSynced
	subnetQueue   workqueue.RateLimitingInterface

	ovnEipsLister kubeovnlister.OvnEipLister
	ovnEipsSynced cache.InformerSynced

	podsLister listerv1.PodLister
	podsSynced cache.InformerSynced
	podQueue   workqueue.RateLimitingInterface

	nodesLister listerv1.NodeLister
	nodesSynced cache.InformerSynced

	recorder record.EventRecorder

	protocol string

	ControllerRuntime
	localPodName   string
	localNamespace string

	k8sExec k8sexec.Interface
}

// NewController init a daemon controller
func NewController(config *Configuration, podInformerFactory informers.SharedInformerFactory, nodeInformerFactory informers.SharedInformerFactory, kubeovnInformerFactory kubeovninformer.SharedInformerFactory) (*Controller, error) {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: config.KubeClient.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: config.NodeName})

	providerNetworkInformer := kubeovnInformerFactory.Kubeovn().V1().ProviderNetworks()
	subnetInformer := kubeovnInformerFactory.Kubeovn().V1().Subnets()
	ovnEipInformer := kubeovnInformerFactory.Kubeovn().V1().OvnEips()
	podInformer := podInformerFactory.Core().V1().Pods()
	nodeInformer := nodeInformerFactory.Core().V1().Nodes()

	controller := &Controller{
		config: config,

		providerNetworksLister:          providerNetworkInformer.Lister(),
		providerNetworksSynced:          providerNetworkInformer.Informer().HasSynced,
		addOrUpdateProviderNetworkQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddOrUpdateProviderNetwork"),
		deleteProviderNetworkQueue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteProviderNetwork"),

		subnetsLister: subnetInformer.Lister(),
		subnetsSynced: subnetInformer.Informer().HasSynced,
		subnetQueue:   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Subnet"),

		ovnEipsLister: ovnEipInformer.Lister(),
		ovnEipsSynced: ovnEipInformer.Informer().HasSynced,

		podsLister: podInformer.Lister(),
		podsSynced: podInformer.Informer().HasSynced,
		podQueue:   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Pod"),

		nodesLister: nodeInformer.Lister(),
		nodesSynced: nodeInformer.Informer().HasSynced,

		recorder: recorder,
		k8sExec:  k8sexec.New(),
	}

	node, err := config.KubeClient.CoreV1().Nodes().Get(context.Background(), config.NodeName, metav1.GetOptions{})
	if err != nil {
		util.LogFatalAndExit(err, "failed to get node %s info", config.NodeName)
	}
	controller.protocol = util.CheckProtocol(node.Annotations[util.IpAddressAnnotation])

	if err = controller.initRuntime(); err != nil {
		return nil, err
	}

	if _, err = providerNetworkInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddProviderNetwork,
		UpdateFunc: controller.enqueueUpdateProviderNetwork,
		DeleteFunc: controller.enqueueDeleteProviderNetwork,
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

func (c *Controller) enqueueUpdateProviderNetwork(old, new interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(new)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}

	klog.V(3).Infof("enqueue update provider network %s", key)
	c.addOrUpdateProviderNetworkQueue.Add(key)
}

func (c *Controller) enqueueDeleteProviderNetwork(obj interface{}) {
	klog.V(3).Infof("enqueue delete provider network %s", obj.(*kubeovnv1.ProviderNetwork).Name)
	c.deleteProviderNetworkQueue.Add(obj)
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
	obj, shutdown := c.addOrUpdateProviderNetworkQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addOrUpdateProviderNetworkQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addOrUpdateProviderNetworkQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddOrUpdateProviderNetwork(key); err != nil {
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addOrUpdateProviderNetworkQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		c.addOrUpdateProviderNetworkQueue.AddRateLimited(obj)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteProviderNetworkWorkItem() bool {
	obj, shutdown := c.deleteProviderNetworkQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.deleteProviderNetworkQueue.Done(obj)
		var pn *kubeovnv1.ProviderNetwork
		var ok bool
		if pn, ok = obj.(*kubeovnv1.ProviderNetwork); !ok {
			c.deleteProviderNetworkQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected ProviderNetwork in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDeleteProviderNetwork(pn); err != nil {
			return fmt.Errorf("error syncing '%s': %v, requeuing", pn.Name, err)
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
		return err
	}
	pn, err := c.providerNetworksLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if util.ContainsString(pn.Spec.ExcludeNodes, node.Name) {
		c.recordProviderNetworkErr(pn.Name, "")
		return c.cleanProviderNetwork(pn.DeepCopy(), node.DeepCopy())
	}
	return c.initProviderNetwork(pn.DeepCopy(), node.DeepCopy())
}

func (c *Controller) initProviderNetwork(pn *kubeovnv1.ProviderNetwork, node *v1.Node) error {
	nic := pn.Spec.DefaultInterface
	for _, item := range pn.Spec.CustomInterfaces {
		if util.ContainsString(item.Nodes, node.Name) {
			nic = item.Interface
			break
		}
	}

	var mtu int
	var err error
	klog.V(3).Infof("ovs init provider network %s", pn.Name)
	if mtu, err = ovsInitProviderNetwork(pn.Name, nic, pn.Spec.ExchangeLinkName, c.config.MacLearningFallback); err != nil {
		if oldLen := len(node.Labels); oldLen != 0 {
			delete(node.Labels, fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name))
			delete(node.Labels, fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, pn.Name))
			delete(node.Labels, fmt.Sprintf(util.ProviderNetworkMtuTemplate, pn.Name))
			if len(node.Labels) != oldLen {
				raw, _ := json.Marshal(node.Labels)
				patchPayload := fmt.Sprintf(`[{ "op": "replace", "path": "/metadata/labels", "value": %s }]`, raw)
				_, err1 := c.config.KubeClient.CoreV1().Nodes().Patch(context.Background(), node.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
				if err1 != nil {
					klog.Errorf("failed to patch node %s: %v", node.Name, err1)
				}
			}
		}
		c.recordProviderNetworkErr(pn.Name, err.Error())
		return err
	}

	delete(node.Labels, fmt.Sprintf(util.ProviderNetworkExcludeTemplate, pn.Name))
	node.Labels[fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name)] = "true"
	node.Labels[fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, pn.Name)] = nic
	node.Labels[fmt.Sprintf(util.ProviderNetworkMtuTemplate, pn.Name)] = strconv.Itoa(mtu)

	patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
	op := "replace"
	if len(node.Labels) == 0 {
		op = "add"
	}

	raw, _ := json.Marshal(node.Labels)
	patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
	_, err = c.config.KubeClient.CoreV1().Nodes().Patch(context.Background(), node.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
	if err != nil {
		klog.Errorf("failed to patch node %s: %v", node.Name, err)
		return err
	}
	c.recordProviderNetworkErr(pn.Name, "")
	return nil
}

func (c *Controller) recordProviderNetworkErr(providerNetwork string, errMsg string) {
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
		if currentPod, err = c.config.KubeClient.CoreV1().Pods(c.localNamespace).Get(context.Background(), c.localPodName, metav1.GetOptions{}); err != nil {
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
	patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
	op := "replace"
	if len(node.Labels) == 0 {
		op = "add"
	}

	var err error
	delete(node.Labels, fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name))
	delete(node.Labels, fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, pn.Name))
	delete(node.Labels, fmt.Sprintf(util.ProviderNetworkMtuTemplate, pn.Name))
	node.Labels[fmt.Sprintf(util.ProviderNetworkExcludeTemplate, pn.Name)] = "true"
	raw, _ := json.Marshal(node.Labels)
	patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
	_, err = c.config.KubeClient.CoreV1().Nodes().Patch(context.Background(), node.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
	if err != nil {
		klog.Errorf("failed to patch node %s: %v", node.Name, err)
		return err
	}

	if err = ovsCleanProviderNetwork(pn.Name); err != nil {
		return err
	}

	return nil
}

func (c *Controller) handleDeleteProviderNetwork(pn *kubeovnv1.ProviderNetwork) error {
	if err := ovsCleanProviderNetwork(pn.Name); err != nil {
		return err
	}

	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		return err
	}
	if len(node.Labels) == 0 {
		return nil
	}

	newNode := node.DeepCopy()
	delete(newNode.Labels, fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name))
	delete(newNode.Labels, fmt.Sprintf(util.ProviderNetworkExcludeTemplate, pn.Name))
	delete(newNode.Labels, fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, pn.Name))
	delete(newNode.Labels, fmt.Sprintf(util.ProviderNetworkMtuTemplate, pn.Name))
	raw, _ := json.Marshal(newNode.Labels)
	patchPayloadTemplate := `[{ "op": "replace", "path": "/metadata/labels", "value": %s }]`
	patchPayload := fmt.Sprintf(patchPayloadTemplate, raw)
	_, err = c.config.KubeClient.CoreV1().Nodes().Patch(context.Background(), node.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
	if err != nil {
		klog.Errorf("failed to patch node %s: %v", node.Name, err)
		return err
	}

	return nil
}

type subnetEvent struct {
	old, new interface{}
}

func (c *Controller) enqueueAddSubnet(obj interface{}) {
	c.subnetQueue.Add(subnetEvent{new: obj})
}

func (c *Controller) enqueueUpdateSubnet(old, new interface{}) {
	c.subnetQueue.Add(subnetEvent{old: old, new: new})
}

func (c *Controller) enqueueDeleteSubnet(obj interface{}) {
	c.subnetQueue.Add(subnetEvent{old: obj})
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

	err := func(obj interface{}) error {
		defer c.subnetQueue.Done(obj)
		event, ok := obj.(subnetEvent)
		if !ok {
			c.subnetQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected subnetEvent in workqueue but got %#v", obj))
			return nil
		}
		if err := c.reconcileRouters(event); err != nil {
			c.subnetQueue.AddRateLimited(event)
			return fmt.Errorf("error syncing '%s': %s, requeuing", event, err.Error())
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

func (c *Controller) enqueuePod(old, new interface{}) {
	oldPod := old.(*v1.Pod)
	newPod := new.(*v1.Pod)

	if oldPod.Annotations[util.IngressRateAnnotation] != newPod.Annotations[util.IngressRateAnnotation] ||
		oldPod.Annotations[util.EgressRateAnnotation] != newPod.Annotations[util.EgressRateAnnotation] ||
		oldPod.Annotations[util.NetemQosLatencyAnnotation] != newPod.Annotations[util.NetemQosLatencyAnnotation] ||
		oldPod.Annotations[util.NetemQosJitterAnnotation] != newPod.Annotations[util.NetemQosJitterAnnotation] ||
		oldPod.Annotations[util.NetemQosLimitAnnotation] != newPod.Annotations[util.NetemQosLimitAnnotation] ||
		oldPod.Annotations[util.NetemQosLossAnnotation] != newPod.Annotations[util.NetemQosLossAnnotation] ||
		oldPod.Annotations[util.MirrorControlAnnotation] != newPod.Annotations[util.MirrorControlAnnotation] {
		var key string
		var err error
		if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
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
		provider := fmt.Sprintf("%s.%s.ovn", multiNet.Name, multiNet.Namespace)
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
				if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
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
	obj, shutdown := c.podQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.podQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.podQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handlePod(key); err != nil {
			c.podQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.podQueue.Forget(obj)
		return nil
	}(obj)

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
				return fmt.Errorf("failed to delete ovs port %v, %q", err, output)
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
	go wait.Until(c.operateMod, 10*time.Second, stopCh)

	if ok := cache.WaitForCacheSync(stopCh, c.providerNetworksSynced, c.subnetsSynced, c.podsSynced, c.nodesSynced); !ok {
		util.LogFatalAndExit(nil, "failed to wait for caches to sync")
	}

	if err := c.setIPSet(); err != nil {
		util.LogFatalAndExit(err, "failed to set ipsets")
	}

	klog.Info("Started workers")
	go wait.Until(c.loopOvn0Check, 5*time.Second, stopCh)
	go wait.Until(c.loopOvnExt0Check, 5*time.Second, stopCh)
	go wait.Until(c.runAddOrUpdateProviderNetworkWorker, time.Second, stopCh)
	go wait.Until(c.runDeleteProviderNetworkWorker, time.Second, stopCh)
	go wait.Until(c.runSubnetWorker, time.Second, stopCh)
	go wait.Until(c.runPodWorker, time.Second, stopCh)
	go wait.Until(c.runGateway, 3*time.Second, stopCh)
	go wait.Until(c.loopEncapIpCheck, 3*time.Second, stopCh)
	go wait.Until(c.ovnMetricsUpdate, 3*time.Second, stopCh)
	go wait.Until(func() {
		if err := c.markAndCleanInternalPort(); err != nil {
			klog.Errorf("gc ovs port error: %v", err)
		}
	}, 5*time.Minute, stopCh)

	<-stopCh
	klog.Info("Shutting down workers")
}

func recompute() {
	output, err := exec.Command("ovn-appctl", "-t", "ovn-controller", "inc-engine/recompute").CombinedOutput()
	if err != nil {
		klog.Errorf("failed to recompute ovn-controller %q", output)
	}
}
