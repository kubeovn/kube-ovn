package speaker

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	api "github.com/osrg/gobgp/api"
	gobgp "github.com/osrg/gobgp/pkg/server"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	clientset "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned"
)

const (
	DefaultBGPGrpcPort                 = 50051
	DefaultBGPClusterAs                = 65000
	DefaultBGPNeighborAs               = 65001
	DefaultBGPHoldtime                 = 90 * time.Second
	DefaultPprofPort                   = 10667
	DefaultGracefulRestartDeferralTime = 360 * time.Second
	DefaultGracefulRestartTime         = 90 * time.Second
)

type Configuration struct {
	GrpcHost                    string
	GrpcPort                    uint32
	ClusterAs                   uint32
	RouterId                    string
	NeighborAddress             string
	NeighborAs                  uint32
	AuthPassword                string
	HoldTime                    float64
	BgpServer                   *gobgp.BgpServer
	AnnounceClusterIP           bool
	GracefulRestart             bool
	GracefulRestartDeferralTime time.Duration
	GracefulRestartTime         time.Duration

	KubeConfigFile string
	KubeClient     kubernetes.Interface
	KubeOvnClient  clientset.Interface

	PprofPort uint32
}

func ParseFlags() (*Configuration, error) {
	var (
		argDefaultGracefulTime         = pflag.Duration("graceful-restart-time", DefaultGracefulRestartTime, "BGP Graceful restart time according to RFC4724 3, maximum 4095s.")
		argGracefulRestartDeferralTime = pflag.Duration("graceful-restart-deferral-time", DefaultGracefulRestartDeferralTime, "BGP Graceful restart deferral time according to RFC4724 4.1, maximum 18h.")
		argGracefulRestart             = pflag.BoolP("graceful-restart", "", false, "Enables the BGP Graceful Restart  so that routes are preserved on unexpected restarts")
		argAnnounceClusterIP           = pflag.BoolP("announce-cluster-ip", "", false, "The Cluster IP of the service to  announce to the BGP peers.")
		argGrpcHost                    = pflag.String("grpc-host", "127.0.0.1", "The host address for grpc to listen, default: 127.0.0.1")
		argGrpcPort                    = pflag.Uint32("grpc-port", DefaultBGPGrpcPort, "The port for grpc to listen, default:50051")
		argClusterAs                   = pflag.Uint32("cluster-as", DefaultBGPClusterAs, "The as number of container network, default 65000")
		argRouterId                    = pflag.String("router-id", "", "The address for the speaker to use as router id, default the node ip")
		argNeighborAddress             = pflag.String("neighbor-address", "", "The router address the speaker connects to.")
		argNeighborAs                  = pflag.Uint32("neighbor-as", DefaultBGPNeighborAs, "The router as number, default 65001")
		argAuthPassword                = pflag.String("auth-password", "", "bgp peer auth password")
		argHoldTime                    = pflag.Duration("holdtime", DefaultBGPHoldtime, "ovn-speaker goes down abnormally, the local saving time of BGP route will be affected.Holdtime must be in the range 3s to 65536s. (default 90s)")
		argPprofPort                   = pflag.Uint32("pprof-port", DefaultPprofPort, "The port to get profiling data, default: 10667")
		argKubeConfigFile              = pflag.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information. If not set use the inCluster token.")
	)
	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)

	// Sync the glog and klog flags.
	pflag.CommandLine.VisitAll(func(f1 *pflag.Flag) {
		f2 := klogFlags.Lookup(f1.Name)
		if f2 != nil {
			value := f1.Value.String()
			if err := f2.Value.Set(value); err != nil {
				klog.Fatalf("failed to set flag, %v", err)
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

	config := &Configuration{
		AnnounceClusterIP:           *argAnnounceClusterIP,
		GrpcHost:                    *argGrpcHost,
		GrpcPort:                    *argGrpcPort,
		ClusterAs:                   *argClusterAs,
		RouterId:                    *argRouterId,
		NeighborAddress:             *argNeighborAddress,
		NeighborAs:                  *argNeighborAs,
		AuthPassword:                *argAuthPassword,
		HoldTime:                    ht,
		PprofPort:                   *argPprofPort,
		KubeConfigFile:              *argKubeConfigFile,
		GracefulRestart:             *argGracefulRestart,
		GracefulRestartDeferralTime: *argGracefulRestartDeferralTime,
		GracefulRestartTime:         *argDefaultGracefulTime,
	}

	if config.RouterId == "" {
		config.RouterId = os.Getenv("POD_IP")
		if config.RouterId == "" {
			return nil, errors.New("no router id or POD_IP")
		}
	}

	if err := config.initKubeClient(); err != nil {
		return nil, fmt.Errorf("failed to init kube client, %v", err)
	}

	if err := config.initBgpServer(); err != nil {
		return nil, fmt.Errorf("failed to init bgp server, %v", err)
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
		return errors.New("GracefuleRestartTime should be less than 4095 seconds or more than 0")
	}
	if config.GracefulRestartDeferralTime > time.Hour*18 || config.GracefulRestartDeferralTime <= 0 {
		return errors.New("GracefuleRestartDeferralTime should be less than 18 hours or more than 0")
	}

	return nil
}

func (config *Configuration) initBgpServer() error {
	maxSize := 256 << 20
	grpcOpts := []grpc.ServerOption{grpc.MaxRecvMsgSize(maxSize), grpc.MaxSendMsgSize(maxSize)}
	s := gobgp.NewBgpServer(
		gobgp.GrpcListenAddress(fmt.Sprintf("%s:%d", config.GrpcHost, config.GrpcPort)),
		gobgp.GrpcOption(grpcOpts))
	go s.Serve()

	if err := s.StartBgp(context.Background(), &api.StartBgpRequest{
		Global: &api.Global{
			As:               config.ClusterAs,
			RouterId:         config.RouterId,
			ListenPort:       -1,
			UseMultiplePaths: true,
		},
	}); err != nil {
		return err
	}

	peer := &api.Peer{
		Timers: &api.Timers{Config: &api.TimersConfig{HoldTime: uint64(config.HoldTime)}},
		Conf: &api.PeerConf{
			NeighborAddress: config.NeighborAddress,
			PeerAs:          config.NeighborAs,
		},
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
					Family:  &api.Family{Afi: api.Family_AFI_IP, Safi: api.Family_SAFI_UNICAST},
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

	if err := s.AddPeer(context.Background(), &api.AddPeerRequest{
		Peer: peer,
	}); err != nil {
		return err
	}
	config.BgpServer = s
	return nil
}
