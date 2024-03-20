package controller

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"golang.org/x/time/rate"
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
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/utils/keymutex"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovninformer "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	ovnipam "github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const controllerAgentName = "kube-ovn-controller"

const (
	logicalSwitchKey      = "ls"
	logicalRouterKey      = "lr"
	portGroupKey          = "pg"
	networkPolicyKey      = "np"
	sgKey                 = "sg"
	associatedSgKeyPrefix = "associated_sg_"
	sgsKey                = "security_groups"
	u2oKey                = "u2o"
)

// Controller is kube-ovn main controller that watch ns/pod/node/svc/ep and operate ovn
type Controller struct {
	config *Configuration
	vpcs   *sync.Map

	// subnetVpcMap *sync.Map
	podSubnetMap *sync.Map
	ipam         *ovnipam.IPAM
	namedPort    *NamedPort

	ovnLegacyClient *ovs.LegacyClient

	OVNNbClient ovs.NbClient
	OVNSbClient ovs.SbClient

	// ExternalGatewayType define external gateway type, centralized
	ExternalGatewayType string

	podsLister             v1.PodLister
	podsSynced             cache.InformerSynced
	addOrUpdatePodQueue    workqueue.RateLimitingInterface
	deletePodQueue         workqueue.RateLimitingInterface
	deletingPodObjMap      *sync.Map
	updatePodSecurityQueue workqueue.RateLimitingInterface
	podKeyMutex            keymutex.KeyMutex

	vpcsLister           kubeovnlister.VpcLister
	vpcSynced            cache.InformerSynced
	addOrUpdateVpcQueue  workqueue.RateLimitingInterface
	delVpcQueue          workqueue.RateLimitingInterface
	updateVpcStatusQueue workqueue.RateLimitingInterface
	vpcKeyMutex          keymutex.KeyMutex

	vpcNatGatewayLister           kubeovnlister.VpcNatGatewayLister
	vpcNatGatewaySynced           cache.InformerSynced
	addOrUpdateVpcNatGatewayQueue workqueue.RateLimitingInterface
	delVpcNatGatewayQueue         workqueue.RateLimitingInterface
	initVpcNatGatewayQueue        workqueue.RateLimitingInterface
	updateVpcEipQueue             workqueue.RateLimitingInterface
	updateVpcFloatingIPQueue      workqueue.RateLimitingInterface
	updateVpcDnatQueue            workqueue.RateLimitingInterface
	updateVpcSnatQueue            workqueue.RateLimitingInterface
	updateVpcSubnetQueue          workqueue.RateLimitingInterface
	vpcNatGwKeyMutex              keymutex.KeyMutex

	switchLBRuleLister      kubeovnlister.SwitchLBRuleLister
	switchLBRuleSynced      cache.InformerSynced
	addSwitchLBRuleQueue    workqueue.RateLimitingInterface
	UpdateSwitchLBRuleQueue workqueue.RateLimitingInterface
	delSwitchLBRuleQueue    workqueue.RateLimitingInterface

	vpcDNSLister           kubeovnlister.VpcDnsLister
	vpcDNSSynced           cache.InformerSynced
	addOrUpdateVpcDNSQueue workqueue.RateLimitingInterface
	delVpcDNSQueue         workqueue.RateLimitingInterface

	subnetsLister           kubeovnlister.SubnetLister
	subnetSynced            cache.InformerSynced
	addOrUpdateSubnetQueue  workqueue.RateLimitingInterface
	deleteSubnetQueue       workqueue.RateLimitingInterface
	updateSubnetStatusQueue workqueue.RateLimitingInterface
	syncVirtualPortsQueue   workqueue.RateLimitingInterface
	subnetKeyMutex          keymutex.KeyMutex

	ippoolLister            kubeovnlister.IPPoolLister
	ippoolSynced            cache.InformerSynced
	addOrUpdateIPPoolQueue  workqueue.RateLimitingInterface
	updateIPPoolStatusQueue workqueue.RateLimitingInterface
	deleteIPPoolQueue       workqueue.RateLimitingInterface
	ippoolKeyMutex          keymutex.KeyMutex

	ipsLister     kubeovnlister.IPLister
	ipSynced      cache.InformerSynced
	addIPQueue    workqueue.RateLimitingInterface
	updateIPQueue workqueue.RateLimitingInterface
	delIPQueue    workqueue.RateLimitingInterface

	virtualIpsLister          kubeovnlister.VipLister
	virtualIpsSynced          cache.InformerSynced
	addVirtualIPQueue         workqueue.RateLimitingInterface
	updateVirtualIPQueue      workqueue.RateLimitingInterface
	updateVirtualParentsQueue workqueue.RateLimitingInterface
	delVirtualIPQueue         workqueue.RateLimitingInterface

	iptablesEipsLister     kubeovnlister.IptablesEIPLister
	iptablesEipSynced      cache.InformerSynced
	addIptablesEipQueue    workqueue.RateLimitingInterface
	updateIptablesEipQueue workqueue.RateLimitingInterface
	resetIptablesEipQueue  workqueue.RateLimitingInterface
	delIptablesEipQueue    workqueue.RateLimitingInterface

	iptablesFipsLister     kubeovnlister.IptablesFIPRuleLister
	iptablesFipSynced      cache.InformerSynced
	addIptablesFipQueue    workqueue.RateLimitingInterface
	updateIptablesFipQueue workqueue.RateLimitingInterface
	delIptablesFipQueue    workqueue.RateLimitingInterface

	iptablesDnatRulesLister     kubeovnlister.IptablesDnatRuleLister
	iptablesDnatRuleSynced      cache.InformerSynced
	addIptablesDnatRuleQueue    workqueue.RateLimitingInterface
	updateIptablesDnatRuleQueue workqueue.RateLimitingInterface
	delIptablesDnatRuleQueue    workqueue.RateLimitingInterface

	iptablesSnatRulesLister     kubeovnlister.IptablesSnatRuleLister
	iptablesSnatRuleSynced      cache.InformerSynced
	addIptablesSnatRuleQueue    workqueue.RateLimitingInterface
	updateIptablesSnatRuleQueue workqueue.RateLimitingInterface
	delIptablesSnatRuleQueue    workqueue.RateLimitingInterface

	ovnEipsLister     kubeovnlister.OvnEipLister
	ovnEipSynced      cache.InformerSynced
	addOvnEipQueue    workqueue.RateLimitingInterface
	updateOvnEipQueue workqueue.RateLimitingInterface
	resetOvnEipQueue  workqueue.RateLimitingInterface
	delOvnEipQueue    workqueue.RateLimitingInterface

	ovnFipsLister     kubeovnlister.OvnFipLister
	ovnFipSynced      cache.InformerSynced
	addOvnFipQueue    workqueue.RateLimitingInterface
	updateOvnFipQueue workqueue.RateLimitingInterface
	delOvnFipQueue    workqueue.RateLimitingInterface

	ovnSnatRulesLister     kubeovnlister.OvnSnatRuleLister
	ovnSnatRuleSynced      cache.InformerSynced
	addOvnSnatRuleQueue    workqueue.RateLimitingInterface
	updateOvnSnatRuleQueue workqueue.RateLimitingInterface
	delOvnSnatRuleQueue    workqueue.RateLimitingInterface

	ovnDnatRulesLister     kubeovnlister.OvnDnatRuleLister
	ovnDnatRuleSynced      cache.InformerSynced
	addOvnDnatRuleQueue    workqueue.RateLimitingInterface
	updateOvnDnatRuleQueue workqueue.RateLimitingInterface
	delOvnDnatRuleQueue    workqueue.RateLimitingInterface

	providerNetworksLister kubeovnlister.ProviderNetworkLister
	providerNetworkSynced  cache.InformerSynced

	vlansLister     kubeovnlister.VlanLister
	vlanSynced      cache.InformerSynced
	addVlanQueue    workqueue.RateLimitingInterface
	delVlanQueue    workqueue.RateLimitingInterface
	updateVlanQueue workqueue.RateLimitingInterface
	vlanKeyMutex    keymutex.KeyMutex

	namespacesLister  v1.NamespaceLister
	namespacesSynced  cache.InformerSynced
	addNamespaceQueue workqueue.RateLimitingInterface
	nsKeyMutex        keymutex.KeyMutex

	nodesLister     v1.NodeLister
	nodesSynced     cache.InformerSynced
	addNodeQueue    workqueue.RateLimitingInterface
	updateNodeQueue workqueue.RateLimitingInterface
	deleteNodeQueue workqueue.RateLimitingInterface
	nodeKeyMutex    keymutex.KeyMutex

	servicesLister     v1.ServiceLister
	serviceSynced      cache.InformerSynced
	addServiceQueue    workqueue.RateLimitingInterface
	deleteServiceQueue workqueue.RateLimitingInterface
	updateServiceQueue workqueue.RateLimitingInterface
	svcKeyMutex        keymutex.KeyMutex

	endpointsLister     v1.EndpointsLister
	endpointsSynced     cache.InformerSynced
	updateEndpointQueue workqueue.RateLimitingInterface
	epKeyMutex          keymutex.KeyMutex

	npsLister     netv1.NetworkPolicyLister
	npsSynced     cache.InformerSynced
	updateNpQueue workqueue.RateLimitingInterface
	deleteNpQueue workqueue.RateLimitingInterface
	npKeyMutex    keymutex.KeyMutex

	sgsLister          kubeovnlister.SecurityGroupLister
	sgSynced           cache.InformerSynced
	addOrUpdateSgQueue workqueue.RateLimitingInterface
	delSgQueue         workqueue.RateLimitingInterface
	syncSgPortsQueue   workqueue.RateLimitingInterface
	sgKeyMutex         keymutex.KeyMutex

	qosPoliciesLister    kubeovnlister.QoSPolicyLister
	qosPolicySynced      cache.InformerSynced
	addQoSPolicyQueue    workqueue.RateLimitingInterface
	updateQoSPolicyQueue workqueue.RateLimitingInterface
	delQoSPolicyQueue    workqueue.RateLimitingInterface

	configMapsLister v1.ConfigMapLister
	configMapsSynced cache.InformerSynced

	recorder               record.EventRecorder
	informerFactory        kubeinformers.SharedInformerFactory
	cmInformerFactory      kubeinformers.SharedInformerFactory
	kubeovnInformerFactory kubeovninformer.SharedInformerFactory
}

