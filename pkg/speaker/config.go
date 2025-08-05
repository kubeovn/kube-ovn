package speaker

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	api "github.com/osrg/gobgp/v3/api"
	bgplog "github.com/osrg/gobgp/v3/pkg/log"
	"github.com/osrg/gobgp/v3/pkg/packet/bgp"
	gobgp "github.com/osrg/gobgp/v3/pkg/server"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	clientset "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	DefaultBGPGrpcPort                 = 50051
	DefaultBGPClusterAs                = 65000
	DefaultBGPNeighborAs               = 65001
	DefaultBGPHoldtime                 = 90 * time.Second
	DefaultPprofPort                   = 10667
	DefaultGracefulRestartDeferralTime = 360 * time.Second
	DefaultGracefulRestartTime         = 90 * time.Second
	DefaultEbgpMultiHop                = 1
)

type Configuration struct {
	GrpcHost                    string
	GrpcPort                    int32
	ClusterAs                   uint32
	RouterID                    string
	PodIPs                      map[string]net.IP
	NodeIPs                     map[string]net.IP
	NeighborAddresses           []string
	NeighborIPv6Addresses       []string
	NeighborAs                  uint32
	AuthPassword                string
	HoldTime                    float64
	BgpServer                   *gobgp.BgpServer
	AnnounceClusterIP           bool
	GracefulRestart             bool
	GracefulRestartDeferralTime time.Duration
	GracefulRestartTime         time.Duration
	PassiveMode                 bool
	EbgpMultihopTTL             uint8
	ExtendedNexthop             bool
	NatGwMode                   bool
	EnableMetrics               bool

	NodeName       string
	KubeConfigFile string
	KubeClient     kubernetes.Interface
	KubeOvnClient  clientset.Interface

	PprofPort         int32
	LogPerm           string
	EdgeRouterMode    bool
	RouteServerClient bool
}

