package pinger

import (
	goping "github.com/sparrc/go-ping"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"net"
	"os/exec"
	"sync"
	"time"
)

func StartPinger(config *Configuration) {
	for {
		checkOvs()
		checkOvnController()
		ping(config)
		if config.Mode != "server" {
			break
		}
		time.Sleep(time.Duration(config.Interval) * time.Second)
	}
}

func ping(config *Configuration) {
	pingNodes(config.KubeClient)
	pingPods(config.KubeClient, config.DaemonSetNamespace, config.DaemonSetName)
	nslookup(config.DNS)
}

func pingNodes(client kubernetes.Interface) {
	klog.Infof("start to check node connectivity")
	nodes, err := client.CoreV1().Nodes().List(metav1.ListOptions{})
	if err != nil {
		klog.Errorf("failed to list nodes, %v", err)
		return
	}
	wg := sync.WaitGroup{}
	for _, no := range nodes.Items {
		for _, addr := range no.Status.Addresses {
			if addr.Type == v1.NodeInternalIP {
				wg.Add(1)
				go func(nodeIP, nodeName string) {
					defer wg.Done()
					pinger, err := goping.NewPinger(nodeIP)
					if err != nil {
						klog.Errorf("failed to init pinger, %v", err)
						return
					}
					pinger.SetPrivileged(true)
					pinger.Count = 5
					pinger.Run()
					stats := pinger.Statistics()
					klog.Infof("ping node: %s %s, count: %d, loss rate %.2f%%, average rtt %.2fms",
						nodeName, nodeIP, pinger.Count, stats.PacketLoss*100, float64(stats.AvgRtt)/float64(time.Millisecond))
				}(addr.Address, no.Name)
			}
		}
	}
	wg.Wait()
}

func pingPods(client kubernetes.Interface, dsNamespace, dsName string) {
	klog.Infof("start to check pod connectivity")
	ds, err := client.AppsV1().DaemonSets(dsNamespace).Get(dsName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("failed to get peer ds: %v", err)
		return
	}
	pods, err := client.CoreV1().Pods(dsNamespace).List(metav1.ListOptions{LabelSelector: labels.Set(ds.Spec.Selector.MatchLabels).String()})
	if err != nil {
		klog.Errorf("failed to list peer pods: %v", err)
		return
	}

	wg := sync.WaitGroup{}
	for _, pod := range pods.Items {
		if pod.Status.PodIP != "" {
			wg.Add(1)
			go func(podIp, podName, podNamespace string) {
				defer wg.Done()
				pinger, err := goping.NewPinger(podIp)
				if err != nil {
					klog.Errorf("failed to init pinger, %v", err)
					return
				}
				pinger.SetPrivileged(true)
				pinger.Count = 5
				pinger.Run()
				stats := pinger.Statistics()
				klog.Infof("ping pod: %s/%s %s, count: %d, loss rate %.2f, average rtt %.2fms",
					podNamespace, podName, podIp, pinger.Count, stats.PacketLoss*100, float64(stats.AvgRtt)/float64(time.Millisecond))
			}(pod.Status.PodIP, pod.Name, pod.Namespace)
		}
	}
	wg.Wait()
}

func nslookup(dns string) {
	klog.Infof("start to check dns connectivity")
	t1 := time.Now()
	addrs, err := net.LookupHost(dns)
	elpased := time.Since(t1)
	if err != nil {
		klog.Errorf("failed to resolve dns %s, %v", dns, err)
		return
	}
	klog.Infof("resolve dns %s to %v in %.2fms", dns, addrs, float64(elpased)/float64(time.Millisecond))
}

func checkOvs() {
	output, err := exec.Command("/usr/share/openvswitch/scripts/ovs-ctl", "status").CombinedOutput()
	if err != nil {
		klog.Errorf("check ovs status failed %v, %s", err, string(output))
	}
	klog.Infof("ovs-vswitchd and ovsdb are up")
}

func checkOvnController() {
	output, err := exec.Command("/usr/share/openvswitch/scripts/ovn-ctl", "status_controller").CombinedOutput()
	if err != nil {
		klog.Errorf("check ovn_controller status failed %v, %s", err, string(output))
	}
	klog.Infof("ovn_controller is up")
}
