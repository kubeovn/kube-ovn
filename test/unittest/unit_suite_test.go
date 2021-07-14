package unittest

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	// tests to run
	_ "github.com/kubeovn/kube-ovn/test/unittest/ipam"
	_ "github.com/kubeovn/kube-ovn/test/unittest/util"
)

func TestE2e(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Kube-OVN unit test Suite")
}
