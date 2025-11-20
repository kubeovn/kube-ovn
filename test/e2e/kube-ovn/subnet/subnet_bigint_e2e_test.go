package subnet

import (
	"context"
	"encoding/json"
	"fmt"
	"net"

	"github.com/onsi/ginkgo/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kotypes "github.com/kubeovn/kube-ovn/pkg/types"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

var _ = framework.Describe("[group:subnet]", func() {
	f := framework.NewDefaultFramework("subnet-bigint")

	var subnetClient *framework.SubnetClient
	var subnetName, cidr, cidrV4, cidrV6 string

	ginkgo.BeforeEach(func() {
		subnetClient = f.SubnetClient()
		subnetName = "subnet-bigint-" + framework.RandomSuffix()
		cidr = framework.RandomCIDR(f.ClusterIPFamily)
		cidrV4, cidrV6 = util.SplitStringIP(cidr)
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
	})

	framework.ConformanceIt("should correctly serialize BigInt fields in K8s API operations", func() {
		ginkgo.By("Creating subnet " + subnetName)
		subnet := framework.MakeSubnet(subnetName, "", cidr, "", "", "", nil, nil, nil)
		subnet = subnetClient.CreateSync(subnet)

		ginkgo.By("Verifying BigInt fields are populated after creation")
		if cidrV4 != "" {
			_, ipnet, _ := net.ParseCIDR(cidrV4)
			expectedV4Available := util.AddressCount(ipnet).Sub(kotypes.NewBigInt(1))
			framework.ExpectEqual(subnet.Status.V4AvailableIPs, expectedV4Available,
				"V4AvailableIPs should match expected value after creation")
			framework.ExpectZero(subnet.Status.V4UsingIPs,
				"V4UsingIPs should be zero initially")
		}

		if cidrV6 != "" {
			_, ipnet, _ := net.ParseCIDR(cidrV6)
			expectedV6Available := util.AddressCount(ipnet).Sub(kotypes.NewBigInt(1))
			framework.ExpectEqual(subnet.Status.V6AvailableIPs, expectedV6Available,
				"V6AvailableIPs should match expected value after creation")
			framework.ExpectZero(subnet.Status.V6UsingIPs,
				"V6UsingIPs should be zero initially")
		}

		ginkgo.By("Fetching subnet raw JSON from K8s API to verify BigInt serialization format")
		rawSubnet, err := f.KubeOVNClientSet.KubeovnV1().Subnets().Get(context.Background(), subnetName, metav1.GetOptions{})
		framework.ExpectNoError(err, "should get subnet from API")

		// Marshal to JSON to verify serialization format
		subnetJSON, err := json.Marshal(rawSubnet)
		framework.ExpectNoError(err, "should marshal subnet to JSON")

		// Parse as generic map to check field types
		var subnetMap map[string]interface{}
		err = json.Unmarshal(subnetJSON, &subnetMap)
		framework.ExpectNoError(err, "should unmarshal subnet JSON to map")

		statusMap, ok := subnetMap["status"].(map[string]interface{})
		framework.ExpectTrue(ok, "subnet should have status field")

		// Verify BigInt fields are serialized as strings (not numbers)
		checkBigIntField := func(fieldName string, shouldExist bool) {
			value, exists := statusMap[fieldName]
			if !shouldExist {
				return
			}
			framework.ExpectTrue(exists, fmt.Sprintf("status.%s should exist", fieldName))

			// CRITICAL: Verify it's a string, not a number
			strValue, isString := value.(string)
			framework.ExpectTrue(isString,
				fmt.Sprintf("status.%s should be JSON string (for CRD type:string), got %T: %v",
					fieldName, value, value))

			framework.Logf("✓ status.%s = %q (correctly serialized as string)", fieldName, strValue)
		}

		checkBigIntField("v4availableIPs", cidrV4 != "")
		checkBigIntField("v4usingIPs", cidrV4 != "")
		checkBigIntField("v6availableIPs", cidrV6 != "")
		checkBigIntField("v6usingIPs", cidrV6 != "")

		ginkgo.By("Testing K8s API patch operation with BigInt fields")
		modifiedSubnet := subnet.DeepCopy()
		modifiedSubnet.Spec.Private = true

		// This patch should succeed without BigInt serialization errors
		patchedSubnet := subnetClient.PatchSync(subnet, modifiedSubnet)
		framework.ExpectTrue(patchedSubnet.Spec.Private, "patch should update spec fields")

		// Verify BigInt status fields remain correct after patch
		if cidrV4 != "" {
			_, ipnet, _ := net.ParseCIDR(cidrV4)
			expectedV4Available := util.AddressCount(ipnet).Sub(kotypes.NewBigInt(1))
			framework.ExpectEqual(patchedSubnet.Status.V4AvailableIPs, expectedV4Available,
				"V4AvailableIPs should remain correct after patch")
		}

		if cidrV6 != "" {
			_, ipnet, _ := net.ParseCIDR(cidrV6)
			expectedV6Available := util.AddressCount(ipnet).Sub(kotypes.NewBigInt(1))
			framework.ExpectEqual(patchedSubnet.Status.V6AvailableIPs, expectedV6Available,
				"V6AvailableIPs should remain correct after patch")
		}

		ginkgo.By("Testing direct status update via K8s API client")
		// Create a status-only patch
		statusPatch := map[string]interface{}{
			"status": map[string]interface{}{
				"v4usingIPs": "999",
			},
		}
		statusPatchBytes, err := json.Marshal(statusPatch)
		framework.ExpectNoError(err)

		// Apply status patch via K8s API
		_, err = f.KubeOVNClientSet.KubeovnV1().Subnets().Patch(
			context.Background(),
			subnetName,
			types.MergePatchType,
			statusPatchBytes,
			metav1.PatchOptions{},
			"status",
		)

		// This should succeed if BigInt JSON serialization is correct
		// If MarshalJSON returns bare number instead of quoted string, this will fail
		framework.ExpectNoError(err, "status patch with BigInt fields should succeed")

		ginkgo.By("Verifying patched status can be read back correctly")
		updatedSubnet, err := f.KubeOVNClientSet.KubeovnV1().Subnets().Get(context.Background(), subnetName, metav1.GetOptions{})
		framework.ExpectNoError(err)

		if cidrV4 != "" {
			framework.ExpectEqual(updatedSubnet.Status.V4UsingIPs.String(), "999",
				"V4UsingIPs should be updated via status patch")
		}

		ginkgo.By("Testing round-trip serialization through K8s API")
		// Create a new subnet with large IP counts
		largeSubnet := framework.MakeSubnet("subnet-bigint-large-"+framework.RandomSuffix(), "", "10.200.0.0/16", "", "", "", nil, nil, nil)
		largeSubnet = subnetClient.CreateSync(largeSubnet)

		// For /16 IPv4 subnet: 2^16 - 2 (network + broadcast) = 65534 IPs
		// After excluding gateway: 65533 available
		expectedLargeCount := kotypes.NewBigInt(65533)
		framework.ExpectEqual(largeSubnet.Status.V4AvailableIPs, expectedLargeCount,
			"large subnet should have correct V4AvailableIPs")

		// Read it back through API
		retrievedLargeSubnet, err := f.KubeOVNClientSet.KubeovnV1().Subnets().Get(context.Background(), largeSubnet.Name, metav1.GetOptions{})
		framework.ExpectNoError(err)

		framework.ExpectEqual(retrievedLargeSubnet.Status.V4AvailableIPs, expectedLargeCount,
			"retrieved subnet should preserve BigInt values")

		// Clean up large subnet
		subnetClient.DeleteSync(largeSubnet.Name)

		framework.Logf("✅ All BigInt K8s API operations succeeded")
	})
})
