package main

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/alauda/kube-ovn/pkg/controller"
	"k8s.io/klog"
	"k8s.io/sample-controller/pkg/signals"
)

func main() {
	defer klog.Flush()

	stopCh := signals.SetupSignalHandler()

	config, err := controller.ParseFlags()
	if err != nil {
		klog.Errorf("parse config failed %v", err)
		os.Exit(1)
	}

	go loopOvnNbctlDaemon(config)
	go func() {
		klog.Fatal(http.ListenAndServe(fmt.Sprintf("localhost:%d", config.PprofPort), nil))
	}()

	err = controller.InitClusterRouter(config)
	if err != nil {
		klog.Errorf("init cluster router failed %v", err)
		os.Exit(1)
	}

	err = controller.InitLoadBalancer(config)
	if err != nil {
		klog.Errorf("init load balancer failed %v", err)
		os.Exit(1)
	}

	err = controller.InitNodeSwitch(config)
	if err != nil {
		klog.Errorf("init node switch failed %v", err)
		os.Exit(1)
	}

	err = controller.InitDefaultLogicalSwitch(config)
	if err != nil {
		klog.Errorf("init default switch failed %v", err)
		os.Exit(1)
	}

	ctl := controller.NewController(config)
	ctl.Run(stopCh)
}

func loopOvnNbctlDaemon(config *controller.Configuration) {
	for {
		daemonSocket := os.Getenv("OVN_NB_DAEMON")
		time.Sleep(5 * time.Second)

		if _, err := os.Stat(daemonSocket); os.IsNotExist(err) || daemonSocket == "" {
			startOvnNbctlDaemon(config.OvnNbHost, config.OvnNbPort)
		}
	}
}

func startOvnNbctlDaemon(nbHost string, nbPort int) (string, error) {
	klog.Infof("start ovn-nbctl daemon")
	output, err := exec.Command(
		"ovn-nbctl",
		fmt.Sprintf("--db=tcp:%s:%d", nbHost, nbPort),
		"--pidfile",
		"--detach",
	).CombinedOutput()
	if err != nil {
		klog.Errorf("start ovn-nbctl daemon failed, %s", string(output))
		return "", err
	}

	daemonSocket := strings.TrimSpace(string(output))
	os.Setenv("OVN_NB_DAEMON", daemonSocket)
	return daemonSocket, nil
}
