package pinger

import (
	"context"
	"fmt"
	"github.com/alauda/kube-ovn/pkg/util"
	goping "github.com/sparrc/go-ping"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog"
	"math"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

func StartPinger(config *Configuration) {
	for {
		checkOvs(config)
		checkOvnController(config)
		checkPortBindings(config)
		checkApiServer(config)
		ping(config)
		if config.Mode != "server" {
			break
		}
		time.Sleep(time.Duration(config.Interval) * time.Second)
	}
}

func ping(config *Configuration) {
	pingNodes(config)
	pingPods(config)
	nslookup(config)
	pingExternal(config)
}

func pingNodes(config *Configuration) {
	klog.Infof("start to check node connectivity")
	nodes, err := config.KubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		klog.Errorf("failed to list nodes, %v", err)
		return
	}
	for _, no := range nodes.Items {
		for _, addr := range no.Status.Addresses {
			if addr.Type == v1.NodeInternalIP {
				func(nodeIP, nodeName string) {
					pinger, err := goping.NewPinger(nodeIP)
					if err != nil {
						klog.Errorf("failed to init pinger, %v", err)
						return
					}
					pinger.SetPrivileged(true)
					pinger.Timeout = 1 * time.Second
					pinger.Count = 3
					pinger.Interval = 1 * time.Millisecond
					pinger.Debug = true
					pinger.Run()
					stats := pinger.Statistics()
					klog.Infof("ping node: %s %s, count: %d, loss rate %.2f%%, average rtt %.2fms",
						nodeName, nodeIP, pinger.Count, math.Abs(stats.PacketLoss)*100, float64(stats.AvgRtt)/float64(time.Millisecond))
					SetNodePingMetrics(
						config.NodeName,
						config.HostIP,
						config.PodName,
						no.Name, addr.Address,
						float64(stats.AvgRtt)/float64(time.Millisecond),
						int(math.Abs(float64(stats.PacketsSent-stats.PacketsRecv))))
				}(addr.Address, no.Name)
			}
		}
	}
}

func pingPods(config *Configuration) {
	klog.Infof("start to check pod connectivity")
	ds, err := config.KubeClient.AppsV1().DaemonSets(config.DaemonSetNamespace).Get(config.DaemonSetName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("failed to get peer ds: %v", err)
		return
	}
	pods, err := config.KubeClient.CoreV1().Pods(config.DaemonSetNamespace).List(metav1.ListOptions{LabelSelector: labels.Set(ds.Spec.Selector.MatchLabels).String()})
	if err != nil {
		klog.Errorf("failed to list peer pods: %v", err)
		return
	}

	for _, pod := range pods.Items {
		if pod.Status.PodIP != "" {
			func(podIp, podName, nodeIP, nodeName string) {
				pinger, err := goping.NewPinger(podIp)
				if err != nil {
					klog.Errorf("failed to init pinger, %v", err)
					return
				}
				pinger.SetPrivileged(true)
				pinger.Timeout = 1 * time.Second
				pinger.Debug = true
				pinger.Count = 3
				pinger.Interval = 1 * time.Millisecond
				pinger.Run()
				stats := pinger.Statistics()
				klog.Infof("ping pod: %s %s, count: %d, loss rate %.2f, average rtt %.2fms",
					podName, podIp, pinger.Count, math.Abs(stats.PacketLoss)*100, float64(stats.AvgRtt)/float64(time.Millisecond))
				SetPodPingMetrics(
					config.NodeName,
					config.HostIP,
					config.PodName,
					nodeName,
					nodeIP,
					podIp,
					float64(stats.AvgRtt)/float64(time.Millisecond),
					int(math.Abs(float64(stats.PacketsSent-stats.PacketsRecv))))
			}(pod.Status.PodIP, pod.Name, pod.Status.HostIP, pod.Spec.NodeName)
		}
	}
}

func pingExternal(config *Configuration) {
	if config.ExternalAddress == "" {
		return
	}
	klog.Infof("start to check ping external to %s", config.ExternalAddress)
	pinger, err := goping.NewPinger(config.ExternalAddress)
	if err != nil {
		klog.Errorf("failed to init pinger, %v", err)
		return
	}
	pinger.SetPrivileged(true)
	pinger.Timeout = 5 * time.Second
	pinger.Debug = true
	pinger.Count = 3
	pinger.Interval = 1 * time.Millisecond
	pinger.Run()
	stats := pinger.Statistics()
	klog.Infof("ping external address: %s, count: %d, loss rate %.2f, average rtt %.2fms",
		config.ExternalAddress, pinger.Count, math.Abs(stats.PacketLoss)*100, float64(stats.AvgRtt)/float64(time.Millisecond))
	SetExternalPingMetrics(
		config.NodeName,
		config.HostIP,
		config.PodIP,
		config.ExternalAddress,
		float64(stats.AvgRtt)/float64(time.Millisecond),
		int(math.Abs(float64(stats.PacketsSent-stats.PacketsRecv))))
}

