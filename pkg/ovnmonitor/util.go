package ovnmonitor

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/greenpau/ovsdb"
	"k8s.io/klog/v2"
)

// IncrementErrorCounter increases the counter of failed queries to OVN server.
func (e *Exporter) IncrementErrorCounter() {
	e.errorsLocker.Lock()
	defer e.errorsLocker.Unlock()
	atomic.AddInt64(&e.errors, 1)
}

func (e *Exporter) getOvnStatus() map[string]int {
	result := make(map[string]int)

	// get ovn-northbound status
	cmdstr := "ovs-appctl -t /var/run/ovn/ovnnb_db.ctl cluster/status OVN_Northbound"
	cmd := exec.Command("sh", "-c", cmdstr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		klog.Errorf("get ovn-northbound status failed, err %v", err)
		result["ovsdb-server-northbound"] = 0
	}
	result["ovsdb-server-northbound"] = parseDbStatus(string(output))

	// get ovn-southbound status
	cmdstr = "ovs-appctl -t /var/run/ovn/ovnsb_db.ctl cluster/status OVN_Southbound"
	cmd = exec.Command("sh", "-c", cmdstr)
	output, err = cmd.CombinedOutput()
	if err != nil {
		klog.Errorf("get ovn-southbound status failed, err %v", err)
		result["ovsdb-server-southbound"] = 0
	}
	result["ovsdb-server-southbound"] = parseDbStatus(string(output))

	// get ovn-northd status
	pid, err := os.ReadFile(e.Client.Service.Northd.File.Pid.Path)
	if err != nil {
		klog.Errorf("read ovn-northd pid failed, err %v", err)
		result["ovn-northd"] = 0
	} else {
		cmdstr := fmt.Sprintf("ovs-appctl -t /var/run/ovn/ovn-northd.%s.ctl status", strings.Trim(string(pid), "\n"))
		klog.V(3).Infof("cmd is %v", cmdstr)
		cmd := exec.Command("sh", "-c", cmdstr)
		output, err := cmd.CombinedOutput()
		if err != nil {
			klog.Errorf("get ovn-northd status failed, err %v", err)
			result["ovn-northd"] = 0
		}
		if len(strings.Split(string(output), ":")) != 2 {
			result["ovn-northd"] = 0
		} else {
			status := strings.TrimSpace(strings.Split(string(output), ":")[1])
			if status == "standby" {
				result["ovn-northd"] = 2
			} else if status == "active" {
				result["ovn-northd"] = 1
			}
		}
	}

	return result
}

func getClusterEnableState(dbName string) (bool, error) {
	cmdstr := fmt.Sprintf("ovsdb-tool db-is-clustered %s", dbName)
	cmd := exec.Command("sh", "-c", cmdstr)
	_, err := cmd.CombinedOutput()
	if err != nil {
		klog.Error(err)
		return false, err
	}
	return true, nil
}

func (e *Exporter) setLogicalSwitchInfoMetric() {
	lsws, err := e.Client.GetLogicalSwitches()
	if err != nil {
		klog.Errorf("%s: %v", e.Client.Database.Southbound.Name, err)
		e.IncrementErrorCounter()
	} else {
		for _, lsw := range lsws {
			metricLogicalSwitchInfo.WithLabelValues(e.Client.System.Hostname, lsw.UUID, lsw.Name).Set(1)
			metricLogicalSwitchPortsNum.WithLabelValues(e.Client.System.Hostname, lsw.UUID, lsw.Name).Set(float64(len(lsw.Ports)))
			if len(lsw.Ports) > 0 {
				for _, p := range lsw.Ports {
					metricLogicalSwitchPortBinding.WithLabelValues(e.Client.System.Hostname, lsw.UUID, p, lsw.Name).Set(1)
				}
			}
			if len(lsw.ExternalIDs) > 0 {
				for k, v := range lsw.ExternalIDs {
					metricLogicalSwitchExternalIDs.WithLabelValues(e.Client.System.Hostname, lsw.UUID, k, v, lsw.Name).Set(1)
				}
			}
			metricLogicalSwitchTunnelKey.WithLabelValues(e.Client.System.Hostname, lsw.UUID, lsw.Name).Set(float64(lsw.TunnelKey))
		}
	}
}

func lspAddress(addresses []ovsdb.OvnLogicalSwitchPortAddress) (mac, ip string) {
	if len(addresses) == 0 {
		return "", ""
	}
	if addresses[0].Router {
		return "router", "router"
	}
	if addresses[0].Unknown {
		return "unknown", "unknown"
	}
	if addresses[0].Dynamic {
		return "dynamic", "dynamic"
	}

	if addresses[0].MacAddress != nil {
		mac = addresses[0].MacAddress.String()
	}
	ips := make([]string, 0, len(addresses[0].IpAddresses))
	for _, address := range addresses[0].IpAddresses {
		ips = append(ips, address.String())
	}
	ip = strings.Join(ips, " ")
	return
}

