package pinger

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/greenpau/ovsdb"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"k8s.io/klog/v2"
)

// IncrementErrorCounter increases the counter of failed queries to OVN server.
func (e *Exporter) IncrementErrorCounter() {
	e.errorsLocker.Lock()
	defer e.errorsLocker.Unlock()
	atomic.AddInt64(&e.errors, 1)
}

func (e *Exporter) getOvsStatus() map[string]bool {
	components := []string{
		"ovsdb-server",
		"ovs-vswitchd",
	}
	result := make(map[string]bool)
	for _, component := range components {
		_, err := e.Client.GetProcessInfo(component)
		if err != nil {
			klog.Errorf("%s: pid-%v", component, err)
			e.IncrementErrorCounter()
			result[component] = false
			continue
		}
		result[component] = true
	}

	return result
}

func (e *Exporter) getOvsDatapath() ([]string, error) {
	var datapathsList []string
	cmdstr := fmt.Sprintf("ovs-appctl -T %v dpctl/dump-dps", e.Client.Timeout)
	cmd := exec.Command("sh", "-c", cmdstr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get output of dpctl/dump-dps: %v", err)
	}

	for _, kvPair := range strings.Split(string(output), "\n") {
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
	cmdstr := fmt.Sprintf("ovs-appctl -T %v dpctl/show %s", e.Client.Timeout, datapathName)
	cmd := exec.Command("sh", "-c", cmdstr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get output of dpctl/show %s: %v", datapathName, err)
	}

	var datapathPortCount float64
	for _, kvPair := range strings.Split(string(output), "\n") {
		line := strings.TrimSpace(kvPair)
		if strings.HasPrefix(line, "lookups:") {
			e.ovsDatapathLookupsMetrics(line, datapathName)
		} else if strings.HasPrefix(line, "masks:") {
			e.ovsDatapathMasksMetrics(line, datapathName)
		} else if strings.HasPrefix(line, "port ") {
			e.ovsDatapathPortMetrics(line, datapathName)
			datapathPortCount++
		} else if strings.HasPrefix(line, "flows:") {
			flowFields := strings.Fields(line)
			value, _ := strconv.ParseFloat(flowFields[1], 64)
			metricOvsDpFlowsTotal.WithLabelValues(e.Client.System.Hostname, datapathName).Set(value)
		}
	}
	metricOvsDpIfTotal.WithLabelValues(e.Client.System.Hostname, datapathName).Set(datapathPortCount)

	return nil
}

func (e *Exporter) ovsDatapathLookupsMetrics(line, datapath string) {
	s := strings.TrimPrefix(line, "lookups:")
	for _, field := range strings.Fields(s) {
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
	s := strings.TrimPrefix(line, "masks:")
	for _, field := range strings.Fields(s) {
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

func (e *Exporter) getInterfaceInfo() ([]ovsdb.OvsInterface, error) {
	var intfs []ovsdb.OvsInterface

	result, err := ovs.CustomFindEntity("Interface", []string{"_uuid", "name", "admin_state", "link_state", "mac_in_use", "mtu", "ofport", "ifindex", "statistics"})
	if err != nil {
		klog.Errorf("customFindEntity failed, %v", err)
		return intfs, err
	}
	if len(result) == 0 {
		return nil, nil
	}

	for _, line := range result {
		var intf ovsdb.OvsInterface
		if intf.Statistics == nil {
			intf.Statistics = make(map[string]int, 13)
		}

		if _, ok := line["_uuid"]; ok {
			intf.UUID = line["_uuid"][0]
		}
		if _, ok := line["name"]; ok {
			intf.Name = line["name"][0]
		}
		if _, ok := line["admin_state"]; ok {
			intf.AdminState = line["admin_state"][0]
		}
		if _, ok := line["link_state"]; ok {
			intf.LinkState = line["link_state"][0]
		}
		if _, ok := line["mac_in_use"]; ok {
			intf.MacInUse = line["mac_in_use"][0]
		}
		if _, ok := line["mtu"]; ok {
			if len(line["mtu"]) != 0 {
				mtu, _ := strconv.ParseFloat(line["mtu"][0], 64)
				intf.Mtu = mtu
			}
		}
		if _, ok := line["ofport"]; ok {
			ofport, _ := strconv.ParseFloat(line["ofport"][0], 64)
			intf.OfPort = ofport
		}
		if _, ok := line["ifindex"]; ok {
			ifindex, _ := strconv.ParseFloat(line["ifindex"][0], 64)
			intf.IfIndex = ifindex
		}

		if stsValues, ok := line["statistics"]; ok {
			for _, l := range stsValues {
				if len(strings.TrimSpace(l)) == 0 {
					continue
				}
				parts := strings.Split(strings.TrimSpace(l), "=")
				if len(parts) != 2 {
					continue
				}
				value, _ := strconv.Atoi(parts[1])

				switch parts[0] {
				case "rx_crc_err":
					intf.Statistics["rx_crc_err"] = value
				case "rx_dropped":
					intf.Statistics["rx_dropped"] = value
				case "rx_frame_err":
					intf.Statistics["rx_frame_err"] = value
				case "rx_missed_errors":
					intf.Statistics["rx_missed_errors"] = value
				case "rx_over_err":
					intf.Statistics["rx_over_err"] = value
				case "rx_errors":
					intf.Statistics["rx_errors"] = value
				case "rx_packets":
					intf.Statistics["rx_packets"] = value
				case "rx_bytes":
					intf.Statistics["rx_bytes"] = value
				case "tx_packets":
					intf.Statistics["tx_packets"] = value
				case "tx_bytes":
					intf.Statistics["tx_bytes"] = value
				case "tx_dropped":
					intf.Statistics["tx_dropped"] = value
				case "tx_errors":
					intf.Statistics["tx_errors"] = value
				case "collisions":
					intf.Statistics["collisions"] = value
				}
			}
		}
		intfs = append(intfs, intf)
	}
	return intfs, nil
}

func (e *Exporter) setOvsInterfaceMetric(intf ovsdb.OvsInterface) {
	interfaceMain.WithLabelValues(e.Client.System.Hostname, intf.UUID, intf.Name).Set(1)
	e.setOvsInterfaceStateMetric(intf)
	interfaceMacInUse.WithLabelValues(e.Client.System.Hostname, intf.Name, intf.MacInUse).Set(1)
	interfaceMtu.WithLabelValues(e.Client.System.Hostname, intf.Name).Set(intf.Mtu)
	interfaceOfPort.WithLabelValues(e.Client.System.Hostname, intf.Name).Set(intf.OfPort)
	interfaceIfIndex.WithLabelValues(e.Client.System.Hostname, intf.Name).Set(intf.IfIndex)
	e.setOvsInterfaceStatisticsMetric(intf)
}

func (e *Exporter) setOvsInterfaceStateMetric(intf ovsdb.OvsInterface) {
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

func (e *Exporter) setOvsInterfaceStatisticsMetric(intf ovsdb.OvsInterface) {
	for key, value := range intf.Statistics {
		switch key {
		case "rx_crc_err":
			interfaceStatRxCrcError.WithLabelValues(e.Client.System.Hostname, intf.Name).Set(float64(value))
		case "rx_dropped":
			interfaceStatRxDropped.WithLabelValues(e.Client.System.Hostname, intf.Name).Set(float64(value))
		case "rx_frame_err":
			interfaceStatRxFrameError.WithLabelValues(e.Client.System.Hostname, intf.Name).Set(float64(value))
		case "rx_missed_errors":
			interfaceStatRxMissedError.WithLabelValues(e.Client.System.Hostname, intf.Name).Set(float64(value))
		case "rx_over_err":
			interfaceStatRxOverrunError.WithLabelValues(e.Client.System.Hostname, intf.Name).Set(float64(value))
		case "rx_errors":
			interfaceStatRxErrorsTotal.WithLabelValues(e.Client.System.Hostname, intf.Name).Set(float64(value))
		case "rx_packets":
			interfaceStatRxPackets.WithLabelValues(e.Client.System.Hostname, intf.Name).Set(float64(value))
		case "rx_bytes":
			interfaceStatRxBytes.WithLabelValues(e.Client.System.Hostname, intf.Name).Set(float64(value))
		case "tx_packets":
			interfaceStatTxPackets.WithLabelValues(e.Client.System.Hostname, intf.Name).Set(float64(value))
		case "tx_bytes":
			interfaceStatTxBytes.WithLabelValues(e.Client.System.Hostname, intf.Name).Set(float64(value))
		case "tx_dropped":
			interfaceStatTxDropped.WithLabelValues(e.Client.System.Hostname, intf.Name).Set(float64(value))
		case "tx_errors":
			interfaceStatTxErrorsTotal.WithLabelValues(e.Client.System.Hostname, intf.Name).Set(float64(value))
		case "collisions":
			interfaceStatCollisions.WithLabelValues(e.Client.System.Hostname, intf.Name).Set(float64(value))
		default:
			klog.Errorf("OVS interface statistics has unsupported key: %s, value: %d", key, value)
		}
	}
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
}