func ParseFlags() (*Configuration, error) {
	var (
		argDefaultGracefulTime         = pflag.Duration("graceful-restart-time", DefaultGracefulRestartTime, "BGP Graceful restart time according to RFC4724 3, maximum 4095s.")
		argGracefulRestartDeferralTime = pflag.Duration("graceful-restart-deferral-time", DefaultGracefulRestartDeferralTime, "BGP Graceful restart deferral time according to RFC4724 4.1, maximum 18h.")
		argGracefulRestart             = pflag.BoolP("graceful-restart", "", false, "Enables the BGP Graceful Restart so that routes are preserved on unexpected restarts")
		argAnnounceClusterIP           = pflag.BoolP("announce-cluster-ip", "", false, "The Cluster IP of the service to announce to the BGP peers.")
		argGrpcHost                    = pflag.String("grpc-host", "127.0.0.1", "The host address for grpc to listen, default: 127.0.0.1")
		argGrpcPort                    = pflag.Int32("grpc-port", DefaultBGPGrpcPort, "The port for grpc to listen, default:50051")
		argClusterAs                   = pflag.Uint32("cluster-as", DefaultBGPClusterAs, "The AS number of container network, default 65000")
		argRouterID                    = pflag.String("router-id", "", "The address for the speaker to use as router id, default the node ip")
		argNodeIPs                     = pflag.String("node-ips", "", "The comma-separated list of node IP addresses to use instead of the pod IP address for the next hop router IP address.")
		argNeighborAddress             = pflag.String("neighbor-address", "", "Comma separated IPv4 router addresses the speaker connects to.")
		argNeighborIPv6Address         = pflag.String("neighbor-ipv6-address", "", "Comma separated IPv6 router addresses the speaker connects to.")
		argNeighborAs                  = pflag.Uint32("neighbor-as", DefaultBGPNeighborAs, "The router as number, default 65001")
		argAuthPassword                = pflag.String("auth-password", "", "bgp peer auth password")
		argHoldTime                    = pflag.Duration("holdtime", DefaultBGPHoldtime, "ovn-speaker goes down abnormally, the local saving time of BGP route will be affected.Holdtime must be in the range 3s to 65536s. (default 90s)")
		argPprofPort                   = pflag.Int32("pprof-port", DefaultPprofPort, "The port to get profiling data, default: 10667")
		argNodeName                    = pflag.String("node-name", os.Getenv(util.HostnameEnv), "Name of the node on which the speaker is running on.")
		argKubeConfigFile              = pflag.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information. If not set use the inCluster token.")
		argPassiveMode                 = pflag.BoolP("passivemode", "", false, "Set BGP Speaker to passive model, do not actively initiate connections to peers")
		argEbgpMultihopTTL             = pflag.Uint8("ebgp-multihop", DefaultEbgpMultiHop, "The TTL value of EBGP peer, default: 1")
		argExtendedNexthop             = pflag.BoolP("extended-nexthop", "", false, "Announce IPv4/IPv6 prefixes to every neighbor, no matter their AFI")
		argNatGwMode                   = pflag.BoolP("nat-gw-mode", "", false, "Make the BGP speaker announce EIPs from inside a NAT gateway, Pod IP/Service/Subnet announcements will be disabled")
		argEnableMetrics               = pflag.BoolP("enable-metrics", "", true, "Whether to support metrics query")
		argLogPerm                     = pflag.String("log-perm", "640", "The permission for the log file")
		argEdgeRouterMode              = pflag.BoolP("edge-router-mode", "", false, "Make the BGP speaker announce inside subnet and get routes from the outside, work as edge router")
		argRouteServerClient           = pflag.BoolP("route-server-client", "", false, "Make the BGP speaker policy route, work as route server client")
	)
	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)

	// Sync the glog and klog flags.
	pflag.CommandLine.VisitAll(func(f1 *pflag.Flag) {
		f2 := klogFlags.Lookup(f1.Name)
		if f2 != nil {
			value := f1.Value.String()
			if err := f2.Value.Set(value); err != nil {
				util.LogFatalAndExit(err, "failed to set flag")
			}
		}
	})

	pflag.CommandLine.AddGoFlagSet(klogFlags)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	ht := argHoldTime.Seconds()
	if ht > 65536 || ht < 3 {
		return nil, errors.New("the bgp holdtime must be in the range 3s to 65536s")
	}
	if *argRouterID != "" && net.ParseIP(*argRouterID) == nil {
		return nil, fmt.Errorf("invalid router-id format: %s", *argRouterID)
	}
	if *argEbgpMultihopTTL == 0 {
		return nil, errors.New("the bgp MultihopTtl must be in the range 1 to 255")
	}

	podIpsEnv := os.Getenv("POD_IPS")
	if podIpsEnv == "" {
		podIpsEnv = os.Getenv("POD_IP")
	}
	podIPv4, podIPv6 := util.SplitStringIP(podIpsEnv)

	nodeIPv4, nodeIPv6 := util.SplitStringIP(*argNodeIPs)

	config := &Configuration{
		AnnounceClusterIP: *argAnnounceClusterIP,
		GrpcHost:          *argGrpcHost,
		GrpcPort:          *argGrpcPort,
		ClusterAs:         *argClusterAs,
		RouterID:          *argRouterID,
		NodeIPs: map[string]net.IP{
			kubeovnv1.ProtocolIPv4: net.ParseIP(nodeIPv4),
			kubeovnv1.ProtocolIPv6: net.ParseIP(nodeIPv6),
		},
		PodIPs: map[string]net.IP{
			kubeovnv1.ProtocolIPv4: net.ParseIP(podIPv4),
			kubeovnv1.ProtocolIPv6: net.ParseIP(podIPv6),
		},
		NeighborAs:                  *argNeighborAs,
		AuthPassword:                *argAuthPassword,
		HoldTime:                    ht,
		PprofPort:                   *argPprofPort,
		NodeName:                    strings.ToLower(*argNodeName),
		KubeConfigFile:              *argKubeConfigFile,
		GracefulRestart:             *argGracefulRestart,
		GracefulRestartDeferralTime: *argGracefulRestartDeferralTime,
		GracefulRestartTime:         *argDefaultGracefulTime,
		PassiveMode:                 *argPassiveMode,
		EbgpMultihopTTL:             *argEbgpMultihopTTL,
		ExtendedNexthop:             *argExtendedNexthop,
		NatGwMode:                   *argNatGwMode,
		EnableMetrics:               *argEnableMetrics,
		LogPerm:                     *argLogPerm,
		EdgeRouterMode:              *argEdgeRouterMode,
		RouteServerClient:           *argRouteServerClient,
	}

	if *argNeighborAddress != "" {
		config.NeighborAddresses = strings.Split(*argNeighborAddress, ",")
		for _, addr := range config.NeighborAddresses {
			if ip := net.ParseIP(addr); ip == nil || ip.To4() == nil {
				return nil, fmt.Errorf("invalid neighbor-address format: %s", *argNeighborAddress)
			}
		}
	}
	if *argNeighborIPv6Address != "" {
		config.NeighborIPv6Addresses = strings.Split(*argNeighborIPv6Address, ",")
		for _, addr := range config.NeighborIPv6Addresses {
			if ip := net.ParseIP(addr); ip == nil || ip.To16() == nil {
				return nil, fmt.Errorf("invalid neighbor-ipv6-address format: %s", *argNeighborIPv6Address)
			}
		}
	}

	if config.RouterID == "" {
		externalIP, err := GetExternalIP()
		if err != nil || externalIP == "" {
			klog.Warningf("failed to get external IP: %v", err)
			return nil, err
		}
		config.RouterID = externalIP
		klog.Infof("using external IP %s as router ID", config.RouterID)

		// if podIPv4 != "" {
		// 	config.RouterID = podIPv4
		// } else {
		// 	config.RouterID = podIPv6
		// }
		if config.RouterID == "" {
			return nil, errors.New("no router id or POD_IPS")
		}
	}

	if err := config.initKubeClient(); err != nil {
		return nil, fmt.Errorf("failed to init kube client, %w", err)
	}

	if err := config.initBgpServer(); err != nil {
		return nil, fmt.Errorf("failed to init bgp server, %w", err)
	}

	return config, nil
}

