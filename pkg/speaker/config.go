package speaker

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"net"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/osrg/gobgp/v4/api"
	"github.com/osrg/gobgp/v4/pkg/apiutil"
	"github.com/osrg/gobgp/v4/pkg/packet/bgp"
	gobgp "github.com/osrg/gobgp/v4/pkg/server"
	"github.com/spf13/pflag"
	"github.com/vishvananda/netlink"
	"google.golang.org/grpc"
	"gopkg.in/yaml.v3"
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

// Duration is a wrapper around time.Duration for YAML serialization.
// Supported formats: string (e.g., "90s", "5m", "1h") or integer (seconds).
type Duration struct {
	time.Duration
}

// UnmarshalYAML implements yaml.Unmarshaler for time.Duration support.
// Accepts both string format ("90s", "5m", "1h") and integer seconds (360).
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		// Check if it's a string scalar
		if node.Tag == "!!str" {
			dur, err := time.ParseDuration(node.Value)
			if err != nil {
				return fmt.Errorf("invalid duration %q: %w", node.Value, err)
			}
			d.Duration = dur
			return nil
		}

		// Check if it's an integer value (seconds)
		if node.Tag == "!!int" {
			var n int64
			if err := node.Decode(&n); err != nil {
				return fmt.Errorf("invalid duration value: %w", err)
			}
			d.Duration = time.Duration(n) * time.Second
			return nil
		}

		return fmt.Errorf("invalid duration value: expected string (e.g., '90s') or integer seconds, got %q", node.Value)
	default:
		return fmt.Errorf("invalid duration value: expected scalar, got %v", node.Kind)
	}
}

func (d Duration) MarshalYAML() (any, error) {
	return d.String(), nil
}

// Seconds returns the duration in seconds as uint32.
func (d Duration) Seconds() uint32 {
	return uint32(d.Duration.Seconds())
}

// IsZero returns true if the duration is not set.
func (d Duration) IsZero() bool {
	return d.Duration == 0
}

// IP is a wrapper around net.IP for YAML serialization.
// It supports string format for both IPv4 and IPv6 addresses (e.g., "192.168.1.1", "2001:db8::1").
// The embedded net.IP provides all standard IP operations while adding YAML marshaling support.
type IP struct {
	net.IP
}

// UnmarshalYAML implements yaml.Unmarshaler for net.IP support.
// Accepts string format for IPv4 and IPv6 addresses.
// Returns an error if the value is not a valid IP address string.
func (ip *IP) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("invalid IP value: expected scalar, got %v", node.Kind)
	}
	// Treat null / empty string as "unset".
	if node.Tag == "!!null" || (node.Tag == "!!str" && node.Value == "") {
		ip.IP = nil
		return nil
	}
	if node.Tag != "!!str" {
		return fmt.Errorf("invalid IP value: expected string, got %s", node.Tag)
	}

	parsedIP := net.ParseIP(node.Value)
	if parsedIP == nil {
		return fmt.Errorf("invalid IP address %q", node.Value)
	}
	ip.IP = parsedIP
	return nil
}

// MarshalYAML implements yaml.Marshaler for net.IP support.
// Returns the string representation of the IP address.
func (ip IP) MarshalYAML() (any, error) {
	if ip.IP == nil {
		return nil, nil
	}
	return ip.IP.String(), nil
}

// String returns the string representation of the IP address.
// Returns an empty string if the IP is nil.
func (ip IP) String() string {
	if ip.IP == nil {
		return ""
	}
	return ip.IP.String()
}