// Run creates and runs a new ovn controller
func Run(ctx context.Context, config *Configuration) {
	utilruntime.Must(kubeovnv1.AddToScheme(scheme.Scheme))
	klog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcasterWithCorrelatorOptions(record.CorrelatorOptions{BurstSize: 100})
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: config.KubeFactoryClient.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})
	custCrdRateLimiter := workqueue.NewMaxOfRateLimiter(
		workqueue.NewItemExponentialFailureRateLimiter(time.Duration(config.CustCrdRetryMinDelay)*time.Second, time.Duration(config.CustCrdRetryMaxDelay)*time.Second),
		&workqueue.BucketRateLimiter{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
	)

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
	ippoolInformer := kubeovnInformerFactory.Kubeovn().V1().IPPools()
	ipInformer := kubeovnInformerFactory.Kubeovn().V1().IPs()
	virtualIPInformer := kubeovnInformerFactory.Kubeovn().V1().Vips()
	iptablesEipInformer := kubeovnInformerFactory.Kubeovn().V1().IptablesEIPs()
	iptablesFipInformer := kubeovnInformerFactory.Kubeovn().V1().IptablesFIPRules()
	iptablesDnatRuleInformer := kubeovnInformerFactory.Kubeovn().V1().IptablesDnatRules()
	iptablesSnatRuleInformer := kubeovnInformerFactory.Kubeovn().V1().IptablesSnatRules()
	vlanInformer := kubeovnInformerFactory.Kubeovn().V1().Vlans()
	providerNetworkInformer := kubeovnInformerFactory.Kubeovn().V1().ProviderNetworks()
	sgInformer := kubeovnInformerFactory.Kubeovn().V1().SecurityGroups()
	podInformer := informerFactory.Core().V1().Pods()
	namespaceInformer := informerFactory.Core().V1().Namespaces()
	nodeInformer := informerFactory.Core().V1().Nodes()
	serviceInformer := informerFactory.Core().V1().Services()
	endpointInformer := informerFactory.Core().V1().Endpoints()
	qosPolicyInformer := kubeovnInformerFactory.Kubeovn().V1().QoSPolicies()
	configMapInformer := cmInformerFactory.Core().V1().ConfigMaps()
	npInformer := informerFactory.Networking().V1().NetworkPolicies()
	switchLBRuleInformer := kubeovnInformerFactory.Kubeovn().V1().SwitchLBRules()
	vpcDNSInformer := kubeovnInformerFactory.Kubeovn().V1().VpcDnses()
	ovnEipInformer := kubeovnInformerFactory.Kubeovn().V1().OvnEips()
	ovnFipInformer := kubeovnInformerFactory.Kubeovn().V1().OvnFips()
	ovnSnatRuleInformer := kubeovnInformerFactory.Kubeovn().V1().OvnSnatRules()
	ovnDnatRuleInformer := kubeovnInformerFactory.Kubeovn().V1().OvnDnatRules()

	numKeyLocks := runtime.NumCPU() * 2
	if numKeyLocks < config.WorkerNum*2 {
		numKeyLocks = config.WorkerNum * 2
	}
	controller := &Controller{
		config:            config,
		vpcs:              &sync.Map{},
		podSubnetMap:      &sync.Map{},
		deletingPodObjMap: &sync.Map{},
		ovnLegacyClient:   ovs.NewLegacyClient(config.OvnTimeout),
		ipam:              ovnipam.NewIPAM(),
		namedPort:         NewNamedPort(),

		vpcsLister:           vpcInformer.Lister(),
		vpcSynced:            vpcInformer.Informer().HasSynced,
		addOrUpdateVpcQueue:  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddOrUpdateVpc"),
		delVpcQueue:          workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteVpc"),
		updateVpcStatusQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateVpcStatus"),
		vpcKeyMutex:          keymutex.NewHashed(numKeyLocks),

		vpcNatGatewayLister:           vpcNatGatewayInformer.Lister(),
		vpcNatGatewaySynced:           vpcNatGatewayInformer.Informer().HasSynced,
		addOrUpdateVpcNatGatewayQueue: workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "AddOrUpdateVpcNatGw"),
		initVpcNatGatewayQueue:        workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "InitVpcNatGw"),
		delVpcNatGatewayQueue:         workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "DeleteVpcNatGw"),
		updateVpcEipQueue:             workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "UpdateVpcEip"),
		updateVpcFloatingIPQueue:      workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "UpdateVpcFloatingIp"),
		updateVpcDnatQueue:            workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "UpdateVpcDnat"),
		updateVpcSnatQueue:            workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "UpdateVpcSnat"),
		updateVpcSubnetQueue:          workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "UpdateVpcSubnet"),
		vpcNatGwKeyMutex:              keymutex.NewHashed(numKeyLocks),

		subnetsLister:           subnetInformer.Lister(),
		subnetSynced:            subnetInformer.Informer().HasSynced,
		addOrUpdateSubnetQueue:  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddSubnet"),
		deleteSubnetQueue:       workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteSubnet"),
		updateSubnetStatusQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateSubnetStatus"),
		syncVirtualPortsQueue:   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "SyncVirtualPort"),
		subnetKeyMutex:          keymutex.NewHashed(numKeyLocks),

		ippoolLister:            ippoolInformer.Lister(),
		ippoolSynced:            ippoolInformer.Informer().HasSynced,
		addOrUpdateIPPoolQueue:  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddIPPool"),
		updateIPPoolStatusQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateIPPoolStatus"),
		deleteIPPoolQueue:       workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteIPPool"),
		ippoolKeyMutex:          keymutex.NewHashed(numKeyLocks),

		ipsLister:     ipInformer.Lister(),
		ipSynced:      ipInformer.Informer().HasSynced,
		addIPQueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddIP"),
		updateIPQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateIP"),
		delIPQueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteIP"),

		virtualIpsLister:          virtualIPInformer.Lister(),
		virtualIpsSynced:          virtualIPInformer.Informer().HasSynced,
		addVirtualIPQueue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddVirtualIp"),
		updateVirtualIPQueue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateVirtualIp"),
		updateVirtualParentsQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateVirtualParents"),
		delVirtualIPQueue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteVirtualIp"),

		iptablesEipsLister:     iptablesEipInformer.Lister(),
		iptablesEipSynced:      iptablesEipInformer.Informer().HasSynced,
		addIptablesEipQueue:    workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "AddIptablesEip"),
		updateIptablesEipQueue: workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "UpdateIptablesEip"),
		resetIptablesEipQueue:  workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "ResetIptablesEip"),
		delIptablesEipQueue:    workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "DeleteIptablesEip"),

		iptablesFipsLister:     iptablesFipInformer.Lister(),
		iptablesFipSynced:      iptablesFipInformer.Informer().HasSynced,
		addIptablesFipQueue:    workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "AddIptablesFip"),
		updateIptablesFipQueue: workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "UpdateIptablesFip"),
		delIptablesFipQueue:    workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "DeleteIptablesFip"),

		iptablesDnatRulesLister:     iptablesDnatRuleInformer.Lister(),
		iptablesDnatRuleSynced:      iptablesDnatRuleInformer.Informer().HasSynced,
		addIptablesDnatRuleQueue:    workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "AddIptablesDnatRule"),
		updateIptablesDnatRuleQueue: workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "UpdateIptablesDnatRule"),
		delIptablesDnatRuleQueue:    workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "DeleteIptablesDnatRule"),

		iptablesSnatRulesLister:     iptablesSnatRuleInformer.Lister(),
		iptablesSnatRuleSynced:      iptablesSnatRuleInformer.Informer().HasSynced,
		addIptablesSnatRuleQueue:    workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "AddIptablesSnatRule"),
		updateIptablesSnatRuleQueue: workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "UpdateIptablesSnatRule"),
		delIptablesSnatRuleQueue:    workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "DeleteIptablesSnatRule"),

		vlansLister:     vlanInformer.Lister(),
		vlanSynced:      vlanInformer.Informer().HasSynced,
		addVlanQueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddVlan"),
		delVlanQueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DelVlan"),
		updateVlanQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateVlan"),
		vlanKeyMutex:    keymutex.NewHashed(numKeyLocks),

		providerNetworksLister: providerNetworkInformer.Lister(),
		providerNetworkSynced:  providerNetworkInformer.Informer().HasSynced,

		podsLister:          podInformer.Lister(),
		podsSynced:          podInformer.Informer().HasSynced,
		addOrUpdatePodQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddOrUpdatePod"),
		deletePodQueue: workqueue.NewRateLimitingQueueWithDelayingInterface(
			workqueue.NewNamedDelayingQueue("DeletePod"),
			workqueue.DefaultControllerRateLimiter(),
		),
		updatePodSecurityQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdatePodSecurity"),
		podKeyMutex:            keymutex.NewHashed(numKeyLocks),

		namespacesLister:  namespaceInformer.Lister(),
		namespacesSynced:  namespaceInformer.Informer().HasSynced,
		addNamespaceQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddNamespace"),
		nsKeyMutex:        keymutex.NewHashed(numKeyLocks),

		nodesLister:     nodeInformer.Lister(),
		nodesSynced:     nodeInformer.Informer().HasSynced,
		addNodeQueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddNode"),
		updateNodeQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateNode"),
		deleteNodeQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteNode"),
		nodeKeyMutex:    keymutex.NewHashed(numKeyLocks),

		servicesLister:     serviceInformer.Lister(),
		serviceSynced:      serviceInformer.Informer().HasSynced,
		addServiceQueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AddService"),
		deleteServiceQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteService"),
		updateServiceQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateService"),
		svcKeyMutex:        keymutex.NewHashed(numKeyLocks),

		endpointsLister:     endpointInformer.Lister(),
		endpointsSynced:     endpointInformer.Informer().HasSynced,
		updateEndpointQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateEndpoint"),
		epKeyMutex:          keymutex.NewHashed(numKeyLocks),

		qosPoliciesLister:    qosPolicyInformer.Lister(),
		qosPolicySynced:      qosPolicyInformer.Informer().HasSynced,
		addQoSPolicyQueue:    workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "AddQoSPolicy"),
		updateQoSPolicyQueue: workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "UpdateQoSPolicy"),
		delQoSPolicyQueue:    workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "DeleteQoSPolicy"),

		configMapsLister: configMapInformer.Lister(),
		configMapsSynced: configMapInformer.Informer().HasSynced,

		sgKeyMutex:         keymutex.NewHashed(numKeyLocks),
		sgsLister:          sgInformer.Lister(),
		sgSynced:           sgInformer.Informer().HasSynced,
		addOrUpdateSgQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateSg"),
		delSgQueue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteSg"),
		syncSgPortsQueue:   workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "SyncSgPorts"),

		ovnEipsLister:     ovnEipInformer.Lister(),
		ovnEipSynced:      ovnEipInformer.Informer().HasSynced,
		addOvnEipQueue:    workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "AddOvnEip"),
		updateOvnEipQueue: workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "UpdateOvnEip"),
		resetOvnEipQueue:  workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "ResetOvnEip"),
		delOvnEipQueue:    workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "DelOvnEip"),

		ovnFipsLister:     ovnFipInformer.Lister(),
		ovnFipSynced:      ovnFipInformer.Informer().HasSynced,
		addOvnFipQueue:    workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "AddOvnFip"),
		updateOvnFipQueue: workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "UpdateOvnFip"),
		delOvnFipQueue:    workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "DeleteOvnFip"),

		ovnSnatRulesLister:     ovnSnatRuleInformer.Lister(),
		ovnSnatRuleSynced:      ovnSnatRuleInformer.Informer().HasSynced,
		addOvnSnatRuleQueue:    workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "AddOvnSnatRule"),
		updateOvnSnatRuleQueue: workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "UpdateOvnSnatRule"),
		delOvnSnatRuleQueue:    workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "DelOvnSnatRule"),

		ovnDnatRulesLister:     ovnDnatRuleInformer.Lister(),
		ovnDnatRuleSynced:      ovnDnatRuleInformer.Informer().HasSynced,
		addOvnDnatRuleQueue:    workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "AddOvnDnatRule"),
		updateOvnDnatRuleQueue: workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "UpdateOvnDnatRule"),
		delOvnDnatRuleQueue:    workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "DeleteOvnDnatRule"),

		recorder:               recorder,
		informerFactory:        informerFactory,
		cmInformerFactory:      cmInformerFactory,
		kubeovnInformerFactory: kubeovnInformerFactory,
	}

	var err error
	if controller.OVNNbClient, err = ovs.NewOvnNbClient(config.OvnNbAddr, config.OvnTimeout); err != nil {
		util.LogFatalAndExit(err, "failed to create ovn nb client")
	}
	if controller.OVNSbClient, err = ovs.NewOvnSbClient(config.OvnSbAddr, config.OvnTimeout); err != nil {
		util.LogFatalAndExit(err, "failed to create ovn sb client")
	}
	if config.EnableLb {
		controller.switchLBRuleLister = switchLBRuleInformer.Lister()
		controller.switchLBRuleSynced = switchLBRuleInformer.Informer().HasSynced
		controller.addSwitchLBRuleQueue = workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "addSwitchLBRule")
		controller.delSwitchLBRuleQueue = workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "delSwitchLBRule")
		controller.UpdateSwitchLBRuleQueue = workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "updateSwitchLBRule")

		controller.vpcDNSLister = vpcDNSInformer.Lister()
		controller.vpcDNSSynced = vpcDNSInformer.Informer().HasSynced
		controller.addOrUpdateVpcDNSQueue = workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "AddOrUpdateVpcDns")
		controller.delVpcDNSQueue = workqueue.NewNamedRateLimitingQueue(custCrdRateLimiter, "DeleteVpcDns")
	}

	if config.EnableNP {
		controller.npsLister = npInformer.Lister()
		controller.npsSynced = npInformer.Informer().HasSynced
		controller.updateNpQueue = workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "UpdateNp")
		controller.deleteNpQueue = workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "DeleteNp")
		controller.npKeyMutex = keymutex.NewHashed(numKeyLocks)
	}

	defer controller.shutdown()
	klog.Info("Starting OVN controller")

	// Wait for the caches to be synced before starting workers
	controller.informerFactory.Start(ctx.Done())
	controller.cmInformerFactory.Start(ctx.Done())
	controller.kubeovnInformerFactory.Start(ctx.Done())

	klog.Info("Waiting for informer caches to sync")
	cacheSyncs := []cache.InformerSynced{
		controller.vpcNatGatewaySynced, controller.vpcSynced, controller.subnetSynced,
		controller.ipSynced, controller.virtualIpsSynced, controller.iptablesEipSynced,
		controller.iptablesFipSynced, controller.iptablesDnatRuleSynced, controller.iptablesSnatRuleSynced,
		controller.vlanSynced, controller.podsSynced, controller.namespacesSynced, controller.nodesSynced,
		controller.serviceSynced, controller.endpointsSynced, controller.configMapsSynced,
		controller.ovnEipSynced, controller.ovnFipSynced, controller.ovnSnatRuleSynced,
		controller.ovnDnatRuleSynced,
	}
	if controller.config.EnableLb {
		cacheSyncs = append(cacheSyncs, controller.switchLBRuleSynced, controller.vpcDNSSynced)
	}
	if controller.config.EnableNP {
		cacheSyncs = append(cacheSyncs, controller.npsSynced)
	}
	if !cache.WaitForCacheSync(ctx.Done(), cacheSyncs...) {
		util.LogFatalAndExit(nil, "failed to wait for caches to sync")
	}

	if _, err = podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddPod,
		DeleteFunc: controller.enqueueDeletePod,
		UpdateFunc: controller.enqueueUpdatePod,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add pod event handler")
	}

	if _, err = namespaceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddNamespace,
		UpdateFunc: controller.enqueueUpdateNamespace,
		DeleteFunc: controller.enqueueDeleteNamespace,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add namespace event handler")
	}

	if _, err = nodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddNode,
		UpdateFunc: controller.enqueueUpdateNode,
		DeleteFunc: controller.enqueueDeleteNode,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add node event handler")
	}

	if _, err = serviceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddService,
		DeleteFunc: controller.enqueueDeleteService,
		UpdateFunc: controller.enqueueUpdateService,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add service event handler")
	}

	if _, err = endpointInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddEndpoint,
		UpdateFunc: controller.enqueueUpdateEndpoint,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add endpoint event handler")
	}

	if _, err = vpcInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddVpc,
		UpdateFunc: controller.enqueueUpdateVpc,
		DeleteFunc: controller.enqueueDelVpc,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add vpc event handler")
	}

	if _, err = vpcNatGatewayInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddVpcNatGw,
		UpdateFunc: controller.enqueueUpdateVpcNatGw,
		DeleteFunc: controller.enqueueDeleteVpcNatGw,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add vpc nat gateway event handler")
	}

	if _, err = subnetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddSubnet,
		UpdateFunc: controller.enqueueUpdateSubnet,
		DeleteFunc: controller.enqueueDeleteSubnet,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add subnet event handler")
	}

	if _, err = ippoolInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddIPPool,
		UpdateFunc: controller.enqueueUpdateIPPool,
		DeleteFunc: controller.enqueueDeleteIPPool,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add ippool event handler")
	}

	if _, err = ipInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddIP,
		UpdateFunc: controller.enqueueUpdateIP,
		DeleteFunc: controller.enqueueDelIP,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add ips event handler")
	}

	if _, err = vlanInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddVlan,
		DeleteFunc: controller.enqueueDelVlan,
		UpdateFunc: controller.enqueueUpdateVlan,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add vlan event handler")
	}

	if _, err = sgInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddSg,
		DeleteFunc: controller.enqueueDeleteSg,
		UpdateFunc: controller.enqueueUpdateSg,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add security group event handler")
	}

	if _, err = virtualIPInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddVirtualIP,
		UpdateFunc: controller.enqueueUpdateVirtualIP,
		DeleteFunc: controller.enqueueDelVirtualIP,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add virtual ip event handler")
	}

	if _, err = iptablesEipInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddIptablesEip,
		UpdateFunc: controller.enqueueUpdateIptablesEip,
		DeleteFunc: controller.enqueueDelIptablesEip,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add iptables eip event handler")
	}

	if _, err = iptablesFipInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddIptablesFip,
		UpdateFunc: controller.enqueueUpdateIptablesFip,
		DeleteFunc: controller.enqueueDelIptablesFip,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add iptables fip event handler")
	}

	if _, err = iptablesDnatRuleInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddIptablesDnatRule,
		UpdateFunc: controller.enqueueUpdateIptablesDnatRule,
		DeleteFunc: controller.enqueueDelIptablesDnatRule,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add iptables dnat event handler")
	}

	if _, err = iptablesSnatRuleInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddIptablesSnatRule,
		UpdateFunc: controller.enqueueUpdateIptablesSnatRule,
		DeleteFunc: controller.enqueueDelIptablesSnatRule,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add iptables snat rule event handler")
	}

	if _, err = ovnEipInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddOvnEip,
		UpdateFunc: controller.enqueueUpdateOvnEip,
		DeleteFunc: controller.enqueueDelOvnEip,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add eip event handler")
	}

	if _, err = ovnFipInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddOvnFip,
		UpdateFunc: controller.enqueueUpdateOvnFip,
		DeleteFunc: controller.enqueueDelOvnFip,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add ovn fip event handler")
	}

	if _, err = ovnSnatRuleInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddOvnSnatRule,
		UpdateFunc: controller.enqueueUpdateOvnSnatRule,
		DeleteFunc: controller.enqueueDelOvnSnatRule,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add ovn snat rule event handler")
	}

	if _, err = ovnDnatRuleInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddOvnDnatRule,
		UpdateFunc: controller.enqueueUpdateOvnDnatRule,
		DeleteFunc: controller.enqueueDelOvnDnatRule,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add ovn dnat rule event handler")
	}

	if _, err = qosPolicyInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddQoSPolicy,
		UpdateFunc: controller.enqueueUpdateQoSPolicy,
		DeleteFunc: controller.enqueueDelQoSPolicy,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add qos policy event handler")
	}

	if config.EnableLb {
		if _, err = switchLBRuleInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    controller.enqueueAddSwitchLBRule,
			UpdateFunc: controller.enqueueUpdateSwitchLBRule,
			DeleteFunc: controller.enqueueDeleteSwitchLBRule,
		}); err != nil {
			util.LogFatalAndExit(err, "failed to add switch lb rule event handler")
		}

		if _, err = vpcDNSInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    controller.enqueueAddVpcDNS,
			UpdateFunc: controller.enqueueUpdateVpcDNS,
			DeleteFunc: controller.enqueueDeleteVPCDNS,
		}); err != nil {
			util.LogFatalAndExit(err, "failed to add vpc dns event handler")
		}
	}

	if config.EnableNP {
		if _, err = npInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    controller.enqueueAddNp,
			UpdateFunc: controller.enqueueUpdateNp,
			DeleteFunc: controller.enqueueDeleteNp,
		}); err != nil {
			util.LogFatalAndExit(err, "failed to add network policy event handler")
		}
	}

	controller.Run(ctx)
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(ctx context.Context) {
	// The init process can only be placed here if the init process do really affect the normal process of controller, such as Nodes/Pods/Subnets...
	// Otherwise, the init process should be placed after all workers have already started working
	if err := c.OVNNbClient.SetLsDnatModDlDst(c.config.LsDnatModDlDst); err != nil {
		util.LogFatalAndExit(err, "failed to set NB_Global option ls_dnat_mod_dl_dst")
	}

	if err := c.OVNNbClient.SetUseCtInvMatch(); err != nil {
		util.LogFatalAndExit(err, "failed to set NB_Global option use_ct_inv_match to false")
	}

	if err := c.OVNNbClient.SetLsCtSkipDstLportIPs(c.config.LsCtSkipDstLportIPs); err != nil {
		util.LogFatalAndExit(err, "failed to set NB_Global option ls_ct_skip_dst_lport_ips")
	}

	if err := c.InitOVN(); err != nil {
		util.LogFatalAndExit(err, "failed to initialize ovn resources")
	}

	if err := c.InitDefaultVpc(); err != nil {
		util.LogFatalAndExit(err, "failed to initialize default vpc")
	}

	// sync ip crd before initIPAM since ip crd will be used to restore vm and statefulset pod in initIPAM
	if err := c.syncIPCR(); err != nil {
		util.LogFatalAndExit(err, "failed to sync crd ips")
	}

	if err := c.syncFinalizers(); err != nil {
		util.LogFatalAndExit(err, "failed to initialize crd finalizers")
	}

	if err := c.InitIPAM(); err != nil {
		util.LogFatalAndExit(err, "failed to initialize ipam")
	}

	if err := c.syncNodeRoutes(); err != nil {
		util.LogFatalAndExit(err, "failed to initialize node routes")
	}

	if err := c.syncSubnetCR(); err != nil {
		util.LogFatalAndExit(err, "failed to sync crd subnets")
	}

	if err := c.syncVlanCR(); err != nil {
		util.LogFatalAndExit(err, "failed to sync crd vlans")
	}

	// start workers to do all the network operations
	c.startWorkers(ctx)

	c.initResourceOnce()
	<-ctx.Done()
	klog.Info("Shutting down workers")
}

