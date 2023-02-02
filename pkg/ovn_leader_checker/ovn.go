package ovn_leader_checker

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	EnvSSL               = "ENABLE_SSL"
	EnvPodName           = "POD_NAME"
	EnvPodNameSpace      = "POD_NAMESPACE"
	OvnNorthdPid         = "/var/run/ovn/ovn-northd.pid"
	DefaultProbeInterval = 15
)

// Configuration is the controller conf
type Configuration struct {
	KubeConfigFile string
	KubeClient     kubernetes.Interface
	ProbeInterval  int
}

// ParseFlags parses cmd args then init kubeclient and conf
// TODO: validate configuration
func ParseFlags() (*Configuration, error) {
	var (
		argKubeConfigFile = pflag.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information. If not set use the inCluster token.")
		argProbeInterval  = pflag.Int("probeInterval", DefaultProbeInterval, "interval of probing leader in seconds")
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
		KubeConfigFile: *argKubeConfigFile,
		ProbeInterval:  *argProbeInterval,
	}
	return config, nil
}

// KubeClientInit funcs to check apiserver alive
func KubeClientInit(cfg *Configuration) error {
	if cfg == nil {
		return fmt.Errorf("invalid cfg")
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
	kubeClient, err := kubernetes.NewForConfig(kubeCfg)
	if err != nil {
		klog.Errorf("init kubernetes client failed %v", err)
		return err
	}
	cfg.KubeClient = kubeClient
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
		cmd := exec.Command("/usr/share/ovn/scripts/ovn-ctl", fmt.Sprintf("status_%s", component))
		if err := getCmdExitCode(cmd); err != 0 {
			klog.Errorf("CheckOvnIsAlive: %s is not alive", component)
			return false
		}
		klog.V(5).Infof("CheckOvnIsAlive: %s is alive", component)
	}
	return true
}

func isDBLeader(dbName string, port int) bool {
	addr := net.JoinHostPort(os.Getenv("POD_IP"), strconv.Itoa(port))
	query := fmt.Sprintf(`["_Server",{"table":"Database","where":[["name","==","%s"]],"columns":["leader"],"op":"select"}]`, dbName)

	var cmd []string
	if os.Getenv(EnvSSL) == "false" {
		cmd = []string{"query", fmt.Sprintf("tcp:%s", addr), query}
	} else {
		cmd = []string{
			"-p", "/var/run/tls/key",
			"-c", "/var/run/tls/cert",
			"-C", "/var/run/tls/cacert",
			"query", fmt.Sprintf("ssl:%s", addr), query,
		}
	}

	output, err := exec.Command("ovsdb-client", cmd...).CombinedOutput()
	if err != nil {
		klog.Errorf("failed to execute cmd %q: err=%v, msg=%v", strings.Join(cmd, " "), err, string(output))
		return false
	}

	result := strings.TrimSpace(string(output))
	if len(result) == 0 {
		klog.Errorf("cmd %q no output", strings.Join(cmd, " "))
		return false
	}

	klog.V(5).Infof("cmd %q output: %s", strings.Join(cmd, " "), string(output))
	return strings.Contains(result, "true")
}

func checkNorthdActive() bool {
	var command []string
	file, err := os.OpenFile(OvnNorthdPid, os.O_RDWR, 0600)
	if err != nil {
		klog.Errorf("failed to open %s err =  %v", OvnNorthdPid, err)
		return false
	}
	fileByte, err := io.ReadAll(file)
	if err != nil {
		klog.Errorf("failed to read %s err = %v", OvnNorthdPid, err)
		return false
	}

	command = []string{
		"-t",
		fmt.Sprintf("/var/run/ovn/ovn-northd.%s.ctl", strings.TrimSpace(string(fileByte))),
		"status",
	}
	output, err := exec.Command("ovs-appctl", command...).CombinedOutput()
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
	var command []string
	if os.Getenv(EnvSSL) == "false" {
		command = []string{
			"-v",
			"-t",
			"1",
			"steal",
			"tcp:127.0.0.1:6642",
			"ovn_northd",
		}
	} else {
		command = []string{
			"-v",
			"-t",
			"1",
			"-p",
			"/var/run/tls/key",
			"-c",
			"/var/run/tls/cert",
			"-C",
			"/var/run/tls/cacert",
			"steal",
			"ssl:127.0.0.1:6642",
			"ovn_northd",
		}
	}

	output, err := exec.Command("ovsdb-client", command...).CombinedOutput()
	if err != nil {
		klog.Errorf("stealLock err %v", err)
		return
	}

	if len(output) != 0 {
		klog.V(5).Infof("stealLock: output %s", string(output))
	}
}

