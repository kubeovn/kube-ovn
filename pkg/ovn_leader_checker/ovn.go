package ovn_leader_checker

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/spf13/pflag"
	"flag"
	"io/ioutil"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"os"
	exec "os/exec"
	"strings"
	"syscall"
	"time"
)

const (
	EnvSSL          = "ENABLE_SSL"
	EnvPodName      = "POD_NAME"
	EnvPodNameSpace = "POD_NAMESPACE"
	OvnNorthdPid    = "/var/run/ovn/ovn-northd.pid"
 
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
		argProbeInterval  = pflag.Int("probeInterval", 15000, "interval of probing leader: ms unit")
	)
	
	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)
	
	// Sync the glog and klog flags.
	flag.CommandLine.VisitAll(func(f1 *flag.Flag) {
		f2 := klogFlags.Lookup(f1.Name)
		if f2 != nil {
			value := f1.Value.String()
			if err := f2.Value.Set(value); err != nil {
				klog.Fatalf("failed to set flag %v", err)
			}
		}
	})
	
	pflag.CommandLine.AddGoFlagSet(klogFlags)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()
	config := &Configuration{
		KubeConfigFile: *argKubeConfigFile,
		ProbeInterval: *argProbeInterval,
	}
	return config, nil
}

// funcs to check apiserver alive
func KubeClientInit(cfg *Configuration) error {
	// init kubeconfig here
	var kubecfg *rest.Config
	var err error

	if cfg.KubeConfigFile == "" {
		klog.Infof("no --kubeconfig, use in-cluster kubernetes config")
		kubecfg, err = rest.InClusterConfig()
	} else {
		kubecfg, err = clientcmd.BuildConfigFromFlags("", cfg.KubeConfigFile)
	}
	if err != nil {
		klog.Errorf("init kubernetes cfg failed %v", err)
		return err
	}

	kubeClient, err := kubernetes.NewForConfig(kubecfg)
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

func checkOvnisAlive() bool {
	ovnProcess := []string{"status_northd", "status_ovnnb", "status_ovnsb"}
	for _, process := range ovnProcess {
		cmd := exec.Command("/usr/share/ovn/scripts/ovn-ctl", process)
		err := getCmdExitCode(cmd)
		if err != 0 {
			klog.Errorf("CheckOvnisAlive: %s is not alive", process)
			return false
		}
		klog.V(5).Infof("CheckOvnisAlive: %s is alive", process)
	}
	return true
}

func checkNbIsLeader() bool {

	var command []string
	if os.Getenv(EnvSSL) == "false" {
		command = []string{
			"query",
			"tcp:127.0.0.1:6641",
			"[\"_Server\",{\"table\":\"Database\",\"where\":[[\"name\",\"==\", \"OVN_Northbound\"]],\"columns\": [\"leader\"],\"op\":\"select\"}]",
		}
	} else {
		command = []string{
			"-p",
			"/var/run/tls/key",
			"-c",
			"/var/run/tls/cert",
			"-C /var/run/tls/cacert",
			"query",
			"ssl:127.0.0.1:6641",
			"[\"_Server\",{\"table\":\"Database\",\"where\":[[\"name\",\"==\", \"OVN_Northbound\"]],\"columns\": [\"leader\"],\"op\":\"select\"}]",
		}
	}

	output, err := exec.Command("ovsdb-client", command...).CombinedOutput()
	if err != nil {
		klog.Errorf("CheckNbIsLeader execute err %v", err)
		return false
	}

	if len(output) == 0 {
		klog.Errorf("CheckNbIsLeader no output %v", err)
		return false
	}

	klog.V(5).Infof("CheckNbIsLeader: output %s", string(output))
	result := strings.TrimSpace(string(output))
	return strings.Contains(result, "true")
}

func checkSbIsLeader() bool {
	var command []string
	if os.Getenv(EnvSSL) == "false" {
		command = []string{
			"query",
			"tcp:127.0.0.1:6642",
			"[\"_Server\",{\"table\":\"Database\",\"where\":[[\"name\",\"==\", \"OVN_Southbound\"]],\"columns\": [\"leader\"],\"op\":\"select\"}]",
		}
	} else {
		command = []string{
			"-p",
			"/var/run/tls/key",
			"-c",
			"/var/run/tls/cert",
			"-C /var/run/tls/cacert",
			"query",
			"ssl:127.0.0.1:6642",
			"[\"_Server\",{\"table\":\"Database\",\"where\":[[\"name\",\"==\", \"OVN_Southbound\"]],\"columns\": [\"leader\"],\"op\":\"select\"}]",
		}
	}

	output, err := exec.Command("ovsdb-client", command...).CombinedOutput()
	if err != nil {
		klog.Errorf("CheckSbIsLeader execute err %v", err)
		return false
	}

	if len(output) == 0 {
		klog.Errorf("CheckSbIsLeader no output %v", err)
		return false
	}

	klog.V(5).Infof("CheckSbIsLeader: output %s ", string(output))
	result := strings.TrimSpace(string(output))
	return strings.Contains(result, "true")
}

func checkNorthdActive() bool {
	var command []string
	file, err := os.OpenFile(OvnNorthdPid, os.O_RDWR, 0600)
	if err != nil {
		klog.Errorf("failed to open %s err =  %v", OvnNorthdPid, err)
		return false
	}
	fileByte, err := ioutil.ReadAll(file)
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
		klog.Errorf("checkNorthdActive execute err %v", err)
		return false
	}

	if len(output) == 0 {
		klog.Errorf("checkNorthdActive no output %v", err)
		return false
	}

	klog.V(5).Infof("checkNorthdActive: output %s  \n", string(output))
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

	if len(output) > 0 {
		klog.V(5).Infof("stealLock: output %s  \n", string(output))
	}

}

