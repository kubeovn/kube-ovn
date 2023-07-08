package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	v1 "k8s.io/api/authorization/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/controller"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/versions"
)

const ovnLeaderResource = "kube-ovn-controller"

func CmdMain() {
	defer klog.Flush()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		stopCh := signals.SetupSignalHandler().Done()
		<-stopCh
		cancel()
	}()

	klog.Infof(versions.String())

	controller.InitClientGoMetrics()
	controller.InitWorkQueueMetrics()
	util.InitKlogMetrics()
	config, err := controller.ParseFlags()
	if err != nil {
		util.LogFatalAndExit(err, "failed to parse config")
	}

	if err := checkPermission(config); err != nil {
		util.LogFatalAndExit(err, "failed to check permission")
	}

	go func() {
		mux := http.NewServeMux()
		if config.EnableMetrics {
			mux.Handle("/metrics", promhttp.Handler())
		}
		if config.EnablePprof {
			mux.HandleFunc("/debug/pprof/", pprof.Index)
			mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
			mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
			mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
			mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
		}

		addr := "0.0.0.0"
		if os.Getenv("ENABLE_BIND_LOCAL_IP") == "true" {
			podIpsEnv := os.Getenv("POD_IPS")
			podIps := strings.Split(podIpsEnv, ",")
			// when pod in dual mode, golang can't support bind v4 and v6 address in the same time,
			// so not support bind local ip when in dual mode
			if len(podIps) == 1 {
				addr = podIps[0]
				if util.CheckProtocol(podIps[0]) == kubeovnv1.ProtocolIPv6 {
					addr = fmt.Sprintf("[%s]", podIps[0])
				}
			}
		}
		// conform to Gosec G114
		// https://github.com/securego/gosec#available-rules
		server := &http.Server{
			Addr:              fmt.Sprintf("%s:%d", addr, config.PprofPort),
			ReadHeaderTimeout: 3 * time.Second,
			Handler:           mux,
		}
		util.LogFatalAndExit(server.ListenAndServe(), "failed to listen and server on %s", server.Addr)
	}()

	//	ctx, cancel := context.WithCancel(context.Background())
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
