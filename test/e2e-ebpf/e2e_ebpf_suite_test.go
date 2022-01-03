package e2e_ebpf

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	kubeadmscheme "k8s.io/kubernetes/cmd/kubeadm/app/apis/kubeadm/scheme"
	kubeadmapi "k8s.io/kubernetes/cmd/kubeadm/app/apis/kubeadm/v1beta3"
	kubeadmconstants "k8s.io/kubernetes/cmd/kubeadm/app/constants"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	kubeovn "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"

	_ "github.com/kubeovn/kube-ovn/test/e2e/ip"
	_ "github.com/kubeovn/kube-ovn/test/e2e/service"
	_ "github.com/kubeovn/kube-ovn/test/e2e/subnet"
)

func TestE2eEbpf(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Kube-OVN E2E ebpf Suite")
}

var _ = SynchronizedAfterSuite(func() {}, func() {
	f := framework.NewFramework("init", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))
	nss, err := f.KubeClientSet.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{LabelSelector: "e2e=true"})
	if err != nil {
		Fail(err.Error())
	}
	if nss != nil {
		for _, ns := range nss.Items {
			err := f.KubeClientSet.CoreV1().Namespaces().Delete(context.Background(), ns.Name, metav1.DeleteOptions{})
			if err != nil {
				Fail(err.Error())
			}
		}
	}

	err = f.OvnClientSet.KubeovnV1().Subnets().DeleteCollection(context.Background(), metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: "e2e=true"})
	if err != nil {
		Fail(err.Error())
	}
})

func setExternalRoute(af int, dst, gw string) {
	if dst == "" || gw == "" {
		return
	}

	cmd := exec.Command("docker", "exec", "kube-ovn-e2e", "ip", fmt.Sprintf("-%d", af), "route", "replace", dst, "via", gw)
	output, err := cmd.CombinedOutput()
	if err != nil {
		Fail((fmt.Sprintf(`failed to execute command "%s": %v, output: %s`, cmd.String(), err, strings.TrimSpace(string(output)))))
	}
}

var _ = SynchronizedBeforeSuite(func() []byte {
	subnetName := "static-ip"
	namespace := "static-ip"
	f := framework.NewFramework("init", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))

	_, err := f.KubeClientSet.CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: map[string]string{"e2e": "true"}}}, metav1.CreateOptions{})
	if err != nil {
		Fail(err.Error())
	}

	s := kubeovn.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name:   subnetName,
			Labels: map[string]string{"e2e": "true"},
		},
		Spec: kubeovn.SubnetSpec{
			CIDRBlock:  "12.10.0.0/16",
			Namespaces: []string{namespace},
		},
	}
	_, err = f.OvnClientSet.KubeovnV1().Subnets().Create(context.Background(), &s, metav1.CreateOptions{})
	if err != nil {
		Fail(err.Error())
	}
	err = f.WaitSubnetReady(subnetName)
	if err != nil {
		Fail(err.Error())
	}

	nodes, err := f.KubeClientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		Fail(err.Error())
	}
	kubeadmConfigMap, err := f.KubeClientSet.CoreV1().ConfigMaps(metav1.NamespaceSystem).Get(context.Background(), kubeadmconstants.KubeadmConfigConfigMap, metav1.GetOptions{})
	if err != nil {
		Fail(err.Error())
	}

	clusterConfig := &kubeadmapi.ClusterConfiguration{}
	if err = k8sruntime.DecodeInto(kubeadmscheme.Codecs.UniversalDecoder(), []byte(kubeadmConfigMap.Data[kubeadmconstants.ClusterConfigurationConfigMapKey]), clusterConfig); err != nil {
		Fail(fmt.Sprintf("failed to decode kubeadm cluster configuration from bytes: %v", err))
	}

	nodeIPv4, nodeIPv6 := util.GetNodeInternalIP(nodes.Items[0])
	podSubnetV4, podSubnetV6 := util.SplitStringIP(clusterConfig.Networking.PodSubnet)
	svcSubnetV4, svcSubnetV6 := util.SplitStringIP(clusterConfig.Networking.ServiceSubnet)
	setExternalRoute(4, podSubnetV4, nodeIPv4)
	setExternalRoute(4, svcSubnetV4, nodeIPv4)
	setExternalRoute(6, podSubnetV6, nodeIPv6)
	setExternalRoute(6, svcSubnetV6, nodeIPv6)

	return nil
}, func(data []byte) {})
