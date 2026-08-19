package frr

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	kubeinformers "k8s.io/client-go/informers"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovninformer "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type Controller struct {
	config  *Configuration
	applier *Applier

	bgpConfLister kubeovnlister.BgpConfLister
	bgpConfSynced cache.InformerSynced
	vpcLister     kubeovnlister.VpcLister
	vpcSynced     cache.InformerSynced
	ovnEipLister  kubeovnlister.OvnEipLister
	ovnEipSynced  cache.InformerSynced
	nodeLister    listerv1.NodeLister
	nodeSynced    cache.InformerSynced
	podLister     listerv1.PodLister
	podSynced     cache.InformerSynced

	informerFactory        kubeinformers.SharedInformerFactory
	podInformerFactory     kubeinformers.SharedInformerFactory
	kubeovnInformerFactory kubeovninformer.SharedInformerFactory

	trigger chan struct{}
}

func NewController(config *Configuration) (*Controller, error) {
	informerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(config.KubeClient, config.ResyncInterval,
		kubeinformers.WithTransform(util.TrimManagedFields),
		kubeinformers.WithTweakListOptions(func(listOption *metav1.ListOptions) {
			listOption.FieldSelector = "metadata.name=" + config.NodeName
			listOption.AllowWatchBookmarks = true
		}))
	podInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(config.KubeClient, config.ResyncInterval,
		kubeinformers.WithTransform(util.TrimManagedFields),
		kubeinformers.WithTweakListOptions(func(listOption *metav1.ListOptions) {
			listOption.FieldSelector = "spec.nodeName=" + config.NodeName
			listOption.AllowWatchBookmarks = true
		}))
	kubeovnInformerFactory := kubeovninformer.NewSharedInformerFactoryWithOptions(config.KubeOvnClient, config.ResyncInterval,
		kubeovninformer.WithTransform(util.TrimManagedFields),
		kubeovninformer.WithTweakListOptions(func(listOption *metav1.ListOptions) {
			listOption.AllowWatchBookmarks = true
		}))

	bgpConfInformer := kubeovnInformerFactory.Kubeovn().V1().BgpConves()
	vpcInformer := kubeovnInformerFactory.Kubeovn().V1().Vpcs()
	ovnEipInformer := kubeovnInformerFactory.Kubeovn().V1().OvnEips()
	nodeInformer := informerFactory.Core().V1().Nodes()
	podInformer := podInformerFactory.Core().V1().Pods()

	c := &Controller{
		config:                 config,
		applier:                NewApplier(config.FrrDir),
		bgpConfLister:          bgpConfInformer.Lister(),
		bgpConfSynced:          bgpConfInformer.Informer().HasSynced,
		vpcLister:              vpcInformer.Lister(),
		vpcSynced:              vpcInformer.Informer().HasSynced,
		ovnEipLister:           ovnEipInformer.Lister(),
		ovnEipSynced:           ovnEipInformer.Informer().HasSynced,
		nodeLister:             nodeInformer.Lister(),
		nodeSynced:             nodeInformer.Informer().HasSynced,
		podLister:              podInformer.Lister(),
		podSynced:              podInformer.Informer().HasSynced,
		informerFactory:        informerFactory,
		podInformerFactory:     podInformerFactory,
		kubeovnInformerFactory: kubeovnInformerFactory,
		trigger:                make(chan struct{}, 1),
	}

	handler := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(any) { c.requestReconcile() },
		UpdateFunc: func(any, any) { c.requestReconcile() },
		DeleteFunc: func(any) { c.requestReconcile() },
	}
	for _, informer := range []cache.SharedIndexInformer{
		bgpConfInformer.Informer(),
		vpcInformer.Informer(),
		ovnEipInformer.Informer(),
		nodeInformer.Informer(),
	} {
		if _, err := informer.AddEventHandler(handler); err != nil {
			return nil, err
		}
	}

	return c, nil
}

func (c *Controller) requestReconcile() {
	select {
	case c.trigger <- struct{}{}:
	default:
	}
}

