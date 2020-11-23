package pinger

import (
	"context"
	"fmt"
	"math"
	"net"
	"os"
	"time"

	goping "github.com/oilbeater/go-ping"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog"
)

func StartPinger(config *Configuration, e *Exporter) {
	errHappens := false
	for {
		if config.NetworkMode == "kube-ovn" {
			if checkOvs(config) != nil ||
				checkOvnController(config) != nil ||
				checkPortBindings(config) != nil {
				errHappens = true
			}
			e.ovsMetricsUpdate()
		}

		if ping(config) != nil {
			errHappens = true
		}
		if config.Mode != "server" {
			break
		}
		time.Sleep(time.Duration(config.Interval) * time.Second)
	}
	if errHappens && config.ExitCode != 0 {
		os.Exit(config.ExitCode)
	}
}

func ping(config *Configuration) error {
	errHappens := false
	if checkApiServer(config) != nil ||
		pingNodes(config) != nil ||
		pingPods(config) != nil ||
		internalNslookup(config) != nil {
		errHappens = true
	}

	if config.ExternalDNS != "" {
		if externalNslookup(config) != nil {
			errHappens = true
		}
	}

	if config.ExternalAddress != "" {
		if pingExternal(config) != nil {
			errHappens = true
		}
	}
	if errHappens {
		return fmt.Errorf("ping failed")
	}
	return nil
}

func pingNodes(config *Configuration) error {
	klog.Infof("start to check node connectivity")
	nodes, err := config.KubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		klog.Errorf("failed to list nodes, %v", err)
		return err
	}

	var pingErr error
	for _, no := range nodes.Items {
		for _, addr := range no.Status.Addresses {
			if addr.Type == v1.NodeInternalIP {
				func(nodeIP, nodeName string) {
					pinger, err := goping.NewPinger(nodeIP)
					if err != nil {
						klog.Errorf("failed to init pinger, %v", err)
						pingErr = err
						return
					}
					pinger.SetPrivileged(true)
					pinger.Timeout = 30 * time.Second
					pinger.Count = 3
					pinger.Interval = 100 * time.Millisecond
					pinger.Debug = true
					pinger.Run()
					stats := pinger.Statistics()
					klog.Infof("ping node: %s %s, count: %d, loss count %d, average rtt %.2fms",
						nodeName, nodeIP, pinger.Count, int(math.Abs(float64(stats.PacketsSent-stats.PacketsRecv))), float64(stats.AvgRtt)/float64(time.Millisecond))
					if int(math.Abs(float64(stats.PacketsSent-stats.PacketsRecv))) != 0 {
						pingErr = fmt.Errorf("ping failed")
					}
					SetNodePingMetrics(
						config.NodeName,
						config.HostIP,
						config.PodName,
						no.Name, addr.Address,
						float64(stats.AvgRtt)/float64(time.Millisecond),
						int(math.Abs(float64(stats.PacketsSent-stats.PacketsRecv))),
						int(float64(stats.PacketsSent)))
				}(addr.Address, no.Name)
			}
		}
	}
	return pingErr
}

func pingPods(config *Configuration) error {
	klog.Infof("start to check pod connectivity")
	ds, err := config.KubeClient.AppsV1().DaemonSets(config.DaemonSetNamespace).Get(config.DaemonSetName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("failed to get peer ds: %v", err)
		return err
	}
	pods, err := config.KubeClient.CoreV1().Pods(config.DaemonSetNamespace).List(metav1.ListOptions{LabelSelector: labels.Set(ds.Spec.Selector.MatchLabels).String()})
	if err != nil {
		klog.Errorf("failed to list peer pods: %v", err)
		return err
	}

	var pingErr error
	for _, pod := range pods.Items {
		if pod.Status.PodIP != "" {
			func(podIp, podName, nodeIP, nodeName string) {
				pinger, err := goping.NewPinger(podIp)
				if err != nil {
					klog.Errorf("failed to init pinger, %v", err)
					pingErr = err
					return
				}
				pinger.SetPrivileged(true)
				pinger.Timeout = 1 * time.Second
				pinger.Debug = true
				pinger.Count = 3
				pinger.Interval = 1 * time.Millisecond
				pinger.Run()
				stats := pinger.Statistics()
				klog.Infof("ping pod: %s %s, count: %d, loss count %d, average rtt %.2fms",
					podName, podIp, pinger.Count, int(math.Abs(float64(stats.PacketsSent-stats.PacketsRecv))), float64(stats.AvgRtt)/float64(time.Millisecond))
				if int(math.Abs(float64(stats.PacketsSent-stats.PacketsRecv))) != 0 {
					pingErr = fmt.Errorf("ping failed")
				}
				SetPodPingMetrics(
					config.NodeName,
					config.HostIP,
					config.PodName,
					nodeName,
					nodeIP,
					podIp,
					float64(stats.AvgRtt)/float64(time.Millisecond),
					int(math.Abs(float64(stats.PacketsSent-stats.PacketsRecv))),
					int(float64(stats.PacketsSent)))
			}(pod.Status.PodIP, pod.Name, pod.Status.HostIP, pod.Spec.NodeName)
		}
	}
	return pingErr
}

