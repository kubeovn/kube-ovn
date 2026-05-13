package daemon

import (
	"os"
	"os/exec"
	"strconv"
	"strings"

	"k8s.io/klog/v2"

	"github.com/containerd/nerdctl/v2/pkg/resolvconf"
)

func (c *Controller) setIPLocalPortRangeMetric() {
	output, err := os.ReadFile("/proc/sys/net/ipv4/ip_local_port_range")
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		klog.Errorf("failed to get value of ip_local_port_range, err %v", err)
		return
	}

	values := strings.Fields(string(output))
	if len(values) != 2 {
		klog.Errorf("unexpected ip_local_port_range value: %q", string(output))
		return
	}
	metricIPLocalPortRange.WithLabelValues(c.config.NodeName, values[0], values[1]).Set(1)
}

func (c *Controller) setCheckSumErrMetric() {
	output, err := exec.Command("netstat", "-us").CombinedOutput()
	if err != nil {
		klog.Errorf("failed to exec cmd 'netstat -us', err %v", err)
		return
	}

	found := false
	lines := strings.SplitSeq(string(output), "\n")
	for line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.Contains(line, "InCsumErrors") {
			values := strings.Split(line, ":")
			if len(values) == 2 {
				val, _ := strconv.Atoi(strings.TrimSpace(values[1]))
				metricCheckSumErr.WithLabelValues(c.config.NodeName).Set(float64(val))
				found = true
			}
		}
	}
	if !found {
		metricCheckSumErr.WithLabelValues(c.config.NodeName).Set(float64(0))
	}
}

func (c *Controller) setDNSSearchMetric() {
	file, err := resolvconf.GetSpecific("/etc/resolv.conf")
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		klog.Errorf("failed to get /etc/resolv.conf content: %v", err)
		return
	}
	domains := resolvconf.GetSearchDomains(file.Content)

	found := false
	for _, domain := range domains {
		if domain == "." {
			// Ignore the root domain
			continue
		}

		found = true
		metricDNSSearch.WithLabelValues(c.config.NodeName, domain).Set(1)
	}
	if !found {
		metricDNSSearch.WithLabelValues(c.config.NodeName, "no additional search domain").Set(1)
	}
}

func (c *Controller) setTCPTwRecycleMetric() {
	output, err := os.ReadFile("/proc/sys/net/ipv4/tcp_tw_recycle")
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		klog.Errorf("failed to get value of tcp_tw_recycle, err %v", err)
		return
	}

	val, _ := strconv.Atoi(strings.TrimSpace(string(output)))
	metricTCPTwRecycle.WithLabelValues(c.config.NodeName).Set(float64(val))
}

func (c *Controller) setTCPMtuProbingMetric() {
	output, err := os.ReadFile("/proc/sys/net/ipv4/tcp_mtu_probing")
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		klog.Errorf("failed to get value of tcp_mtu_probing, err %v", err)
		return
	}

	val, _ := strconv.Atoi(strings.TrimSpace(string(output)))
	metricTCPMtuProbing.WithLabelValues(c.config.NodeName).Set(float64(val))
}

func (c *Controller) setConntrackTCPLiberalMetric() {
	output, err := os.ReadFile("/proc/sys/net/netfilter/nf_conntrack_tcp_be_liberal")
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		klog.Errorf("failed to get value of nf_conntrack_tcp_be_liberal, err %v", err)
		return
	}

	val, _ := strconv.Atoi(strings.TrimSpace(string(output)))
	metricConntrackTCPLiberal.WithLabelValues(c.config.NodeName).Set(float64(val))
}

func (c *Controller) setBridgeNfCallIptablesMetric() {
	output, err := os.ReadFile("/proc/sys/net/bridge/bridge-nf-call-iptables")
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		klog.Errorf("failed to get value of bridge-nf-call-iptables, err %v", err)
		return
	}

	val, _ := strconv.Atoi(strings.TrimSpace(string(output)))
	metricBridgeNfCallIptables.WithLabelValues(c.config.NodeName).Set(float64(val))
}

func (c *Controller) setIPv6RouteMaxsizeMetric() {
	output, err := os.ReadFile("/proc/sys/net/ipv6/route/max_size")
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		klog.Errorf("failed to get value of  ipv6 route max_size, err %v", err)
		return
	}

	val, _ := strconv.Atoi(strings.TrimSpace(string(output)))
	metricIPv6RouteMaxsize.WithLabelValues(c.config.NodeName).Set(float64(val))
}

func (c *Controller) setTCPMemMetric() {
	output, err := os.ReadFile("/proc/sys/net/ipv4/tcp_mem")
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		klog.Errorf("failed to get value of ipv4 tcp_mem, err %v", err)
		return
	}

	values := strings.Fields(string(output))
	if len(values) != 3 {
		klog.Errorf("unexpected tcp_mem value: %q", string(output))
		return
	}
	metricTCPMem.WithLabelValues(c.config.NodeName, values[0], values[1], values[2]).Set(1)
}
