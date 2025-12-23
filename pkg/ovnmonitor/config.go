package ovnmonitor

import (
	"flag"

	"github.com/spf13/pflag"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// Configuration contains parameters information.
type Configuration struct {
	PollTimeout                     int
	PollInterval                    int
	SystemRunDir                    string
	DatabaseVswitchName             string
	DatabaseVswitchSocketRemote     string
	DatabaseVswitchFileDataPath     string
	DatabaseVswitchFileLogPath      string
	DatabaseVswitchFilePidPath      string
	DatabaseVswitchFileSystemIDPath string
	DatabaseNorthboundName          string
	DatabaseNorthboundSocketRemote  string
	DatabaseNorthboundSocketControl string
	DatabaseNorthboundFileDataPath  string
	DatabaseNorthboundFileLogPath   string
	DatabaseNorthboundFilePidPath   string
	DatabaseNorthboundPortDefault   int
	DatabaseNorthboundPortSsl       int
	DatabaseNorthboundPortRaft      int
	DatabaseSouthboundName          string
	DatabaseSouthboundSocketRemote  string
	DatabaseSouthboundSocketControl string
	DatabaseSouthboundFileDataPath  string
	DatabaseSouthboundFileLogPath   string
	DatabaseSouthboundFilePidPath   string
	DatabaseSouthboundPortDefault   int
	DatabaseSouthboundPortSsl       int
	DatabaseSouthboundPortRaft      int
	ServiceVswitchdFileLogPath      string
	ServiceVswitchdFilePidPath      string
	ServiceNorthdFileLogPath        string
	ServiceNorthdFilePidPath        string
	EnableMetrics                   bool
	SecureServing                   bool
	MetricsPort                     int32
	LogPerm                         string

	// TLS configuration for secure serving
	TLSMinVersion   string
	TLSMaxVersion   string
	TLSCipherSuites []string
}

// ParseFlags get parameters information.
func ParseFlags() (*Configuration, error) {
	var (
		argPollTimeout   = pflag.Int("ovs.timeout", 2, "Timeout on JSON-RPC requests to OVN.")
		argPollInterval  = pflag.Int("ovs.poll-interval", 30, "The minimum interval (in seconds) between collections from OVN server.")
		argEnableMetrics = pflag.Bool("enable-metrics", true, "Whether to support metrics query")
		argSecureServing = pflag.Bool("secure-serving", false, "Whether to serve metrics securely")
		argMetricsPort   = pflag.Int32("metrics-port", 10661, "The port to get metrics data")

		argSystemRunDir                    = pflag.String("system.run.dir", "/var/run/openvswitch", "OVS default run directory.")
		argDatabaseVswitchName             = pflag.String("database.vswitch.name", "Open_vSwitch", "The name of OVS db.")
		argDatabaseVswitchSocketRemote     = pflag.String("database.vswitch.socket.remote", "unix:/var/run/openvswitch/db.sock", "JSON-RPC unix socket to OVS db.")
		argDatabaseVswitchFileDataPath     = pflag.String("database.vswitch.file.data.path", "/etc/openvswitch/conf.db", "OVS db file.")
		argDatabaseVswitchFileLogPath      = pflag.String("database.vswitch.file.log.path", "/var/log/openvswitch/ovsdb-server.log", "OVS db log file.")
		argDatabaseVswitchFilePidPath      = pflag.String("database.vswitch.file.pid.path", "/var/run/openvswitch/ovsdb-server.pid", "OVS db process id file.")
		argDatabaseVswitchFileSystemIDPath = pflag.String("database.vswitch.file.system.id.path", "/etc/openvswitch/system-id.conf", "OVS system id file.")

		argDatabaseNorthboundName          = pflag.String("database.northbound.name", ovnnb.DatabaseName, "The name of OVN NB (northbound) db.")
		argDatabaseNorthboundSocketRemote  = pflag.String("database.northbound.socket.remote", "unix:/run/ovn/ovnnb_db.sock", "JSON-RPC unix socket to OVN NB db.")
		argDatabaseNorthboundSocketControl = pflag.String("database.northbound.socket.control", "unix:/run/ovn/ovnnb_db.ctl", "JSON-RPC unix socket to OVN NB app.")
		argDatabaseNorthboundFileDataPath  = pflag.String("database.northbound.file.data.path", "/etc/ovn/ovnnb_db.db", "OVN NB db file.")
		argDatabaseNorthboundFileLogPath   = pflag.String("database.northbound.file.log.path", "/var/log/ovn/ovsdb-server-nb.log", "OVN NB db log file.")
		argDatabaseNorthboundFilePidPath   = pflag.String("database.northbound.file.pid.path", "/run/ovn/ovnnb_db.pid", "OVN NB db process id file.")
		argDatabaseNorthboundPortDefault   = pflag.Int("database.northbound.port.default", int(util.NBDatabasePort), "OVN NB db network socket port.")
		argDatabaseNorthboundPortSsl       = pflag.Int("database.northbound.port.ssl", 6631, "OVN NB db network socket secure port.")
		argDatabaseNorthboundPortRaft      = pflag.Int("database.northbound.port.raft", int(util.NBRaftPort), "OVN NB db network port for clustering (raft)")

		argDatabaseSouthboundName          = pflag.String("database.southbound.name", ovnsb.DatabaseName, "The name of OVN SB (southbound) db.")
		argDatabaseSouthboundSocketRemote  = pflag.String("database.southbound.socket.remote", "unix:/run/ovn/ovnsb_db.sock", "JSON-RPC unix socket to OVN SB db.")
		argDatabaseSouthboundSocketControl = pflag.String("database.southbound.socket.control", "unix:/run/ovn/ovnsb_db.ctl", "JSON-RPC unix socket to OVN SB app.")
		argDatabaseSouthboundFileDataPath  = pflag.String("database.southbound.file.data.path", "/etc/ovn/ovnsb_db.db", "OVN SB db file.")
		argDatabaseSouthboundFileLogPath   = pflag.String("database.southbound.file.log.path", "/var/log/ovn/ovsdb-server-sb.log", "OVN SB db log file.")
		argDatabaseSouthboundFilePidPath   = pflag.String("database.southbound.file.pid.path", "/run/ovn/ovnsb_db.pid", "OVN SB db process id file.")
		argDatabaseSouthboundPortDefault   = pflag.Int("database.southbound.port.default", int(util.SBDatabasePort), "OVN SB db network socket port.")
		argDatabaseSouthboundPortSsl       = pflag.Int("database.southbound.port.ssl", 6632, "OVN SB db network socket secure port.")
		argDatabaseSouthboundPortRaft      = pflag.Int("database.southbound.port.raft", int(util.SBRaftPort), "OVN SB db network port for clustering (raft)")

		argServiceVswitchdFileLogPath = pflag.String("service.vswitchd.file.log.path", "/var/log/openvswitch/ovs-vswitchd.log", "OVS vswitchd daemon log file.")
		argServiceVswitchdFilePidPath = pflag.String("service.vswitchd.file.pid.path", "/var/run/openvswitch/ovs-vswitchd.pid", "OVS vswitchd daemon process id file.")
		argServiceNorthdFileLogPath   = pflag.String("service.ovn.northd.file.log.path", "/var/log/ovn/ovn-northd.log", "OVN northd daemon log file.")
		argServiceNorthdFilePidPath   = pflag.String("service.ovn.northd.file.pid.path", "/var/run/ovn/ovn-northd.pid", "OVN northd daemon process id file.")

		argLogPerm = pflag.String("log-perm", "640", "The permission for the log file")

		argTLSMinVersion   = pflag.String("tls-min-version", "", "The minimum TLS version to use for secure serving. Supported values: TLS10, TLS11, TLS12, TLS13. If not set, the default is used based on the Go version.")
		argTLSMaxVersion   = pflag.String("tls-max-version", "", "The maximum TLS version to use for secure serving. Supported values: TLS10, TLS11, TLS12, TLS13. If not set, the default is used based on the Go version.")
		argTLSCipherSuites = pflag.StringSlice("tls-cipher-suites", nil, "Comma-separated list of TLS cipher suite names to use for secure serving (e.g., 'TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384'). Names must match Go's crypto/tls package. See Go documentation for available suites. If not set, defaults are used. Users are responsible for selecting secure cipher suites.")
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

	config := &Configuration{
		PollTimeout:                     *argPollTimeout,
		PollInterval:                    *argPollInterval,
		SystemRunDir:                    *argSystemRunDir,
		DatabaseVswitchName:             *argDatabaseVswitchName,
		DatabaseVswitchSocketRemote:     *argDatabaseVswitchSocketRemote,
		DatabaseVswitchFileDataPath:     *argDatabaseVswitchFileDataPath,
		DatabaseVswitchFileLogPath:      *argDatabaseVswitchFileLogPath,
		DatabaseVswitchFilePidPath:      *argDatabaseVswitchFilePidPath,
		DatabaseVswitchFileSystemIDPath: *argDatabaseVswitchFileSystemIDPath,
		DatabaseNorthboundName:          *argDatabaseNorthboundName,
		DatabaseNorthboundSocketRemote:  *argDatabaseNorthboundSocketRemote,
		DatabaseNorthboundSocketControl: *argDatabaseNorthboundSocketControl,
		DatabaseNorthboundFileDataPath:  *argDatabaseNorthboundFileDataPath,
		DatabaseNorthboundFileLogPath:   *argDatabaseNorthboundFileLogPath,
		DatabaseNorthboundFilePidPath:   *argDatabaseNorthboundFilePidPath,
		DatabaseNorthboundPortDefault:   *argDatabaseNorthboundPortDefault,
		DatabaseNorthboundPortSsl:       *argDatabaseNorthboundPortSsl,
		DatabaseNorthboundPortRaft:      *argDatabaseNorthboundPortRaft,

		DatabaseSouthboundName:          *argDatabaseSouthboundName,
		DatabaseSouthboundSocketRemote:  *argDatabaseSouthboundSocketRemote,
		DatabaseSouthboundSocketControl: *argDatabaseSouthboundSocketControl,
		DatabaseSouthboundFileDataPath:  *argDatabaseSouthboundFileDataPath,
		DatabaseSouthboundFileLogPath:   *argDatabaseSouthboundFileLogPath,
		DatabaseSouthboundFilePidPath:   *argDatabaseSouthboundFilePidPath,
		DatabaseSouthboundPortDefault:   *argDatabaseSouthboundPortDefault,
		DatabaseSouthboundPortSsl:       *argDatabaseSouthboundPortSsl,
		DatabaseSouthboundPortRaft:      *argDatabaseSouthboundPortRaft,
		ServiceVswitchdFileLogPath:      *argServiceVswitchdFileLogPath,
		ServiceVswitchdFilePidPath:      *argServiceVswitchdFilePidPath,
		ServiceNorthdFileLogPath:        *argServiceNorthdFileLogPath,
		ServiceNorthdFilePidPath:        *argServiceNorthdFilePidPath,
		EnableMetrics:                   *argEnableMetrics,
		SecureServing:                   *argSecureServing,
		MetricsPort:                     *argMetricsPort,
		LogPerm:                         *argLogPerm,
		TLSMinVersion:                   *argTLSMinVersion,
		TLSMaxVersion:                   *argTLSMaxVersion,
		TLSCipherSuites:                 *argTLSCipherSuites,
	}

	klog.Infof("ovn monitor config is %+v", config)
	return config, nil
}
