package pinger

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/kubeovn/ovsdb"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
)

// IncrementErrorCounter increases the counter of failed queries to OVN server.
func (e *Exporter) IncrementErrorCounter() {
	e.errorsLocker.Lock()
	defer e.errorsLocker.Unlock()
	atomic.AddInt64(&e.errors, 1)
}

func getOvsStatus(e *Exporter) map[string]error {
	components := [...]string{ovs.OvsdbServer, ovs.OvsVswitchd}
	result := make(map[string]error, len(components))
	for _, component := range components {
		_, err := ovs.Appctl(component, "-T", "1", "version")
		if err != nil {
			klog.Errorf("failed to get %s status: %v", component, err)
			if e != nil {
				e.IncrementErrorCounter()
			}
			result[component] = err
			continue
		}
	}

	return result
}

func (e *Exporter) getOvsDatapath() ([]string, error) {
	output, err := ovs.Appctl(ovs.OvsVswitchd, "-T", strconv.Itoa(e.timeout), "dpctl/dump-dps")
	if err != nil {
		return nil, fmt.Errorf("failed to get output of dpctl/dump-dps: %w", err)
	}

	var datapathsList []string
	for kvPair := range strings.SplitSeq(string(output), "\n") {
		var datapathType, datapathName string
		line := strings.TrimSpace(strings.TrimSuffix(kvPair, "\n"))
		if strings.Contains(line, "@") {
			datapath := strings.Split(line, "@")
			datapathType, datapathName = datapath[0], datapath[1]
		} else {
			// There is two line for "system@ovs-system\n", the second line is nil, ignore this situation
			continue
		}
		metricOvsDp.WithLabelValues(e.Client.System.Hostname, datapathName, datapathType).Set(1)
		datapathsList = append(datapathsList, datapathName)
	}
	metricOvsDpTotal.WithLabelValues(e.Client.System.Hostname).Set(float64(len(datapathsList)))

	return datapathsList, nil
}

func (e *Exporter) setOvsDpIfMetric(datapathName string) error {
	output, err := ovs.Appctl(ovs.OvsVswitchd, "-T", strconv.Itoa(e.timeout), "dpctl/show", datapathName)
	if err != nil {
		return fmt.Errorf("failed to get output of dpctl/show %s: %w", datapathName, err)
	}

	var datapathPortCount float64
	for kvPair := range strings.SplitSeq(string(output), "\n") {
		line := strings.TrimSpace(kvPair)
		switch {
		case strings.HasPrefix(line, "lookups:"):
			e.ovsDatapathLookupsMetrics(line, datapathName)
		case strings.HasPrefix(line, "masks:"):
			e.ovsDatapathMasksMetrics(line, datapathName)
		case strings.HasPrefix(line, "port "):
			e.ovsDatapathPortMetrics(line, datapathName)
			datapathPortCount++
		case strings.HasPrefix(line, "flows:"):
			flowFields := strings.Fields(line)
			value, _ := strconv.ParseFloat(flowFields[1], 64)
			metricOvsDpFlowsTotal.WithLabelValues(e.Client.System.Hostname, datapathName).Set(value)
		}
	}
	metricOvsDpIfTotal.WithLabelValues(e.Client.System.Hostname, datapathName).Set(datapathPortCount)

	return nil
}

func (e *Exporter) ovsDatapathLookupsMetrics(line, datapath string) {
	for field := range strings.FieldsSeq(strings.TrimPrefix(line, "lookups:")) {
		elem := strings.Split(field, ":")
		if len(elem) != 2 {
			continue
		}
		value, err := strconv.ParseFloat(elem[1], 64)
		if err != nil {
			klog.Errorf("Failed to parse value %v into float in DpFlowsLookup:(%v)", value, err)
			value = 0
		}
		switch elem[0] {
		case "hit":
			metricOvsDpFlowsLookupHit.WithLabelValues(e.Client.System.Hostname, datapath).Set(value)
		case "missed":
			metricOvsDpFlowsLookupMissed.WithLabelValues(e.Client.System.Hostname, datapath).Set(value)
		case "lost":
			metricOvsDpFlowsLookupLost.WithLabelValues(e.Client.System.Hostname, datapath).Set(value)
		}
	}
}

func (e *Exporter) ovsDatapathMasksMetrics(line, datapath string) {
	for field := range strings.FieldsSeq(strings.TrimPrefix(line, "masks:")) {
		elem := strings.Split(field, ":")
		if len(elem) != 2 {
			continue
		}
		value, err := strconv.ParseFloat(elem[1], 64)
		if err != nil {
			klog.Errorf("Failed to parse value %v into float in OvsDpMasks:(%v)", value, err)
			value = 0
		}
		switch elem[0] {
		case "hit":
			metricOvsDpMasksHit.WithLabelValues(e.Client.System.Hostname, datapath).Set(value)
		case "total":
			metricOvsDpMasksTotal.WithLabelValues(e.Client.System.Hostname, datapath).Set(value)
		case "hit/pkt":
			metricOvsDpMasksHitRatio.WithLabelValues(e.Client.System.Hostname, datapath).Set(value)
		}
	}
}

func (e *Exporter) ovsDatapathPortMetrics(line, datapath string) {
	portFields := strings.Fields(line)
	portType := "system"
	if len(portFields) > 3 {
		portType = strings.Trim(portFields[3], "():")
	}

	portName := strings.TrimSpace(portFields[2])
	portNumber := strings.Trim(portFields[1], ":")
	metricOvsDpIf.WithLabelValues(e.Client.System.Hostname, datapath, portName, portType, portNumber).Set(1)
}