func patchPodLabels(cfg *Configuration, cachedPod *corev1.Pod, labels map[string]string) error {
	if reflect.DeepEqual(cachedPod.Labels, labels) {
		return nil
	}

	pod := cachedPod.DeepCopy()
	pod.Labels = labels
	patch, err := util.GenerateStrategicMergePatchPayload(cachedPod, pod)
	if err != nil {
		return err
	}
	_, err = cfg.KubeClient.CoreV1().Pods(pod.Namespace).Patch(context.Background(), pod.Name,
		types.StrategicMergePatchType, patch, metav1.PatchOptions{}, "")
	return err
}

func checkNorthdSvcExist(cfg *Configuration, namespace, svcName string) bool {
	_, err := cfg.KubeClient.CoreV1().Services(namespace).Get(context.Background(), svcName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("get svc %v namespace %v error %v", svcName, namespace, err)
		return false
	}
	return true
}

func checkNorthdSvcValidIP(cfg *Configuration, namespace, epName string) bool {
	eps, err := cfg.KubeClient.CoreV1().Endpoints(namespace).Get(context.Background(), epName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("get ep %v namespace %v error %v", epName, namespace, err)
		return false
	}

	if len(eps.Subsets) == 0 {
		klog.V(5).Infof("epName %v has no address assigned", epName)
		return false
	}

	if len(eps.Subsets[0].Addresses) == 0 {
		klog.V(5).Infof("epName %v has no address assigned", epName)
		return false
	}

	klog.V(5).Infof("epName %v address assigned %+v", epName, eps.Subsets[0].Addresses[0].IP)
	return true
}

func updatePodLabels(labels map[string]string, key string, isLeader bool) {
	if isLeader {
		labels[key] = "true"
	} else {
		delete(labels, key)
	}
}

func compactOvnDatabase(db string) {
	var command = []string{
		"-t",
		fmt.Sprintf("/var/run/ovn/ovn%s_db.ctl", db),
		"ovsdb-server/compact",
	}

	output, err := exec.Command("ovn-appctl", command...).CombinedOutput()
	if err != nil {
		if !strings.Contains(string(output), "not storing a duplicate snapshot") {
			klog.Errorf("failed to compact ovn%s database: %s", db, string(output))
		}
		return
	}

	if len(output) != 0 {
		klog.V(5).Infof("compact ovn%s database: %s", string(output))
	}
}

func doOvnLeaderCheck(cfg *Configuration, podName string, podNamespace string) {
	if podName == "" || podNamespace == "" {
		util.LogFatalAndExit(nil, "env variables POD_NAME and POD_NAMESPACE must be set")
	}
	if cfg == nil || cfg.KubeClient == nil {
		util.LogFatalAndExit(nil, "preValidChkCfg: invalid cfg")
	}

	if !checkOvnIsAlive() {
		klog.Errorf("ovn is not alive")
		return
	}

	cachedPod, err := cfg.KubeClient.CoreV1().Pods(podNamespace).Get(context.Background(), podName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("get pod %v namespace %v error %v", podName, podNamespace, err)
		return
	}

	labels := make(map[string]string, len(cachedPod.Labels))
	for k, v := range cachedPod.Labels {
		labels[k] = v
	}
	nbLeader := isDBLeader("OVN_Northbound", 6641)
	sbLeader := isDBLeader("OVN_Southbound", 6642)
	northdLeader := checkNorthdActive()
	updatePodLabels(labels, "ovn-nb-leader", nbLeader)
	updatePodLabels(labels, "ovn-sb-leader", sbLeader)
	updatePodLabels(labels, "ovn-northd-leader", northdLeader)
	if err = patchPodLabels(cfg, cachedPod, labels); err != nil {
		klog.Errorf("patch label error %v", err)
		return
	}
	if sbLeader && checkNorthdSvcExist(cfg, podNamespace, "ovn-northd") {
		if !checkNorthdSvcValidIP(cfg, podNamespace, "ovn-northd") {
			klog.Warning("no available northd leader, try to release the lock")
			stealLock()
		}
	}
	compactOvnDatabase("nb")
	compactOvnDatabase("sb")
}

func StartOvnLeaderCheck(cfg *Configuration) {
	podName := os.Getenv(EnvPodName)
	podNamespace := os.Getenv(EnvPodNameSpace)
	interval := time.Duration(cfg.ProbeInterval) * time.Second
	for {
		doOvnLeaderCheck(cfg, podName, podNamespace)
		time.Sleep(interval)
	}
}
