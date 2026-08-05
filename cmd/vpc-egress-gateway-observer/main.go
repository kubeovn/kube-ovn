package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/vegoobserver"
)

func main() {
	configPath := flag.String("config", "/etc/kube-ovn-observer/config.json", "Path to observer configuration")
	networkStatusPath := flag.String("network-status", "/etc/podinfo/network-status", "Path to the Multus network-status annotation")
	listenAddress := flag.String("listen-address", vegoobserver.DefaultListenAddress, "HTTP listen address")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if err := vegoobserver.Run(ctx, vegoobserver.Options{
		ConfigPath: *configPath, NetworkStatusPath: *networkStatusPath, ListenAddress: *listenAddress,
		Pod: os.Getenv("POD_NAME"), Node: os.Getenv("NODE_NAME"), Stdout: os.Stdout, Stderr: os.Stderr,
	}); err != nil {
		klog.Fatalf("VPC egress gateway observer failed: %v", err)
	}
}