func (e *Exporter) getInterfaceInfo() ([]*ovsdb.OvsInterface, error) {
	intfs, err := e.Client.GetDbInterfaces()
	if err != nil {
		klog.Errorf("GetDbInterfaces error: %v", err)
		e.IncrementErrorCounter()
		return nil, err
	}

	return intfs, nil
}

func (e *Exporter) setOvsInterfaceMetric(intf *ovsdb.OvsInterface) {
	interfaceMain.WithLabelValues(e.Client.System.Hostname, intf.UUID, intf.Name).Set(1)
	e.setOvsInterfaceStateMetric(intf)
	interfaceMacInUse.WithLabelValues(e.Client.System.Hostname, intf.Name, intf.MacInUse).Set(1)
	interfaceMtu.WithLabelValues(e.Client.System.Hostname, intf.Name).Set(intf.Mtu)
	interfaceOfPort.WithLabelValues(e.Client.System.Hostname, intf.Name).Set(intf.OfPort)
	interfaceIfIndex.WithLabelValues(e.Client.System.Hostname, intf.Name).Set(intf.IfIndex)
	e.setOvsInterfaceStatisticsMetric(intf)
}

func (e *Exporter) setOvsInterfaceStateMetric(intf *ovsdb.OvsInterface) {
	var adminState float64
	switch intf.AdminState {
	case "down":
		adminState = 0
	case "up":
		adminState = 1
	default:
		adminState = 2
	}
	interfaceAdminState.WithLabelValues(e.Client.System.Hostname, intf.Name).Set(adminState)

	var linkState float64
	switch intf.LinkState {
	case "down":
		linkState = 0
	case "up":
		linkState = 1
	default:
		linkState = 2
	}
	interfaceLinkState.WithLabelValues(e.Client.System.Hostname, intf.Name).Set(linkState)
}

func (e *Exporter) setOvsInterfaceStatisticsMetric(intf *ovsdb.OvsInterface) {
	podName := ""
	podNamespace := ""
	if intf.ExternalIDs != nil {
		podName = intf.ExternalIDs["pod_name"]
		podNamespace = intf.ExternalIDs["pod_namespace"]
	}

	interfaceStatsMetricMap := map[string]*prometheus.GaugeVec{
		"rx_crc_err":           interfaceStatRxCrcError,
		"rx_dropped":           interfaceStatRxDropped,
		"rx_frame_err":         interfaceStatRxFrameError,
		"rx_missed_errors":     interfaceStatRxMissedError,
		"rx_over_err":          interfaceStatRxOverrunError,
		"rx_errors":            interfaceStatRxErrorsTotal,
		"rx_packets":           interfaceStatRxPackets,
		"rx_bytes":             interfaceStatRxBytes,
		"tx_packets":           interfaceStatTxPackets,
		"tx_bytes":             interfaceStatTxBytes,
		"tx_dropped":           interfaceStatTxDropped,
		"tx_errors":            interfaceStatTxErrorsTotal,
		"collisions":           interfaceStatCollisions,
		"rx_multicast_packets": interfaceStatRxMulticastPackets,
	}

	labels := prometheus.Labels{
		"hostname":      e.Client.System.Hostname,
		"interfaceName": intf.Name,
		"pod_name":      podName,
		"pod_namespace": podNamespace,
	}

	for key, value := range intf.Statistics {
		if metric, ok := interfaceStatsMetricMap[key]; ok {
			metric.With(labels).Set(float64(value))
		} else {
			klog.V(3).Infof("unknown statistics %s with value %d on OVS interface %s", key, value, intf.Name)
		}
	}
}

func resetOvsDatapathMetrics() {
	metricOvsDpFlowsTotal.Reset()
	metricOvsDpFlowsLookupHit.Reset()
	metricOvsDpFlowsLookupMissed.Reset()
	metricOvsDpFlowsLookupLost.Reset()

	metricOvsDpMasksHit.Reset()
	metricOvsDpMasksTotal.Reset()
	metricOvsDpMasksHitRatio.Reset()

	metricOvsDp.Reset()
	metricOvsDpTotal.Reset()
	metricOvsDpIf.Reset()
	metricOvsDpIfTotal.Reset()
}

func resetOvsInterfaceMetrics() {
	interfaceMain.Reset()
	interfaceAdminState.Reset()
	interfaceLinkState.Reset()
	interfaceMacInUse.Reset()
	interfaceMtu.Reset()
	interfaceOfPort.Reset()
	interfaceIfIndex.Reset()

	interfaceStatRxCrcError.Reset()
	interfaceStatRxDropped.Reset()
	interfaceStatRxFrameError.Reset()
	interfaceStatRxMissedError.Reset()
	interfaceStatRxOverrunError.Reset()
	interfaceStatRxErrorsTotal.Reset()
	interfaceStatRxPackets.Reset()
	interfaceStatRxBytes.Reset()

	interfaceStatTxPackets.Reset()
	interfaceStatTxBytes.Reset()
	interfaceStatTxDropped.Reset()
	interfaceStatTxErrorsTotal.Reset()
	interfaceStatCollisions.Reset()
	interfaceStatRxMulticastPackets.Reset()
}
