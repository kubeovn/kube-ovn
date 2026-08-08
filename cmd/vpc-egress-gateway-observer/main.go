package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/vegobserver"
)

func main() {
	klog.InitFlags(nil)
	configPath := flag.String("config", "/etc/kube-ovn-observer/config.json", "Path to observer configuration")
	networkStatusPath := flag.String("network-status", "/etc/podinfo/network-status", "Path to the Multus network-status annotation")
	listenAddress := flag.String("listen-address", vegobserver.DefaultListenAddress, "HTTP listen address")
	healthCheck := flag.Bool("health-check", false, "Check the local observer HTTP endpoint and exit")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if *healthCheck {
		if err := vegobserver.CheckHealth(ctx, vegobserver.DefaultHealthCheckURL); err != nil {
			klog.Fatalf("VPC egress gateway observer health check failed: %v", err)
		}
		return
	}
	if err := vegobserver.Run(ctx, vegobserver.Options{
		ConfigPath: *configPath, NetworkStatusPath: *networkStatusPath, ListenAddress: *listenAddress,
		Pod: os.Getenv("POD_NAME"), Node: os.Getenv("NODE_NAME"), Stdout: os.Stdout, Stderr: os.Stderr,
	}); err != nil {
		klog.Fatalf("VPC egress gateway observer failed: %v", err)
	}
}
