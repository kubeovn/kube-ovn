package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	kubeovninformer "github.com/kubeovn/kube-ovn/pkg/client/informers/externalversions"
	"github.com/kubeovn/kube-ovn/pkg/daemon"
	"github.com/kubeovn/kube-ovn/pkg/metrics"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/versions"
)

func main() {
	defer klog.Flush()
	config := daemon.ParseFlags()
	klog.Info(versions.String())

	if config.InstallCNIConfig {
		if err := mvCNIConf(config.CniConfDir, config.CniConfFile, config.CniConfName); err != nil {
			util.LogFatalAndExit(err, "failed to mv cni config file")
		}
		return
	}
	perm, err := strconv.ParseUint(config.LogPerm, 8, 32)
	if err != nil {
		util.LogFatalAndExit(err, "failed to parse log-perm")
	}
	util.InitLogFilePerm("kube-ovn-cni", os.FileMode(perm))
	printCaps()

	ovs.UpdateOVSVsctlLimiter(config.OVSVsctlConcurrency)

	nicBridgeMappings, err := daemon.InitOVSBridges()
	if err != nil {
		util.LogFatalAndExit(err, "failed to initialize OVS bridges")
	}

	if err = config.Init(nicBridgeMappings); err != nil {
		util.LogFatalAndExit(err, "failed to initialize config")
	}

	if err := Retry(util.ChassisRetryMaxTimes, util.ChassisCniDaemonRetryInterval, initChassisAnno, config); err != nil {
		util.LogFatalAndExit(err, "failed to initialize ovn chassis annotation")
	}

	if err := Retry(util.MirrosRetryMaxTimes, util.MirrosRetryInterval, daemon.InitMirror, config); err != nil {
		util.LogFatalAndExit(err, "failed to initialize ovs mirror")
	}

	klog.Info("init node gw")
	if err = daemon.InitNodeGateway(config); err != nil {
		util.LogFatalAndExit(err, "failed to initialize node gateway")
	}

	if err := initForOS(); err != nil {
		util.LogFatalAndExit(err, "failed to do the OS initialization")
	}

	if config.SetVxlanTxOff && config.NetworkType == util.NetworkTypeVxlan {
		if err := setVxlanNicTxOff(); err != nil {
			util.LogFatalAndExit(err, "failed to do the OS initialization for vxlan case")
		}
	}

	ctrl.SetLogger(klog.NewKlogr())
	ctx := signals.SetupSignalHandler()
	stopCh := ctx.Done()
	podInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(config.KubeClient, 0,
		kubeinformers.WithTweakListOptions(func(listOption *v1.ListOptions) {
			listOption.FieldSelector = "spec.nodeName=" + config.NodeName
			listOption.AllowWatchBookmarks = true
		}))
	nodeInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(config.KubeClient, 0,
		kubeinformers.WithTweakListOptions(func(listOption *v1.ListOptions) {
			listOption.AllowWatchBookmarks = true
		}))
	kubeovnInformerFactory := kubeovninformer.NewSharedInformerFactoryWithOptions(config.KubeOvnClient, 0,
		kubeovninformer.WithTweakListOptions(func(listOption *v1.ListOptions) {
			listOption.AllowWatchBookmarks = true
		}))

	caSecretInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(config.KubeClient, 0,
		kubeinformers.WithTweakListOptions(func(listOption *v1.ListOptions) {
			listOption.FieldSelector = fmt.Sprintf("metadata.name=%s", util.DefaultOVNIPSecCA)
		}),
		kubeinformers.WithNamespace(os.Getenv(util.EnvPodNamespace)),
	)

	ctl, err := daemon.NewController(config, stopCh, podInformerFactory, nodeInformerFactory, caSecretInformerFactory, kubeovnInformerFactory)
	if err != nil {
		util.LogFatalAndExit(err, "failed to create controller")
	}
	klog.Info("start daemon controller")
	go ctl.Run(stopCh)
	go daemon.RunServer(config, ctl)

	addrs := util.GetDefaultListenAddr()
	if config.EnableVerboseConnCheck {
		for _, addr := range addrs {
			go func() {
				connListenaddr := util.JoinHostPort(addr, config.TCPConnCheckPort)
				if err := util.TCPConnectivityListen(connListenaddr); err != nil {
					util.LogFatalAndExit(err, "failed to start TCP listen on addr %s", addr)
				}
			}()
			go func() {
				connListenaddr := util.JoinHostPort(addr, config.UDPConnCheckPort)
				if err := util.UDPConnectivityListen(connListenaddr); err != nil {
					util.LogFatalAndExit(err, "failed to start UDP listen on addr %s", addr)
				}
			}()
		}
	}

	servePprofInMetricsServer := config.EnableMetrics && slices.Contains(addrs, "0.0.0.0")
	if config.EnablePprof && !servePprofInMetricsServer {
		mux := http.NewServeMux()
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

		listerner, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: int(config.PprofPort)})
		if err != nil {
			util.LogFatalAndExit(err, "failed to listen on %s", util.JoinHostPort("127.0.0.1", config.PprofPort))
		}
		svr := manager.Server{
			Name: "pprof",
			Server: &http.Server{
				Handler:           mux,
				MaxHeaderBytes:    1 << 20,
				IdleTimeout:       90 * time.Second,
				ReadHeaderTimeout: 32 * time.Second,
			},
			Listener: listerner,
		}
		go func() {
			if err = svr.Start(ctx); err != nil {
				util.LogFatalAndExit(err, "failed to run pprof server")
			}
		}()
	}

	if config.EnableMetrics {
		daemon.InitMetrics()
		metrics.InitKlogMetrics()
		for _, addr := range addrs {
			listenAddr := util.JoinHostPort(addr, config.PprofPort)
			go func() {
				if err := metrics.Run(ctx, nil, listenAddr, config.SecureServing, servePprofInMetricsServer, config.TLSMinVersion, config.TLSMaxVersion, config.TLSCipherSuites); err != nil {
					util.LogFatalAndExit(err, "failed to run metrics server")
				}
			}()
		}
	} else {
		klog.Info("metrics server is disabled")
		listerner, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP(addrs[0]), Port: int(config.PprofPort)})
		if err != nil {
			util.LogFatalAndExit(err, "failed to listen on %s", util.JoinHostPort(addrs[0], config.PprofPort))
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", util.DefaultHealthCheckHandler)
		mux.HandleFunc("/livez", util.DefaultHealthCheckHandler)
		mux.HandleFunc("/readyz", util.DefaultHealthCheckHandler)
		svr := manager.Server{
			Name: "health-check",
			Server: &http.Server{
				Handler:           mux,
				MaxHeaderBytes:    1 << 20,
				IdleTimeout:       90 * time.Second,
				ReadHeaderTimeout: 32 * time.Second,
			},
			Listener: listerner,
		}
		go func() {
			if err = svr.Start(ctx); err != nil {
				util.LogFatalAndExit(err, "failed to run health check server")
			}
		}()
	}

	<-stopCh
}

