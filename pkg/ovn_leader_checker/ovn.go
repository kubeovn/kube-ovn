package ovn_leader_checker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/ovn-kubernetes/libovsdb/ovsdb/serverdb"
	"github.com/spf13/pflag"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	OvnNorthdServiceName = "ovn-northd"
	OvnNorthdPid         = "/var/run/ovn/ovn-northd.pid"
	DefaultProbeInterval = 5
	MaxFailCount         = 3
)

var failCount int

var labelSelector = labels.Set{discoveryv1.LabelServiceName: OvnNorthdServiceName}.AsSelector().String()

// Configuration is the controller conf
type Configuration struct {
	KubeConfigFile  string
	KubeClient      kubernetes.Interface
	ProbeInterval   int
	EnableCompact   bool
	ISICDBServer    bool
	localAddress    string
	remoteAddresses []string
}

// ParseFlags parses cmd args then init kubeclient and conf
// TODO: validate configuration
func ParseFlags() (*Configuration, error) {
	podIP := os.Getenv(util.EnvPodIP)
	var (
		argKubeConfigFile = pflag.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information. If not set use the inCluster token.")
		argProbeInterval  = pflag.Int("probeInterval", DefaultProbeInterval, "interval of probing leader in seconds")
		argEnableCompact  = pflag.Bool("enableCompact", true, "is enable compact")
		argIsICDBServer   = pflag.Bool("isICDBServer", false, "is ic db server")
		localAddress      = pflag.String("localAddress", podIP, "local ovsdb server address")
		remoteAddresses   = pflag.StringSlice("remoteAddresses", nil, "remote ovsdb server addresses")
	)

	klogFlags := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(klogFlags)

	// Sync the glog and klog flags.
	pflag.CommandLine.VisitAll(func(f1 *pflag.Flag) {
		f2 := klogFlags.Lookup(f1.Name)
		if f2 != nil {
			value := f1.Value.String()
			if err := f2.Value.Set(value); err != nil {
				klog.Errorf("failed to set flag %v", err)
			}
		}
	})

	// change the behavior of cmdline
	// not exit. not good
	pflag.CommandLine.Init(os.Args[0], pflag.ContinueOnError)
	pflag.CommandLine.AddGoFlagSet(klogFlags)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	if err := pflag.CommandLine.Parse(os.Args[1:]); err != nil {
		return nil, err
	}

	config := &Configuration{
		KubeConfigFile:  *argKubeConfigFile,
		ProbeInterval:   *argProbeInterval,
		EnableCompact:   *argEnableCompact,
		ISICDBServer:    *argIsICDBServer,
		localAddress:    *localAddress,
		remoteAddresses: slices.DeleteFunc(*remoteAddresses, func(s string) bool { return s == *localAddress }),
	}

	return config, nil
}

// KubeClientInit funcs to check apiserver alive
func KubeClientInit(cfg *Configuration) error {
	if cfg == nil {
		return errors.New("invalid cfg")
	}

	// init kubeconfig here
	var kubeCfg *rest.Config
	var err error
	if cfg.KubeConfigFile == "" {
		klog.Infof("no --kubeconfig, use in-cluster kubernetes config")
		kubeCfg, err = rest.InClusterConfig()
	} else {
		kubeCfg, err = clientcmd.BuildConfigFromFlags("", cfg.KubeConfigFile)
	}
	if err != nil {
		klog.Errorf("init kubernetes cfg failed %v", err)
		return err
	}

	kubeCfg.ContentType = util.ContentTypeProtobuf
	kubeCfg.AcceptContentTypes = util.AcceptContentTypes
	if cfg.KubeClient, err = kubernetes.NewForConfig(kubeCfg); err != nil {
		klog.Errorf("init kubernetes client failed %v", err)
		return err
	}
	return nil
}

func getCmdExitCode(cmd *exec.Cmd) int {
	err := cmd.Run()
	if err != nil {
		klog.Errorf("getCmdExitCode run error %v", err)
		return -1
	}
	if cmd.ProcessState == nil {
		klog.Errorf("getCmdExitCode run error %v", err)
		return -1
	}
	status := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if status.Exited() {
		return status.ExitStatus()
	}
	return -1
}

