package controller

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"time"

	v1 "k8s.io/api/authorization/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"kernel.org/pub/linux/libs/security/libcap/cap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/controller"
	"github.com/kubeovn/kube-ovn/pkg/metrics"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/versions"
)

const ovnLeaderResource = "kube-ovn-controller"

func CmdMain() {
	defer klog.Flush()

	klog.Infof(versions.String())

	currentCaps := cap.GetProc()
	klog.Infof("current capabilities: %s", currentCaps.String())

	config, err := controller.ParseFlags()
	if err != nil {
		util.LogFatalAndExit(err, "failed to parse config")
	}

	if err := checkPermission(config); err != nil {
		util.LogFatalAndExit(err, "failed to check permission")
	}
	utilruntime.Must(kubeovnv1.AddToScheme(scheme.Scheme))

	ctx := signals.SetupSignalHandler()
	go func() {
		if config.EnablePprof {
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

		if !config.EnableMetrics {
			return
		}
		metrics.InitKlogMetrics()
		metrics.InitClientGoMetrics()
		addr := util.JoinHostPort(util.GetDefaultListenAddr(), config.PprofPort)
		if err := metrics.Run(ctx, config.KubeRestConfig, addr, config.SecureServing); err != nil {
			util.LogFatalAndExit(err, "failed to run metrics server")
		}
		<-ctx.Done()
	}()

	recorder := record.NewBroadcaster().NewRecorder(scheme.Scheme, apiv1.EventSource{
		Component: ovnLeaderResource,
		Host:      os.Getenv(util.HostnameEnv),
	})
	rl, err := resourcelock.NewFromKubeconfig("leases",
		config.PodNamespace,
		ovnLeaderResource,
		resourcelock.ResourceLockConfig{
			Identity:      config.PodName,
			EventRecorder: recorder,
		},
		config.KubeRestConfig,
		20*time.Second)
	if err != nil {
		klog.Fatalf("error creating lock: %v", err)
	}

	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:          rl,
		LeaseDuration: 30 * time.Second,
		RenewDeadline: 20 * time.Second,
		RetryPeriod:   6 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				controller.Run(ctx, config)
			},
			OnStoppedLeading: func() {
				select {
				case <-ctx.Done():
					klog.InfoS("Requested to terminate, exiting")
					os.Exit(0)
				default:
					klog.ErrorS(nil, "leaderelection lost")
					klog.FlushAndExit(klog.ExitFlushTimeout, 1)
				}
			},
		},
		WatchDog:        nil,
		ReleaseOnCancel: true,
		Name:            ovnLeaderResource,
	})
}

func checkPermission(config *controller.Configuration) error {
	resources := []string{"vpcs", "subnets", "ips", "vlans", "vpc-nat-gateways"}
	for _, res := range resources {
		ssar := &v1.SelfSubjectAccessReview{
			Spec: v1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &v1.ResourceAttributes{
					Verb:     "watch",
					Group:    "kubeovn.io",
					Resource: res,
				},
			},
		}
		ssar, err := config.KubeClient.AuthorizationV1().SelfSubjectAccessReviews().Create(context.Background(), ssar, metav1.CreateOptions{})
		if err != nil {
			klog.Errorf("failed to get permission for resource %s, %v", res, err)
			return err
		}
		if !ssar.Status.Allowed {
			return fmt.Errorf("no permission to watch resource %s, %s", res, ssar.Status.Reason)
		}
	}
	return nil
}
