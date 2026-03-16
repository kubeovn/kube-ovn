package speaker

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	"github.com/osrg/gobgp/v4/api"
	"github.com/osrg/gobgp/v4/pkg/apiutil"
	"github.com/osrg/gobgp/v4/pkg/packet/bgp"
	gobgp "github.com/osrg/gobgp/v4/pkg/server"
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
	DefaultBGPHoldtime                 = 90 * time.Second
	DefaultPprofPort                   = 10667
	DefaultGracefulRestartDeferralTime = 360 * time.Second
	DefaultGracefulRestartTime         = 90 * time.Second
	DefaultEbgpMultiHop                = 1
	addPeerMaxRetries                  = 12
	addPeerRetryInterval               = 5 * time.Second
)

type Configuration struct {
	GrpcHost                    net.IP
	GrpcPort                    int32
	ClusterAs                   uint32
	RouterID                    net.IP
	PodIPs                      map[string]net.IP
	NodeIPs                     map[string]net.IP
	NeighborAddresses           []net.IP
	NeighborIPv6Addresses       []net.IP
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

	VpcNatGwNamespace string
	NodeRouteEIPMode  bool

	NodeName       string
	KubeConfigFile string
	KubeClient     kubernetes.Interface
	KubeOvnClient  clientset.Interface

	PprofPort int32
	LogPerm   string
}