func (e *Exporter) setLogicalSwitchPortInfoMetric() {
	lswps, err := e.Client.GetLogicalSwitchPorts()
	if err != nil {
		klog.Errorf("%s: %v", e.Client.Database.Southbound.Name, err)
		e.IncrementErrorCounter()
	} else {
		for _, port := range lswps {
			mac, ip := lspAddress(port.Addresses)
			metricLogicalSwitchPortInfo.WithLabelValues(e.Client.System.Hostname, port.UUID, port.Name, port.ChassisUUID,
				port.LogicalSwitchName, port.DatapathUUID, port.PortBindingUUID, mac, ip).Set(1)
			metricLogicalSwitchPortTunnelKey.WithLabelValues(e.Client.System.Hostname, port.UUID, port.LogicalSwitchName, port.Name).Set(float64(port.TunnelKey))
		}
	}
}

func getClusterInfo(direction, dbName string) (*OVNDBClusterStatus, error) {
	clusterStatus := &OVNDBClusterStatus{}
	var err error

	cmdstr := fmt.Sprintf("ovs-appctl -t /var/run/ovn/ovn%s_db.ctl cluster/status %s", direction, dbName)
	cmd := exec.Command("sh", "-c", cmdstr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve cluster/status info for database %s: %v", dbName, err)
	}

	for _, line := range strings.Split(string(output), "\n") {
		idx := strings.Index(line, ":")
		if idx == -1 {
			continue
		}
		switch line[:idx] {
		case "Cluster ID":
			// the value is of the format `45ef (45ef51b9-9401-46e7-810d-6db0fc344ea2)`
			clusterStatus.cid = strings.Trim(strings.Fields(line[idx+2:])[1], "()")
		case "Server ID":
			clusterStatus.sid = strings.Trim(strings.Fields(line[idx+2:])[1], "()")
		case "Status":
			clusterStatus.status = line[idx+2:]
		case "Role":
			clusterStatus.role = line[idx+2:]
		case "Term":
			if value, err := strconv.ParseFloat(line[idx+2:], 64); err == nil {
				clusterStatus.term = value
			}
		case "Leader":
			clusterStatus.leader = line[idx+2:]
		case "Vote":
			clusterStatus.vote = line[idx+2:]
		case "Election timer":
			if value, err := strconv.ParseFloat(line[idx+2:], 64); err == nil {
				clusterStatus.electionTimer = value
			}
		case "Log":
			// the value is of the format [2, 1108]
			values := strings.Split(strings.Trim(line[idx+2:], "[]"), ", ")
			if value, err := strconv.ParseFloat(values[0], 64); err == nil {
				clusterStatus.logIndexStart = value
			}
			if value, err := strconv.ParseFloat(values[1], 64); err == nil {
				clusterStatus.logIndexNext = value
			}
		case "Entries not yet committed":
			if value, err := strconv.ParseFloat(line[idx+2:], 64); err == nil {
				clusterStatus.logNotCommitted = value
			}
		case "Entries not yet applied":
			if value, err := strconv.ParseFloat(line[idx+2:], 64); err == nil {
				clusterStatus.logNotApplied = value
			}
		case "Connections":
			// The value could be nil
			if len(line[idx+1:]) != 0 {
				// the value is of the format `->0000 (->56d7) <-46ac <-56d7`
				var connIn, connOut, connInErr, connOutErr float64
				for _, conn := range strings.Fields(line[idx+2:]) {
					switch {
					case strings.HasPrefix(conn, "->"):
						connOut++
					case strings.HasPrefix(conn, "<-"):
						connIn++
					case strings.HasPrefix(conn, "(->"):
						connOutErr++
					case strings.HasPrefix(conn, "(<-"):
						connInErr++
					}
				}
				clusterStatus.connIn = connIn
				clusterStatus.connOut = connOut
				clusterStatus.connInErr = connInErr
				clusterStatus.connOutErr = connOutErr
			}
		}
	}

	return clusterStatus, nil
}

