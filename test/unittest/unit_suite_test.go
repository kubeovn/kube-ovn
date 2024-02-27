package unittest

import (
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	// tests to run
	_ "github.com/kubeovn/kube-ovn/test/unittest/ipam"
	_ "github.com/kubeovn/kube-ovn/test/unittest/util"
)

func TestE2e(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Kube-OVN unit test Suite")
}
