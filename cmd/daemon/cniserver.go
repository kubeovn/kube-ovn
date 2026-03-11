package main

import (
	"errors"
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

	if err := Retry(util.MirrorsRetryMaxTimes, util.MirrorsRetryInterval, daemon.InitMirror, config); err != nil {
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
			listOption.FieldSelector = "spec.nodeName=" + config.NodeName + ",spec.hostNetwork=false"
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
			listOption.FieldSelector = "metadata.name=" + util.DefaultOVNIPSecCA
			listOption.AllowWatchBookmarks = true
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
	metrics.StartPprofServerIfNeeded(ctx, config.EnablePprof, servePprofInMetricsServer, "127.0.0.1", int(config.PprofPort))
	if config.EnableMetrics {
		daemon.InitMetrics()
	}
	metrics.StartMetricsOrHealthServer(ctx, config.EnableMetrics, addrs, int(config.PprofPort), nil, config.SecureServing, servePprofInMetricsServer, config.TLSMinVersion, config.TLSMaxVersion, config.TLSCipherSuites)

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

func Retry(attempts, sleep int, f func(configuration *daemon.Configuration) error, cfg *daemon.Configuration) (err error) {
	for i := 0; ; i++ {
		err = f(cfg)
		if err == nil {
			return nil
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

	chassisName := strings.TrimSpace(string(chassisID))
	if chassisName == "" {
		// not ready yet
		err = errors.New("chassis id is empty")
		klog.Error(err)
		return err
	}
	patch := util.KVPatch{util.ChassisAnnotation: chassisName}
	if err = util.PatchAnnotations(cfg.KubeClient.CoreV1().Nodes(), cfg.NodeName, patch); err != nil {
		klog.Errorf("failed to patch chassis annotation of node %s: %v", cfg.NodeName, err)
		return err
	}

	return nil
}