func (c *Controller) Run(ctx context.Context) error {
	c.informerFactory.Start(ctx.Done())
	c.podInformerFactory.Start(ctx.Done())
	c.kubeovnInformerFactory.Start(ctx.Done())

	if !cache.WaitForCacheSync(ctx.Done(), c.bgpConfSynced, c.vpcSynced, c.ovnEipSynced, c.nodeSynced, c.podSynced) {
		return errors.New("failed to wait for caches to sync")
	}
	klog.Info("caches synced, starting reconcile loop")

	reassert := time.NewTicker(c.config.ReassertInterval)
	defer reassert.Stop()
	vrfPoll := time.NewTicker(2 * time.Second)
	defer vrfPoll.Stop()

	lastSignature := c.advertisementSignature()
	c.requestReconcile()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-c.trigger:
			time.Sleep(c.config.DebounceInterval)
			c.drainTrigger()
			c.reconcile()
		case <-reassert.C:
			c.reconcile()
		case <-vrfPoll.C:
			if signature := c.advertisementSignature(); signature != lastSignature {
				lastSignature = signature
				c.requestReconcile()
			}
		}
	}
}

func (c *Controller) drainTrigger() {
	select {
	case <-c.trigger:
	default:
	}
}

func (c *Controller) reconcile() {
	config, err := c.desiredConfig()
	if err != nil {
		klog.Errorf("failed to compute desired FRR configuration: %v", err)
		return
	}

	s, err := c.applier.Apply(config)
	if err != nil {
		klog.Errorf("failed to apply FRR configuration: %v", err)
		return
	}

	st := c.applier.Status()
	switch {
	case st.AppliedSerial == s:
		klog.V(5).Infof("FRR configuration %s applied", s)
	case st.ResultSerial == s && st.ResultState == "error":
		klog.Errorf("FRR reload failed for configuration %s: %s", s, st.Detail)
	default:
		klog.V(3).Infof("FRR configuration %s pending, applied %s", s, st.AppliedSerial)
	}
}

func (c *Controller) desiredConfig() (string, error) {
	node, err := c.nodeLister.Get(c.config.NodeName)
	if err != nil {
		return "", fmt.Errorf("failed to get node %s: %w", c.config.NodeName, err)
	}

	conf, err := c.selectBgpConf(node)
	if err != nil {
		return "", err
	}
	if conf == nil {
		return Render(RenderInput{NodeName: c.config.NodeName}), nil
	}

	if err = c.checkSpeakerConflict(conf); err != nil {
		return "", err
	}

	routerID := conf.Spec.RouterID
	if routerID == "" {
		routerID, _ = util.GetNodeInternalIP(*node)
	}
	if routerID == "" {
		return "", fmt.Errorf("node %s has no IPv4 internal address, set spec.routerId on bgp-conf %s", c.config.NodeName, conf.Name)
	}

	vpcs, err := c.collectVpcAdvertisements()
	if err != nil {
		return "", err
	}

	imports := make([]string, 0, len(vpcs))
	for _, vpc := range vpcs {
		imports = append(imports, vpc.VrfName)
	}

	input := BuildRenderInput(conf, c.config.NodeName, routerID, vpcs, imports)
	if err = ValidateRenderInput(input); err != nil {
		return "", fmt.Errorf("invalid configuration from bgp-conf %s: %w", conf.Name, err)
	}
	return Render(input), nil
}

func (c *Controller) selectBgpConf(node *corev1.Node) (*kubeovnv1.BgpConf, error) {
	confs, err := c.bgpConfLister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("failed to list bgp-confs: %w", err)
	}

	var matched []*kubeovnv1.BgpConf
	for _, conf := range confs {
		if len(conf.Spec.NodeSelector) == 0 {
			continue
		}
		if labels.SelectorFromSet(conf.Spec.NodeSelector).Matches(labels.Set(node.Labels)) {
			matched = append(matched, conf)
		}
	}
	if len(matched) == 0 {
		return nil, nil
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].Name < matched[j].Name })
	if len(matched) > 1 {
		names := make([]string, 0, len(matched))
		for _, conf := range matched {
			names = append(names, conf.Name)
		}
		klog.Warningf("multiple bgp-confs match node %s: %s, using %s", c.config.NodeName, strings.Join(names, ", "), matched[0].Name)
	}
	return matched[0], nil
}

