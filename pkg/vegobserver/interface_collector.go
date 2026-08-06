package vegobserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

type networkStatus struct {
	Name      string `json:"name"`
	Interface string `json:"interface"`
	Default   bool   `json:"default"`
}

type interfaceValues struct {
	rxBytes, rxPackets, rxErrors, rxDrops uint64
	txBytes, txPackets, txErrors, txDrops uint64
}

type interfaceCollector struct {
	mu                sync.Mutex
	networkStatusPath string
	procNetDevPath    string
	internal          string
	external          string
	previous          map[string]interfaceValues
}

func newInterfaceCollector(networkStatusPath string) *interfaceCollector {
	return &interfaceCollector{networkStatusPath: networkStatusPath, procNetDevPath: "/proc/net/dev", previous: map[string]interfaceValues{}}
}

func (c *interfaceCollector) update(config Config, identity observerIdentity, metrics *observerMetrics) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.internal == "" || c.external == "" {
		if err := c.resolveInterfaces(config.ExternalNetwork); err != nil {
			return err
		}
	}
	values, err := readInterfaceValues(c.procNetDevPath)
	if err != nil {
		return err
	}
	for interfaceType, interfaceName := range map[string]string{"internal": c.internal, "external": c.external} {
		current, ok := values[interfaceName]
		if !ok {
			return fmt.Errorf("interface %q (%s) is missing from /proc/net/dev", interfaceName, interfaceType)
		}
		previous := c.previous[interfaceName]
		labels := append(identity.labels(), interfaceName, interfaceType)
		metrics.interfaceCounters["rx_bytes"].WithLabelValues(labels...).Add(counterDelta(previous.rxBytes, current.rxBytes))
		metrics.interfaceCounters["tx_bytes"].WithLabelValues(labels...).Add(counterDelta(previous.txBytes, current.txBytes))
		metrics.interfaceCounters["rx_packets"].WithLabelValues(labels...).Add(counterDelta(previous.rxPackets, current.rxPackets))
		metrics.interfaceCounters["tx_packets"].WithLabelValues(labels...).Add(counterDelta(previous.txPackets, current.txPackets))
		metrics.interfaceCounters["drops"].WithLabelValues(append(labels, "rx")...).Add(counterDelta(previous.rxDrops, current.rxDrops))
		metrics.interfaceCounters["drops"].WithLabelValues(append(labels, "tx")...).Add(counterDelta(previous.txDrops, current.txDrops))
		metrics.interfaceCounters["errors"].WithLabelValues(append(labels, "rx")...).Add(counterDelta(previous.rxErrors, current.rxErrors))
		metrics.interfaceCounters["errors"].WithLabelValues(append(labels, "tx")...).Add(counterDelta(previous.txErrors, current.txErrors))
		c.previous[interfaceName] = current
	}
	return nil
}

func (c *interfaceCollector) resolveInterfaces(externalNetwork string) error {
	data, err := os.ReadFile(c.networkStatusPath)
	if err != nil {
		return fmt.Errorf("read Multus network status: %w", err)
	}
	var statuses []networkStatus
	if err := json.Unmarshal(data, &statuses); err != nil {
		return fmt.Errorf("decode Multus network status: %w", err)
	}
	for _, status := range statuses {
		if status.Default && status.Interface != "" {
			c.internal = status.Interface
		}
		if status.Name == externalNetwork && status.Interface != "" {
			c.external = status.Interface
		}
	}
	if c.internal == "" || c.external == "" {
		return errors.New("could not resolve internal and external interfaces from Multus network status")
	}
	return nil
}

func readInterfaceValues(path string) (map[string]interfaceValues, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	result := map[string]interfaceValues{}
	for line := range strings.SplitSeq(string(data), "\n") {
		nameAndFields := strings.SplitN(line, ":", 2)
		if len(nameAndFields) != 2 {
			continue
		}
		fields := strings.Fields(nameAndFields[1])
		if len(fields) < 16 {
			continue
		}
		parsed := make([]uint64, 16)
		for i, field := range fields[:16] {
			parsed[i], err = strconv.ParseUint(field, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse interface %q counter: %w", strings.TrimSpace(nameAndFields[0]), err)
			}
		}
		result[strings.TrimSpace(nameAndFields[0])] = interfaceValues{
			rxBytes: parsed[0], rxPackets: parsed[1], rxErrors: parsed[2], rxDrops: parsed[3],
			txBytes: parsed[8], txPackets: parsed[9], txErrors: parsed[10], txDrops: parsed[11],
		}
	}
	return result, nil
}

func counterDelta(previous, current uint64) float64 {
	if current >= previous {
		return float64(current - previous)
	}
	return float64(current)
}