func checkOvnIsAlive() bool {
	components := [...]string{"northd", "ovnnb", "ovnsb"}
	for _, component := range components {
		cmd := exec.Command("/usr/share/ovn/scripts/ovn-ctl", "status_"+component) // #nosec G204
		if err := getCmdExitCode(cmd); err != 0 {
			klog.Errorf("CheckOvnIsAlive: %s is not alive", component)
			return false
		}
		klog.V(5).Infof("CheckOvnIsAlive: %s is alive", component)
	}
	return true
}

// isDBLeader checks whether the ovn db at address is leader for the given database
func isDBLeader(address, database string) bool {
	var dbAddr string
	switch database {
	case ovnnb.DatabaseName:
		dbAddr = ovs.OvsdbServerAddress(address, intstr.FromInt32(util.NBDatabasePort))
	case ovnsb.DatabaseName:
		dbAddr = ovs.OvsdbServerAddress(address, intstr.FromInt32(util.SBDatabasePort))
	case util.DatabaseICNB:
		dbAddr = ovs.OvsdbServerAddress(address, intstr.FromInt32(util.ICNBDatabasePort))
	case util.DatabaseICSB:
		dbAddr = ovs.OvsdbServerAddress(address, intstr.FromInt32(util.ICSBDatabasePort))
	default:
		klog.Errorf("isDBLeader: unsupported database %s", database)
		return false
	}

	result, err := ovs.Query(dbAddr, serverdb.DatabaseName, 1, ovsdb.Operation{
		Op:    ovsdb.OperationSelect,
		Table: serverdb.DatabaseTable,
		Where: []ovsdb.Condition{{
			Column:   "name",
			Function: ovsdb.ConditionEqual,
			Value:    database,
		}},
		Columns: []string{"leader"},
	})
	if err != nil {
		klog.Errorf("failed to query leader info from ovsdb-server %s for database %s: %v", address, database, err)
		return false
	}
	if len(result) != 1 {
		klog.Errorf("unexpected number of results when querying leader info from ovsdb-server %s for database %s: %d", address, database, len(result))
		return false
	}
	if len(result[0].Rows) == 0 {
		klog.Errorf("no rows returned when querying leader info from ovsdb-server %s for database %s", address, database)
		return false
	}
	if len(result[0].Rows) != 1 {
		klog.Errorf("unexpected number of rows when querying leader info from ovsdb-server %s for database %s: %d", address, database, len(result[0].Rows))
		return false
	}

	leader, ok := result[0].Rows[0]["leader"].(bool)
	if !ok {
		klog.Errorf("unexpected data format for leader info from ovsdb-server %s for database %s: %v", address, database, result[0].Rows[0]["leader"])
		return false
	}

	return leader
}

func checkNorthdActive() bool {
	pid, err := os.ReadFile(OvnNorthdPid)
	if err != nil {
		klog.Errorf("failed to read file %q: %v", OvnNorthdPid, err)
		return false
	}

	command := []string{
		"-t",
		fmt.Sprintf("/var/run/ovn/ovn-northd.%s.ctl", strings.TrimSpace(string(pid))),
		"status",
	}
	output, err := exec.Command("ovn-appctl", command...).CombinedOutput() // #nosec G204
	if err != nil {
		klog.Errorf("checkNorthdActive execute err %v error msg %v", err, string(output))
		return false
	}

	if len(output) == 0 {
		klog.Errorf("checkNorthdActive no output")
		return false
	}

	klog.V(5).Infof("checkNorthdActive: output %s", string(output))
	result := strings.TrimSpace(string(output))
	return strings.Contains(result, "active")
}

func stealLock() {
	args := []string{
		"-v", "-t", "1", "steal",
		ovs.OvsdbServerAddress(os.Getenv(util.EnvPodIP), intstr.FromInt32(util.SBDatabasePort)),
		"ovn_northd",
	}
	if os.Getenv(util.EnvSSLEnabled) == "true" {
		args = slices.Insert(args, 0, ovs.CmdSSLArgs()...)
	}

	output, err := exec.Command("ovsdb-client", args...).CombinedOutput() // #nosec G204
	if err != nil {
		klog.Errorf("stealLock err %v", err)
		return
	}

	if len(output) != 0 {
		klog.V(5).Infof("stealLock: output %s", string(output))
	}
}

