package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"k8s.io/klog/v2"
)

type Configuration struct {
	DurationSeconds int64
	RemoteAddress   string
	RemotePort      uint32
	Output          string
}

type Result struct {
	DurationSeconds     int64
	RemoteAddress       string
	RemotePort          uint32
	TotalIcmpEcho       int
	IcmpLost            int
	TotalTcpOutSegments int
	TcpRetransSegment   int
	TotalTcpConnection  int
	FailedTcpConnection int
}

func parseFlag() *Configuration {
	var (
		argDurationSeconds = pflag.Int64("duration-seconds", 60, "The duration to run network break detection.")
		argRemoteAddress   = pflag.String("remote-address", "100.64.0.1", "The remote address to connect.")
		argRemotePort      = pflag.Uint32("remote-port", 80, "The remote port to connect.")
		argOutput          = pflag.String("output", "text", "text or json.")
	)

	pflag.Parse()

	config := &Configuration{
		DurationSeconds: *argDurationSeconds,
		RemoteAddress:   *argRemoteAddress,
		RemotePort:      *argRemotePort,
		Output:          *argOutput,
	}
	return config
}

func ReadSnmp() (map[string]map[string]int, error) {
	buf, err := os.ReadFile("/proc/net/snmp")
	if err != nil {
		return nil, err
	}
	snmp := make(map[string]map[string]int)
	snmpLine := strings.Split(string(buf), "\n")
	for index := range snmpLine {
		if index%2 == 1 || len(snmpLine[index]) == 0 {
			continue
		}
		keys := strings.Split(snmpLine[index], " ")
		values := strings.Split(snmpLine[index+1], " ")
		snmpType := strings.TrimSuffix(keys[0], ":")

		for i := range keys {
			if i == 0 {
				snmp[snmpType] = make(map[string]int)
				continue
			}
			v, _ := strconv.Atoi(values[i])
			snmp[snmpType][keys[i]] = v
		}
	}
	return snmp, err
}

func main() {
	defer klog.Flush()
	icmpDone := make(chan string)
	tcpConnDone := make(chan string)
	tcpRetransDone := make(chan string)
	preSnmp, err := ReadSnmp()
	if err != nil {
		klog.Error(err)
		return
	}
	preIcmpEcho := preSnmp["Icmp"]["OutEchos"]
	preDiff := preSnmp["Icmp"]["OutEchos"] - preSnmp["Icmp"]["InEchoReps"]
	preOutSegs := preSnmp["Tcp"]["OutSegs"]
	preRetrans := preSnmp["Tcp"]["RetransSegs"]

	config := parseFlag()

	go func() {
		output, err := exec.Command("ping", "-D", "-O", "-c", fmt.Sprintf("%d", config.DurationSeconds*100), "-i", "0.01", config.RemoteAddress).CombinedOutput()
		if err != nil {
			klog.Errorf("%s, %v", output, err)
		}
		icmpDone <- ""
	}()

	failedConnection := 0
	totalConnection := 0
	go func() {
		startTime := time.Now()
		for {
			if time.Since(startTime) > (time.Duration(config.DurationSeconds) * time.Second) {
				break
			}
			time.Sleep(100 * time.Millisecond)
			totalConnection += 1
			_, err := exec.Command("curl", "-m", "1", fmt.Sprintf("%s:%d", config.RemoteAddress, config.RemotePort)).CombinedOutput()
			if err != nil {
				failedConnection += 1
			}
		}
		tcpConnDone <- ""
	}()

	go func() {
		output, err := exec.Command("iperf3", "-c", config.RemoteAddress, "-b", "10M", "-t", "60", "-l", "1K").CombinedOutput()
		if err != nil {
			klog.Errorf("%s, %v", output, err)
		}
		tcpRetransDone <- ""
	}()

	<-icmpDone
	<-tcpConnDone
	<-tcpRetransDone

	curSnmp, err := ReadSnmp()
	if err != nil {
		klog.Error(err)
		return
	}

	curIcmpEcho := curSnmp["Icmp"]["OutEchos"]
	curIcmpResponse := curSnmp["Icmp"]["InEchoReps"]
	curDiff := curIcmpEcho - curIcmpResponse
	curOutSegs := curSnmp["Tcp"]["OutSegs"]
	curRetrans := curSnmp["Tcp"]["RetransSegs"]

	result := Result{
		DurationSeconds:     config.DurationSeconds,
		RemoteAddress:       config.RemoteAddress,
		RemotePort:          config.RemotePort,
		TotalIcmpEcho:       curIcmpEcho - preIcmpEcho,
		IcmpLost:            curDiff - preDiff,
		TotalTcpOutSegments: curOutSegs - preOutSegs,
		TcpRetransSegment:   curRetrans - preRetrans,
		TotalTcpConnection:  totalConnection,
		FailedTcpConnection: failedConnection,
	}

	if config.Output == "text" {
		klog.Infof("remote address = %s, remote port = %d", result.RemoteAddress, result.RemotePort)
		klog.Infof("total icmp echo %d, lost %d icmp response", result.TotalIcmpEcho, result.IcmpLost)
		klog.Infof("total out %d tcp segments, retrans %d tcp segments", result.TotalTcpOutSegments, result.TcpRetransSegment)
		klog.Infof("%d failed connection, %d total connection", result.TotalTcpConnection, result.FailedTcpConnection)
	} else {
		output, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(output))
	}
}