func (c *Controller) collectVpcAdvertisements() ([]VpcAdvertisement, error) {
	vpcs, err := c.vpcLister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("failed to list vpcs: %w", err)
	}
	eips, err := c.ovnEipLister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("failed to list ovn-eips: %w", err)
	}

	result := make([]VpcAdvertisement, 0, len(vpcs))
	for _, vpc := range vpcs {
		dr := vpc.Spec.DynamicRouting
		if !dr.IsEnabled() {
			continue
		}
		if dr.VrfID == 0 {
			klog.Warningf("vpc %s has dynamic routing enabled without an explicit vrfId, skipping advertisement", vpc.Name)
			continue
		}
		if !vrfPresent(vrfDeviceName(dr)) {
			klog.V(3).Infof("vpc %s vrf is not present on this chassis, skipping advertisement", vpc.Name)
			continue
		}
		lrpIP := lrpAddress(eips, vpc.Name)
		if lrpIP == "" {
			klog.Warningf("vpc %s has no ready lrp ovn-eip, skipping advertisement", vpc.Name)
			continue
		}
		result = append(result, VpcAdvertisement{
			VpcName: vpc.Name,
			VrfName: vrfDeviceName(dr),
			TableID: dr.VrfID,
			LrpIP:   lrpIP,
		})
	}
	return result, nil
}

func vrfPresent(vrfName string) bool {
	_, err := os.Stat("/sys/class/net/" + vrfName)
	return err == nil
}

func vrfDeviceName(dr *kubeovnv1.VpcDynamicRouting) string {
	if dr.VrfName != "" {
		return dr.VrfName
	}
	return fmt.Sprintf("ovnvrf%d", dr.VrfID)
}

func (c *Controller) advertisementSignature() string {
	vpcs, err := c.vpcLister.List(labels.Everything())
	if err != nil {
		return ""
	}
	names := make([]string, 0, len(vpcs))
	for _, vpc := range vpcs {
		dr := vpc.Spec.DynamicRouting
		if !dr.IsEnabled() || dr.VrfID == 0 {
			continue
		}
		if vrfPresent(vrfDeviceName(dr)) {
			names = append(names, vpc.Name)
		}
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

func lrpAddress(eips []*kubeovnv1.OvnEip, vpcName string) string {
	var names []string
	byName := make(map[string]string)
	for _, eip := range eips {
		if eip.Spec.Type != util.OvnEipTypeLRP {
			continue
		}
		if eip.Spec.ExternalSubnet == "" || eip.Name != vpcName+"-"+eip.Spec.ExternalSubnet {
			continue
		}
		if eip.Status.V4Ip == "" {
			continue
		}
		names = append(names, eip.Name)
		byName[eip.Name] = eip.Status.V4Ip
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return byName[names[0]]
}

func (c *Controller) checkSpeakerConflict(conf *kubeovnv1.BgpConf) error {
	pods, err := c.podLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("failed to list pods on node: %w", err)
	}

	neighbors := make(map[string]struct{}, len(conf.Spec.Neighbours)+len(conf.Spec.Peers))
	for _, addr := range conf.Spec.Neighbours {
		neighbors[addr] = struct{}{}
	}
	for _, n := range conf.Spec.Peers {
		neighbors[n.Address] = struct{}{}
	}

	for _, pod := range pods {
		if pod.Labels["app"] != "kube-ovn-speaker" && pod.Labels["app.kubernetes.io/name"] != "kube-ovn-speaker" {
			continue
		}
		for _, container := range pod.Spec.Containers {
			for _, arg := range container.Args {
				if strings.HasPrefix(arg, "--passivemode") && !strings.HasSuffix(arg, "=false") {
					return fmt.Errorf("kube-ovn-speaker pod %s runs in passive mode on this node and would conflict with FRR on port 179", pod.Name)
				}
				for _, flagName := range []string{"--neighbor-address=", "--neighbor-ipv6-address="} {
					if addrs, ok := strings.CutPrefix(arg, flagName); ok {
						for addr := range strings.SplitSeq(addrs, ",") {
							if _, found := neighbors[strings.TrimSpace(addr)]; found {
								return fmt.Errorf("kube-ovn-speaker pod %s already peers with %s from this node", pod.Name, strings.TrimSpace(addr))
							}
						}
					}
				}
			}
		}
	}
	return nil
}
