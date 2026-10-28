package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/spf13/pflag"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	"github.com/kubeovn/kube-ovn/pkg/ipsec"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/versions"
)

func main() {
	if err := run(); err != nil {
		klog.ErrorS(err, "IPsec agent failed")
		klog.Flush()
		os.Exit(1)
	}
	klog.Flush()
}

func run() error {
	config := ipsec.Configuration{}
	pflag.StringVar(&config.NodeName, "node-name", os.Getenv(util.EnvNodeName), "Node name")
	pflag.StringVar(&config.Namespace, "namespace", os.Getenv(util.EnvPodNamespace), "IPsec trust namespace")
	pflag.StringVar(&config.PodUID, "pod-uid", os.Getenv("POD_UID"), "Current bound Pod UID")
	pflag.StringVar(&config.OVSSocket, "ovs-socket", "/run/openvswitch/db.sock", "Local OVSDB socket")
	pflag.StringVar(&config.KeyDir, "key-dir", "/etc/ovs_ipsec_keys", "Persistent IPsec identity directory")
	pflag.StringVar(&config.RuntimeDir, "runtime-dir", "/run/kube-ovn-ipsec", "Private runtime directory")
	pflag.StringVar(&config.ProtectionDir, "protection-dir", "/run/kube-ovn-ipsec-protection", "Public node protection endpoint directory")
	ovsUUID := pflag.String("ovs-uuid", "", "Current local OVS row UUID required by the protection probe")
	duration := pflag.Int("ovn-ipsec-cert-duration", 2*365*24*60*60, "Requested certificate duration in seconds")
	pflag.DurationVar(&config.RequestTimeout, "request-timeout", 300*time.Second, "Certificate request timeout")
	pflag.IntVar(&config.Priority, "priority", -5, "IPsec subprocess nice priority")
	pflag.BoolVar(&config.CleanupOnly, "cleanup-only", false, "Restore existing protection and reconcile coordinated cleanup without starting IKE or issuing certificates")
	kubeconfig := pflag.String("kubeconfig", "", "Kubernetes client configuration; empty uses in-cluster credentials")
	check := pflag.String("check", "", "Probe the private livez or readyz endpoint")
	klog.InitFlags(flag.CommandLine)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()
	if *check != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if *check == "protection" || *check == "startup" {
			if *check == "startup" {
				return ipsec.CheckStartup(ctx, config.ProtectionDir, *ovsUUID)
			}
			return ipsec.CheckProtection(ctx, config.ProtectionDir, *ovsUUID)
		}
		return ipsec.Check(ctx, config.RuntimeDir, *check)
	}
	config.Duration = time.Duration(*duration) * time.Second
	restConfig, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		return fmt.Errorf("configure IPsec Kubernetes client: %w", err)
	}
	restConfig.Timeout = 30 * time.Second
	if config.Kube, err = kubernetes.NewForConfig(restConfig); err != nil {
		return err
	}
	klog.Info(versions.String())
	agent, err := ipsec.New(config)
	if err != nil {
		return err
	}
	return agent.Run(signals.SetupSignalHandler())
}