func mvCNIConf(configDir, configFile, confName string) error {
	cniConfPath := filepath.Join(configDir, confName)
	if info, err := os.Stat(cniConfPath); err == nil {
		// File exists, check permissions.
		if info.Mode().Perm() == 0o600 {
			klog.Infof("CNI config file %q already exists with correct permissions, skipping.", cniConfPath)
			return nil
		}
		klog.Infof("Fixing permission of existing CNI config file %q to 600", cniConfPath)
		return os.Chmod(cniConfPath, 0o600)
	}

	data, err := os.ReadFile(configFile) // #nosec G304
	if err != nil {
		klog.Errorf("failed to read cni config file %s, %v", configFile, err)
		return err
	}

	klog.Infof("Installing cni config file %q to %q", configFile, cniConfPath)
	return os.WriteFile(cniConfPath, data, 0o600) // #nosec G306
}

func Retry(attempts, sleep int, f func(configuration *daemon.Configuration) error, ctrl *daemon.Configuration) (err error) {
	for i := 0; ; i++ {
		err = f(ctrl)
		if err == nil {
			return err
		}
		if i >= (attempts - 1) {
			break
		}
		time.Sleep(time.Duration(sleep) * time.Second)
	}
	return err
}

func initChassisAnno(cfg *daemon.Configuration) error {
	chassisID, err := os.ReadFile(util.ChassisLoc)
	if err != nil {
		klog.Errorf("read chassis file failed, %v", err)
		return err
	}

	chassesName := strings.TrimSpace(string(chassisID))
	if chassesName == "" {
		// not ready yet
		err = errors.New("chassis id is empty")
		klog.Error(err)
		return err
	}
	patch := util.KVPatch{util.ChassisAnnotation: chassesName}
	if err = util.PatchAnnotations(cfg.KubeClient.CoreV1().Nodes(), cfg.NodeName, patch); err != nil {
		klog.Errorf("failed to patch chassis annotation of node %s: %v", cfg.NodeName, err)
		return err
	}

	return nil
}
