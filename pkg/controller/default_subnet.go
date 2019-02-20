package controller

import (
	"bitbucket.org/mathildetech/kube-ovn/pkg/ovs"
	"bitbucket.org/mathildetech/kube-ovn/pkg/util"
	"encoding/json"
	"fmt"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"os"
	"strings"
)

func InitDefaultLogicalSwitch(config *Configuration) error {
	namespace := os.Getenv("KUBE_NAMESPACE")
	if namespace == "" {
		klog.Errorf("env KUBE_NAMESPACE not exists")
		return fmt.Errorf("env KUBE_NAMESPACE not exists")
	}

	ns, err := config.KubeClient.CoreV1().Namespaces().Get(namespace, v1.GetOptions{})
	if err != nil {
		return err
	}

	patchPayloadTemplate :=
		`[{
        "op": "%s",
        "path": "/metadata/annotations",
        "value": %s
    }]`
	payload := map[string]string{
		"ovn.kubernetes.io/logical_switch": config.DefaultLogicalSwitch,
		"ovn.kubernetes.io/cidr":           config.DefaultCIDR,
		"ovn.kubernetes.io/gateway":        config.DefaultGateway,
		"ovn.kubernetes.io/exclude_ips":    config.DefaultExcludeIps,
	}
	raw, _ := json.Marshal(payload)
	op := "replace"
	if len(ns.GetAnnotations()) == 0 {
		op = "add"
	}
	patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
	_, err = config.KubeClient.CoreV1().Namespaces().Patch(namespace, types.JSONPatchType, []byte(patchPayload))
	if err != nil {
		klog.Errorf("patch namespace %s failed %v", namespace, err)
	}
	return err
}

func InitNodeSwitch(config *Configuration) error {
	client := ovs.NewClient(config.OvnNbHost, config.OvnNbPort, config.ClusterRouter)
	err := client.CreateLogicalSwitch(config.NodeSwitch, config.NodeSwitchCIDR, config.NodeSwitchGateway, config.NodeSwitchGateway)
	if err != nil {
		return err
	}
	mac := util.GenerateMac()
	mask := strings.Split(config.NodeSwitchCIDR, "/")[1]
	return client.CreateRouterPort(config.NodeSwitch, config.ClusterRouter, config.NodeSwitchGateway+"/"+mask, mac)
}

func InitClusterRouter(config *Configuration) error {
	client := ovs.NewClient(config.OvnNbHost, config.OvnNbPort, config.ClusterRouter)
	return client.CreateLogicalRouter(config.ClusterRouter)
}