func (c *Controller) shutdown() {
	utilruntime.HandleCrash()

	c.addOrUpdatePodQueue.ShutDown()
	c.deletePodQueue.ShutDown()
	c.updatePodSecurityQueue.ShutDown()

	c.addNamespaceQueue.ShutDown()

	c.addOrUpdateSubnetQueue.ShutDown()
	c.deleteSubnetQueue.ShutDown()
	c.updateSubnetStatusQueue.ShutDown()
	c.syncVirtualPortsQueue.ShutDown()

	c.addOrUpdateIPPoolQueue.ShutDown()
	c.updateIPPoolStatusQueue.ShutDown()
	c.deleteIPPoolQueue.ShutDown()

	c.addNodeQueue.ShutDown()
	c.updateNodeQueue.ShutDown()
	c.deleteNodeQueue.ShutDown()

	c.addServiceQueue.ShutDown()
	c.deleteServiceQueue.ShutDown()
	c.updateServiceQueue.ShutDown()
	c.updateEndpointQueue.ShutDown()

	c.addVlanQueue.ShutDown()
	c.delVlanQueue.ShutDown()
	c.updateVlanQueue.ShutDown()

	c.addOrUpdateVpcQueue.ShutDown()
	c.updateVpcStatusQueue.ShutDown()
	c.delVpcQueue.ShutDown()

	c.addOrUpdateVpcNatGatewayQueue.ShutDown()
	c.initVpcNatGatewayQueue.ShutDown()
	c.delVpcNatGatewayQueue.ShutDown()
	c.updateVpcEipQueue.ShutDown()
	c.updateVpcFloatingIPQueue.ShutDown()
	c.updateVpcDnatQueue.ShutDown()
	c.updateVpcSnatQueue.ShutDown()
	c.updateVpcSubnetQueue.ShutDown()

	if c.config.EnableLb {
		c.addSwitchLBRuleQueue.ShutDown()
		c.delSwitchLBRuleQueue.ShutDown()
		c.UpdateSwitchLBRuleQueue.ShutDown()

		c.addOrUpdateVpcDNSQueue.ShutDown()
		c.delVpcDNSQueue.ShutDown()
	}

	c.addIPQueue.ShutDown()
	c.updateIPQueue.ShutDown()
	c.delIPQueue.ShutDown()

	c.addVirtualIPQueue.ShutDown()
	c.updateVirtualIPQueue.ShutDown()
	c.updateVirtualParentsQueue.ShutDown()
	c.delVirtualIPQueue.ShutDown()

	c.addIptablesEipQueue.ShutDown()
	c.updateIptablesEipQueue.ShutDown()
	c.resetIptablesEipQueue.ShutDown()
	c.delIptablesEipQueue.ShutDown()

	c.addIptablesFipQueue.ShutDown()
	c.updateIptablesFipQueue.ShutDown()
	c.delIptablesFipQueue.ShutDown()

	c.addIptablesDnatRuleQueue.ShutDown()
	c.updateIptablesDnatRuleQueue.ShutDown()
	c.delIptablesDnatRuleQueue.ShutDown()

	c.addIptablesSnatRuleQueue.ShutDown()
	c.updateIptablesSnatRuleQueue.ShutDown()
	c.delIptablesSnatRuleQueue.ShutDown()

	c.addQoSPolicyQueue.ShutDown()
	c.updateQoSPolicyQueue.ShutDown()
	c.delQoSPolicyQueue.ShutDown()

	c.addOvnEipQueue.ShutDown()
	c.updateOvnEipQueue.ShutDown()
	c.resetOvnEipQueue.ShutDown()
	c.delOvnEipQueue.ShutDown()

	c.addOvnFipQueue.ShutDown()
	c.updateOvnFipQueue.ShutDown()
	c.delOvnFipQueue.ShutDown()

	c.addOvnSnatRuleQueue.ShutDown()
	c.updateOvnSnatRuleQueue.ShutDown()
	c.delOvnSnatRuleQueue.ShutDown()

	c.addOvnDnatRuleQueue.ShutDown()
	c.updateOvnDnatRuleQueue.ShutDown()
	c.delOvnDnatRuleQueue.ShutDown()

	if c.config.EnableNP {
		c.updateNpQueue.ShutDown()
		c.deleteNpQueue.ShutDown()
	}
	c.addOrUpdateSgQueue.ShutDown()
	c.delSgQueue.ShutDown()
	c.syncSgPortsQueue.ShutDown()
}