func checkNorthdSvcExist(cfg *Configuration, namespace, svcName string) bool {
	_, err := cfg.KubeClient.CoreV1().Services(namespace).Get(context.Background(), svcName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("get svc %v namespace %v error %v", svcName, namespace, err)
		return false
	}
	return true
}

func checkNorthdEpAvailable(ip string) bool {
	address := util.JoinHostPort(ip, util.NBRaftPort)
	conn, err := net.DialTimeout("tcp", address, 3*time.Second)
	if err != nil {
		klog.Errorf("failed to connect to northd leader %s, err: %v", ip, err)
		failCount++
		if failCount >= MaxFailCount {
			return false
		}
	} else {
		failCount = 0
		klog.V(5).Infof("succeed to connect to northd leader %s", ip)
		_ = conn.Close()
	}
	return true
}

func checkNorthdEpAlive(cfg *Configuration, namespace, service string) bool {
	epsList, err := cfg.KubeClient.DiscoveryV1().EndpointSlices(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		klog.Errorf("failed to list endpoint slices for service %s/%s: %v", namespace, service, err)
		return false
	}

	for _, eps := range epsList.Items {
		for _, ep := range eps.Endpoints {
			if (ep.Conditions.Ready != nil && !*ep.Conditions.Ready) || len(ep.Addresses) == 0 {
				continue
			}

			// Found an address, check its availability. We only need one.
			klog.V(5).Infof("found address %s in endpoint slice %s/%s for service %s, checking availability", ep.Addresses[0], eps.Namespace, eps.Name, service)
			return checkNorthdEpAvailable(ep.Addresses[0])
		}
	}

	klog.V(5).Infof("no address found in any endpoint slices for service %s/%s", namespace, service)
	return false
}

func compactOvnDatabase(db string) {
	args := []string{
		"-t",
		fmt.Sprintf("/var/run/ovn/ovn%s_db.ctl", db),
		"ovsdb-server/compact",
	}
	output, err := exec.Command("ovn-appctl", args...).CombinedOutput() // #nosec G204
	if err != nil {
		if !strings.Contains(string(output), "not storing a duplicate snapshot") {
			klog.Errorf("failed to compact ovn%s database: %s", db, string(output))
		}
		return
	}

	if len(output) != 0 {
		klog.V(5).Infof("compact ovn%s database: %s", db, string(output))
	}
}

// backupRaftHeader backs up the raft header of the ovn db file.
// The backup file name is ovn<db>_db.hdr, e.g., ovnnb_db.hdr for ovnnb database file named ovnnb_db.db.
// Example content of the header file:
//
//	{
//	  "server_id": "8d77699d-8dc6-4f32-b1ba-b66aad05ba46",
//	  "name": "OVN_Northbound",
//	  "local_address": "tcp:[172.18.0.2]:6643",
//	  "cluster_id": "6d240b86-177e-4f17-aded-ed1b7b364d97"
//	}
func backupRaftHeader(db string) {
	args := []string{"db-raft-header", fmt.Sprintf("/etc/ovn/ovn%s_db.db", db)}
	hdr, err := exec.Command("ovsdb-tool", args...).CombinedOutput() // #nosec G204
	if err != nil {
		klog.Errorf("failed to backup raft header of ovn%s database: error = %v, output = %s", db, err, string(hdr))
		return
	}

	var data map[string]any
	if err = json.Unmarshal(hdr, &data); err != nil {
		klog.Errorf("failed to unmarshal raft header json content for ovn%s database: %v", db, err)
		return
	}

	hdr, _ = json.MarshalIndent(data, "", "  ")
	hdrFile := fmt.Sprintf("/etc/ovn/ovn%s_db.hdr", db)
	content, err := os.ReadFile(hdrFile)
	if err != nil {
		if !os.IsNotExist(err) {
			klog.Errorf("failed to read raft header file %s: %v", hdrFile, err)
		}
		klog.V(5).Infof("raft header file %s does not exist, created new one", hdrFile)
	}

	if bytes.Equal(content, hdr) {
		klog.V(5).Infof("raft header file %s is up-to-date, no need to update", hdrFile)
		return
	}

	klog.Infof("Found changes in raft header for ovn%s database, updating file %s", db, hdrFile)
	klog.Infof("Previous content of raft header file %s:\n%s", hdrFile, string(content))

	if err = os.WriteFile(hdrFile, hdr, 0o600); err != nil {
		klog.Errorf("failed to write raft header file %s: %v", hdrFile, err)
		return
	}

	klog.Infof("succeeded to backup raft header of ovn%s database to file %s with content:\n%s", db, hdrFile, string(hdr))
}