func pingExternal(config *Configuration) error {
	if config.ExternalAddress == "" {
		return nil
	}
	klog.Infof("start to check ping external to %s", config.ExternalAddress)
	pinger, err := goping.NewPinger(config.ExternalAddress)
	if err != nil {
		klog.Errorf("failed to init pinger, %v", err)
		return err
	}
	pinger.SetPrivileged(true)
	pinger.Timeout = 5 * time.Second
	pinger.Debug = true
	pinger.Count = 3
	pinger.Interval = 1 * time.Millisecond
	pinger.Run()
	stats := pinger.Statistics()
	klog.Infof("ping external address: %s, total count: %d, loss count %d, average rtt %.2fms",
		config.ExternalAddress, pinger.Count, int(math.Abs(float64(stats.PacketsSent-stats.PacketsRecv))), float64(stats.AvgRtt)/float64(time.Millisecond))
	SetExternalPingMetrics(
		config.NodeName,
		config.HostIP,
		config.PodIP,
		config.ExternalAddress,
		float64(stats.AvgRtt)/float64(time.Millisecond),
		int(math.Abs(float64(stats.PacketsSent-stats.PacketsRecv))))
	if int(math.Abs(float64(stats.PacketsSent-stats.PacketsRecv))) != 0 {
		return fmt.Errorf("ping failed")
	}
	return nil
}

func internalNslookup(config *Configuration) error {
	klog.Infof("start to check dns connectivity")
	t1 := time.Now()
	ctx, cancel := context.WithTimeout(context.TODO(), 10*time.Second)
	defer cancel()
	var r net.Resolver
	addrs, err := r.LookupHost(ctx, config.InternalDNS)
	elpased := time.Since(t1)
	if err != nil {
		klog.Errorf("failed to resolve dns %s, %v", config.InternalDNS, err)
		SetInternalDnsUnhealthyMetrics(config.NodeName)
		return err
	}
	SetInternalDnsHealthyMetrics(config.NodeName, float64(elpased)/float64(time.Millisecond))
	klog.Infof("resolve dns %s to %v in %.2fms", config.InternalDNS, addrs, float64(elpased)/float64(time.Millisecond))
	return nil
}

func externalNslookup(config *Configuration) error {
	klog.Infof("start to check dns connectivity")
	t1 := time.Now()
	ctx, cancel := context.WithTimeout(context.TODO(), 10*time.Second)
	defer cancel()
	var r net.Resolver
	addrs, err := r.LookupHost(ctx, config.ExternalDNS)
	elpased := time.Since(t1)
	if err != nil {
		klog.Errorf("failed to resolve dns %s, %v", config.ExternalDNS, err)
		SetExternalDnsUnhealthyMetrics(config.NodeName)
		return err
	}
	SetExternalDnsHealthyMetrics(config.NodeName, float64(elpased)/float64(time.Millisecond))
	klog.Infof("resolve dns %s to %v in %.2fms", config.ExternalDNS, addrs, float64(elpased)/float64(time.Millisecond))
	return nil
}

func checkApiServer(config *Configuration) error {
	klog.Infof("start to check apiserver connectivity")
	t1 := time.Now()
	_, err := config.KubeClient.Discovery().ServerVersion()
	elpased := time.Since(t1)
	if err != nil {
		klog.Errorf("failed to connect to apiserver: %v", err)
		SetApiserverUnhealthyMetrics(config.NodeName)
		return err
	}
	klog.Infof("connect to apiserver success in %.2fms", float64(elpased)/float64(time.Millisecond))
	SetApiserverHealthyMetrics(config.NodeName, float64(elpased)/float64(time.Millisecond))
	return nil
}
