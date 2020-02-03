package e2e_test

import (
	"fmt"
	kubeovn "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	// tests to run
	_ "github.com/alauda/kube-ovn/test/e2e/ip"
	_ "github.com/alauda/kube-ovn/test/e2e/subnet"
)

func TestE2e(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Kube-OVN E2E Suite")
}

var _ = SynchronizedAfterSuite(func() {}, func() {
	f := framework.NewFramework("init", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))
	nss, err := f.KubeClientSet.CoreV1().Namespaces().List(metav1.ListOptions{LabelSelector: "e2e=true"})
	if err != nil {
		Fail(err.Error())
	}
	if nss != nil {
		for _, ns := range nss.Items {
			err := f.KubeClientSet.CoreV1().Namespaces().Delete(ns.Name, &metav1.DeleteOptions{})
			if err != nil {
				Fail(err.Error())
			}
		}
	}

	err = f.OvnClientSet.KubeovnV1().Subnets().DeleteCollection(&metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: "e2e=true"})
	if err != nil {
		Fail(err.Error())
	}
})

var _ = SynchronizedBeforeSuite(func() []byte {
	subnetName := "static-ip"
	namespace := "static-ip"
	f := framework.NewFramework("init", fmt.Sprintf("%s/.kube/config", os.Getenv("HOME")))

	_, err := f.KubeClientSet.CoreV1().Namespaces().Create(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: map[string]string{"e2e": "true"}}})
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
	_, err = f.OvnClientSet.KubeovnV1().Subnets().Create(&s)
	if err != nil {
		Fail(err.Error())
	}
	err = f.WaitSubnetReady(subnetName)
	if err != nil {
		Fail(err.Error())
	}
	return nil
}, func(data []byte) {})
