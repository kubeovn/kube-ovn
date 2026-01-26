package controller

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	netAttach "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/informers/externalversions"
	netAttachv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/listers/k8s.cni.cncf.io/v1"
	"github.com/puzpuzpuz/xsync/v4"
	"golang.org/x/time/rate"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	appsv1 "k8s.io/client-go/listers/apps/v1"
	certListerv1 "k8s.io/client-go/listers/certificates/v1"
	v1 "k8s.io/client-go/listers/core/v1"
	discoveryv1 "k8s.io/client-go/listers/discovery/v1"
	netv1 "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/utils/keymutex"
	"k8s.io/utils/set"
	v1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"
	netpolv1alpha2 "sigs.k8s.io/network-policy-api/apis/v1alpha2"
	anpinformer "sigs.k8s.io/network-policy-api/pkg/client/informers/externalversions"
	anplister "sigs.k8s.io/network-policy-api/pkg/client/listers/apis/v1alpha1"
	anplisterv1alpha2 "sigs.k8s.io/network-policy-api/pkg/client/listers/apis/v1alpha2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	kubeovninformer "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions"
	kubeovnlister "github.com/kubeovn/kube-ovn/pkg/client/listers/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/informer"
	ovnipam "github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const controllerAgentName = "kube-ovn-controller"

const (
	logicalSwitchKey              = "ls"
	logicalRouterKey              = "lr"
	portGroupKey                  = "pg"
	networkPolicyKey              = "np"
	sgKey                         = "sg"
	sgsKey                        = "security_groups"
	u2oKey                        = "u2o"
	adminNetworkPolicyKey         = "anp"
	baselineAdminNetworkPolicyKey = "banp"
	ippoolKey                     = "ippool"
	clusterNetworkPolicyKey       = "cnp"
)