func (config *Configuration) initKubeClient() error {
	var cfg *rest.Config
	var err error
	if config.KubeConfigFile == "" {
		klog.Infof("no --kubeconfig, use in-cluster kubernetes config")
		cfg, err = rest.InClusterConfig()
		if err != nil {
			klog.Errorf("use in cluster config failed %v", err)
			return err
		}
	} else {
		cfg, err = clientcmd.BuildConfigFromFlags("", config.KubeConfigFile)
		if err != nil {
			klog.Errorf("use --kubeconfig %s failed %v", config.KubeConfigFile, err)
			return err
		}
	}
	cfg.QPS = 1000
	cfg.Burst = 2000

	kubeOvnClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		klog.Errorf("init kubeovn client failed %v", err)
		return err
	}
	config.KubeOvnClient = kubeOvnClient

	cfg.ContentType = "application/vnd.kubernetes.protobuf"
	cfg.AcceptContentTypes = "application/vnd.kubernetes.protobuf,application/json"
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Errorf("init kubernetes client failed %v", err)
		return err
	}
	config.KubeClient = kubeClient
	return nil
}

func (config *Configuration) checkGracefulRestartOptions() error {
	if config.GracefulRestartTime > time.Second*4095 || config.GracefulRestartTime <= 0 {
		return errors.New("GracefulRestartTime should be less than 4095 seconds or more than 0")
	}
	if config.GracefulRestartDeferralTime > time.Hour*18 || config.GracefulRestartDeferralTime <= 0 {
		return errors.New("GracefulRestartDeferralTime should be less than 18 hours or more than 0")
	}

	return nil
}