func doOvnLeaderCheck(cfg *Configuration, podName, podNamespace string) {
	if podName == "" || podNamespace == "" {
		util.LogFatalAndExit(nil, "env variables POD_NAME and POD_NAMESPACE must be set")
	}
	if cfg == nil || cfg.KubeClient == nil {
		util.LogFatalAndExit(nil, "preValidChkCfg: invalid cfg")
	}

	if !cfg.ISICDBServer && !checkOvnIsAlive() {
		klog.Errorf("ovn is not alive")
		return
	}

	if !cfg.ISICDBServer {
		nbLeader := isDBLeader(cfg.localAddress, ovnnb.DatabaseName)
		sbLeader := isDBLeader(cfg.localAddress, ovnsb.DatabaseName)
		northdActive := checkNorthdActive()
		patch := util.KVPatch{
			"ovn-nb-leader":     strconv.FormatBool(nbLeader),
			"ovn-sb-leader":     strconv.FormatBool(sbLeader),
			"ovn-northd-leader": strconv.FormatBool(northdActive),
		}
		if err := util.PatchLabels(cfg.KubeClient.CoreV1().Pods(podNamespace), podName, patch); err != nil {
			klog.Errorf("failed to patch labels for pod %s/%s: %v", podNamespace, podName, err)
			return
		}
		if sbLeader && checkNorthdSvcExist(cfg, podNamespace, "ovn-northd") {
			if !checkNorthdEpAlive(cfg, podNamespace, "ovn-northd") {
				klog.Warning("no available northd leader, try to release the lock")
				stealLock()
			}
		}

		for addr := range slices.Values(cfg.remoteAddresses) {
			if nbLeader && isDBLeader(addr, ovnnb.DatabaseName) {
				klog.Fatalf("found another ovn-nb leader at %s, exiting process to restart", addr)
			}
			if sbLeader && isDBLeader(addr, ovnsb.DatabaseName) {
				klog.Fatalf("found another ovn-sb leader at %s, exiting process to restart", addr)
			}
		}

		if cfg.EnableCompact {
			compactOvnDatabase("nb")
			compactOvnDatabase("sb")
		}

		backupRaftHeader("nb")
		backupRaftHeader("sb")
	} else {
		icNbLeader := isDBLeader(cfg.localAddress, util.DatabaseICNB)
		icSbLeader := isDBLeader(cfg.localAddress, util.DatabaseICSB)
		patch := util.KVPatch{
			"ovn-ic-nb-leader": strconv.FormatBool(icNbLeader),
			"ovn-ic-sb-leader": strconv.FormatBool(icSbLeader),
		}
		if err := util.PatchLabels(cfg.KubeClient.CoreV1().Pods(podNamespace), podName, patch); err != nil {
			klog.Errorf("failed to patch labels for pod %s/%s: %v", podNamespace, podName, err)
			return
		}

		if icNbLeader {
			if err := updateTS(); err != nil {
				klog.Errorf("update ts num failed err: %v", err)
				return
			}
		}

		for addr := range slices.Values(cfg.remoteAddresses) {
			if icNbLeader && isDBLeader(addr, util.DatabaseICNB) {
				klog.Fatalf("found another ovn-ic-nb leader at %s, exiting process to restart", addr)
			}
			if icSbLeader && isDBLeader(addr, util.DatabaseICSB) {
				klog.Fatalf("found another ovn-ic-sb leader at %s, exiting process to restart", addr)
			}
		}
	}
}

