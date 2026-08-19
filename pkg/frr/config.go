package frr

import (
	"errors"
	"flag"
	"os"
	"time"

	"github.com/spf13/pflag"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	clientset "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type Configuration struct {
	KubeConfigFile   string
	NodeName         string
	FrrDir           string
	DebounceInterval time.Duration
	ReassertInterval time.Duration
	ResyncInterval   time.Duration
	EnableMetrics    bool
	PprofPort        int32
	LogPerm          string

	KubeClient    kubernetes.Interface
	KubeOvnClient clientset.Interface
}

func ParseFlags() (*Configuration, error) {
	var (
		argKubeConfigFile   = pflag.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information. If not set use the inCluster token.")
		argFrrDir           = pflag.String("frr-dir", "/etc/frr", "Directory shared with the FRR container")
		argDebounceInterval = pflag.Duration("debounce-interval", 3*time.Second, "Delay before applying accumulated changes")
		argReassertInterval = pflag.Duration("reassert-interval", 15*time.Second, "Interval for re-asserting the desired FRR configuration")
		argResyncInterval   = pflag.Duration("resync-interval", 5*time.Minute, "Interval for full state resynchronization")
		argEnableMetrics    = pflag.Bool("enable-metrics", true, "Whether to support metrics query")
		argPprofPort        = pflag.Int32("pprof-port", 10668, "The port to get profiling data")
		argLogPerm          = pflag.String("log-perm", "640", "The permission for the log file")
	)

	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)

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

	nodeName := os.Getenv(util.EnvNodeName)
	if nodeName == "" {
		return nil, errors.New("environment variable NODE_NAME is not set")
	}

	config := &Configuration{
		KubeConfigFile:   *argKubeConfigFile,
		NodeName:         nodeName,
		FrrDir:           *argFrrDir,
		DebounceInterval: *argDebounceInterval,
		ReassertInterval: *argReassertInterval,
		ResyncInterval:   *argResyncInterval,
		EnableMetrics:    *argEnableMetrics,
		PprofPort:        *argPprofPort,
		LogPerm:          *argLogPerm,
	}

	if err := config.initKubeClient(); err != nil {
		return nil, err
	}

	return config, nil
}

func (config *Configuration) initKubeClient() error {
	var cfg *rest.Config
	var err error
	if config.KubeConfigFile == "" {
		cfg, err = rest.InClusterConfig()
	} else {
		cfg, err = clientcmd.BuildConfigFromFlags("", config.KubeConfigFile)
	}
	if err != nil {
		klog.Errorf("failed to build kubeconfig %v", err)
		return err
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