type Configuration struct {
	GrpcHost                    IP                `yaml:"grpc-host,omitempty"`
	GrpcPort                    int32             `yaml:"grpc-port,omitempty"`
	ClusterAs                   uint32            `yaml:"cluster-as,omitempty"`
	RouterID                    IP                `yaml:"router-id,omitempty"`
	PodIPs                      map[string]net.IP `yaml:"-"`
	NodeIPs                     map[string]IP     `yaml:"node-ips,omitempty"`
	NeighborAddresses           []IP              `yaml:"neighbor-address,omitempty"`
	NeighborIPv6Addresses       []IP              `yaml:"neighbor-ipv6-address,omitempty"`
	AllowedSourceAddresses      []IP              `yaml:"allowed-source-addresses,omitempty"`
	AllowedSourceIPv6Addresses  []IP              `yaml:"allowed-source-ipv6-addresses,omitempty"`
	NeighborLocalAddresses      map[string]net.IP `yaml:"-"`
	NeighborAs                  uint32            `yaml:"neighbor-as,omitempty"`
	AuthPassword                string            `yaml:"auth-password,omitempty"`
	HoldTime                    Duration          `yaml:"holdtime,omitempty"`
	BgpServer                   *gobgp.BgpServer  `yaml:"-"`
	AnnounceClusterIP           *bool             `yaml:"announce-cluster-ip,omitempty"`
	GracefulRestart             *bool             `yaml:"graceful-restart,omitempty"`
	GracefulRestartDeferralTime Duration          `yaml:"graceful-restart-deferral-time,omitempty"`
	GracefulRestartTime         Duration          `yaml:"graceful-restart-time,omitempty"`
	PassiveMode                 *bool             `yaml:"passivemode,omitempty"`
	EbgpMultihopTTL             uint8             `yaml:"ebgp-multihop,omitempty"`
	ExtendedNexthop             *bool             `yaml:"extended-nexthop,omitempty"`
	NatGwMode                   *bool             `yaml:"nat-gw-mode,omitempty"`
	EnableMetrics               *bool             `yaml:"enable-metrics,omitempty"`

	// BFD (Bidirectional Forwarding Detection) configuration
	EnableBFD              *bool  `yaml:"enable-bfd,omitempty"`
	BFDMinTX               uint32 `yaml:"bfd-min-tx,omitempty"`               // minimum transmit interval in milliseconds (converted to microseconds for GoBGP)
	BFDMinRX               uint32 `yaml:"bfd-min-rx,omitempty"`               // minimum receive interval in milliseconds (converted to microseconds for GoBGP)
	BFDDetectionMultiplier uint8  `yaml:"bfd-detection-multiplier,omitempty"` // RFC 5880 §6.8.1: valid range 1-255

	NodeName       string               `yaml:"node-name,omitempty"`
	KubeConfigFile string               `yaml:"kubeconfig,omitempty"`
	KubeClient     kubernetes.Interface `yaml:"-"`
	KubeOvnClient  clientset.Interface  `yaml:"-"`

	PprofPort int32  `yaml:"pprof-port,omitempty"`
	LogPerm   string `yaml:"log-perm,omitempty"`

	ConfigFile string `yaml:"-"`
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
		argAllowedSourceAddresses      = pflag.IPSlice("allowed-source-addresses", nil, "Comma separated IPv4 source addresses allowed for BGP peering and next-hop advertisement.")
		argAllowedSourceIPv6Addresses  = pflag.IPSlice("allowed-source-ipv6-addresses", nil, "Comma separated IPv6 source addresses allowed for BGP peering and next-hop advertisement.")
		argNeighborAs                  = pflag.Uint32("neighbor-as", 0, "The AS number of the BGP neighbor/peer (required)")
		argAuthPassword                = pflag.String("auth-password", "", "bgp peer auth password")
		argHoldTime                    = pflag.Duration("holdtime", DefaultBGPHoldtime, "ovn-speaker goes down abnormally, the local saving time of BGP route will be affected.Holdtime must be in the range 3s to 65536s. (default 90s)")
		argPprofPort                   = pflag.Int32("pprof-port", DefaultPprofPort, "The port to get profiling data, default: 10667")
		argNodeName                    = pflag.String("node-name", os.Getenv(util.EnvNodeName), "Name of the node on which the speaker is running on.")
		argKubeConfigFile              = pflag.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information. If not set use the inCluster token.")
		argPassiveMode                 = pflag.BoolP("passivemode", "", false, "Set BGP Speaker to passive model, do not actively initiate connections to peers")
		argEbgpMultihopTTL             = pflag.Uint8("ebgp-multihop", DefaultEbgpMultiHop, "The TTL value of EBGP peer, default: 1")
		argExtendedNexthop             = pflag.BoolP("extended-nexthop", "", false, "Announce IPv4/IPv6 prefixes to every neighbor, no matter their AFI")
		argNatGwMode                   = pflag.BoolP("nat-gw-mode", "", false, "Make the BGP speaker announce EIPs from inside a NAT gateway, Pod IP/Service/Subnet announcements will be disabled")
		argEnableMetrics               = pflag.BoolP("enable-metrics", "", true, "Whether to support metrics query")
		argLogPerm                     = pflag.String("log-perm", "640", "The permission for the log file")
		argEnableBFD                   = pflag.BoolP("enable-bfd", "", false, "Enable BFD (Bidirectional Forwarding Detection) for fast failure detection")
		argBFDMinTX                    = pflag.Uint32("bfd-min-tx", 1000, "BFD minimum transmit interval in milliseconds (default 1000, max 4294967)")
		argBFDMinRX                    = pflag.Uint32("bfd-min-rx", 1000, "BFD minimum receive interval in milliseconds (default 1000, max 4294967)")
		argBFDDetectionMultiplier      = pflag.Uint8("bfd-detection-multiplier", 3, "BFD detection multiplier (default 3, valid range 1-255 per RFC 5880)")
		argConfigFile                  = pflag.String("config", os.Getenv(util.EnvKubeOVNBGPSpeakerConfigFile), "Path to speaker config file in yaml format")
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

	// Convert []net.IP from pflag to []IP for YAML support.
	neighborAddresses := make([]IP, 0, len(*argNeighborAddress))
	for _, ip := range *argNeighborAddress {
		neighborAddresses = append(neighborAddresses, IP{IP: ip})
	}
	neighborIPv6Addresses := make([]IP, 0, len(*argNeighborIPv6Address))
	for _, ip := range *argNeighborIPv6Address {
		neighborIPv6Addresses = append(neighborIPv6Addresses, IP{IP: ip})
	}
	allowedSourceAddresses := make([]IP, 0, len(*argAllowedSourceAddresses))
	for _, ip := range *argAllowedSourceAddresses {
		allowedSourceAddresses = append(allowedSourceAddresses, IP{IP: ip})
	}
	allowedSourceIPv6Addresses := make([]IP, 0, len(*argAllowedSourceIPv6Addresses))
	for _, ip := range *argAllowedSourceIPv6Addresses {
		allowedSourceIPv6Addresses = append(allowedSourceIPv6Addresses, IP{IP: ip})
	}

	config := &Configuration{
		AnnounceClusterIP:           argAnnounceClusterIP,
		GrpcHost:                    IP{IP: *argGrpcHost},
		GrpcPort:                    *argGrpcPort,
		ClusterAs:                   *argClusterAs,
		RouterID:                    IP{IP: *argRouterID},
		NeighborAddresses:           neighborAddresses,
		NeighborIPv6Addresses:       neighborIPv6Addresses,
		AllowedSourceAddresses:      allowedSourceAddresses,
		AllowedSourceIPv6Addresses:  allowedSourceIPv6Addresses,
		NodeIPs:                     map[string]IP{kubeovnv1.ProtocolIPv4: {IP: nodeIPv4}, kubeovnv1.ProtocolIPv6: {IP: nodeIPv6}},
		PodIPs:                      make(map[string]net.IP, 2),
		NeighborAs:                  *argNeighborAs,
		AuthPassword:                *argAuthPassword,
		HoldTime:                    Duration{Duration: *argHoldTime},
		PprofPort:                   *argPprofPort,
		NodeName:                    strings.ToLower(*argNodeName),
		KubeConfigFile:              *argKubeConfigFile,
		GracefulRestart:             argGracefulRestart,
		GracefulRestartDeferralTime: Duration{Duration: *argGracefulRestartDeferralTime},
		GracefulRestartTime:         Duration{Duration: *argDefaultGracefulTime},
		PassiveMode:                 argPassiveMode,
		EbgpMultihopTTL:             *argEbgpMultihopTTL,
		ExtendedNexthop:             argExtendedNexthop,
		NatGwMode:                   argNatGwMode,
		EnableMetrics:               argEnableMetrics,
		LogPerm:                     *argLogPerm,
		EnableBFD:                   argEnableBFD,
		BFDMinTX:                    *argBFDMinTX,
		BFDMinRX:                    *argBFDMinRX,
		BFDDetectionMultiplier:      *argBFDDetectionMultiplier,
		ConfigFile:                  *argConfigFile,
	}

	if config.ConfigFile != "" {
		fileConfig, err := config.loadFileConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to load speaker config, %w", err)
		}

		if fileConfig != nil {
			config.mergeFileConfig(fileConfig)
		}
	}

	if podIPv4 != "" {
		if ip := net.ParseIP(podIPv4); ip != nil {
			config.PodIPs[kubeovnv1.ProtocolIPv4] = ip
		} else {
			return nil, fmt.Errorf("failed to parse pod IPv4 address %q", podIPv4)
		}
	}
	if podIPv6 != "" {
		if ip := net.ParseIP(podIPv6); ip != nil {
			config.PodIPs[kubeovnv1.ProtocolIPv6] = ip
		} else {
			return nil, fmt.Errorf("failed to parse pod IPv6 address %q", podIPv6)
		}
	}

	if err := config.validateRequiredFlags(); err != nil {
		return nil, err
	}

	// Validate holdtime range after merging file config
	if config.HoldTime.Duration > 65536*time.Second || config.HoldTime.Duration < 3*time.Second {
		return nil, errors.New("the bgp holdtime must be in the range 3s to 65536s")
	}

	if config.EbgpMultihopTTL == 0 {
		return nil, errors.New("the bgp MultihopTtl must be in the range 1 to 255")
	}

	for _, addr := range config.NeighborAddresses {
		if addr.To4() == nil {
			return nil, fmt.Errorf("invalid neighbor-address format: expected IPv4, got %v", addr.IP)
		}
	}
	for _, addr := range config.NeighborIPv6Addresses {
		if addr.To4() != nil {
			return nil, fmt.Errorf("invalid neighbor-ipv6-address format: expected IPv6, got %v", addr.IP)
		}
	}
	for _, addr := range config.AllowedSourceAddresses {
		if addr.To4() == nil {
			return nil, fmt.Errorf("invalid allowed-source-addresses format: expected IPv4, got %v", addr.IP)
		}
	}
	for _, addr := range config.AllowedSourceIPv6Addresses {
		if addr.To4() != nil {
			return nil, fmt.Errorf("invalid allowed-source-ipv6-addresses format: expected IPv6, got %v", addr.IP)
		}
	}

	// RouterID must be IPv4 per BGP spec (32-bit). Reject IPv6 values early.
	if config.RouterID.IP != nil && config.RouterID.To4() == nil {
		return nil, fmt.Errorf("invalid router-id: expected IPv4, got %v", config.RouterID.IP)
	}

	if config.RouterID.IP == nil {
		if podIPv4 != "" {
			config.RouterID = IP{IP: net.ParseIP(podIPv4)}
		}

		if config.RouterID.IP == nil {
			// RouterID must be an IPv4. If no IPv4 exists on the speaker, fallback to 0.0.0.0 to avoid GoBGP crashing.
			config.RouterID = IP{IP: net.ParseIP("0.0.0.0")}
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

// loadFileConfig loads the speaker config from the file.
func (config *Configuration) loadFileConfig() (*Configuration, error) {
	data, err := os.ReadFile(config.ConfigFile)
	if err != nil {
		return nil, err
	}

	var cfg Configuration
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// mergeFileConfig merges the file config into the current config.
// Variables from the file will override command line arguments and the default configuration.
func (config *Configuration) mergeFileConfig(cfg *Configuration) {
	if cfg.GrpcHost.IP != nil {
		config.GrpcHost = cfg.GrpcHost
	}
	if cfg.GrpcPort != 0 {
		config.GrpcPort = cfg.GrpcPort
	}
	if cfg.ClusterAs != 0 {
		config.ClusterAs = cfg.ClusterAs
	}
	if cfg.RouterID.IP != nil {
		config.RouterID = cfg.RouterID
	}
	if len(cfg.NodeIPs) != 0 {
		if config.NodeIPs == nil {
			config.NodeIPs = map[string]IP{}
		}
		for k, v := range cfg.NodeIPs {
			if v.IP != nil {
				config.NodeIPs[k] = v
			}
		}
	}
	if len(cfg.NeighborAddresses) != 0 {
		config.NeighborAddresses = cfg.NeighborAddresses
	}
	if len(cfg.NeighborIPv6Addresses) != 0 {
		config.NeighborIPv6Addresses = cfg.NeighborIPv6Addresses
	}
	if len(cfg.AllowedSourceAddresses) != 0 {
		config.AllowedSourceAddresses = cfg.AllowedSourceAddresses
	}
	if len(cfg.AllowedSourceIPv6Addresses) != 0 {
		config.AllowedSourceIPv6Addresses = cfg.AllowedSourceIPv6Addresses
	}
	if cfg.NeighborAs != 0 {
		config.NeighborAs = cfg.NeighborAs
	}
	if cfg.AuthPassword != "" {
		config.AuthPassword = cfg.AuthPassword
	}
	if !cfg.HoldTime.IsZero() {
		config.HoldTime = cfg.HoldTime
	}
	// Boolean pointer fields: only merge if explicitly set in YAML (non-nil)
	if cfg.AnnounceClusterIP != nil {
		config.AnnounceClusterIP = cfg.AnnounceClusterIP
	}
	if cfg.GracefulRestart != nil {
		config.GracefulRestart = cfg.GracefulRestart
	}
	if !cfg.GracefulRestartDeferralTime.IsZero() {
		config.GracefulRestartDeferralTime = cfg.GracefulRestartDeferralTime
	}
	if !cfg.GracefulRestartTime.IsZero() {
		config.GracefulRestartTime = cfg.GracefulRestartTime
	}
	if cfg.PassiveMode != nil {
		config.PassiveMode = cfg.PassiveMode
	}
	if cfg.EbgpMultihopTTL != 0 {
		config.EbgpMultihopTTL = cfg.EbgpMultihopTTL
	}
	if cfg.ExtendedNexthop != nil {
		config.ExtendedNexthop = cfg.ExtendedNexthop
	}
	if cfg.NatGwMode != nil {
		config.NatGwMode = cfg.NatGwMode
	}
	if cfg.EnableMetrics != nil {
		config.EnableMetrics = cfg.EnableMetrics
	}
	if cfg.EnableBFD != nil {
		config.EnableBFD = cfg.EnableBFD
	}
	if cfg.BFDMinTX != 0 {
		config.BFDMinTX = cfg.BFDMinTX
	}
	if cfg.BFDMinRX != 0 {
		config.BFDMinRX = cfg.BFDMinRX
	}
	if cfg.BFDDetectionMultiplier != 0 {
		config.BFDDetectionMultiplier = cfg.BFDDetectionMultiplier
	}
	if cfg.NodeName != "" {
		config.NodeName = strings.ToLower(cfg.NodeName)
	}
	if cfg.KubeConfigFile != "" {
		config.KubeConfigFile = cfg.KubeConfigFile
	}
	if cfg.PprofPort != 0 {
		config.PprofPort = cfg.PprofPort
	}
	if cfg.LogPerm != "" {
		config.LogPerm = cfg.LogPerm
	}
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
	// NodeName is only used for the BGP "local" policy match in syncSubnetRoutes;
	// NAT GW mode runs syncEIPRoutes exclusively and never reads NodeName, so skip
	// the requirement there to stay compatible with GenNatGwBgpSpeakerContainer.
	if (config.NatGwMode == nil || !*config.NatGwMode) && config.NodeName == "" {
		missingFlags = append(missingFlags, "--node-name must be specified (usually via NODE_NAME env from downward API)")
	}

	if config.EnableBFD != nil && *config.EnableBFD {
		if config.BFDDetectionMultiplier == 0 {
			missingFlags = append(missingFlags, "--bfd-detection-multiplier must be between 1 and 255")
		}
		if config.BFDMinTX == 0 {
			missingFlags = append(missingFlags, "--bfd-min-tx must be > 0")
		} else if config.BFDMinTX > math.MaxUint32/1000 {
			missingFlags = append(missingFlags, "--bfd-min-tx must be <= 4294967 ms to avoid uint32 overflow in ms-to-μs conversion")
		}
		if config.BFDMinRX == 0 {
			missingFlags = append(missingFlags, "--bfd-min-rx must be > 0")
		} else if config.BFDMinRX > math.MaxUint32/1000 {
			missingFlags = append(missingFlags, "--bfd-min-rx must be <= 4294967 ms to avoid uint32 overflow in ms-to-μs conversion")
		}
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
	if config.GracefulRestartTime.Duration > 4095*time.Second || config.GracefulRestartTime.Duration <= 0 {
		return errors.New("GracefulRestartTime should be less than 4095 seconds or more than 0")
	}
	if config.GracefulRestartDeferralTime.Duration > 18*time.Hour || config.GracefulRestartDeferralTime.Duration <= 0 {
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
		gobgp.GrpcListenAddress(util.JoinHostPort(config.GrpcHost.IP.String(), config.GrpcPort)),
		gobgp.GrpcOption(grpcOpts),
		gobgp.LoggerOption(slog.Default(), &logLevel),
	)
	go s.Serve()

	peersMap := map[api.Family_Afi][]IP{
		api.Family_AFI_IP:  config.NeighborAddresses,
		api.Family_AFI_IP6: config.NeighborIPv6Addresses,
	}

	if config.PassiveMode != nil && *config.PassiveMode {
		listenPort = bgp.BGP_PORT
	}

	if err := config.initNeighborLocalAddresses(); err != nil {
		err = fmt.Errorf("failed to initialize BGP peer local addresses: %w", err)
		klog.Error(err)
		return err
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
			transport := &api.Transport{
				PassiveMode: config.PassiveMode != nil && *config.PassiveMode,
			}
			if localAddr := config.getNeighborLocalAddress(addr.IP); localAddr != nil {
				transport.LocalAddress = localAddr.String()
			}
			peer := &api.Peer{
				Timers: &api.Timers{Config: &api.TimersConfig{HoldTime: uint64(config.HoldTime.Seconds())}},
				Conf: &api.PeerConf{
					NeighborAddress: addr.IP.String(),
					PeerAsn:         config.NeighborAs,
				},
				Transport: transport,
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
			if config.GracefulRestart != nil && *config.GracefulRestart {
				if err := config.checkGracefulRestartOptions(); err != nil {
					err = fmt.Errorf("failed to check graceful restart options: %w", err)
					klog.Error(err)
					return err
				}
				peer.GracefulRestart = &api.GracefulRestart{
					Enabled:         true,
					RestartTime:     config.GracefulRestartTime.Seconds(),
					DeferralTime:    config.GracefulRestartDeferralTime.Seconds(),
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
			if config.ExtendedNexthop != nil && *config.ExtendedNexthop {
				peer.AfiSafis = append(peer.AfiSafis, &api.AfiSafi{
					Config: &api.AfiSafiConfig{
						Family: &api.Family{
							Afi:  api.Family_AFI_IP,
							Safi: api.Family_SAFI_UNICAST,
						},
					},
				})
			}

			peer.Bfd = newBFDPeerConfig(config)
			if peer.Bfd != nil {
				klog.Infof("BFD enabled for peer %s: MinTX=%dms(%dμs), MinRX=%dms(%dμs), Multiplier=%d",
					addr.String(), config.BFDMinTX, config.BFDMinTX*1000,
					config.BFDMinRX, config.BFDMinRX*1000, config.BFDDetectionMultiplier)
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
	for i := range addPeerMaxRetries {
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
	klog.Infof("BGP Peer Configuration: NeighborAddress=%s, LocalAddress=%s, PeerAsn=%d, HoldTime=%d, PassiveMode=%v, EbgpMultihop=%v, GracefulRestart=%v, AfiSafis=%v, BFD=%v",
		peer.Conf.NeighborAddress,
		peer.Transport.LocalAddress,
		peer.Conf.PeerAsn,
		peer.Timers.Config.HoldTime,
		peer.Transport.PassiveMode,
		peer.EbgpMultihop,
		peer.GracefulRestart,
		peer.AfiSafis,
		peer.Bfd)
}

// watchPeerState monitors BGP peer state changes and logs detailed information
// including local address when peers go up or down.
func (config *Configuration) watchPeerState() {
	err := config.BgpServer.WatchEvent(context.Background(), gobgp.WatchEventMessageCallbacks{
		OnPeerUpdate: func(peer *apiutil.WatchEventMessage_PeerEvent, _ time.Time) {
			if peer == nil || peer.Type != apiutil.PEER_EVENT_STATE {
				return
			}
			p := peer.Peer
			neighborAddr := p.Conf.NeighborAddress.String()

			var bfdState string
			if config.EnableBFD != nil && *config.EnableBFD {
				config.BgpServer.ListBfdPeer(context.Background(), func(addr string, st *api.BfdPeerState) {
					if addr == neighborAddr && st != nil {
						bfdState = bfdSessionStateString(st.SessionState)
					}
				})
			}

			if bfdState != "" {
				klog.Infof("BGP peer state changed: neighbor=%s, state=%s, localAddress=%s, peerAS=%d, bfd=%s",
					neighborAddr, p.State.SessionState, p.Transport.LocalAddress, p.Conf.PeerASN, bfdState)
			} else {
				klog.Infof("BGP peer state changed: neighbor=%s, state=%s, localAddress=%s, peerAS=%d",
					neighborAddr, p.State.SessionState, p.Transport.LocalAddress, p.Conf.PeerASN)
			}
		},
	}, gobgp.WatchPeer())
	if err != nil {
		klog.Errorf("failed to watch peer state: %v", err)
	}
}

func (config *Configuration) initNeighborLocalAddresses() error {
	config.NeighborLocalAddresses = make(map[string]net.IP, len(config.NeighborAddresses)+len(config.NeighborIPv6Addresses))

	for _, neighbor := range config.NeighborAddresses {
		if len(config.AllowedSourceAddresses) != 0 {
			klog.Infof("Resolving BGP local address for neighbor %s with allowed IPv4 source addresses %v", neighbor.String(), config.AllowedSourceAddresses)
			localAddr, err := config.resolveWhitelistedNeighborLocalAddress(neighbor.IP, toNetIPs(config.AllowedSourceAddresses))
			if err != nil {
				return err
			}
			config.NeighborLocalAddresses[neighbor.IP.String()] = localAddr
		}
	}

	for _, neighbor := range config.NeighborIPv6Addresses {
		if len(config.AllowedSourceIPv6Addresses) != 0 {
			klog.Infof("Resolving BGP local address for neighbor %s with allowed IPv6 source addresses %v", neighbor.String(), config.AllowedSourceIPv6Addresses)
			localAddr, err := config.resolveWhitelistedNeighborLocalAddress(neighbor.IP, toNetIPs(config.AllowedSourceIPv6Addresses))
			if err != nil {
				return err
			}
			config.NeighborLocalAddresses[neighbor.IP.String()] = localAddr
		}
	}

	return nil
}

func (config *Configuration) getNeighborLocalAddress(neighborAddress net.IP) net.IP {
	if localAddr := config.NeighborLocalAddresses[neighborAddress.String()]; localAddr != nil {
		return localAddr
	}

	neighborIsIPv4 := neighborAddress.To4() != nil
	if neighborIsIPv4 && len(config.AllowedSourceAddresses) > 0 {
		panic(fmt.Sprintf("invariant violated: failed to determine local address for BGP neighbor %s: no allowed source address matched the whitelist", neighborAddress))
	}
	if !neighborIsIPv4 && len(config.AllowedSourceIPv6Addresses) > 0 {
		panic(fmt.Sprintf("invariant violated: failed to determine local address for BGP neighbor %s: no allowed IPv6 source address matched the whitelist", neighborAddress))
	}

	return nil
}

func (config *Configuration) resolveWhitelistedNeighborLocalAddress(neighborAddress net.IP, allowedLocalAddresses []net.IP) (net.IP, error) {
	routes, err := netlink.RouteGet(neighborAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to determine local address for BGP neighbor %s from route lookup: %w", neighborAddress, err)
	}
	localAddr, err := selectNeighborLocalAddressFromRoutes(neighborAddress, routes, allowedLocalAddresses)
	if err != nil {
		return nil, err
	}
	klog.Infof("Route lookup selected BGP local address %s for neighbor %s from whitelist %v", localAddr, neighborAddress, allowedLocalAddresses)
	return localAddr, nil
}

func selectNeighborLocalAddressFromRoutes(neighborAddress net.IP, routes []netlink.Route, allowedLocalAddresses []net.IP) (net.IP, error) {
	// Track the most useful failure reason across all route candidates so callers get
	// a deterministic error when no source address satisfies the whitelist.
	var (
		sawSource         bool
		firstFamilyErr    error
		firstWhitelistErr error
	)

	// RouteGet reflects the kernel's effective route decision for this neighbor.
	// Prefer the first source address that matches both the neighbor family and the whitelist.
	for _, route := range routes {
		if route.Src == nil {
			continue
		}
		sawSource = true
		if err := validateLocalAddressFamily(neighborAddress, route.Src); err != nil {
			if firstFamilyErr == nil {
				firstFamilyErr = err
			}
			klog.V(4).Infof("Skipping candidate BGP local address %s for neighbor %s: %v", route.Src, neighborAddress, err)
			continue
		}
		if err := validateAllowedLocalAddress(neighborAddress, route.Src, allowedLocalAddresses); err != nil {
			if firstWhitelistErr == nil {
				firstWhitelistErr = err
			}
			klog.V(4).Infof("Skipping candidate BGP local address %s for neighbor %s: %v", route.Src, neighborAddress, err)
			continue
		}
		return route.Src, nil
	}

	if !sawSource {
		return nil, fmt.Errorf("failed to determine local address for BGP neighbor %s: route lookup returned no source address", neighborAddress)
	}
	if firstWhitelistErr != nil {
		return nil, firstWhitelistErr
	}
	if firstFamilyErr != nil {
		if len(allowedLocalAddresses) > 0 {
			return nil, fmt.Errorf("no route source matched the required address family for whitelist evaluation; first family error: %w", firstFamilyErr)
		}
		return nil, firstFamilyErr
	}
	return nil, fmt.Errorf("failed to determine local address for BGP neighbor %s: route lookup returned no valid source address", neighborAddress)
}

func validateLocalAddressFamily(neighborAddress, localAddr net.IP) error {
	if neighborAddress == nil {
		return errors.New("invalid nil BGP neighbor address")
	}
	if localAddr == nil {
		return fmt.Errorf("invalid nil local address for BGP neighbor %s", neighborAddress)
	}

	neighborIsIPv4 := neighborAddress.To4() != nil
	localIsIPv4 := localAddr.To4() != nil
	if neighborIsIPv4 == localIsIPv4 {
		return nil
	}

	if neighborIsIPv4 {
		return fmt.Errorf("invalid local address %s for IPv4 BGP neighbor %s", localAddr, neighborAddress)
	}
	return fmt.Errorf("invalid local address %s for IPv6 BGP neighbor %s", localAddr, neighborAddress)
}

func validateAllowedLocalAddress(neighborAddress, localAddr net.IP, allowedLocalAddresses []net.IP) error {
	if len(allowedLocalAddresses) == 0 {
		return nil
	}
	if slices.ContainsFunc(allowedLocalAddresses, localAddr.Equal) {
		return nil
	}
	return fmt.Errorf("selected local address %s for BGP neighbor %s is not in allowed source address list %v; speaker startup is rejected until route lookup selects an allowed source address", localAddr, neighborAddress, allowedLocalAddresses)
}

// toNetIPs converts a slice of IP wrappers to a slice of net.IP.
// It filters out nil IP addresses to prevent downstream errors.
func toNetIPs(ips []IP) []net.IP {
	result := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		if ip.IP != nil {
			result = append(result, ip.IP)
		}
	}
	return result
}
