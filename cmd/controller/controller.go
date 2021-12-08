package controller

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof" // #nosec
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	v1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/sample-controller/pkg/signals"

	"github.com/kubeovn/kube-ovn/pkg/controller"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/versions"
)

func CmdMain() {
	defer klog.Flush()

	stopCh := signals.SetupSignalHandler()
	klog.Infof(versions.String())

	controller.InitClientGoMetrics()
	controller.InitWorkQueueMetrics()
	config, err := controller.ParseFlags()
	if err != nil {
		klog.Fatalf("parse config failed %v", err)
	}

	if err := checkPermission(config); err != nil {
		klog.Fatalf("failed to check permission %v", err)
	}

	go loopOvnNbctlDaemon(config)
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		klog.Fatal(http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", config.PprofPort), nil))
	}()

	ctl := controller.NewController(config)
	ctl.Run(stopCh)
}

func loopOvnNbctlDaemon(config *controller.Configuration) {
	for {
		daemonSocket := os.Getenv("OVN_NB_DAEMON")
		time.Sleep(5 * time.Second)

		if _, err := os.Stat(daemonSocket); os.IsNotExist(err) || daemonSocket == "" {
			if err := ovs.StartOvnNbctlDaemon(config.OvnNbAddr); err != nil {
				klog.Errorf("failed to start ovn-nbctl daemon %v", err)
			}
		}

		// ovn-nbctl daemon may hang and cannot process further request.
		// In case of that, we need to start a new daemon.
		if err := ovs.CheckAlive(); err != nil {
			klog.Warningf("ovn-nbctl daemon doesn't return, start a new daemon")
			if err := ovs.StartOvnNbctlDaemon(config.OvnNbAddr); err != nil {
				klog.Errorf("failed to start ovn-nbctl daemon %v", err)
			}
		}
	}
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
			return fmt.Errorf("no permission to wath resource %s, %s", res, ssar.Status.Reason)
		}
	}
	return nil
}
