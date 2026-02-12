package gc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.Describe("[group:gc]", func() {
	f := framework.NewDefaultFramework("gc")

	var (
		switchLBRuleClient *framework.SwitchLBRuleClient
		podClient          *framework.PodClient
		subnetClient       *framework.SubnetClient
		vpcClient          *framework.VpcClient

		namespaceName, suffix              string
		vpcName, subnetName, clientPodName string
		slrName                            string
		overlaySubnetCidr                  string
		frontPort, backendPort             int32
	)

	ginkgo.BeforeEach(func() {
		switchLBRuleClient = f.SwitchLBRuleClient()
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		vpcClient = f.VpcClient()
		suffix = framework.RandomSuffix()
		namespaceName = f.Namespace.Name
		slrName = "slr-" + suffix
		clientPodName = "client-" + suffix
		subnetName = "subnet-" + suffix
		vpcName = "vpc-" + suffix
		frontPort = 8888
		backendPort = 80
		overlaySubnetCidr = framework.RandomCIDR(f.ClusterIPFamily)

		ginkgo.By("Creating vpc " + vpcName)
		vpc := framework.MakeVpc(vpcName, "", false, false, []string{namespaceName})
		vpcClient.CreateSync(vpc)

		ginkgo.By("Creating subnet " + subnetName)
		subnet := framework.MakeSubnet(subnetName, "", overlaySubnetCidr, "", vpcName, "", nil, nil, nil)
		subnetClient.CreateSync(subnet)
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting switch-lb-rule " + slrName)
		switchLBRuleClient.DeleteSync(slrName)
		ginkgo.By("Deleting client pod " + clientPodName)
		podClient.DeleteSync(clientPodName)
		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
		ginkgo.By("Deleting vpc " + vpcName)
		vpcClient.DeleteSync(vpcName)
	})

	ginkgo.It("should gc stale ip_port_mappings in OVN LoadBalancer", func() {
		ginkgo.By("1. Creating a SwitchLBRule to populate LoadBalancer")
		labels := map[string]string{"app": "gc-test"}
		annotations := map[string]string{util.LogicalSwitchAnnotation: subnetName}

		ginkgo.By("Creating backend pod")
		backendPodName := "backend-" + suffix
		pod := framework.MakePod(namespaceName, backendPodName, labels, annotations, framework.AgnhostImage, nil, nil)
		podClient.CreateSync(pod)
		pod = podClient.GetPod(backendPodName)
		backendIP := pod.Status.PodIP

		ginkgo.By("Creating SwitchLBRule " + slrName)
		slrPorts := []kubeovnv1.SwitchLBRulePort{{
			Name:       "http",
			Port:       frontPort,
			TargetPort: backendPort,
			Protocol:   "TCP",
		}}
		slr := framework.MakeSwitchLBRule(slrName, namespaceName, "1.1.1.1", corev1.ServiceAffinityNone, nil, []string{"app:gc-test"}, nil, slrPorts)
		switchLBRuleClient.CreateSync(slr, func(_ *kubeovnv1.SwitchLBRule) (bool, error) {
			return true, nil
		}, "switch-lb-rule is created")

		ginkgo.By("2. Identifying the OVN LoadBalancer")
		// LBs created for VPCs are named like "vpc-suffix_tcp" or similar in status
		vpc := vpcClient.Get(vpcName)
		lbName := vpc.Status.TCPLoadBalancer
		framework.ExpectNotEmpty(lbName)

		ginkgo.By("Verifying active ip_port_mapping exists for backend " + backendIP)
		cmd := []string{"ovn-nbctl", "get", "load_balancer", lbName, "ip_port_mappings"}
		stdout, _, err := framework.NBExec(cmd...)
		framework.ExpectNil(err)
		framework.ExpectContainSubstring(string(stdout), backendIP)

		ginkgo.By("3. Manually injecting a stale ip_port_mapping entry")
		staleIP := "1.2.3.4"
		staleMapping := "stale-node"

		// Get existing mappings to ensure we don't overwrite them
		cmd = []string{"ovn-nbctl", "get", "load_balancer", lbName, "ip_port_mappings"}
		stdout, _, err = framework.NBExec(cmd...)
		framework.ExpectNil(err)
		existingMappings := strings.TrimSpace(string(stdout))

		// Inject the stale mapping while preserving existing ones
		// Using 'add' instead of 'set' for map columns in ovn-nbctl is safer as it adds to the map
		setCmd := []string{"ovn-nbctl", "add", "load_balancer", lbName, "ip_port_mappings", fmt.Sprintf("{\"%s\"=\"%s\"}", staleIP, staleMapping)}
		_, _, err = framework.NBExec(setCmd...)
		framework.ExpectNil(err)

		ginkgo.By("Verifying stale entry was injected and existing ones preserved")
		stdout, _, err = framework.NBExec(cmd...)
		framework.ExpectNil(err)
		stdoutStr := string(stdout)
		framework.ExpectContainSubstring(stdoutStr, staleIP)
		framework.ExpectContainSubstring(stdoutStr, backendIP)
		if existingMappings != "{}" && existingMappings != "" {
			// Basic check that we didn't lose what was there before
			framework.ExpectContainSubstring(stdoutStr, strings.Trim(existingMappings, "{}"))
		}

		ginkgo.By("4. Waiting for GC to clean up the stale entry")
		// The default GC interval might be long, but in E2E tests we expect the controller to be running.
		// If GC interval is e.g. 60s, we might need to wait.
		framework.WaitUntil(5*time.Second, 2*time.Minute, func(_ context.Context) (bool, error) {
			stdout, _, err = framework.NBExec(cmd...)
			if err != nil {
				return false, err
			}
			return !strings.Contains(string(stdout), staleIP), nil
		}, "stale ip_port_mapping is removed by GC")

		ginkgo.By("Verifying active entry still exists")
		stdout, _, err = framework.NBExec(cmd...)
		framework.ExpectNil(err)
		framework.ExpectContainSubstring(string(stdout), backendIP)
	})
})