func (config *Configuration) initBgpServer() error {
	maxSize := 256 << 20
	var listenPort int32 = -1

	// Set logger options for GoBGP based on klog's verbosity
	var logger bgpLogger
	if klog.V(3).Enabled() {
		logger.SetLevel(bgplog.TraceLevel)
	} else {
		logger.SetLevel(bgplog.InfoLevel)
	}

	grpcOpts := []grpc.ServerOption{grpc.MaxRecvMsgSize(maxSize), grpc.MaxSendMsgSize(maxSize)}
	s := gobgp.NewBgpServer(
		gobgp.GrpcListenAddress(util.JoinHostPort(config.GrpcHost, config.GrpcPort)),
		gobgp.GrpcOption(grpcOpts),
		gobgp.LoggerOption(logger),
	)
	go s.Serve()

	peersMap := map[api.Family_Afi][]string{
		api.Family_AFI_IP:  config.NeighborAddresses,
		api.Family_AFI_IP6: config.NeighborIPv6Addresses,
	}

	if config.PassiveMode {
		listenPort = bgp.BGP_PORT
	}

	klog.Infof("Starting bgp server with asn %d, routerId %s on port %d",
		config.ClusterAs,
		config.RouterID,
		listenPort)

	if err := s.StartBgp(context.Background(), &api.StartBgpRequest{
		Global: &api.Global{
			Asn:              config.ClusterAs,
			RouterId:         config.RouterID,
			ListenPort:       listenPort,
			UseMultiplePaths: true,
		},
	}); err != nil {
		return err
	}
	for ipFamily, addresses := range peersMap {
		for _, addr := range addresses {
			peer := &api.Peer{
				Timers: &api.Timers{Config: &api.TimersConfig{HoldTime: uint64(config.HoldTime)}},
				Conf: &api.PeerConf{
					NeighborAddress: addr,
					PeerAsn:         config.NeighborAs,
				},
				Transport: &api.Transport{
					PassiveMode: config.PassiveMode,
				},
			}
			if config.EbgpMultihopTTL != DefaultEbgpMultiHop {
				peer.EbgpMultihop = &api.EbgpMultihop{
					Enabled:     true,
					MultihopTtl: uint32(config.EbgpMultihopTTL),
				}
			}
			if config.AuthPassword != "" {
				peer.Conf.AuthPassword = config.AuthPassword
			}
			if config.GracefulRestart {
				if err := config.checkGracefulRestartOptions(); err != nil {
					return err
				}
				peer.GracefulRestart = &api.GracefulRestart{
					Enabled:         true,
					RestartTime:     uint32(config.GracefulRestartTime.Seconds()),
					DeferralTime:    uint32(config.GracefulRestartDeferralTime.Seconds()),
					LocalRestarting: true,
				}
				peer.AfiSafis = []*api.AfiSafi{
					{
						Config: &api.AfiSafiConfig{
							Family:  &api.Family{Afi: ipFamily, Safi: api.Family_SAFI_UNICAST},
							Enabled: true,
						},
						MpGracefulRestart: &api.MpGracefulRestart{
							Config: &api.MpGracefulRestartConfig{
								Enabled: true,
							},
						},
					},
				}
			}

			// If extended nexthop is enabled, advertise the IPv4 unicast AFI/SAFI even if
			// we have no IPv4 neighbor
			if config.ExtendedNexthop {
				peer.AfiSafis = append(peer.AfiSafis, &api.AfiSafi{
					Config: &api.AfiSafiConfig{
						Family: &api.Family{
							Afi:  api.Family_AFI_IP,
							Safi: api.Family_SAFI_UNICAST,
						},
					},
				})
			}

			if err := s.AddPeer(context.Background(), &api.AddPeerRequest{
				Peer: peer,
			}); err != nil {
				return err
			}
		}
	}

	config.BgpServer = s
	return nil
}

func GetExternalIP() (string, error) {
	raw := os.Getenv("MULTI_NET_STATUS")
	if raw == "" {
		return "", errors.New("MULTI_NET_STATUS annotation is empty")
	}

	type networkStatusEntry struct {
		Name      string   `json:"name"`
		Interface string   `json:"interface"`
		IPs       []string `json:"ips"`
		Default   bool     `json:"default"`
		DNS       struct{} `json:"dns"`
	}

	var entries []networkStatusEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return "", err
	}

	for _, e := range entries {
		// search for CNI network name is not "kube-ovn"
		if e.Name != "kube-ovn" && len(e.IPs) > 0 {
			return e.IPs[0], nil
		}
	}

	return "", errors.New("non–kube-ovn interface not found")
}
