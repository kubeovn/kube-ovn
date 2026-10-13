package daemon

import (
	"bufio"
	"errors"
	"fmt"
	"os"
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
	value, err := readInCsumErrors("/proc/net/snmp")
	if err != nil {
		klog.Errorf("failed to read InCsumErrors from /proc/net/snmp, err %v", err)
		return
	}
	metricCheckSumErr.WithLabelValues(c.config.NodeName).Set(float64(value))
}

func readInCsumErrors(path string) (value int, resultErr error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		const prefix = "Udp:"
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		headers := strings.Fields(strings.TrimPrefix(line, prefix))
		if !scanner.Scan() {
			return 0, errors.Join(scanner.Err(), errors.New("missing UDP counters in /proc/net/snmp"))
		}
		if !strings.HasPrefix(scanner.Text(), prefix) {
			return 0, errors.New("invalid UDP counters in /proc/net/snmp")
		}
		values := strings.Fields(strings.TrimPrefix(scanner.Text(), prefix))
		for index, header := range headers {
			if header != "InCsumErrors" {
				continue
			}
			if index >= len(values) {
				return 0, errors.New("missing UDP InCsumErrors value in /proc/net/snmp")
			}
			value, err := strconv.Atoi(values[index])
			if err != nil {
				return 0, fmt.Errorf("parse InCsumErrors value %q: %w", values[index], err)
			}
			return value, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return 0, nil
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
