package pinger

import (
	"context"
	"fmt"
	"math"
	"net"
	"os"
	"slices"
	"strings"
	"time"

	goping "github.com/prometheus-community/pro-bing"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func StartPinger(config *Configuration) {
	errHappens := false
	var exporter *Exporter
	withMetrics := config.Mode == "server" && config.EnableMetrics
	for {
		if config.NetworkMode == "kube-ovn" {
			if checkOvs(config, withMetrics) != nil {
				errHappens = true
			}
			if checkOvnController(config, withMetrics) != nil {
				errHappens = true
			}
			if checkPortBindings(config, withMetrics) != nil {
				errHappens = true
			}
			if withMetrics {
				if exporter == nil {
					exporter = NewExporter(config)
				}
				exporter.ovsMetricsUpdate()
			}
		}
		if ping(config, withMetrics) != nil {
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

func ping(config *Configuration, withMetrics bool) error {
	errHappens := false
	if checkAPIServer(config, withMetrics) != nil {
		errHappens = true
	}
	if pingPods(config, withMetrics) != nil {
		errHappens = true
	}
	if pingNodes(config, withMetrics) != nil {
		errHappens = true
	}
	if internalNslookup(config, withMetrics) != nil {
		errHappens = true
	}

	if config.ExternalDNS != "" {
		if externalNslookup(config, withMetrics) != nil {
			errHappens = true
		}
	}

	if config.TargetIPPorts != "" {
		if checkAccessTargetIPPorts(config) != nil {
			errHappens = true
		}
	}

	if config.ExternalAddress != "" {
		if pingExternal(config, withMetrics) != nil {
			errHappens = true
		}
	}
	if errHappens {
		return fmt.Errorf("ping failed")
	}
	return nil
}

func pingNodes(config *Configuration, setMetrics bool) error {
	klog.Infof("start to check node connectivity")
	nodes, err := config.KubeClient.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		klog.Errorf("failed to list nodes, %v", err)
		return err
	}

	var pingErr error
	for _, no := range nodes.Items {
		for _, addr := range no.Status.Addresses {
			if addr.Type == v1.NodeInternalIP && slices.Contains(config.PodProtocols, util.CheckProtocol(addr.Address)) {
				func(nodeIP, nodeName string) {
					if config.EnableVerboseConnCheck {
						if err := util.TCPConnectivityCheck(fmt.Sprintf("%s:%d", nodeIP, config.TCPConnCheckPort)); err != nil {
							klog.Infof("TCP connectivity to node %s %s failed", nodeName, nodeIP)
							pingErr = err
						} else {
							klog.Infof("TCP connectivity to node %s %s success", nodeName, nodeIP)
						}
						if err := util.UDPConnectivityCheck(fmt.Sprintf("%s:%d", nodeIP, config.UDPConnCheckPort)); err != nil {
							klog.Infof("UDP connectivity to node %s %s failed", nodeName, nodeIP)
							pingErr = err
						} else {
							klog.Infof("UDP connectivity to node %s %s success", nodeName, nodeIP)
						}
					}

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
					if err = pinger.Run(); err != nil {
						klog.Errorf("failed to run pinger for destination %s: %v", nodeIP, err)
						pingErr = err
						return
					}

					stats := pinger.Statistics()
					klog.Infof("ping node: %s %s, count: %d, loss count %d, average rtt %.2fms",
						nodeName, nodeIP, pinger.Count, int(math.Abs(float64(stats.PacketsSent-stats.PacketsRecv))), float64(stats.AvgRtt)/float64(time.Millisecond))
					if int(math.Abs(float64(stats.PacketsSent-stats.PacketsRecv))) != 0 {
						pingErr = fmt.Errorf("ping failed")
					}
					if setMetrics {
						SetNodePingMetrics(
							config.NodeName,
							config.HostIP,
							config.PodName,
							no.Name, addr.Address,
							float64(stats.AvgRtt)/float64(time.Millisecond),
							int(math.Abs(float64(stats.PacketsSent-stats.PacketsRecv))),
							int(float64(stats.PacketsSent)))
					}
				}(addr.Address, no.Name)
			}
		}
	}
	return pingErr
}

func pingPods(config *Configuration, setMetrics bool) error {
	klog.Infof("start to check pod connectivity")
	ds, err := config.KubeClient.AppsV1().DaemonSets(config.DaemonSetNamespace).Get(context.Background(), config.DaemonSetName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("failed to get peer ds: %v", err)
		return err
	}
	pods, err := config.KubeClient.CoreV1().Pods(config.DaemonSetNamespace).List(context.Background(), metav1.ListOptions{LabelSelector: labels.Set(ds.Spec.Selector.MatchLabels).String()})
	if err != nil {
		klog.Errorf("failed to list peer pods: %v", err)
		return err
	}

	var pingErr error
	for _, pod := range pods.Items {
		for _, podIP := range pod.Status.PodIPs {
			if slices.Contains(config.PodProtocols, util.CheckProtocol(podIP.IP)) {
				func(podIP, podName, nodeIP, nodeName string) {
					if config.EnableVerboseConnCheck {
						if err := util.TCPConnectivityCheck(fmt.Sprintf("%s:%d", podIP, config.TCPConnCheckPort)); err != nil {
							klog.Infof("TCP connectivity to pod %s %s failed", podName, podIP)
							pingErr = err
						} else {
							klog.Infof("TCP connectivity to pod %s %s success", podName, podIP)
						}

						if err := util.UDPConnectivityCheck(fmt.Sprintf("%s:%d", podIP, config.UDPConnCheckPort)); err != nil {
							klog.Infof("UDP connectivity to pod %s %s failed", podName, podIP)
							pingErr = err
						} else {
							klog.Infof("UDP connectivity to pod %s %s success", podName, podIP)
						}
					}

					pinger, err := goping.NewPinger(podIP)
					if err != nil {
						klog.Errorf("failed to init pinger, %v", err)
						pingErr = err
						return
					}
					pinger.SetPrivileged(true)
					pinger.Timeout = 1 * time.Second
					pinger.Debug = true
					pinger.Count = 3
					pinger.Interval = 100 * time.Millisecond
					if err = pinger.Run(); err != nil {
						klog.Errorf("failed to run pinger for destination %s: %v", podIP, err)
						pingErr = err
						return
					}

					stats := pinger.Statistics()
					klog.Infof("ping pod: %s %s, count: %d, loss count %d, average rtt %.2fms",
						podName, podIP, pinger.Count, int(math.Abs(float64(stats.PacketsSent-stats.PacketsRecv))), float64(stats.AvgRtt)/float64(time.Millisecond))
					if int(math.Abs(float64(stats.PacketsSent-stats.PacketsRecv))) != 0 {
						pingErr = fmt.Errorf("ping failed")
					}
					if setMetrics {
						SetPodPingMetrics(
							config.NodeName,
							config.HostIP,
							config.PodName,
							nodeName,
							nodeIP,
							podIP,
							float64(stats.AvgRtt)/float64(time.Millisecond),
							int(math.Abs(float64(stats.PacketsSent-stats.PacketsRecv))),
							int(float64(stats.PacketsSent)))
					}
				}(podIP.IP, pod.Name, pod.Status.HostIP, pod.Spec.NodeName)
			}
		}
	}
	return pingErr
}

func pingExternal(config *Configuration, setMetrics bool) error {
	if config.ExternalAddress == "" {
		return nil
	}

	addresses := strings.Split(config.ExternalAddress, ",")
	for _, addr := range addresses {
		if !slices.Contains(config.PodProtocols, util.CheckProtocol(addr)) {
			continue
		}

		klog.Infof("start to check ping external to %s", addr)
		pinger, err := goping.NewPinger(addr)
		if err != nil {
			klog.Errorf("failed to init pinger, %v", err)
			return err
		}
		pinger.SetPrivileged(true)
		pinger.Timeout = 5 * time.Second
		pinger.Debug = true
		pinger.Count = 3
		pinger.Interval = 100 * time.Millisecond
		if err = pinger.Run(); err != nil {
			klog.Errorf("failed to run pinger for destination %s: %v", addr, err)
			return err
		}
		stats := pinger.Statistics()
		klog.Infof("ping external address: %s, total count: %d, loss count %d, average rtt %.2fms",
			addr, pinger.Count, int(math.Abs(float64(stats.PacketsSent-stats.PacketsRecv))), float64(stats.AvgRtt)/float64(time.Millisecond))
		if setMetrics {
			SetExternalPingMetrics(
				config.NodeName,
				config.HostIP,
				config.PodIP,
				addr,
				float64(stats.AvgRtt)/float64(time.Millisecond),
				int(math.Abs(float64(stats.PacketsSent-stats.PacketsRecv))))
		}
		if int(math.Abs(float64(stats.PacketsSent-stats.PacketsRecv))) != 0 {
			return fmt.Errorf("ping failed")
		}
	}

	return nil
}

func checkAccessTargetIPPorts(config *Configuration) error {
	klog.Infof("start to check Service or externalIPPort connectivity")
	if config.TargetIPPorts == "" {
		return nil
	}
	var checkErr error
	targetIPPorts := strings.Split(config.TargetIPPorts, ",")
	for _, targetIPPort := range targetIPPorts {
		klog.Infof("checking targetIPPort %s", targetIPPort)
		items := strings.Split(targetIPPort, "-")
		if len(items) != 3 {
			klog.Infof("targetIPPort format failed")
			continue
		}
		proto := items[0]
		addr := items[1]
		port := items[2]

		if !slices.Contains(config.PodProtocols, util.CheckProtocol(addr)) {
			continue
		}
		if util.CheckProtocol(addr) == kubeovnv1.ProtocolIPv6 {
			addr = fmt.Sprintf("[%s]", addr)
		}

		switch proto {
		case util.ProtocolTCP:
			if err := util.TCPConnectivityCheck(fmt.Sprintf("%s:%s", addr, port)); err != nil {
				klog.Infof("TCP connectivity to targetIPPort %s:%s failed", addr, port)
				checkErr = err
			} else {
				klog.Infof("TCP connectivity to targetIPPort %s:%s success", addr, port)
			}
		case util.ProtocolUDP:
			if err := util.UDPConnectivityCheck(fmt.Sprintf("%s:%s", addr, port)); err != nil {
				klog.Infof("UDP connectivity to target %s:%s failed", addr, port)
				checkErr = err
			} else {
				klog.Infof("UDP connectivity to target %s:%s success", addr, port)
			}
		default:
			klog.Infof("unrecognized protocol %s", proto)
			continue
		}
	}
	return checkErr
}

func internalNslookup(config *Configuration, setMetrics bool) error {
	klog.Infof("start to check dns connectivity")
	t1 := time.Now()
	ctx, cancel := context.WithTimeout(context.TODO(), 10*time.Second)
	defer cancel()
	var r net.Resolver
	addrs, err := r.LookupHost(ctx, config.InternalDNS)
	elapsed := time.Since(t1)
	if err != nil {
		klog.Errorf("failed to resolve dns %s, %v", config.InternalDNS, err)
		if setMetrics {
			SetInternalDNSUnhealthyMetrics(config.NodeName)
		}
		return err
	}
	if setMetrics {
		SetInternalDNSHealthyMetrics(config.NodeName, float64(elapsed)/float64(time.Millisecond))
	}
	klog.Infof("resolve dns %s to %v in %.2fms", config.InternalDNS, addrs, float64(elapsed)/float64(time.Millisecond))
	return nil
}

func externalNslookup(config *Configuration, setMetrics bool) error {
	klog.Infof("start to check dns connectivity")
	t1 := time.Now()
	ctx, cancel := context.WithTimeout(context.TODO(), 10*time.Second)
	defer cancel()
	var r net.Resolver
	addrs, err := r.LookupHost(ctx, config.ExternalDNS)
	elapsed := time.Since(t1)
	if err != nil {
		klog.Errorf("failed to resolve dns %s, %v", config.ExternalDNS, err)
		if setMetrics {
			SetExternalDNSUnhealthyMetrics(config.NodeName)
		}
		return err
	}
	if setMetrics {
		SetExternalDNSHealthyMetrics(config.NodeName, float64(elapsed)/float64(time.Millisecond))
	}
	klog.Infof("resolve dns %s to %v in %.2fms", config.ExternalDNS, addrs, float64(elapsed)/float64(time.Millisecond))
	return nil
}

func checkAPIServer(config *Configuration, setMetrics bool) error {
	klog.Infof("start to check apiserver connectivity")
	t1 := time.Now()
	_, err := config.KubeClient.Discovery().ServerVersion()
	elapsed := time.Since(t1)
	if err != nil {
		klog.Errorf("failed to connect to apiserver: %v", err)
		if setMetrics {
			SetApiserverUnhealthyMetrics(config.NodeName)
		}
		return err
	}
	klog.Infof("connect to apiserver success in %.2fms", float64(elapsed)/float64(time.Millisecond))
	if setMetrics {
		SetApiserverHealthyMetrics(config.NodeName, float64(elapsed)/float64(time.Millisecond))
	}
	return nil
}