// Controller is kube-ovn main controller that watch ns/pod/node/svc/ep and operate ovn
type Controller struct {
	config *Configuration

	ipam           *ovnipam.IPAM
	namedPort      *NamedPort
	anpPrioNameMap map[int32]string
	anpNamePrioMap map[string]int32
	bnpPrioNameMap map[int32]string
	bnpNamePrioMap map[string]int32

	OVNNbClient ovs.NbClient
	OVNSbClient ovs.SbClient

	// ExternalGatewayType define external gateway type, centralized
	ExternalGatewayType string

	podsLister             v1.PodLister
	podsSynced             cache.InformerSynced
	addOrUpdatePodQueue    workqueue.TypedRateLimitingInterface[string]
	deletePodQueue         workqueue.TypedRateLimitingInterface[string]
	deletingPodObjMap      *xsync.Map[string, *corev1.Pod]
	deletingNodeObjMap     *xsync.Map[string, *corev1.Node]
	updatePodSecurityQueue workqueue.TypedRateLimitingInterface[string]
	podKeyMutex            keymutex.KeyMutex

	vpcsLister           kubeovnlister.VpcLister
	vpcSynced            cache.InformerSynced
	addOrUpdateVpcQueue  workqueue.TypedRateLimitingInterface[string]
	vpcLastPoliciesMap   *xsync.Map[string, string]
	delVpcQueue          workqueue.TypedRateLimitingInterface[*kubeovnv1.Vpc]
	updateVpcStatusQueue workqueue.TypedRateLimitingInterface[string]
	vpcKeyMutex          keymutex.KeyMutex

	vpcNatGatewayLister           kubeovnlister.VpcNatGatewayLister
	vpcNatGatewaySynced           cache.InformerSynced
	addOrUpdateVpcNatGatewayQueue workqueue.TypedRateLimitingInterface[string]
	delVpcNatGatewayQueue         workqueue.TypedRateLimitingInterface[string]
	initVpcNatGatewayQueue        workqueue.TypedRateLimitingInterface[string]
	updateVpcEipQueue             workqueue.TypedRateLimitingInterface[string]
	updateVpcFloatingIPQueue      workqueue.TypedRateLimitingInterface[string]
	updateVpcDnatQueue            workqueue.TypedRateLimitingInterface[string]
	updateVpcSnatQueue            workqueue.TypedRateLimitingInterface[string]
	updateVpcSubnetQueue          workqueue.TypedRateLimitingInterface[string]
	vpcNatGwKeyMutex              keymutex.KeyMutex
	vpcNatGwExecKeyMutex          keymutex.KeyMutex

	vpcEgressGatewayLister           kubeovnlister.VpcEgressGatewayLister
	vpcEgressGatewaySynced           cache.InformerSynced
	addOrUpdateVpcEgressGatewayQueue workqueue.TypedRateLimitingInterface[string]
	delVpcEgressGatewayQueue         workqueue.TypedRateLimitingInterface[string]
	vpcEgressGatewayKeyMutex         keymutex.KeyMutex

	switchLBRuleLister      kubeovnlister.SwitchLBRuleLister
	switchLBRuleSynced      cache.InformerSynced
	addSwitchLBRuleQueue    workqueue.TypedRateLimitingInterface[string]
	updateSwitchLBRuleQueue workqueue.TypedRateLimitingInterface[*SlrInfo]
	delSwitchLBRuleQueue    workqueue.TypedRateLimitingInterface[*SlrInfo]

	vpcDNSLister           kubeovnlister.VpcDnsLister
	vpcDNSSynced           cache.InformerSynced
	addOrUpdateVpcDNSQueue workqueue.TypedRateLimitingInterface[string]
	delVpcDNSQueue         workqueue.TypedRateLimitingInterface[string]

	subnetsLister           kubeovnlister.SubnetLister
	subnetSynced            cache.InformerSynced
	addOrUpdateSubnetQueue  workqueue.TypedRateLimitingInterface[string]
	deleteSubnetQueue       workqueue.TypedRateLimitingInterface[*kubeovnv1.Subnet]
	updateSubnetStatusQueue workqueue.TypedRateLimitingInterface[string]
	syncVirtualPortsQueue   workqueue.TypedRateLimitingInterface[string]
	subnetKeyMutex          keymutex.KeyMutex

	ippoolLister            kubeovnlister.IPPoolLister
	ippoolSynced            cache.InformerSynced
	addOrUpdateIPPoolQueue  workqueue.TypedRateLimitingInterface[string]
	updateIPPoolStatusQueue workqueue.TypedRateLimitingInterface[string]
	deleteIPPoolQueue       workqueue.TypedRateLimitingInterface[*kubeovnv1.IPPool]
	ippoolKeyMutex          keymutex.KeyMutex

	ipsLister     kubeovnlister.IPLister
	ipSynced      cache.InformerSynced
	addIPQueue    workqueue.TypedRateLimitingInterface[string]
	updateIPQueue workqueue.TypedRateLimitingInterface[string]
	delIPQueue    workqueue.TypedRateLimitingInterface[*kubeovnv1.IP]

	virtualIpsLister          kubeovnlister.VipLister
	virtualIpsSynced          cache.InformerSynced
	addVirtualIPQueue         workqueue.TypedRateLimitingInterface[string]
	updateVirtualIPQueue      workqueue.TypedRateLimitingInterface[string]
	updateVirtualParentsQueue workqueue.TypedRateLimitingInterface[string]
	delVirtualIPQueue         workqueue.TypedRateLimitingInterface[*kubeovnv1.Vip]

	iptablesEipsLister     kubeovnlister.IptablesEIPLister
	iptablesEipSynced      cache.InformerSynced
	addIptablesEipQueue    workqueue.TypedRateLimitingInterface[string]
	updateIptablesEipQueue workqueue.TypedRateLimitingInterface[string]
	resetIptablesEipQueue  workqueue.TypedRateLimitingInterface[string]
	delIptablesEipQueue    workqueue.TypedRateLimitingInterface[*kubeovnv1.IptablesEIP]

	iptablesFipsLister     kubeovnlister.IptablesFIPRuleLister
	iptablesFipSynced      cache.InformerSynced
	addIptablesFipQueue    workqueue.TypedRateLimitingInterface[string]
	updateIptablesFipQueue workqueue.TypedRateLimitingInterface[string]
	delIptablesFipQueue    workqueue.TypedRateLimitingInterface[string]

	iptablesDnatRulesLister     kubeovnlister.IptablesDnatRuleLister
	iptablesDnatRuleSynced      cache.InformerSynced
	addIptablesDnatRuleQueue    workqueue.TypedRateLimitingInterface[string]
	updateIptablesDnatRuleQueue workqueue.TypedRateLimitingInterface[string]
	delIptablesDnatRuleQueue    workqueue.TypedRateLimitingInterface[string]

	iptablesSnatRulesLister     kubeovnlister.IptablesSnatRuleLister
	iptablesSnatRuleSynced      cache.InformerSynced
	addIptablesSnatRuleQueue    workqueue.TypedRateLimitingInterface[string]
	updateIptablesSnatRuleQueue workqueue.TypedRateLimitingInterface[string]
	delIptablesSnatRuleQueue    workqueue.TypedRateLimitingInterface[string]

	ovnEipsLister     kubeovnlister.OvnEipLister
	ovnEipSynced      cache.InformerSynced
	addOvnEipQueue    workqueue.TypedRateLimitingInterface[string]
	updateOvnEipQueue workqueue.TypedRateLimitingInterface[string]
	resetOvnEipQueue  workqueue.TypedRateLimitingInterface[string]
	delOvnEipQueue    workqueue.TypedRateLimitingInterface[*kubeovnv1.OvnEip]

	ovnFipsLister     kubeovnlister.OvnFipLister
	ovnFipSynced      cache.InformerSynced
	addOvnFipQueue    workqueue.TypedRateLimitingInterface[string]
	updateOvnFipQueue workqueue.TypedRateLimitingInterface[string]
	delOvnFipQueue    workqueue.TypedRateLimitingInterface[string]

	ovnSnatRulesLister     kubeovnlister.OvnSnatRuleLister
	ovnSnatRuleSynced      cache.InformerSynced
	addOvnSnatRuleQueue    workqueue.TypedRateLimitingInterface[string]
	updateOvnSnatRuleQueue workqueue.TypedRateLimitingInterface[string]
	delOvnSnatRuleQueue    workqueue.TypedRateLimitingInterface[string]

	ovnDnatRulesLister     kubeovnlister.OvnDnatRuleLister
	ovnDnatRuleSynced      cache.InformerSynced
	addOvnDnatRuleQueue    workqueue.TypedRateLimitingInterface[string]
	updateOvnDnatRuleQueue workqueue.TypedRateLimitingInterface[string]
	delOvnDnatRuleQueue    workqueue.TypedRateLimitingInterface[string]

	providerNetworksLister kubeovnlister.ProviderNetworkLister
	providerNetworkSynced  cache.InformerSynced

	vlansLister     kubeovnlister.VlanLister
	vlanSynced      cache.InformerSynced
	addVlanQueue    workqueue.TypedRateLimitingInterface[string]
	delVlanQueue    workqueue.TypedRateLimitingInterface[string]
	updateVlanQueue workqueue.TypedRateLimitingInterface[string]
	vlanKeyMutex    keymutex.KeyMutex

	namespacesLister  v1.NamespaceLister
	namespacesSynced  cache.InformerSynced
	addNamespaceQueue workqueue.TypedRateLimitingInterface[string]
	nsKeyMutex        keymutex.KeyMutex

	nodesLister     v1.NodeLister
	nodesSynced     cache.InformerSynced
	addNodeQueue    workqueue.TypedRateLimitingInterface[string]
	updateNodeQueue workqueue.TypedRateLimitingInterface[string]
	deleteNodeQueue workqueue.TypedRateLimitingInterface[string]
	nodeKeyMutex    keymutex.KeyMutex

	servicesLister     v1.ServiceLister
	serviceSynced      cache.InformerSynced
	addServiceQueue    workqueue.TypedRateLimitingInterface[string]
	deleteServiceQueue workqueue.TypedRateLimitingInterface[*vpcService]
	updateServiceQueue workqueue.TypedRateLimitingInterface[*updateSvcObject]
	svcKeyMutex        keymutex.KeyMutex

	endpointSlicesLister          discoveryv1.EndpointSliceLister
	endpointSlicesSynced          cache.InformerSynced
	addOrUpdateEndpointSliceQueue workqueue.TypedRateLimitingInterface[string]
	epKeyMutex                    keymutex.KeyMutex

	deploymentsLister appsv1.DeploymentLister
	deploymentsSynced cache.InformerSynced

	npsLister     netv1.NetworkPolicyLister
	npsSynced     cache.InformerSynced
	updateNpQueue workqueue.TypedRateLimitingInterface[string]
	deleteNpQueue workqueue.TypedRateLimitingInterface[string]
	npKeyMutex    keymutex.KeyMutex

	sgsLister          kubeovnlister.SecurityGroupLister
	sgSynced           cache.InformerSynced
	addOrUpdateSgQueue workqueue.TypedRateLimitingInterface[string]
	delSgQueue         workqueue.TypedRateLimitingInterface[string]
	syncSgPortsQueue   workqueue.TypedRateLimitingInterface[string]
	sgKeyMutex         keymutex.KeyMutex

	qosPoliciesLister    kubeovnlister.QoSPolicyLister
	qosPolicySynced      cache.InformerSynced
	addQoSPolicyQueue    workqueue.TypedRateLimitingInterface[string]
	updateQoSPolicyQueue workqueue.TypedRateLimitingInterface[string]
	delQoSPolicyQueue    workqueue.TypedRateLimitingInterface[string]

	configMapsLister v1.ConfigMapLister
	configMapsSynced cache.InformerSynced

	anpsLister     anplister.AdminNetworkPolicyLister
	anpsSynced     cache.InformerSynced
	addAnpQueue    workqueue.TypedRateLimitingInterface[string]
	updateAnpQueue workqueue.TypedRateLimitingInterface[*AdminNetworkPolicyChangedDelta]
	deleteAnpQueue workqueue.TypedRateLimitingInterface[*v1alpha1.AdminNetworkPolicy]
	anpKeyMutex    keymutex.KeyMutex

	dnsNameResolversLister          kubeovnlister.DNSNameResolverLister
	dnsNameResolversSynced          cache.InformerSynced
	addOrUpdateDNSNameResolverQueue workqueue.TypedRateLimitingInterface[string]
	deleteDNSNameResolverQueue      workqueue.TypedRateLimitingInterface[*kubeovnv1.DNSNameResolver]

	banpsLister     anplister.BaselineAdminNetworkPolicyLister
	banpsSynced     cache.InformerSynced
	addBanpQueue    workqueue.TypedRateLimitingInterface[string]
	updateBanpQueue workqueue.TypedRateLimitingInterface[*AdminNetworkPolicyChangedDelta]
	deleteBanpQueue workqueue.TypedRateLimitingInterface[*v1alpha1.BaselineAdminNetworkPolicy]
	banpKeyMutex    keymutex.KeyMutex

	cnpsLister     anplisterv1alpha2.ClusterNetworkPolicyLister
	cnpsSynced     cache.InformerSynced
	addCnpQueue    workqueue.TypedRateLimitingInterface[string]
	updateCnpQueue workqueue.TypedRateLimitingInterface[*ClusterNetworkPolicyChangedDelta]
	deleteCnpQueue workqueue.TypedRateLimitingInterface[*netpolv1alpha2.ClusterNetworkPolicy]
	cnpKeyMutex    keymutex.KeyMutex

	csrLister           certListerv1.CertificateSigningRequestLister
	csrSynced           cache.InformerSynced
	addOrUpdateCsrQueue workqueue.TypedRateLimitingInterface[string]

	addOrUpdateVMIMigrationQueue workqueue.TypedRateLimitingInterface[string]
	deleteVMQueue                workqueue.TypedRateLimitingInterface[string]
	kubevirtInformerFactory      informer.KubeVirtInformerFactory

	netAttachLister          netAttachv1.NetworkAttachmentDefinitionLister
	netAttachSynced          cache.InformerSynced
	netAttachInformerFactory netAttach.SharedInformerFactory

	recorder               record.EventRecorder
	informerFactory        kubeinformers.SharedInformerFactory
	cmInformerFactory      kubeinformers.SharedInformerFactory
	deployInformerFactory  kubeinformers.SharedInformerFactory
	kubeovnInformerFactory kubeovninformer.SharedInformerFactory
	anpInformerFactory     anpinformer.SharedInformerFactory

	// Database health check
	dbFailureCount int

	distributedSubnetNeedSync atomic.Bool
}

func newTypedRateLimitingQueue[T comparable](name string, rateLimiter workqueue.TypedRateLimiter[T]) workqueue.TypedRateLimitingInterface[T] {
	if rateLimiter == nil {
		rateLimiter = workqueue.DefaultTypedControllerRateLimiter[T]()
	}
	return workqueue.NewTypedRateLimitingQueueWithConfig(rateLimiter, workqueue.TypedRateLimitingQueueConfig[T]{Name: name})
}