func ParseFlags() (*Configuration, error) {
	var (
		argDefaultGracefulTime         = pflag.Duration("graceful-restart-time", DefaultGracefulRestartTime, "BGP Graceful restart time according to RFC4724 3, maximum 4095s.")
		argGracefulRestartDeferralTime = pflag.Duration("graceful-restart-deferral-time", DefaultGracefulRestartDeferralTime, "BGP Graceful restart deferral time according to RFC4724 4.1, maximum 18h.")
		argGracefulRestart             = pflag.BoolP("graceful-restart", "", false, "Enables the BGP Graceful Restart so that routes are preserved on unexpected restarts")
		argAnnounceClusterIP           = pflag.BoolP("announce-cluster-ip", "", false, "The Cluster IP of the service to announce to the BGP peers.")
		argGrpcHost                    = pflag.IP("grpc-host", net.IP{127, 0, 0, 1}, "The host address for grpc to listen, default: 127.0.0.1")
		argGrpcPort                    = pflag.Int32("grpc-port", DefaultBGPGrpcPort, "The port for grpc to listen, default:50051")
		argClusterAs                   = pflag.Uint32("cluster-as", 0, "The AS number of the local BGP speaker (required)")
		argRouterID                    = pflag.IP("router-id", nil, "The address for the speaker to use as router id, default the node ip")
		argNodeIPs                     = pflag.IPSlice("node-ips", nil, "The comma-separated list of node IP addresses to use instead of the pod IP address for the next hop router IP address.")
		argNeighborAddress             = pflag.IPSlice("neighbor-address", nil, "Comma separated IPv4 router addresses the speaker connects to.")
		argNeighborIPv6Address         = pflag.IPSlice("neighbor-ipv6-address", nil, "Comma separated IPv6 router addresses the speaker connects to.")
		argNeighborAs                  = pflag.Uint32("neighbor-as", 0, "The AS number of the BGP neighbor/peer (required)")
		argAuthPassword                = pflag.String("auth-password", "", "bgp peer auth password")
		argHoldTime                    = pflag.Duration("holdtime", DefaultBGPHoldtime, "ovn-speaker goes down abnormally, the local saving time of BGP route will be affected.Holdtime must be in the range 3s to 65536s. (default 90s)")
		argPprofPort                   = pflag.Int32("pprof-port", DefaultPprofPort, "The port to get profiling data, default: 10667")
		argNodeName                    = pflag.String("node-name", os.Getenv(util.EnvNodeName), "Name of the node on which the speaker is running on.")
		argKubeConfigFile              = pflag.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information. If not set use the inCluster token.")
		argPassiveMode                 = pflag.BoolP("passivemode", "", false, "Set BGP Speaker to passive model, do not actively initiate connections to peers")
		argEbgpMultihopTTL             = pflag.Uint8("ebgp-multihop", DefaultEbgpMultiHop, "The TTL value of EBGP peer, default: 1")
		argExtendedNexthop             = pflag.BoolP("extended-nexthop", "", false, "Announce IPv4/IPv6 prefixes to every neighbor, no matter their AFI")
		argNatGwMode                   = pflag.BoolP("nat-gw-mode", "", false, "Make the BGP speaker announce EIPs from inside a NAT gateway pod. Mutually exclusive with --node-route-eip-mode. Pod IP/Service/Subnet announcements will be disabled")
		argEnableMetrics               = pflag.BoolP("enable-metrics", "", true, "Whether to support metrics query")
		argLogPerm                     = pflag.String("log-perm", "640", "The permission for the log file")
		argVpcNatGwNamespace           = pflag.String("vpc-nat-gw-namespace", "kube-system", "The namespace where VPC NAT Gateway pods are deployed, default: kube-system")
		argNodeRouteEIPMode            = pflag.BoolP("node-route-eip-mode", "", false, "Make the BGP speaker announce EIPs for local NAT gateway pods. Mutually exclusive with --nat-gw-mode. Speaker runs on node with host network and announces EIPs for vpc-nat-gw pods on the same node")
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
	if *argEbgpMultihopTTL == 0 {
		return nil, errors.New("the bgp MultihopTtl must be in the range 1 to 255")
	}

	podIpsEnv := os.Getenv(util.EnvPodIPs)
	if podIpsEnv == "" {
		podIpsEnv = os.Getenv(util.EnvPodIP)
	}
	podIPv4, podIPv6 := util.SplitStringIP(podIpsEnv)

	var nodeIPv4, nodeIPv6 net.IP
	for _, ip := range *argNodeIPs {
		if ip.To4() != nil {
			nodeIPv4 = ip
		} else if ip.To16() != nil {
			nodeIPv6 = ip
		}
	}

	config := &Configuration{
		AnnounceClusterIP:     *argAnnounceClusterIP,
		GrpcHost:              *argGrpcHost,
		GrpcPort:              *argGrpcPort,
		ClusterAs:             *argClusterAs,
		RouterID:              *argRouterID,
		NeighborAddresses:     *argNeighborAddress,
		NeighborIPv6Addresses: *argNeighborIPv6Address,
		NodeIPs: map[string]net.IP{
			kubeovnv1.ProtocolIPv4: nodeIPv4,
			kubeovnv1.ProtocolIPv6: nodeIPv6,
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
		VpcNatGwNamespace:           *argVpcNatGwNamespace,
		NodeRouteEIPMode:            *argNodeRouteEIPMode,
	}

	if err := config.validateMutuallyExclusiveModes(); err != nil {
		return nil, err
	}

	if err := config.validateRequiredFlags(); err != nil {
		return nil, err
	}

	for _, addr := range config.NeighborAddresses {
		if addr.To4() == nil {
			return nil, fmt.Errorf("invalid neighbor-address format: %s", *argNeighborAddress)
		}
	}
	for _, addr := range config.NeighborIPv6Addresses {
		if addr.To4() != nil {
			return nil, fmt.Errorf("invalid neighbor-ipv6-address format: %s is not an IPv6 address", addr)
		}
	}

	if config.RouterID == nil {
		if podIPv4 != "" {
			config.RouterID = net.ParseIP(podIPv4)
		}

		if config.RouterID == nil {
			// RouterID must be an IPv4. If no IPv4 exists on the speaker, fallback to 0.0.0.0 to avoid GoBGP crashing.
			config.RouterID = net.ParseIP("0.0.0.0")
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

// validateMutuallyExclusiveModes checks that mutually exclusive modes are not enabled simultaneously.
// - NatGwMode: speaker runs inside NAT gateway pod, announces EIPs for that specific gateway
// - NodeRouteEIPMode: speaker runs on node, announces EIPs for all local NAT gateway pods
func (config *Configuration) validateMutuallyExclusiveModes() error {
	if config.NatGwMode && config.NodeRouteEIPMode {
		return errors.New("--nat-gw-mode and --node-route-eip-mode are mutually exclusive")
	}
	return nil
}

// validateRequiredFlags checks that all required BGP configuration flags are provided.
// It collects all missing flags and returns them in a single error message.
func (config *Configuration) validateRequiredFlags() error {
	var missingFlags []string

	if len(config.NeighborAddresses) == 0 && len(config.NeighborIPv6Addresses) == 0 {
		missingFlags = append(missingFlags, "at least one of --neighbor-address or --neighbor-ipv6-address must be specified")
	}
	if config.ClusterAs == 0 {
		missingFlags = append(missingFlags, "--cluster-as must be specified")
	}
	if config.NeighborAs == 0 {
		missingFlags = append(missingFlags, "--neighbor-as must be specified")
	}

	// NodeRouteEIPMode requires NodeName to identify local NAT gateway pods
	if config.NodeRouteEIPMode && config.NodeName == "" {
		missingFlags = append(missingFlags, "--node-route-eip-mode requires --node-name to be specified")
	}

	if len(missingFlags) > 0 {
		return fmt.Errorf("missing required flags: %s", strings.Join(missingFlags, "; "))
	}
	return nil
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

	cfg.ContentType = util.ContentTypeProtobuf
	cfg.AcceptContentTypes = util.AcceptContentTypes
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
	var logLevel slog.LevelVar
	if klog.V(3).Enabled() {
		logLevel.Set(slog.LevelDebug)
	} else {
		logLevel.Set(slog.LevelInfo)
	}

	grpcOpts := []grpc.ServerOption{grpc.MaxRecvMsgSize(maxSize), grpc.MaxSendMsgSize(maxSize)}
	s := gobgp.NewBgpServer(
		gobgp.GrpcListenAddress(util.JoinHostPort(config.GrpcHost.String(), config.GrpcPort)),
		gobgp.GrpcOption(grpcOpts),
		gobgp.LoggerOption(slog.Default(), &logLevel),
	)
	go s.Serve()

	peersMap := map[api.Family_Afi][]net.IP{
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
			RouterId:         config.RouterID.String(),
			ListenPort:       listenPort,
			UseMultiplePaths: true,
		},
	}); err != nil {
		err = fmt.Errorf("failed to start bgp server: %w", err)
		klog.Error(err)
		return err
	}
	for ipFamily, addresses := range peersMap {
		for _, addr := range addresses {
			peer := &api.Peer{
				Timers: &api.Timers{Config: &api.TimersConfig{HoldTime: uint64(config.HoldTime)}},
				Conf: &api.PeerConf{
					NeighborAddress: addr.String(),
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
					err = fmt.Errorf("failed to check graceful restart options: %w", err)
					klog.Error(err)
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

			logBgpPeer(peer)
			if err := addPeerWithRetry(s, peer); err != nil {
				err = fmt.Errorf("failed to add peer %s: %w", addr.String(), err)
				klog.Error(err)
				return err
			}
		}
	}

	config.BgpServer = s

	// Start watching peer state changes for detailed logging
	go config.watchPeerState()

	return nil
}

// addPeerWithRetry attempts to add a BGP peer with retry logic.
// It retries up to addPeerMaxRetries times with addPeerRetryInterval between attempts.
func addPeerWithRetry(s *gobgp.BgpServer, peer *api.Peer) error {
	var err error
	for i := 0; i < addPeerMaxRetries; i++ {
		if err = s.AddPeer(context.Background(), &api.AddPeerRequest{
			Peer: peer,
		}); err == nil {
			return nil
		}
		klog.Errorf("failed to add peer %s (attempt %d/%d): %v", peer.Conf.NeighborAddress, i+1, addPeerMaxRetries, err)
		if i < addPeerMaxRetries-1 {
			time.Sleep(addPeerRetryInterval)
		}
	}
	err = fmt.Errorf("failed to add peer %s after %d attempts: %w", peer.Conf.NeighborAddress, addPeerMaxRetries, err)
	klog.Error(err)
	return err
}

// logBgpPeer logs the BGP peer configuration details, masking sensitive information.
func logBgpPeer(peer *api.Peer) {
	klog.Infof("BGP Peer Configuration: NeighborAddress=%s, PeerAsn=%d, HoldTime=%d, LocalAddress=%s, PassiveMode=%v, EbgpMultihop=%v, GracefulRestart=%v, AfiSafis=%v",
		peer.Conf.NeighborAddress,
		peer.Conf.PeerAsn,
		peer.Timers.Config.HoldTime,
		peer.Transport.LocalAddress,
		peer.Transport.PassiveMode,
		peer.EbgpMultihop,
		peer.GracefulRestart,
		peer.AfiSafis)
}

// watchPeerState monitors BGP peer state changes and logs detailed information
// including local address when peers go up or down.
func (config *Configuration) watchPeerState() {
	err := config.BgpServer.WatchEvent(context.Background(), gobgp.WatchEventMessageCallbacks{
		OnPeerUpdate: func(peer *apiutil.WatchEventMessage_PeerEvent, _ time.Time) {
			if peer == nil {
				return
			}
			p := peer.Peer
			neighborAddr := p.Conf.NeighborAddress.String()
			localAddr := p.Transport.LocalAddress.String()
			state := p.State.SessionState.String()
			peerAS := p.Conf.PeerASN

			if peer.Type == apiutil.PEER_EVENT_STATE {
				klog.Infof("BGP peer state changed: neighbor=%s, state=%s, localAddress=%s, peerAS=%d",
					neighborAddr, state, localAddr, peerAS)
			}
		},
	}, gobgp.WatchPeer())
	if err != nil {
		klog.Errorf("failed to watch peer state: %v", err)
	}
}