func (e *Exporter) setOvnClusterInfoMetric(c *OVNDBClusterStatus, dbName string) {
	metricClusterRole.WithLabelValues(e.Client.System.Hostname, dbName, c.sid, c.cid, c.role).Set(1)
	metricClusterStatus.WithLabelValues(e.Client.System.Hostname, dbName, c.sid, c.cid, c.status).Set(1)
	metricClusterTerm.WithLabelValues(e.Client.System.Hostname, dbName, c.sid, c.cid).Set(c.term)

	if c.leader == "self" {
		metricClusterLeaderSelf.WithLabelValues(e.Client.System.Hostname, dbName, c.sid, c.cid).Set(1)
	} else {
		metricClusterLeaderSelf.WithLabelValues(e.Client.System.Hostname, dbName, c.sid, c.cid).Set(0)
	}
	if c.vote == "self" {
		metricClusterVoteSelf.WithLabelValues(e.Client.System.Hostname, dbName, c.sid, c.cid).Set(1)
	} else {
		metricClusterVoteSelf.WithLabelValues(e.Client.System.Hostname, dbName, c.sid, c.cid).Set(0)
	}

	metricClusterElectionTimer.WithLabelValues(e.Client.System.Hostname, dbName, c.sid, c.cid).Set(c.electionTimer)
	metricClusterNotCommittedEntryCount.WithLabelValues(e.Client.System.Hostname, dbName, c.sid, c.cid).Set(c.logNotCommitted)
	metricClusterNotAppliedEntryCount.WithLabelValues(e.Client.System.Hostname, dbName, c.sid, c.cid).Set(c.logNotApplied)
	metricClusterLogIndexStart.WithLabelValues(e.Client.System.Hostname, dbName, c.sid, c.cid).Set(c.logIndexStart)
	metricClusterLogIndexNext.WithLabelValues(e.Client.System.Hostname, dbName, c.sid, c.cid).Set(c.logIndexNext)

	metricClusterInConnTotal.WithLabelValues(e.Client.System.Hostname, dbName, c.sid, c.cid).Set(c.connIn)
	metricClusterOutConnTotal.WithLabelValues(e.Client.System.Hostname, dbName, c.sid, c.cid).Set(c.connOut)
	metricClusterInConnErrTotal.WithLabelValues(e.Client.System.Hostname, dbName, c.sid, c.cid).Set(c.connInErr)
	metricClusterOutConnErrTotal.WithLabelValues(e.Client.System.Hostname, dbName, c.sid, c.cid).Set(c.connOutErr)
}

func parseDbStatus(output string) int {
	var status string
	var result int
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "Role:") {
			status = strings.TrimSpace(strings.Split(line, ":")[1])
			break
		}
	}

	switch status {
	case "leader":
		result = 1
	case "follower":
		result = 2
	default:
		result = 0
	}
	return result
}

func getDBStatus(dbName string) (bool, error) {
	var cmdstr string
	var result bool
	switch dbName {
	case "OVN_Northbound":
		cmdstr = fmt.Sprintf("ovn-appctl -t /var/run/ovn/ovnnb_db.ctl ovsdb-server/get-db-storage-status %s", dbName)
	case "OVN_Southbound":
		cmdstr = fmt.Sprintf("ovn-appctl -t /var/run/ovn/ovnsb_db.ctl ovsdb-server/get-db-storage-status %s", dbName)
	}

	cmd := exec.Command("sh", "-c", cmdstr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		klog.Errorf("get ovn-northbound status failed, err %v", err)
		return false, err
	}
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "status: ok") {
			result = true
			break
		}
		if strings.Contains(line, "ovsdb error") {
			result = false
			break
		}
	}

	return result, nil
}

func resetLogicalSwitchMetrics() {
	metricLogicalSwitchInfo.Reset()
	metricLogicalSwitchPortsNum.Reset()
	metricLogicalSwitchPortBinding.Reset()
	metricLogicalSwitchExternalIDs.Reset()
	metricLogicalSwitchTunnelKey.Reset()
}

func resetLogicalSwitchPortMetrics() {
	metricLogicalSwitchPortInfo.Reset()
	metricLogicalSwitchPortTunnelKey.Reset()
}

func resetOvnClusterMetrics() {
	metricClusterRole.Reset()
	metricClusterStatus.Reset()
	metricClusterTerm.Reset()
	metricClusterLeaderSelf.Reset()
	metricClusterVoteSelf.Reset()

	metricClusterElectionTimer.Reset()
	metricClusterNotCommittedEntryCount.Reset()
	metricClusterNotAppliedEntryCount.Reset()
	metricClusterLogIndexStart.Reset()
	metricClusterLogIndexNext.Reset()

	metricClusterInConnTotal.Reset()
	metricClusterOutConnTotal.Reset()
	metricClusterInConnErrTotal.Reset()
	metricClusterOutConnErrTotal.Reset()
}
