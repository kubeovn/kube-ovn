package pod

import (
	"context"
	"fmt"
	"math/big"
	"net"
	"strconv"
	"time"

	e2enode "k8s.io/kubernetes/test/e2e/framework/node"
	e2epodoutput "k8s.io/kubernetes/test/e2e/framework/pod/output"

	"github.com/onsi/ginkgo/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.SerialDescribe("[group:pod]", func() {
	f := framework.NewDefaultFramework("static-ip-recreation")

	var podClient *framework.PodClient
	var subnetClient *framework.SubnetClient
	var namespaceName, subnetName, backendName, clientName string

	ginkgo.BeforeEach(func() {
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		namespaceName = f.Namespace.Name
		subnetName = "subnet-" + framework.RandomSuffix()
		backendName = "backend-" + framework.RandomSuffix()
		clientName = "client-" + framework.RandomSuffix()
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting backend pod " + backendName)
		podClient.DeleteGracefully(backendName)
		ginkgo.By("Deleting client pod " + clientName)
		podClient.DeleteGracefully(clientName)
		podClient.WaitForNotFound(backendName)
		podClient.WaitForNotFound(clientName)
		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
	})

	framework.ConformanceIt("should immediately reach a recreated static IPv6 pod after its MAC changes", func() {
		f.SkipVersionPriorTo(1, 15, "Unsolicited IPv6 neighbor advertisements were introduced in v1.15")
		if !f.HasIPv6() {
			ginkgo.Skip("This case requires IPv6")
		}

		nodes, err := e2enode.GetReadySchedulableNodes(context.Background(), f.ClientSet)
		framework.ExpectNoError(err)
		if len(nodes.Items) < 2 {
			ginkgo.Skip("This case requires at least two ready schedulable nodes")
		}

		cidr := framework.RandomCIDR(framework.IPv6)
		firstIP, err := util.FirstIP(cidr)
		framework.ExpectNoError(err)
		fixedIP := util.BigInt2Ip(new(big.Int).Add(util.IP2BigInt(firstIP), big.NewInt(10)))

		ginkgo.By("Creating IPv6 subnet " + subnetName)
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		_ = subnetClient.CreateSync(subnet)

		ginkgo.By("Creating client pod " + clientName)
		clientAnnotations := map[string]string{util.LogicalSwitchAnnotation: subnetName}
		client := framework.MakePod(namespaceName, clientName, nil, clientAnnotations, framework.AgnhostImage, nil, nil)
		client.Spec.NodeName = nodes.Items[0].Name
		_ = podClient.CreateSync(client)

		const (
			oldMAC = "02:00:00:00:00:11"
			newMAC = "02:00:00:00:00:12"
			port   = 8080
		)
		makeBackend := func(mac string) {
			ginkgo.GinkgoHelper()
			annotations := map[string]string{
				util.LogicalSwitchAnnotation: subnetName,
				util.IPAddressAnnotation:     fixedIP,
				util.MacAddressAnnotation:    mac,
			}
			args := []string{"netexec", "--http-port", strconv.Itoa(port)}
			backend := framework.MakePod(namespaceName, backendName, nil, annotations, framework.AgnhostImage, nil, args)
			backend.Spec.NodeName = nodes.Items[1].Name
			backend = podClient.CreateSync(backend)
			framework.ExpectHaveKeyWithValue(backend.Annotations, util.IPAddressAnnotation, fixedIP)
			framework.ExpectHaveKeyWithValue(backend.Annotations, util.MacAddressAnnotation, mac)
		}

		url := "http://" + net.JoinHostPort(fixedIP, strconv.Itoa(port)) + "/hostname"
		curlCommand := fmt.Sprintf("curl -q -s --connect-timeout 1 --max-time 1 %s", url)

		ginkgo.By("Creating the first backend and priming the client's IPv6 neighbor cache")
		makeBackend(oldMAC)
		framework.WaitUntil(time.Second, 30*time.Second, func(_ context.Context) (bool, error) {
			_, err := e2epodoutput.RunHostCmd(namespaceName, clientName, curlCommand)
			return err == nil, nil
		}, "the client can reach the first backend")

		ginkgo.By("Recreating the backend with the same IPv6 address and a different MAC address")
		podClient.DeleteSync(backendName)
		makeBackend(newMAC)

		ginkgo.By("Checking the first direct request after the recreated backend becomes ready")
		_, err = e2epodoutput.RunHostCmd(namespaceName, clientName, curlCommand)
		framework.ExpectNoError(err, "the recreated static IPv6 pod should be reachable without waiting for neighbor discovery timeout")
	})
})
