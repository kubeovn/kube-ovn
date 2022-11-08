package main

import (
	"fmt"
	"k8s.io/klog/v2"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

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
	klog.Infof("start")
	icmpDone := make(chan string)
	tcpConnDone := make(chan string)
	tcpRetransDone := make(chan string)
	preSnmp, err := ReadSnmp()
	if err != nil {
		klog.Error(err)
		return
	}
	preDiff := preSnmp["Icmp"]["OutEchos"] - preSnmp["Icmp"]["InEchoReps"]
	preRetrans := preSnmp["Tcp"]["RetransSegs"]

	go func() {
		output, err := exec.Command("ping", "-D", "-O", "-c", "6000", "-i", "0.01", os.Args[1]).CombinedOutput()
		klog.Infof("%s, %v", output, err)
		icmpDone <- ""
	}()

	failedConnection := 0
	totalConnection := 0
	go func() {
		startTime := time.Now()
		for {
			if time.Since(startTime) > 60*time.Second {
				break
			}
			time.Sleep(100 * time.Millisecond)
			totalConnection += 1
			output, err := exec.Command("curl", "-m", "1", fmt.Sprintf("%s:80", os.Args[1])).CombinedOutput()
			if err != nil {
				klog.Infof("%s, %v", output, err)
				failedConnection += 1
			}
		}
		tcpConnDone <- ""
	}()

	go func() {
		output, err := exec.Command("iperf3", "-c", os.Args[1], "-b", "10M", "-t", "60", "-l", "1K").CombinedOutput()
		klog.Infof("%s, %v", output, err)
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
	curDiff := curSnmp["Icmp"]["OutEchos"] - curSnmp["Icmp"]["InEchoReps"]
	curRetrans := curSnmp["Tcp"]["RetransSegs"]
	klog.Infof("lost %d icmp response", curDiff-preDiff)
	klog.Infof("retrans %d tcp segment", curRetrans-preRetrans)
	klog.Infof("%d failed connection, %d total connection", failedConnection, totalConnection)

	klog.Infof("Done")
}