// Run creates and runs a new ovn controller
func Run(ctx context.Context, config *Configuration) {
	klog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcasterWithCorrelatorOptions(record.CorrelatorOptions{BurstSize: 100})
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: config.KubeFactoryClient.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})
	custCrdRateLimiter := workqueue.NewTypedMaxOfRateLimiter(
		workqueue.NewTypedItemExponentialFailureRateLimiter[string](time.Duration(config.CustCrdRetryMinDelay)*time.Second, time.Duration(config.CustCrdRetryMaxDelay)*time.Second),
		&workqueue.TypedBucketRateLimiter[string]{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
	)

	selector, err := labels.Parse(util.VpcEgressGatewayLabel)
	if err != nil {
		util.LogFatalAndExit(err, "failed to create label selector for vpc egress gateway workload")
	}

	informerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(config.KubeFactoryClient, 0,
		kubeinformers.WithTweakListOptions(func(listOption *metav1.ListOptions) {
			listOption.AllowWatchBookmarks = true
		}))
	cmInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(config.KubeFactoryClient, 0,
		kubeinformers.WithTweakListOptions(func(listOption *metav1.ListOptions) {
			listOption.AllowWatchBookmarks = true
		}), kubeinformers.WithNamespace(config.PodNamespace))
	// deployment informer used to list/watch vpc egress gateway workloads
	deployInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(config.KubeFactoryClient, 0,
		kubeinformers.WithTweakListOptions(func(listOption *metav1.ListOptions) {
			listOption.AllowWatchBookmarks = true
			listOption.LabelSelector = selector.String()
		}))
	kubeovnInformerFactory := kubeovninformer.NewSharedInformerFactoryWithOptions(config.KubeOvnFactoryClient, 0,
		kubeovninformer.WithTweakListOptions(func(listOption *metav1.ListOptions) {
			listOption.AllowWatchBookmarks = true
		}))
	anpInformerFactory := anpinformer.NewSharedInformerFactoryWithOptions(config.AnpClient, 0,
		anpinformer.WithTweakListOptions(func(listOption *metav1.ListOptions) {
			listOption.AllowWatchBookmarks = true
		}))

	attachNetInformerFactory := netAttach.NewSharedInformerFactoryWithOptions(config.AttachNetClient, 0,
		netAttach.WithTweakListOptions(func(listOption *metav1.ListOptions) {
			listOption.AllowWatchBookmarks = true
		}),
	)

	kubevirtInformerFactory := informer.NewKubeVirtInformerFactory(config.KubevirtClient.RestClient(), config.KubevirtClient, nil, util.KubevirtNamespace)

	vpcInformer := kubeovnInformerFactory.Kubeovn().V1().Vpcs()
	vpcNatGatewayInformer := kubeovnInformerFactory.Kubeovn().V1().VpcNatGateways()
	vpcEgressGatewayInformer := kubeovnInformerFactory.Kubeovn().V1().VpcEgressGateways()
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
	endpointSliceInformer := informerFactory.Discovery().V1().EndpointSlices()
	deploymentInformer := deployInformerFactory.Apps().V1().Deployments()
	qosPolicyInformer := kubeovnInformerFactory.Kubeovn().V1().QoSPolicies()
	configMapInformer := cmInformerFactory.Core().V1().ConfigMaps()
	npInformer := informerFactory.Networking().V1().NetworkPolicies()
	switchLBRuleInformer := kubeovnInformerFactory.Kubeovn().V1().SwitchLBRules()
	vpcDNSInformer := kubeovnInformerFactory.Kubeovn().V1().VpcDnses()
	ovnEipInformer := kubeovnInformerFactory.Kubeovn().V1().OvnEips()
	ovnFipInformer := kubeovnInformerFactory.Kubeovn().V1().OvnFips()
	ovnSnatRuleInformer := kubeovnInformerFactory.Kubeovn().V1().OvnSnatRules()
	ovnDnatRuleInformer := kubeovnInformerFactory.Kubeovn().V1().OvnDnatRules()
	anpInformer := anpInformerFactory.Policy().V1alpha1().AdminNetworkPolicies()
	banpInformer := anpInformerFactory.Policy().V1alpha1().BaselineAdminNetworkPolicies()
	cnpInformer := anpInformerFactory.Policy().V1alpha2().ClusterNetworkPolicies()
	dnsNameResolverInformer := kubeovnInformerFactory.Kubeovn().V1().DNSNameResolvers()
	csrInformer := informerFactory.Certificates().V1().CertificateSigningRequests()
	netAttachInformer := attachNetInformerFactory.K8sCniCncfIo().V1().NetworkAttachmentDefinitions()

	numKeyLocks := max(runtime.NumCPU()*2, config.WorkerNum*2)
	controller := &Controller{
		config:             config,
		deletingPodObjMap:  xsync.NewMap[string, *corev1.Pod](),
		deletingNodeObjMap: xsync.NewMap[string, *corev1.Node](),
		ipam:               ovnipam.NewIPAM(),
		namedPort:          NewNamedPort(),

		vpcsLister:           vpcInformer.Lister(),
		vpcSynced:            vpcInformer.Informer().HasSynced,
		addOrUpdateVpcQueue:  newTypedRateLimitingQueue[string]("AddOrUpdateVpc", nil),
		vpcLastPoliciesMap:   xsync.NewMap[string, string](),
		delVpcQueue:          newTypedRateLimitingQueue[*kubeovnv1.Vpc]("DeleteVpc", nil),
		updateVpcStatusQueue: newTypedRateLimitingQueue[string]("UpdateVpcStatus", nil),
		vpcKeyMutex:          keymutex.NewHashed(numKeyLocks),

		vpcNatGatewayLister:              vpcNatGatewayInformer.Lister(),
		vpcNatGatewaySynced:              vpcNatGatewayInformer.Informer().HasSynced,
		addOrUpdateVpcNatGatewayQueue:    newTypedRateLimitingQueue("AddOrUpdateVpcNatGw", custCrdRateLimiter),
		initVpcNatGatewayQueue:           newTypedRateLimitingQueue("InitVpcNatGw", custCrdRateLimiter),
		delVpcNatGatewayQueue:            newTypedRateLimitingQueue("DeleteVpcNatGw", custCrdRateLimiter),
		updateVpcEipQueue:                newTypedRateLimitingQueue("UpdateVpcEip", custCrdRateLimiter),
		updateVpcFloatingIPQueue:         newTypedRateLimitingQueue("UpdateVpcFloatingIp", custCrdRateLimiter),
		updateVpcDnatQueue:               newTypedRateLimitingQueue("UpdateVpcDnat", custCrdRateLimiter),
		updateVpcSnatQueue:               newTypedRateLimitingQueue("UpdateVpcSnat", custCrdRateLimiter),
		updateVpcSubnetQueue:             newTypedRateLimitingQueue("UpdateVpcSubnet", custCrdRateLimiter),
		vpcNatGwKeyMutex:                 keymutex.NewHashed(numKeyLocks),
		vpcNatGwExecKeyMutex:             keymutex.NewHashed(numKeyLocks),
		vpcEgressGatewayLister:           vpcEgressGatewayInformer.Lister(),
		vpcEgressGatewaySynced:           vpcEgressGatewayInformer.Informer().HasSynced,
		addOrUpdateVpcEgressGatewayQueue: newTypedRateLimitingQueue("AddOrUpdateVpcEgressGateway", custCrdRateLimiter),
		delVpcEgressGatewayQueue:         newTypedRateLimitingQueue("DeleteVpcEgressGateway", custCrdRateLimiter),
		vpcEgressGatewayKeyMutex:         keymutex.NewHashed(numKeyLocks),

		subnetsLister:           subnetInformer.Lister(),
		subnetSynced:            subnetInformer.Informer().HasSynced,
		addOrUpdateSubnetQueue:  newTypedRateLimitingQueue[string]("AddSubnet", nil),
		deleteSubnetQueue:       newTypedRateLimitingQueue[*kubeovnv1.Subnet]("DeleteSubnet", nil),
		updateSubnetStatusQueue: newTypedRateLimitingQueue[string]("UpdateSubnetStatus", nil),
		syncVirtualPortsQueue:   newTypedRateLimitingQueue[string]("SyncVirtualPort", nil),
		subnetKeyMutex:          keymutex.NewHashed(numKeyLocks),

		ippoolLister:            ippoolInformer.Lister(),
		ippoolSynced:            ippoolInformer.Informer().HasSynced,
		addOrUpdateIPPoolQueue:  newTypedRateLimitingQueue[string]("AddIPPool", nil),
		updateIPPoolStatusQueue: newTypedRateLimitingQueue[string]("UpdateIPPoolStatus", nil),
		deleteIPPoolQueue:       newTypedRateLimitingQueue[*kubeovnv1.IPPool]("DeleteIPPool", nil),
		ippoolKeyMutex:          keymutex.NewHashed(numKeyLocks),

		ipsLister:     ipInformer.Lister(),
		ipSynced:      ipInformer.Informer().HasSynced,
		addIPQueue:    newTypedRateLimitingQueue[string]("AddIP", nil),
		updateIPQueue: newTypedRateLimitingQueue[string]("UpdateIP", nil),
		delIPQueue:    newTypedRateLimitingQueue[*kubeovnv1.IP]("DeleteIP", nil),

		virtualIpsLister:          virtualIPInformer.Lister(),
		virtualIpsSynced:          virtualIPInformer.Informer().HasSynced,
		addVirtualIPQueue:         newTypedRateLimitingQueue[string]("AddVirtualIP", nil),
		updateVirtualIPQueue:      newTypedRateLimitingQueue[string]("UpdateVirtualIP", nil),
		updateVirtualParentsQueue: newTypedRateLimitingQueue[string]("UpdateVirtualParents", nil),
		delVirtualIPQueue:         newTypedRateLimitingQueue[*kubeovnv1.Vip]("DeleteVirtualIP", nil),

		iptablesEipsLister:     iptablesEipInformer.Lister(),
		iptablesEipSynced:      iptablesEipInformer.Informer().HasSynced,
		addIptablesEipQueue:    newTypedRateLimitingQueue("AddIptablesEip", custCrdRateLimiter),
		updateIptablesEipQueue: newTypedRateLimitingQueue("UpdateIptablesEip", custCrdRateLimiter),
		resetIptablesEipQueue:  newTypedRateLimitingQueue("ResetIptablesEip", custCrdRateLimiter),
		delIptablesEipQueue:    newTypedRateLimitingQueue[*kubeovnv1.IptablesEIP]("DeleteIptablesEip", nil),

		iptablesFipsLister:     iptablesFipInformer.Lister(),
		iptablesFipSynced:      iptablesFipInformer.Informer().HasSynced,
		addIptablesFipQueue:    newTypedRateLimitingQueue("AddIptablesFip", custCrdRateLimiter),
		updateIptablesFipQueue: newTypedRateLimitingQueue("UpdateIptablesFip", custCrdRateLimiter),
		delIptablesFipQueue:    newTypedRateLimitingQueue("DeleteIptablesFip", custCrdRateLimiter),

		iptablesDnatRulesLister:     iptablesDnatRuleInformer.Lister(),
		iptablesDnatRuleSynced:      iptablesDnatRuleInformer.Informer().HasSynced,
		addIptablesDnatRuleQueue:    newTypedRateLimitingQueue("AddIptablesDnatRule", custCrdRateLimiter),
		updateIptablesDnatRuleQueue: newTypedRateLimitingQueue("UpdateIptablesDnatRule", custCrdRateLimiter),
		delIptablesDnatRuleQueue:    newTypedRateLimitingQueue("DeleteIptablesDnatRule", custCrdRateLimiter),

		iptablesSnatRulesLister:     iptablesSnatRuleInformer.Lister(),
		iptablesSnatRuleSynced:      iptablesSnatRuleInformer.Informer().HasSynced,
		addIptablesSnatRuleQueue:    newTypedRateLimitingQueue("AddIptablesSnatRule", custCrdRateLimiter),
		updateIptablesSnatRuleQueue: newTypedRateLimitingQueue("UpdateIptablesSnatRule", custCrdRateLimiter),
		delIptablesSnatRuleQueue:    newTypedRateLimitingQueue("DeleteIptablesSnatRule", custCrdRateLimiter),

		vlansLister:     vlanInformer.Lister(),
		vlanSynced:      vlanInformer.Informer().HasSynced,
		addVlanQueue:    newTypedRateLimitingQueue[string]("AddVlan", nil),
		delVlanQueue:    newTypedRateLimitingQueue[string]("DeleteVlan", nil),
		updateVlanQueue: newTypedRateLimitingQueue[string]("UpdateVlan", nil),
		vlanKeyMutex:    keymutex.NewHashed(numKeyLocks),

		providerNetworksLister: providerNetworkInformer.Lister(),
		providerNetworkSynced:  providerNetworkInformer.Informer().HasSynced,

		podsLister:          podInformer.Lister(),
		podsSynced:          podInformer.Informer().HasSynced,
		addOrUpdatePodQueue: newTypedRateLimitingQueue[string]("AddOrUpdatePod", nil),
		deletePodQueue: workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.DefaultTypedControllerRateLimiter[string](),
			workqueue.TypedRateLimitingQueueConfig[string]{
				Name:          "DeletePod",
				DelayingQueue: workqueue.NewTypedDelayingQueue[string](),
			},
		),
		updatePodSecurityQueue: newTypedRateLimitingQueue[string]("UpdatePodSecurity", nil),
		podKeyMutex:            keymutex.NewHashed(numKeyLocks),

		namespacesLister:  namespaceInformer.Lister(),
		namespacesSynced:  namespaceInformer.Informer().HasSynced,
		addNamespaceQueue: newTypedRateLimitingQueue[string]("AddNamespace", nil),
		nsKeyMutex:        keymutex.NewHashed(numKeyLocks),

		nodesLister:     nodeInformer.Lister(),
		nodesSynced:     nodeInformer.Informer().HasSynced,
		addNodeQueue:    newTypedRateLimitingQueue[string]("AddNode", nil),
		updateNodeQueue: newTypedRateLimitingQueue[string]("UpdateNode", nil),
		deleteNodeQueue: newTypedRateLimitingQueue[string]("DeleteNode", nil),
		nodeKeyMutex:    keymutex.NewHashed(numKeyLocks),

		servicesLister:     serviceInformer.Lister(),
		serviceSynced:      serviceInformer.Informer().HasSynced,
		addServiceQueue:    newTypedRateLimitingQueue[string]("AddService", nil),
		deleteServiceQueue: newTypedRateLimitingQueue[*vpcService]("DeleteService", nil),
		updateServiceQueue: newTypedRateLimitingQueue[*updateSvcObject]("UpdateService", nil),
		svcKeyMutex:        keymutex.NewHashed(numKeyLocks),

		endpointSlicesLister:          endpointSliceInformer.Lister(),
		endpointSlicesSynced:          endpointSliceInformer.Informer().HasSynced,
		addOrUpdateEndpointSliceQueue: newTypedRateLimitingQueue[string]("UpdateEndpointSlice", nil),
		epKeyMutex:                    keymutex.NewHashed(numKeyLocks),

		deploymentsLister: deploymentInformer.Lister(),
		deploymentsSynced: deploymentInformer.Informer().HasSynced,

		qosPoliciesLister:    qosPolicyInformer.Lister(),
		qosPolicySynced:      qosPolicyInformer.Informer().HasSynced,
		addQoSPolicyQueue:    newTypedRateLimitingQueue("AddQoSPolicy", custCrdRateLimiter),
		updateQoSPolicyQueue: newTypedRateLimitingQueue("UpdateQoSPolicy", custCrdRateLimiter),
		delQoSPolicyQueue:    newTypedRateLimitingQueue("DeleteQoSPolicy", custCrdRateLimiter),

		configMapsLister: configMapInformer.Lister(),
		configMapsSynced: configMapInformer.Informer().HasSynced,

		sgKeyMutex:         keymutex.NewHashed(numKeyLocks),
		sgsLister:          sgInformer.Lister(),
		sgSynced:           sgInformer.Informer().HasSynced,
		addOrUpdateSgQueue: newTypedRateLimitingQueue[string]("UpdateSecurityGroup", nil),
		delSgQueue:         newTypedRateLimitingQueue[string]("DeleteSecurityGroup", nil),
		syncSgPortsQueue:   newTypedRateLimitingQueue[string]("SyncSecurityGroupPorts", nil),

		ovnEipsLister:     ovnEipInformer.Lister(),
		ovnEipSynced:      ovnEipInformer.Informer().HasSynced,
		addOvnEipQueue:    newTypedRateLimitingQueue("AddOvnEip", custCrdRateLimiter),
		updateOvnEipQueue: newTypedRateLimitingQueue("UpdateOvnEip", custCrdRateLimiter),
		resetOvnEipQueue:  newTypedRateLimitingQueue("ResetOvnEip", custCrdRateLimiter),
		delOvnEipQueue:    newTypedRateLimitingQueue[*kubeovnv1.OvnEip]("DeleteOvnEip", nil),

		ovnFipsLister:     ovnFipInformer.Lister(),
		ovnFipSynced:      ovnFipInformer.Informer().HasSynced,
		addOvnFipQueue:    newTypedRateLimitingQueue("AddOvnFip", custCrdRateLimiter),
		updateOvnFipQueue: newTypedRateLimitingQueue("UpdateOvnFip", custCrdRateLimiter),
		delOvnFipQueue:    newTypedRateLimitingQueue("DeleteOvnFip", custCrdRateLimiter),

		ovnSnatRulesLister:     ovnSnatRuleInformer.Lister(),
		ovnSnatRuleSynced:      ovnSnatRuleInformer.Informer().HasSynced,
		addOvnSnatRuleQueue:    newTypedRateLimitingQueue("AddOvnSnatRule", custCrdRateLimiter),
		updateOvnSnatRuleQueue: newTypedRateLimitingQueue("UpdateOvnSnatRule", custCrdRateLimiter),
		delOvnSnatRuleQueue:    newTypedRateLimitingQueue("DeleteOvnSnatRule", custCrdRateLimiter),

		ovnDnatRulesLister:     ovnDnatRuleInformer.Lister(),
		ovnDnatRuleSynced:      ovnDnatRuleInformer.Informer().HasSynced,
		addOvnDnatRuleQueue:    newTypedRateLimitingQueue("AddOvnDnatRule", custCrdRateLimiter),
		updateOvnDnatRuleQueue: newTypedRateLimitingQueue("UpdateOvnDnatRule", custCrdRateLimiter),
		delOvnDnatRuleQueue:    newTypedRateLimitingQueue("DeleteOvnDnatRule", custCrdRateLimiter),

		csrLister:           csrInformer.Lister(),
		csrSynced:           csrInformer.Informer().HasSynced,
		addOrUpdateCsrQueue: newTypedRateLimitingQueue("AddOrUpdateCSR", custCrdRateLimiter),

		addOrUpdateVMIMigrationQueue: newTypedRateLimitingQueue[string]("AddOrUpdateVMIMigration", nil),
		deleteVMQueue:                newTypedRateLimitingQueue[string]("DeleteVM", nil),
		kubevirtInformerFactory:      kubevirtInformerFactory,

		netAttachLister:          netAttachInformer.Lister(),
		netAttachSynced:          netAttachInformer.Informer().HasSynced,
		netAttachInformerFactory: attachNetInformerFactory,

		recorder:               recorder,
		informerFactory:        informerFactory,
		cmInformerFactory:      cmInformerFactory,
		deployInformerFactory:  deployInformerFactory,
		kubeovnInformerFactory: kubeovnInformerFactory,
		anpInformerFactory:     anpInformerFactory,
	}

	if controller.OVNNbClient, err = ovs.NewOvnNbClient(
		config.OvnNbAddr,
		config.OvnTimeout,
		config.OvsDbConnectTimeout,
		config.OvsDbInactivityTimeout,
		config.OvsDbConnectMaxRetry,
	); err != nil {
		util.LogFatalAndExit(err, "failed to create ovn nb client")
	}
	if controller.OVNSbClient, err = ovs.NewOvnSbClient(
		config.OvnSbAddr,
		config.OvnTimeout,
		config.OvsDbConnectTimeout,
		config.OvsDbInactivityTimeout,
		config.OvsDbConnectMaxRetry,
	); err != nil {
		util.LogFatalAndExit(err, "failed to create ovn sb client")
	}
	if config.EnableLb {
		controller.switchLBRuleLister = switchLBRuleInformer.Lister()
		controller.switchLBRuleSynced = switchLBRuleInformer.Informer().HasSynced
		controller.addSwitchLBRuleQueue = newTypedRateLimitingQueue("AddSwitchLBRule", custCrdRateLimiter)
		controller.delSwitchLBRuleQueue = newTypedRateLimitingQueue(
			"DeleteSwitchLBRule",
			workqueue.NewTypedMaxOfRateLimiter(
				workqueue.NewTypedItemExponentialFailureRateLimiter[*SlrInfo](time.Duration(config.CustCrdRetryMinDelay)*time.Second, time.Duration(config.CustCrdRetryMaxDelay)*time.Second),
				&workqueue.TypedBucketRateLimiter[*SlrInfo]{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
			),
		)
		controller.updateSwitchLBRuleQueue = newTypedRateLimitingQueue(
			"UpdateSwitchLBRule",
			workqueue.NewTypedMaxOfRateLimiter(
				workqueue.NewTypedItemExponentialFailureRateLimiter[*SlrInfo](time.Duration(config.CustCrdRetryMinDelay)*time.Second, time.Duration(config.CustCrdRetryMaxDelay)*time.Second),
				&workqueue.TypedBucketRateLimiter[*SlrInfo]{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
			),
		)

		controller.vpcDNSLister = vpcDNSInformer.Lister()
		controller.vpcDNSSynced = vpcDNSInformer.Informer().HasSynced
		controller.addOrUpdateVpcDNSQueue = newTypedRateLimitingQueue("AddOrUpdateVpcDns", custCrdRateLimiter)
		controller.delVpcDNSQueue = newTypedRateLimitingQueue("DeleteVpcDns", custCrdRateLimiter)
	}

	if config.EnableNP {
		controller.npsLister = npInformer.Lister()
		controller.npsSynced = npInformer.Informer().HasSynced
		controller.updateNpQueue = newTypedRateLimitingQueue[string]("UpdateNetworkPolicy", nil)
		controller.deleteNpQueue = newTypedRateLimitingQueue[string]("DeleteNetworkPolicy", nil)
		controller.npKeyMutex = keymutex.NewHashed(numKeyLocks)
	}

	if config.EnableANP {
		controller.anpsLister = anpInformer.Lister()
		controller.anpsSynced = anpInformer.Informer().HasSynced
		controller.addAnpQueue = newTypedRateLimitingQueue[string]("AddAdminNetworkPolicy", nil)
		controller.updateAnpQueue = newTypedRateLimitingQueue[*AdminNetworkPolicyChangedDelta]("UpdateAdminNetworkPolicy", nil)
		controller.deleteAnpQueue = newTypedRateLimitingQueue[*v1alpha1.AdminNetworkPolicy]("DeleteAdminNetworkPolicy", nil)
		controller.anpKeyMutex = keymutex.NewHashed(numKeyLocks)

		controller.banpsLister = banpInformer.Lister()
		controller.banpsSynced = banpInformer.Informer().HasSynced
		controller.addBanpQueue = newTypedRateLimitingQueue[string]("AddBaseAdminNetworkPolicy", nil)
		controller.updateBanpQueue = newTypedRateLimitingQueue[*AdminNetworkPolicyChangedDelta]("UpdateBaseAdminNetworkPolicy", nil)
		controller.deleteBanpQueue = newTypedRateLimitingQueue[*v1alpha1.BaselineAdminNetworkPolicy]("DeleteBaseAdminNetworkPolicy", nil)
		controller.banpKeyMutex = keymutex.NewHashed(numKeyLocks)

		controller.cnpsLister = cnpInformer.Lister()
		controller.cnpsSynced = cnpInformer.Informer().HasSynced
		controller.addCnpQueue = newTypedRateLimitingQueue[string]("AddClusterNetworkPolicy", nil)
		controller.updateCnpQueue = newTypedRateLimitingQueue[*ClusterNetworkPolicyChangedDelta]("UpdateClusterNetworkPolicy", nil)
		controller.deleteCnpQueue = newTypedRateLimitingQueue[*netpolv1alpha2.ClusterNetworkPolicy]("DeleteClusterNetworkPolicy", nil)
		controller.cnpKeyMutex = keymutex.NewHashed(numKeyLocks)
	}

	if config.EnableDNSNameResolver {
		controller.dnsNameResolversLister = dnsNameResolverInformer.Lister()
		controller.dnsNameResolversSynced = dnsNameResolverInformer.Informer().HasSynced
		controller.addOrUpdateDNSNameResolverQueue = newTypedRateLimitingQueue[string]("AddOrUpdateDNSNameResolver", nil)
		controller.deleteDNSNameResolverQueue = newTypedRateLimitingQueue[*kubeovnv1.DNSNameResolver]("DeleteDNSNameResolver", nil)
	}

	defer controller.shutdown()
	klog.Info("Starting OVN controller")

	// Start and sync NAD informer first, as many resources depend on NAD cache
	// NAD CRD is optional, so we check if it exists before starting the informer
	controller.StartNetAttachInformerFactory(ctx)

	// Wait for the caches to be synced before starting workers
	controller.informerFactory.Start(ctx.Done())
	controller.cmInformerFactory.Start(ctx.Done())
	controller.deployInformerFactory.Start(ctx.Done())
	controller.kubeovnInformerFactory.Start(ctx.Done())
	controller.anpInformerFactory.Start(ctx.Done())
	controller.StartKubevirtInformerFactory(ctx, kubevirtInformerFactory)

	klog.Info("Waiting for informer caches to sync")
	cacheSyncs := []cache.InformerSynced{
		controller.vpcNatGatewaySynced, controller.vpcEgressGatewaySynced,
		controller.vpcSynced, controller.subnetSynced,
		controller.ipSynced, controller.virtualIpsSynced, controller.iptablesEipSynced,
		controller.iptablesFipSynced, controller.iptablesDnatRuleSynced, controller.iptablesSnatRuleSynced,
		controller.vlanSynced, controller.podsSynced, controller.namespacesSynced, controller.nodesSynced,
		controller.serviceSynced, controller.endpointSlicesSynced, controller.deploymentsSynced, controller.configMapsSynced,
		controller.ovnEipSynced, controller.ovnFipSynced, controller.ovnSnatRuleSynced,
		controller.ovnDnatRuleSynced,
	}
	if controller.config.EnableLb {
		cacheSyncs = append(cacheSyncs, controller.switchLBRuleSynced, controller.vpcDNSSynced)
	}
	if controller.config.EnableNP {
		cacheSyncs = append(cacheSyncs, controller.npsSynced)
	}
	if controller.config.EnableANP {
		cacheSyncs = append(cacheSyncs, controller.anpsSynced, controller.banpsSynced, controller.cnpsSynced)
	}
	if controller.config.EnableDNSNameResolver {
		cacheSyncs = append(cacheSyncs, controller.dnsNameResolversSynced)
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

	if _, err = endpointSliceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddEndpointSlice,
		UpdateFunc: controller.enqueueUpdateEndpointSlice,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add endpoint slice event handler")
	}

	if _, err = deploymentInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddDeployment,
		UpdateFunc: controller.enqueueUpdateDeployment,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add deployment event handler")
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

	if _, err = vpcEgressGatewayInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueAddVpcEgressGateway,
		UpdateFunc: controller.enqueueUpdateVpcEgressGateway,
		DeleteFunc: controller.enqueueDeleteVpcEgressGateway,
	}); err != nil {
		util.LogFatalAndExit(err, "failed to add vpc egress gateway event handler")
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
		util.LogFatalAndExit(err, "failed to add ovn eip event handler")
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

	if config.EnableANP {
		if _, err = anpInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    controller.enqueueAddAnp,
			UpdateFunc: controller.enqueueUpdateAnp,
			DeleteFunc: controller.enqueueDeleteAnp,
		}); err != nil {
			util.LogFatalAndExit(err, "failed to add admin network policy event handler")
		}

		if _, err = banpInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    controller.enqueueAddBanp,
			UpdateFunc: controller.enqueueUpdateBanp,
			DeleteFunc: controller.enqueueDeleteBanp,
		}); err != nil {
			util.LogFatalAndExit(err, "failed to add baseline admin network policy event handler")
		}

		if _, err = cnpInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    controller.enqueueAddCnp,
			UpdateFunc: controller.enqueueUpdateCnp,
			DeleteFunc: controller.enqueueDeleteCnp,
		}); err != nil {
			util.LogFatalAndExit(err, "failed to add cluster network policy event handler")
		}

		maxPriorityPerMap := util.CnpMaxPriority + 1
		controller.anpPrioNameMap = make(map[int32]string, maxPriorityPerMap)
		controller.anpNamePrioMap = make(map[string]int32, maxPriorityPerMap)
		controller.bnpPrioNameMap = make(map[int32]string, maxPriorityPerMap)
		controller.bnpNamePrioMap = make(map[string]int32, maxPriorityPerMap)
	}

	if config.EnableDNSNameResolver {
		if _, err = dnsNameResolverInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    controller.enqueueAddDNSNameResolver,
			UpdateFunc: controller.enqueueUpdateDNSNameResolver,
			DeleteFunc: controller.enqueueDeleteDNSNameResolver,
		}); err != nil {
			util.LogFatalAndExit(err, "failed to add dns name resolver event handler")
		}
	}

	if config.EnableOVNIPSec {
		if _, err = csrInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    controller.enqueueAddCsr,
			UpdateFunc: controller.enqueueUpdateCsr,
			// no need to add delete func for csr
		}); err != nil {
			util.LogFatalAndExit(err, "failed to add csr event handler")
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

	if err := c.OVNNbClient.SetNodeLocalDNSIP(strings.Join(c.config.NodeLocalDNSIPs, ",")); err != nil {
		util.LogFatalAndExit(err, "failed to set NB_Global option node_local_dns_ip")
	}

	if err := c.OVNNbClient.SetSkipConntrackCidrs(c.config.SkipConntrackDstCidrs); err != nil {
		util.LogFatalAndExit(err, "failed to set NB_Global option skip_conntrack_ipcidrs")
	}

	if err := c.OVNNbClient.SetOVNIPSec(c.config.EnableOVNIPSec); err != nil {
		util.LogFatalAndExit(err, "failed to set NB_Global ipsec")
	}

	if err := c.InitOVN(); err != nil {
		util.LogFatalAndExit(err, "failed to initialize ovn resources")
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

	if c.config.EnableOVNIPSec && !c.config.CertManagerIPSecCert {
		if err := c.InitDefaultOVNIPsecCA(); err != nil {
			util.LogFatalAndExit(err, "failed to init ovn ipsec CA")
		}
	}

	// start workers to do all the network operations
	c.startWorkers(ctx)

	c.initResourceOnce()
	<-ctx.Done()
	klog.Info("Shutting down workers")
}

func (c *Controller) dbStatus() {
	const maxFailures = 5

	done := make(chan error, 2)
	go func() {
		done <- c.OVNNbClient.Echo(context.Background())
	}()
	go func() {
		done <- c.OVNSbClient.Echo(context.Background())
	}()

	resultsReceived := 0
	timeout := time.After(time.Duration(c.config.OvnTimeout) * time.Second)

	for resultsReceived < 2 {
		select {
		case err := <-done:
			resultsReceived++
			if err != nil {
				c.dbFailureCount++
				klog.Errorf("OVN database echo failed (%d/%d): %v", c.dbFailureCount, maxFailures, err)
				if c.dbFailureCount >= maxFailures {
					util.LogFatalAndExit(err, "OVN database connection failed after %d attempts", maxFailures)
				}
				return
			}
		case <-timeout:
			c.dbFailureCount++
			klog.Errorf("OVN database echo timeout (%d/%d) after %ds", c.dbFailureCount, maxFailures, c.config.OvnTimeout)
			if c.dbFailureCount >= maxFailures {
				util.LogFatalAndExit(nil, "OVN database connection timeout after %d attempts", maxFailures)
			}
			return
		}
	}

	if c.dbFailureCount > 0 {
		klog.Infof("OVN database connection recovered after %d failures", c.dbFailureCount)
		c.dbFailureCount = 0
	}
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
	c.addOrUpdateEndpointSliceQueue.ShutDown()

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

	c.addOrUpdateVpcEgressGatewayQueue.ShutDown()
	c.delVpcEgressGatewayQueue.ShutDown()

	if c.config.EnableLb {
		c.addSwitchLBRuleQueue.ShutDown()
		c.delSwitchLBRuleQueue.ShutDown()
		c.updateSwitchLBRuleQueue.ShutDown()

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
	if c.config.EnableANP {
		c.addAnpQueue.ShutDown()
		c.updateAnpQueue.ShutDown()
		c.deleteAnpQueue.ShutDown()

		c.addBanpQueue.ShutDown()
		c.updateBanpQueue.ShutDown()
		c.deleteBanpQueue.ShutDown()

		c.addCnpQueue.ShutDown()
		c.updateCnpQueue.ShutDown()
		c.deleteCnpQueue.ShutDown()
	}

	if c.config.EnableDNSNameResolver {
		c.addOrUpdateDNSNameResolverQueue.ShutDown()
		c.deleteDNSNameResolverQueue.ShutDown()
	}

	c.addOrUpdateSgQueue.ShutDown()
	c.delSgQueue.ShutDown()
	c.syncSgPortsQueue.ShutDown()

	c.addOrUpdateCsrQueue.ShutDown()

	if c.config.EnableLiveMigrationOptimize {
		c.addOrUpdateVMIMigrationQueue.ShutDown()
	}
}

func (c *Controller) startWorkers(ctx context.Context) {
	klog.Info("Starting workers")

	go wait.Until(runWorker("add/update vpc", c.addOrUpdateVpcQueue, c.handleAddOrUpdateVpc), time.Second, ctx.Done())
	go wait.Until(runWorker("delete vpc", c.delVpcQueue, c.handleDelVpc), time.Second, ctx.Done())
	go wait.Until(runWorker("update status of vpc", c.updateVpcStatusQueue, c.handleUpdateVpcStatus), time.Second, ctx.Done())

	go wait.Until(runWorker("add/update vpc nat gateway", c.addOrUpdateVpcNatGatewayQueue, c.handleAddOrUpdateVpcNatGw), time.Second, ctx.Done())
	go wait.Until(runWorker("init vpc nat gateway", c.initVpcNatGatewayQueue, c.handleInitVpcNatGw), time.Second, ctx.Done())
	go wait.Until(runWorker("delete vpc nat gateway", c.delVpcNatGatewayQueue, c.handleDelVpcNatGw), time.Second, ctx.Done())
	go wait.Until(runWorker("add/update vpc egress gateway", c.addOrUpdateVpcEgressGatewayQueue, c.handleAddOrUpdateVpcEgressGateway), time.Second, ctx.Done())
	go wait.Until(runWorker("delete vpc egress gateway", c.delVpcEgressGatewayQueue, c.handleDelVpcEgressGateway), time.Second, ctx.Done())
	go wait.Until(runWorker("update fip for vpc nat gateway", c.updateVpcFloatingIPQueue, c.handleUpdateVpcFloatingIP), time.Second, ctx.Done())
	go wait.Until(runWorker("update eip for vpc nat gateway", c.updateVpcEipQueue, c.handleUpdateVpcEip), time.Second, ctx.Done())
	go wait.Until(runWorker("update dnat for vpc nat gateway", c.updateVpcDnatQueue, c.handleUpdateVpcDnat), time.Second, ctx.Done())
	go wait.Until(runWorker("update snat for vpc nat gateway", c.updateVpcSnatQueue, c.handleUpdateVpcSnat), time.Second, ctx.Done())
	go wait.Until(runWorker("update subnet route for vpc nat gateway", c.updateVpcSubnetQueue, c.handleUpdateNatGwSubnetRoute), time.Second, ctx.Done())
	go wait.Until(runWorker("add/update csr", c.addOrUpdateCsrQueue, c.handleAddOrUpdateCsr), time.Second, ctx.Done())
	// add default and join subnet and wait them ready
	for range c.config.WorkerNum {
		go wait.Until(runWorker("add/update subnet", c.addOrUpdateSubnetQueue, c.handleAddOrUpdateSubnet), time.Second, ctx.Done())
	}
	go wait.Until(runWorker("add/update ippool", c.addOrUpdateIPPoolQueue, c.handleAddOrUpdateIPPool), time.Second, ctx.Done())
	go wait.Until(runWorker("add vlan", c.addVlanQueue, c.handleAddVlan), time.Second, ctx.Done())
	go wait.Until(runWorker("add namespace", c.addNamespaceQueue, c.handleAddNamespace), time.Second, ctx.Done())
	err := wait.PollUntilContextCancel(ctx, 3*time.Second, true, func(_ context.Context) (done bool, err error) {
		subnets := []string{c.config.DefaultLogicalSwitch, c.config.NodeSwitch}
		klog.Infof("wait for subnets %v ready", subnets)

		return c.allSubnetReady(subnets...)
	})
	if err != nil {
		klog.Fatalf("wait default and join subnet ready, error: %v", err)
	}

	go wait.Until(runWorker("add/update security group", c.addOrUpdateSgQueue, func(key string) error { return c.handleAddOrUpdateSg(key, false) }), time.Second, ctx.Done())
	go wait.Until(runWorker("delete security group", c.delSgQueue, c.handleDeleteSg), time.Second, ctx.Done())
	go wait.Until(runWorker("ports for security group", c.syncSgPortsQueue, c.syncSgLogicalPort), time.Second, ctx.Done())

	// run node worker before handle any pods
	for range c.config.WorkerNum {
		go wait.Until(runWorker("add node", c.addNodeQueue, c.handleAddNode), time.Second, ctx.Done())
		go wait.Until(runWorker("update node", c.updateNodeQueue, c.handleUpdateNode), time.Second, ctx.Done())
		go wait.Until(runWorker("delete node", c.deleteNodeQueue, c.handleDeleteNode), time.Second, ctx.Done())
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

	if c.config.EnableLb {
		go wait.Until(runWorker("add service", c.addServiceQueue, c.handleAddService), time.Second, ctx.Done())
		// run in a single worker to avoid delete the last vip, which will lead ovn to delete the loadbalancer
		go wait.Until(runWorker("delete service", c.deleteServiceQueue, c.handleDeleteService), time.Second, ctx.Done())

		go wait.Until(runWorker("add/update switch lb rule", c.addSwitchLBRuleQueue, c.handleAddOrUpdateSwitchLBRule), time.Second, ctx.Done())
		go wait.Until(runWorker("delete switch lb rule", c.delSwitchLBRuleQueue, c.handleDelSwitchLBRule), time.Second, ctx.Done())
		go wait.Until(runWorker("delete switch lb rule", c.updateSwitchLBRuleQueue, c.handleUpdateSwitchLBRule), time.Second, ctx.Done())

		go wait.Until(runWorker("add/update vpc dns", c.addOrUpdateVpcDNSQueue, c.handleAddOrUpdateVPCDNS), time.Second, ctx.Done())
		go wait.Until(runWorker("delete vpc dns", c.delVpcDNSQueue, c.handleDelVpcDNS), time.Second, ctx.Done())
		go wait.Until(func() {
			c.resyncVpcDNSConfig()
		}, 5*time.Second, ctx.Done())
	}

	for range c.config.WorkerNum {
		go wait.Until(runWorker("delete pod", c.deletePodQueue, c.handleDeletePod), time.Second, ctx.Done())
		go wait.Until(runWorker("add/update pod", c.addOrUpdatePodQueue, c.handleAddOrUpdatePod), time.Second, ctx.Done())
		go wait.Until(runWorker("update pod security", c.updatePodSecurityQueue, c.handleUpdatePodSecurity), time.Second, ctx.Done())

		go wait.Until(runWorker("delete subnet", c.deleteSubnetQueue, c.handleDeleteSubnet), time.Second, ctx.Done())
		go wait.Until(runWorker("delete ippool", c.deleteIPPoolQueue, c.handleDeleteIPPool), time.Second, ctx.Done())
		go wait.Until(runWorker("update status of subnet", c.updateSubnetStatusQueue, c.handleUpdateSubnetStatus), time.Second, ctx.Done())
		go wait.Until(runWorker("update status of ippool", c.updateIPPoolStatusQueue, c.handleUpdateIPPoolStatus), time.Second, ctx.Done())
		go wait.Until(runWorker("virtual port for subnet", c.syncVirtualPortsQueue, c.syncVirtualPort), time.Second, ctx.Done())

		if c.config.EnableLb {
			go wait.Until(runWorker("update service", c.updateServiceQueue, c.handleUpdateService), time.Second, ctx.Done())
			go wait.Until(runWorker("add/update endpoint slice", c.addOrUpdateEndpointSliceQueue, c.handleUpdateEndpointSlice), time.Second, ctx.Done())
		}

		if c.config.EnableNP {
			go wait.Until(runWorker("update network policy", c.updateNpQueue, c.handleUpdateNp), time.Second, ctx.Done())
			go wait.Until(runWorker("delete network policy", c.deleteNpQueue, c.handleDeleteNp), time.Second, ctx.Done())
		}

		go wait.Until(runWorker("delete vlan", c.delVlanQueue, c.handleDelVlan), time.Second, ctx.Done())
		go wait.Until(runWorker("update vlan", c.updateVlanQueue, c.handleUpdateVlan), time.Second, ctx.Done())
	}

	if c.config.EnableEipSnat {
		go wait.Until(func() {
			// init l3 about the default vpc external lrp binding to the gw chassis
			c.resyncExternalGateway()
		}, time.Second, ctx.Done())

		// maintain l3 ha about the vpc external lrp binding to the gw chassis
		c.OVNNbClient.MonitorBFD()
	}
	// TODO: we should merge these two vpc nat config into one config and resync them together
	go wait.Until(func() {
		c.resyncVpcNatGwConfig()
	}, time.Second, ctx.Done())

	go wait.Until(func() {
		c.resyncVpcNatConfig()
	}, time.Second, ctx.Done())

	if c.config.GCInterval != 0 {
		go wait.Until(func() {
			if err := c.markAndCleanLSP(); err != nil {
				klog.Errorf("gc lsp error: %v", err)
			}
		}, time.Duration(c.config.GCInterval)*time.Second, ctx.Done())
	}

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
	go wait.Until(c.exportSubnetMetrics, 30*time.Second, ctx.Done())
	go wait.Until(c.checkSubnetGateway, 5*time.Second, ctx.Done())
	go wait.Until(c.syncDistributedSubnetRoutes, 5*time.Second, ctx.Done())

	go wait.Until(runWorker("add ovn eip", c.addOvnEipQueue, c.handleAddOvnEip), time.Second, ctx.Done())
	go wait.Until(runWorker("update ovn eip", c.updateOvnEipQueue, c.handleUpdateOvnEip), time.Second, ctx.Done())
	go wait.Until(runWorker("reset ovn eip", c.resetOvnEipQueue, c.handleResetOvnEip), time.Second, ctx.Done())
	go wait.Until(runWorker("delete ovn eip", c.delOvnEipQueue, c.handleDelOvnEip), time.Second, ctx.Done())

	go wait.Until(runWorker("add ovn fip", c.addOvnFipQueue, c.handleAddOvnFip), time.Second, ctx.Done())
	go wait.Until(runWorker("update ovn fip", c.updateOvnFipQueue, c.handleUpdateOvnFip), time.Second, ctx.Done())
	go wait.Until(runWorker("delete ovn fip", c.delOvnFipQueue, c.handleDelOvnFip), time.Second, ctx.Done())

	go wait.Until(runWorker("add ovn snat rule", c.addOvnSnatRuleQueue, c.handleAddOvnSnatRule), time.Second, ctx.Done())
	go wait.Until(runWorker("update ovn snat rule", c.updateOvnSnatRuleQueue, c.handleUpdateOvnSnatRule), time.Second, ctx.Done())
	go wait.Until(runWorker("delete ovn snat rule", c.delOvnSnatRuleQueue, c.handleDelOvnSnatRule), time.Second, ctx.Done())

	go wait.Until(runWorker("add ovn dnat", c.addOvnDnatRuleQueue, c.handleAddOvnDnatRule), time.Second, ctx.Done())
	go wait.Until(runWorker("update ovn dnat", c.updateOvnDnatRuleQueue, c.handleUpdateOvnDnatRule), time.Second, ctx.Done())
	go wait.Until(runWorker("delete ovn dnat", c.delOvnDnatRuleQueue, c.handleDelOvnDnatRule), time.Second, ctx.Done())

	go wait.Until(c.CheckNodePortGroup, time.Duration(c.config.NodePgProbeTime)*time.Minute, ctx.Done())

	go wait.Until(runWorker("add ip", c.addIPQueue, c.handleAddReservedIP), time.Second, ctx.Done())
	go wait.Until(runWorker("update ip", c.updateIPQueue, c.handleUpdateIP), time.Second, ctx.Done())
	go wait.Until(runWorker("delete ip", c.delIPQueue, c.handleDelIP), time.Second, ctx.Done())

	go wait.Until(runWorker("add vip", c.addVirtualIPQueue, c.handleAddVirtualIP), time.Second, ctx.Done())
	go wait.Until(runWorker("update vip", c.updateVirtualIPQueue, c.handleUpdateVirtualIP), time.Second, ctx.Done())
	go wait.Until(runWorker("update virtual parent for vip", c.updateVirtualParentsQueue, c.handleUpdateVirtualParents), time.Second, ctx.Done())
	go wait.Until(runWorker("delete vip", c.delVirtualIPQueue, c.handleDelVirtualIP), time.Second, ctx.Done())

	go wait.Until(runWorker("add iptables eip", c.addIptablesEipQueue, c.handleAddIptablesEip), time.Second, ctx.Done())
	go wait.Until(runWorker("update iptables eip", c.updateIptablesEipQueue, c.handleUpdateIptablesEip), time.Second, ctx.Done())
	go wait.Until(runWorker("reset iptables eip", c.resetIptablesEipQueue, c.handleResetIptablesEip), time.Second, ctx.Done())
	go wait.Until(runWorker("delete iptables eip", c.delIptablesEipQueue, c.handleDelIptablesEip), time.Second, ctx.Done())

	go wait.Until(runWorker("add iptables fip", c.addIptablesFipQueue, c.handleAddIptablesFip), time.Second, ctx.Done())
	go wait.Until(runWorker("update iptables fip", c.updateIptablesFipQueue, c.handleUpdateIptablesFip), time.Second, ctx.Done())
	go wait.Until(runWorker("delete iptables fip", c.delIptablesFipQueue, c.handleDelIptablesFip), time.Second, ctx.Done())

	go wait.Until(runWorker("add iptables dnat rule", c.addIptablesDnatRuleQueue, c.handleAddIptablesDnatRule), time.Second, ctx.Done())
	go wait.Until(runWorker("update iptables dnat rule", c.updateIptablesDnatRuleQueue, c.handleUpdateIptablesDnatRule), time.Second, ctx.Done())
	go wait.Until(runWorker("delete iptables dnat rule", c.delIptablesDnatRuleQueue, c.handleDelIptablesDnatRule), time.Second, ctx.Done())

	go wait.Until(runWorker("add iptables snat rule", c.addIptablesSnatRuleQueue, c.handleAddIptablesSnatRule), time.Second, ctx.Done())
	go wait.Until(runWorker("update iptables snat rule", c.updateIptablesSnatRuleQueue, c.handleUpdateIptablesSnatRule), time.Second, ctx.Done())
	go wait.Until(runWorker("delete iptables snat rule", c.delIptablesSnatRuleQueue, c.handleDelIptablesSnatRule), time.Second, ctx.Done())

	go wait.Until(runWorker("add qos policy", c.addQoSPolicyQueue, c.handleAddQoSPolicy), time.Second, ctx.Done())
	go wait.Until(runWorker("update qos policy", c.updateQoSPolicyQueue, c.handleUpdateQoSPolicy), time.Second, ctx.Done())
	go wait.Until(runWorker("delete qos policy", c.delQoSPolicyQueue, c.handleDelQoSPolicy), time.Second, ctx.Done())

	if c.config.EnableANP {
		go wait.Until(runWorker("add admin network policy", c.addAnpQueue, c.handleAddAnp), time.Second, ctx.Done())
		go wait.Until(runWorker("update admin network policy", c.updateAnpQueue, c.handleUpdateAnp), time.Second, ctx.Done())
		go wait.Until(runWorker("delete admin network policy", c.deleteAnpQueue, c.handleDeleteAnp), time.Second, ctx.Done())

		go wait.Until(runWorker("add base admin network policy", c.addBanpQueue, c.handleAddBanp), time.Second, ctx.Done())
		go wait.Until(runWorker("update base admin network policy", c.updateBanpQueue, c.handleUpdateBanp), time.Second, ctx.Done())
		go wait.Until(runWorker("delete base admin network policy", c.deleteBanpQueue, c.handleDeleteBanp), time.Second, ctx.Done())

		go wait.Until(runWorker("add cluster network policy", c.addCnpQueue, c.handleAddCnp), time.Second, ctx.Done())
		go wait.Until(runWorker("update cluster network policy", c.updateCnpQueue, c.handleUpdateCnp), time.Second, ctx.Done())
		go wait.Until(runWorker("delete cluster network policy", c.deleteCnpQueue, c.handleDeleteCnp), time.Second, ctx.Done())
	}

	if c.config.EnableDNSNameResolver {
		go wait.Until(runWorker("add or update dns name resolver", c.addOrUpdateDNSNameResolverQueue, c.handleAddOrUpdateDNSNameResolver), time.Second, ctx.Done())
		go wait.Until(runWorker("delete dns name resolver", c.deleteDNSNameResolverQueue, c.handleDeleteDNSNameResolver), time.Second, ctx.Done())
	}

	if c.config.EnableLiveMigrationOptimize {
		go wait.Until(runWorker("add/update vmiMigration ", c.addOrUpdateVMIMigrationQueue, c.handleAddOrUpdateVMIMigration), 50*time.Millisecond, ctx.Done())
	}

	go wait.Until(runWorker("delete vm", c.deleteVMQueue, c.handleDeleteVM), time.Second, ctx.Done())

	go wait.Until(c.dbStatus, 15*time.Second, ctx.Done())
}

func (c *Controller) allSubnetReady(subnets ...string) (bool, error) {
	for _, lsName := range subnets {
		exist, err := c.OVNNbClient.LogicalSwitchExists(lsName)
		if err != nil {
			klog.Error(err)
			return false, fmt.Errorf("check logical switch %s exist: %w", lsName, err)
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

	if err := c.initDefaultDenyAllSecurityGroup(); err != nil {
		util.LogFatalAndExit(err, "failed to initialize 'deny_all' security group")
	}
	if err := c.syncSecurityGroup(); err != nil {
		util.LogFatalAndExit(err, "failed to sync security group")
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

func processNextWorkItem[T comparable](action string, queue workqueue.TypedRateLimitingInterface[T], handler func(T) error, getItemKey func(any) string) bool {
	item, shutdown := queue.Get()
	if shutdown {
		return false
	}

	err := func(item T) error {
		defer queue.Done(item)
		if err := handler(item); err != nil {
			queue.AddRateLimited(item)
			return fmt.Errorf("error syncing %s %q: %w, requeuing", action, getItemKey(item), err)
		}
		queue.Forget(item)
		return nil
	}(item)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func getWorkItemKey(obj any) string {
	switch v := obj.(type) {
	case string:
		return v
	case *vpcService:
		return cache.MetaObjectToName(obj.(*vpcService).Svc).String()
	case *AdminNetworkPolicyChangedDelta:
		return v.key
	case *SlrInfo:
		return v.Name
	default:
		key, err := cache.MetaNamespaceKeyFunc(obj)
		if err != nil {
			utilruntime.HandleError(err)
			return ""
		}
		return key
	}
}

func runWorker[T comparable](action string, queue workqueue.TypedRateLimitingInterface[T], handler func(T) error) func() {
	return func() {
		for processNextWorkItem(action, queue, handler, getWorkItemKey) {
		}
	}
}

// apiResourceExists checks if all specified kinds exist in the given group version.
// It returns true if all kinds are found, false otherwise.
// Parameters:
// - discoveryClient: The discovery client to use for querying API resources.
// - gv: The group version string (e.g., "apps/v1").
// - kinds: A variadic list of kind names to check for existence (e.g., "Deployment", "StatefulSet").
func apiResourceExists(discoveryClient discovery.DiscoveryInterface, gv string, kinds ...string) (bool, error) {
	apiResourceLists, err := discoveryClient.ServerResourcesForGroupVersion(gv)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to discover api resources for %s: %w", gv, err)
	}

	existingKinds := set.New[string]()
	for _, apiResource := range apiResourceLists.APIResources {
		existingKinds.Insert(apiResource.Kind)
	}

	return existingKinds.HasAll(kinds...), nil
}