func generatePatchPayload(labels map[string]string, op string) []byte {
	patchPayloadTemplate :=
		`[{
        "op": "%s",
        "path": "/metadata/labels",
        "value": %s
          }]`

	raw, _ := json.Marshal(labels)
	return []byte(fmt.Sprintf(patchPayloadTemplate, op, raw))
}

func patchPodLabels(cfg *Configuration, pod *v1.Pod, labels map[string]string) error {
	_, err := cfg.KubeClient.CoreV1().Pods(pod.ObjectMeta.Namespace).Patch(context.Background(), pod.ObjectMeta.Name, types.JSONPatchType, generatePatchPayload(labels, "replace"), metav1.PatchOptions{}, "")
	return err
}


func checkNorthdSvcExsit(cfg *Configuration, nameSpace string, svcName  string) bool{
	_, err := cfg.KubeClient.CoreV1().Services(nameSpace).Get(context.Background(), svcName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("get svc %v namespace %v error %v", svcName, nameSpace, err)
		return false
	}
	return true
}
func checkNorthdSvcValidIP(cfg *Configuration, nameSpace string, epName string) bool {
	eps, err := cfg.KubeClient.CoreV1().Endpoints(nameSpace).Get(context.Background(), epName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("get ep %v namespace %v error %v", epName, nameSpace, err)
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

func tryUpdateLabel(labels map[string]string, key string, isleader bool, modify_labels map[string]string) bool {
	//update pod labels
	if isleader {
		if _, ok := labels[key]; !ok {
			modify_labels[key] = "true"
			return true
		}
	} else {
		if _, ok := labels[key]; ok {
			delete(modify_labels, key)
			return true
		}
	}
	return false
}

func compactDataBase(ctrlSock string) {
	var command = []string{
		"-t",
		ctrlSock,
		"ovsdb-server/compact",
	}
	
	output, err := exec.Command("ovn-appctl", command...).CombinedOutput()
	if len(output) > 0 {
		klog.V(5).Infof("compactDataBase output %v ", string(output))
	}
	if err != nil {
		klog.Errorf("compactDataBase err %v", err)
	}	
}


func doOvnLeaderCheck(cfg *Configuration,  podName string, podNamespace string) {
	pod, err := cfg.KubeClient.CoreV1().Pods(podNamespace).Get(context.Background(), podName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("get pod %v namespace %v error %v", podName, podNamespace, err)
		return
	}

	labels := pod.ObjectMeta.Labels
	if !checkOvnisAlive() {
		klog.Errorf("ovn is not alive")
		return 
	}
	
	//clone  pod labels
	modify_labels := make(map[string]string)
	for k, v := range labels {
		modify_labels[k] = v
	}

	klog.V(5).Infof("OvnLeaderCheck clonedlabels %+v \n", modify_labels)
	var needUpdate bool = false
	isleader := checkNbIsLeader()
	if tryUpdateLabel(labels, "ovn-nb-leader", isleader, modify_labels) {
		needUpdate = true
	}

	//update pod labels
	isleader = checkNorthdActive()
	tryUpdateLabel(labels, "ovn-northd-leader", isleader, modify_labels)

	isleader = checkSbIsLeader()
	if tryUpdateLabel(labels, "ovn-sb-leader", isleader, modify_labels) {
		needUpdate = true
	}
	
	if needUpdate {
		klog.V(5).Infof("OvnLeaderCheck need replace labels %+v \n", modify_labels)
		err = patchPodLabels(cfg, pod, modify_labels)
		if err != nil {
			klog.Errorf("patch label error %v", err)
			return
		}
	}	
	if checkNorthdSvcExsit(cfg, podNamespace, "ovn-northd") {
		if !checkNorthdSvcValidIP(cfg, podNamespace, "ovn-northd") {
			stealLock()
		} 		
	}
	compactDataBase("/var/run/ovn/ovnnb_db.ctl")
	compactDataBase("/var/run/ovn/ovnsb_db.ctl")
}
func StartOvnLeaderCheck(cfg *Configuration) error {
	podName := os.Getenv(EnvPodName)
	podNamespace := os.Getenv(EnvPodNameSpace)
	if podName == "" || podNamespace == "" {
		return fmt.Errorf("env variables POD_NAME and POD_NAMESPACE must be set")
	}
	for {
		doOvnLeaderCheck(cfg, podName , podNamespace);
		time.Sleep(time.Duration(cfg.ProbeInterval) * time.Millisecond)
	}
}