func (c *Controller) startWorkers(ctx context.Context) {
	klog.Info("Starting workers")

	go wait.Until(c.runAddVpcWorker, time.Second, ctx.Done())

	go wait.Until(c.runAddOrUpdateVpcNatGwWorker, time.Second, ctx.Done())
	go wait.Until(c.runInitVpcNatGwWorker, time.Second, ctx.Done())
	go wait.Until(c.runDelVpcNatGwWorker, time.Second, ctx.Done())
	go wait.Until(c.runUpdateVpcFloatingIPWorker, time.Second, ctx.Done())
	go wait.Until(c.runUpdateVpcEipWorker, time.Second, ctx.Done())
	go wait.Until(c.runUpdateVpcDnatWorker, time.Second, ctx.Done())
	go wait.Until(c.runUpdateVpcSnatWorker, time.Second, ctx.Done())
	go wait.Until(c.runUpdateVpcSubnetWorker, time.Second, ctx.Done())

	// add default/join subnet and wait them ready
	go wait.Until(c.runAddSubnetWorker, time.Second, ctx.Done())
	go wait.Until(c.runAddIPPoolWorker, time.Second, ctx.Done())
	go wait.Until(c.runAddVlanWorker, time.Second, ctx.Done())
	go wait.Until(c.runAddNamespaceWorker, time.Second, ctx.Done())
	err := wait.PollUntilContextCancel(ctx, 3*time.Second, true, func(_ context.Context) (done bool, err error) {
		subnets := []string{c.config.DefaultLogicalSwitch, c.config.NodeSwitch}
		klog.Infof("wait for subnets %v ready", subnets)

		return c.allSubnetReady(subnets...)
	})
	if err != nil {
		klog.Fatalf("wait default/join subnet ready error: %v", err)
	}

	go wait.Until(c.runAddSgWorker, time.Second, ctx.Done())
	go wait.Until(c.runDelSgWorker, time.Second, ctx.Done())
	go wait.Until(c.runSyncSgPortsWorker, time.Second, ctx.Done())

	// run node worker before handle any pods
	for i := 0; i < c.config.WorkerNum; i++ {
		go wait.Until(c.runAddNodeWorker, time.Second, ctx.Done())
		go wait.Until(c.runUpdateNodeWorker, time.Second, ctx.Done())
		go wait.Until(c.runDeleteNodeWorker, time.Second, ctx.Done())
	}
	for {
		ready := true
		time.Sleep(3 * time.Second)
		nodes, err := c.nodesLister.List(labels.Everything())
		if err != nil {
			util.LogFatalAndExit(err, "failed to list nodes")
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

	go wait.Until(c.runDelVpcWorker, time.Second, ctx.Done())
	go wait.Until(c.runUpdateVpcStatusWorker, time.Second, ctx.Done())

	if c.config.EnableLb {
		go wait.Until(c.runAddServiceWorker, time.Second, ctx.Done())
		// run in a single worker to avoid delete the last vip, which will lead ovn to delete the loadbalancer
		go wait.Until(c.runDeleteServiceWorker, time.Second, ctx.Done())

		go wait.Until(c.runAddSwitchLBRuleWorker, time.Second, ctx.Done())
		go wait.Until(c.runDelSwitchLBRuleWorker, time.Second, ctx.Done())
		go wait.Until(c.runUpdateSwitchLBRuleWorker, time.Second, ctx.Done())

		go wait.Until(c.runAddOrUpdateVPCDNSWorker, time.Second, ctx.Done())
		go wait.Until(c.runDelVPCDNSWorker, time.Second, ctx.Done())
		go wait.Until(func() {
			c.resyncVpcDNSConfig()
		}, 5*time.Second, ctx.Done())
	}

	for i := 0; i < c.config.WorkerNum; i++ {
		go wait.Until(c.runDeletePodWorker, time.Second, ctx.Done())
		go wait.Until(c.runAddOrUpdatePodWorker, time.Second, ctx.Done())
		go wait.Until(c.runUpdatePodSecurityWorker, time.Second, ctx.Done())

		go wait.Until(c.runDeleteSubnetWorker, time.Second, ctx.Done())
		go wait.Until(c.runDeleteIPPoolWorker, time.Second, ctx.Done())
		go wait.Until(c.runUpdateSubnetStatusWorker, time.Second, ctx.Done())
		go wait.Until(c.runUpdateIPPoolStatusWorker, time.Second, ctx.Done())
		go wait.Until(c.runSyncVirtualPortsWorker, time.Second, ctx.Done())

		if c.config.EnableLb {
			go wait.Until(c.runUpdateServiceWorker, time.Second, ctx.Done())
			go wait.Until(c.runUpdateEndpointWorker, time.Second, ctx.Done())
		}

		if c.config.EnableNP {
			go wait.Until(c.runUpdateNpWorker, time.Second, ctx.Done())
			go wait.Until(c.runDeleteNpWorker, time.Second, ctx.Done())
		}

		go wait.Until(c.runDelVlanWorker, time.Second, ctx.Done())
		go wait.Until(c.runUpdateVlanWorker, time.Second, ctx.Done())
	}

	if c.config.EnableEipSnat {
		go wait.Until(func() {
			// init l3 about the default vpc external lrp binding to the gw chassis
			c.resyncExternalGateway()
		}, time.Second, ctx.Done())

		// maintain l3 ha about the vpc external lrp binding to the gw chassis
		c.OVNNbClient.MonitorBFD()
	}

	go wait.Until(func() {
		c.resyncVpcNatGwConfig()
	}, time.Second, ctx.Done())

	go wait.Until(func() {
		if err := c.markAndCleanLSP(); err != nil {
			klog.Errorf("gc lsp error: %v", err)
		}
	}, time.Duration(c.config.GCInterval)*time.Second, ctx.Done())

	go wait.Until(func() {
		if err := c.inspectPod(); err != nil {
			klog.Errorf("inspection error: %v", err)
		}
	}, time.Duration(c.config.InspectInterval)*time.Second, ctx.Done())

	if c.config.EnableExternalVpc {
		go wait.Until(func() {
			c.syncExternalVpc()
		}, 5*time.Second, ctx.Done())
	}

	go wait.Until(c.resyncProviderNetworkStatus, 30*time.Second, ctx.Done())
	go wait.Until(c.resyncSubnetMetrics, 30*time.Second, ctx.Done())
	go wait.Until(c.CheckGatewayReady, 5*time.Second, ctx.Done())

	go wait.Until(c.runAddOvnEipWorker, time.Second, ctx.Done())
	go wait.Until(c.runUpdateOvnEipWorker, time.Second, ctx.Done())
	go wait.Until(c.runResetOvnEipWorker, time.Second, ctx.Done())
	go wait.Until(c.runDelOvnEipWorker, time.Second, ctx.Done())

	go wait.Until(c.runAddOvnFipWorker, time.Second, ctx.Done())
	go wait.Until(c.runUpdateOvnFipWorker, time.Second, ctx.Done())
	go wait.Until(c.runDelOvnFipWorker, time.Second, ctx.Done())

	go wait.Until(c.runAddOvnSnatRuleWorker, time.Second, ctx.Done())
	go wait.Until(c.runUpdateOvnSnatRuleWorker, time.Second, ctx.Done())
	go wait.Until(c.runDelOvnSnatRuleWorker, time.Second, ctx.Done())

	go wait.Until(c.runAddOvnDnatRuleWorker, time.Second, ctx.Done())
	go wait.Until(c.runUpdateOvnDnatRuleWorker, time.Second, ctx.Done())
	go wait.Until(c.runDelOvnDnatRuleWorker, time.Second, ctx.Done())

	if c.config.EnableNP {
		go wait.Until(c.CheckNodePortGroup, time.Duration(c.config.NodePgProbeTime)*time.Minute, ctx.Done())
	}

	go wait.Until(c.runAddIPWorker, time.Second, ctx.Done())
	go wait.Until(c.runUpdateIPWorker, time.Second, ctx.Done())
	go wait.Until(c.runDelIPWorker, time.Second, ctx.Done())

	go wait.Until(c.runAddVirtualIPWorker, time.Second, ctx.Done())
	go wait.Until(c.runUpdateVirtualIPWorker, time.Second, ctx.Done())
	go wait.Until(c.runUpdateVirtualParentsWorker, time.Second, ctx.Done())
	go wait.Until(c.runDelVirtualIPWorker, time.Second, ctx.Done())

	go wait.Until(c.runAddIptablesEipWorker, time.Second, ctx.Done())
	go wait.Until(c.runUpdateIptablesEipWorker, time.Second, ctx.Done())
	go wait.Until(c.runResetIptablesEipWorker, time.Second, ctx.Done())
	go wait.Until(c.runDelIptablesEipWorker, time.Second, ctx.Done())

	go wait.Until(c.runAddIptablesFipWorker, time.Second, ctx.Done())
	go wait.Until(c.runUpdateIptablesFipWorker, time.Second, ctx.Done())
	go wait.Until(c.runDelIptablesFipWorker, time.Second, ctx.Done())

	go wait.Until(c.runAddIptablesDnatRuleWorker, time.Second, ctx.Done())
	go wait.Until(c.runUpdateIptablesDnatRuleWorker, time.Second, ctx.Done())
	go wait.Until(c.runDelIptablesDnatRuleWorker, time.Second, ctx.Done())

	go wait.Until(c.runAddIptablesSnatRuleWorker, time.Second, ctx.Done())
	go wait.Until(c.runUpdateIptablesSnatRuleWorker, time.Second, ctx.Done())
	go wait.Until(c.runDelIptablesSnatRuleWorker, time.Second, ctx.Done())

	go wait.Until(c.runAddQoSPolicyWorker, time.Second, ctx.Done())
	go wait.Until(c.runUpdateQoSPolicyWorker, time.Second, ctx.Done())
	go wait.Until(c.runDelQoSPolicyWorker, time.Second, ctx.Done())
}

func (c *Controller) allSubnetReady(subnets ...string) (bool, error) {
	for _, lsName := range subnets {
		exist, err := c.OVNNbClient.LogicalSwitchExists(lsName)
		if err != nil {
			klog.Error(err)
			return false, fmt.Errorf("check logical switch %s exist: %v", lsName, err)
		}

		if !exist {
			return false, nil
		}
	}

	return true, nil
}

func (c *Controller) initResourceOnce() {
	c.registerSubnetMetrics()

	if err := c.initNodeChassis(); err != nil {
		util.LogFatalAndExit(err, "failed to initialize node chassis")
	}

	if err := c.initDenyAllSecurityGroup(); err != nil {
		util.LogFatalAndExit(err, "failed to initialize 'deny_all' security group")
	}

	if err := c.syncVpcNatGatewayCR(); err != nil {
		util.LogFatalAndExit(err, "failed to sync crd vpc nat gateways")
	}

	if err := c.initVpcNatGw(); err != nil {
		util.LogFatalAndExit(err, "failed to initialize vpc nat gateways")
	}
	if c.config.EnableLb {
		if err := c.initVpcDNSConfig(); err != nil {
			util.LogFatalAndExit(err, "failed to initialize vpc-dns")
		}
	}

	// remove resources in ovndb that not exist any more in kubernetes resources
	// process gc at last in case of affecting other init process
	if err := c.gc(); err != nil {
		util.LogFatalAndExit(err, "failed to run gc")
	}
}
