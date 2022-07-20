package e2e_multus_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	kubeovn "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"

	// tests to run
	_ "github.com/kubeovn/kube-ovn/test/e2e-multus/lbsvc"
)

func TestE2eMultus(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Kube-OVN multus E2E Suite")
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

var _ = SynchronizedBeforeSuite(func() []byte {
	subnetName := "attach-subnet"
	namespace := "lb-test"
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
			CIDRBlock:  "172.18.0.0/16",
			Protocol:   util.CheckProtocol("172.18.0.0/16"),
			Provider:   "lb-svc-attachment.kube-system",
			ExcludeIps: []string{"172.18.0.0..172.18.0.10"},
		},
	}
	_, err = f.OvnClientSet.KubeovnV1().Subnets().Create(context.Background(), &s, metav1.CreateOptions{})
	if err != nil {
		Fail(err.Error())
	}

	return nil
}, func(data []byte) {})