func StartOvnLeaderCheck(cfg *Configuration) {
	podName := os.Getenv(util.EnvPodName)
	podNamespace := os.Getenv(util.EnvPodNamespace)
	interval := time.Duration(cfg.ProbeInterval) * time.Second
	for {
		doOvnLeaderCheck(cfg, podName, podNamespace)
		time.Sleep(interval)
	}
}

func getTSName(index int) string {
	if index == 0 {
		return util.InterconnectionSwitch
	}
	return fmt.Sprintf("%s%d", util.InterconnectionSwitch, index)
}

func getTSCidr(index int) (string, error) {
	var proto, cidr string
	podIpsEnv := os.Getenv(util.EnvPodIPs)
	podIps := strings.Split(podIpsEnv, ",")
	if len(podIps) == 1 {
		if util.CheckProtocol(podIps[0]) == kubeovnv1.ProtocolIPv6 {
			proto = kubeovnv1.ProtocolIPv6
		} else {
			proto = kubeovnv1.ProtocolIPv4
		}
	} else if len(podIps) == 2 {
		proto = kubeovnv1.ProtocolDual
	}

	switch proto {
	case kubeovnv1.ProtocolIPv4:
		cidr = fmt.Sprintf("169.254.%d.0/24", 100+index)
	case kubeovnv1.ProtocolIPv6:
		cidr = fmt.Sprintf("fe80:a9fe:%02x::/112", 100+index)
	case kubeovnv1.ProtocolDual:
		cidr = fmt.Sprintf("169.254.%d.0/24,fe80:a9fe:%02x::/112", 100+index, 100+index)
	}
	return cidr, nil
}

func updateTS() error {
	cmd := exec.Command("ovn-ic-nbctl", "show")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ovn-ic-nbctl show output: %s, err: %w", output, err)
	}
	var existTSCount int
	if lines := strings.TrimSpace(string(output)); lines != "" {
		existTSCount = len(strings.Split(lines, "\n"))
	}
	expectTSCount, err := strconv.Atoi(os.Getenv("TS_NUM"))
	if err != nil {
		return fmt.Errorf("expectTSCount atoi failed output: %s, err: %w", output, err)
	}
	if expectTSCount == existTSCount {
		klog.V(3).Infof("expectTSCount %d no changes required.", expectTSCount)
		return nil
	}

	if expectTSCount > existTSCount {
		for i := expectTSCount - 1; i > existTSCount-1; i-- {
			tsName := getTSName(i)
			subnet, err := getTSCidr(i)
			if err != nil {
				return err
			}
			args := []string{}
			if os.Getenv(util.EnvSSLEnabled) == "true" {
				args = append(args,
					"--private-key=/var/run/tls/key",
					"--certificate=/var/run/tls/cert",
					"--ca-cert=/var/run/tls/cacert",
				)
			}
			args = append(args,
				ovs.MayExist, "ts-add", tsName,
				"--", "set", "Transit_Switch", tsName,
				fmt.Sprintf(`external_ids:subnet="%s"`, subnet),
				fmt.Sprintf(`external_ids:vendor="%s"`, util.CniTypeName),
			)
			// #nosec G204
			cmd = exec.Command("ovn-ic-nbctl", args...)
			output, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("output: %s, err: %w", output, err)
			}
		}
	} else {
		for i := existTSCount - 1; i >= expectTSCount; i-- {
			tsName := getTSName(i)
			cmd := exec.Command("ovn-ic-nbctl", "ts-del", tsName) // #nosec G204
			if os.Getenv(util.EnvSSLEnabled) == "true" {
				// #nosec G204
				cmd = exec.Command("ovn-ic-nbctl",
					"--private-key=/var/run/tls/key",
					"--certificate=/var/run/tls/cert",
					"--ca-cert=/var/run/tls/cacert",
					"ts-del", tsName)
			}
			output, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("output: %s, err: %w", output, err)
			}
		}
	}

	return nil
}