func nslookup(config *Configuration) {
	klog.Infof("start to check dns connectivity")
	t1 := time.Now()
	ctx, cancel := context.WithTimeout(context.TODO(), 10 * time.Second)
	defer cancel()
	var r net.Resolver
	addrs, err := r.LookupHost(ctx, config.DNS)
	elpased := time.Since(t1)
	if err != nil {
		klog.Errorf("failed to resolve dns %s, %v", config.DNS, err)
		SetDnsUnhealthyMetrics(config.NodeName)
		return
	}
	SetDnsHealthyMetrics(config.NodeName, float64(elpased)/float64(time.Millisecond))
	klog.Infof("resolve dns %s to %v in %.2fms", config.DNS, addrs, float64(elpased)/float64(time.Millisecond))
}

func checkOvs(config *Configuration) {
	output, err := exec.Command("/usr/share/openvswitch/scripts/ovs-ctl", "status").CombinedOutput()
	if err != nil {
		klog.Errorf("check ovs status failed %v, %s", err, string(output))
		SetOvsDownMetrics(config.NodeName)
		return
	}
	klog.Infof("ovs-vswitchd and ovsdb are up")
	SetOvsUpMetrics(config.NodeName)
	return
}

func checkOvnController(config *Configuration) {
	output, err := exec.Command("/usr/share/openvswitch/scripts/ovn-ctl", "status_controller").CombinedOutput()
	if err != nil {
		klog.Errorf("check ovn_controller status failed %v, %s", err, string(output))
		SetOvnControllerDownMetrics(config.NodeName)
		return
	}
	klog.Infof("ovn_controller is up")
	SetOvnControllerUpMetrics(config.NodeName)
}

func checkApiServer(config *Configuration) {
	klog.Infof("start to check apiserver connectivity")
	t1 := time.Now()
	_, err := config.KubeClient.Discovery().ServerVersion()
	elpased := time.Since(t1)
	if err != nil {
		klog.Errorf("failed to connect to apiserver: %v", err)
		SetApiserverUnhealthyMetrics(config.NodeName)
		return
	}
	klog.Infof("connect to apiserver success in %.2fms", float64(elpased)/float64(time.Millisecond))
	SetApiserverHealthyMetrics(config.NodeName, float64(elpased)/float64(time.Millisecond))
	return
}

func checkPortBindings(config *Configuration) error {
	klog.Infof("start to check por binding")
	ovsBindings, err := checkOvsBindings()
	if err != nil {
		return err
	}

	sbBindings, err := checkSBBindings(config)
	if err != nil {
		return err
	}
	klog.Infof("port in sb is %v", sbBindings)
	misMatch := []string{}
	for _, port := range ovsBindings {
		if !util.IsStringIn(port, sbBindings) {
			misMatch = append(misMatch, port)
		}
	}
	if len(misMatch) > 0 {
		klog.Errorf("%d port %v not exist in sb-bindings", len(misMatch), misMatch)
		inconsistentPortBindingGauge.WithLabelValues(config.NodeName).Set(float64(len(misMatch)))
	} else {
		klog.Infof("ovs and ovn-sb binding check passed")
		inconsistentPortBindingGauge.WithLabelValues(config.NodeName).Set(0)
	}
	return nil
}

func checkOvsBindings() ([]string, error) {
	output, err := exec.Command("ovs-vsctl", "--no-heading", "--data=bare", "--format=csv", "--columns=external_ids", "find", "interface", "external_ids:iface-id!=\"\"").CombinedOutput()
	if err != nil {
		klog.Errorf("failed to get ovs interface %v", err)
		return nil, err
	}
	result := make([]string, 0, len(strings.Split(string(output), "\n")))
	for _, line := range strings.Split(string(output), "\n") {
		result = append(result, strings.TrimPrefix(line, "iface-id="))
	}
	return result, nil
}

func checkSBBindings(config *Configuration) ([]string, error) {
	sbHost := os.Getenv("OVN_SB_SERVICE_HOST")
	sbPort := os.Getenv("OVN_SB_SERVICE_PORT")
	output, err := exec.Command(
		"ovn-sbctl",
		fmt.Sprintf("--db=tcp:%s:%s", sbHost, sbPort),
		"--format=csv",
		"--no-heading",
		"--data=bare",
		"--columns=_uuid",
		"find",
		"chassis",
		fmt.Sprintf("hostname=%s", config.NodeName)).CombinedOutput()
	if err != nil {
		klog.Errorf("failed to find chassis %v", err)
		return nil, err
	}
	if len(output) == 0 {
		klog.Errorf("chassis for node %s not exist", config.NodeName)
		return nil, fmt.Errorf("chassis for node %s not exist", config.NodeName)
	}

	chassis := strings.TrimSpace(string(output))
	klog.Infof("chassis id is %s", chassis)
	output, err = exec.Command(
		"ovn-sbctl",
		fmt.Sprintf("--db=tcp:%s:%s", sbHost, sbPort),
		"--format=csv",
		"--no-heading",
		"--data=bare",
		"--columns=logical_port",
		"find",
		"port_binding",
		fmt.Sprintf("chassis=%s", chassis)).CombinedOutput()
	if err != nil {
		klog.Errorf("failed to list port_binding in ovn-sb %v", err)
		return nil, err
	}

	return strings.Split(string(output), "\n"), nil
}
