package dra

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/test/e2e"
	k8sframework "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"
	e2enode "k8s.io/kubernetes/test/e2e/framework/node"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

// driverName is the default --dra-nic-driver-name of kube-ovn-controller.
// Values the specs share.
const (
	driverName        = "nic.kubeovn.io"
	controllerName    = "kube-ovn-controller"
	enableDRANicFlag  = "--enable-dra-nic=true"
	frameworkBaseName = "dra"
	subnetPrefix      = "dra-subnet-"
	draNicPrefix      = "dra-nic-"
	claimPrefix       = "claim-"
	podPrefix         = "pod-"
	devicePrefix      = "subnet-"
	attrSubnetName    = "subnetName"
	requestName       = "nic0"
	podClaimName      = "nic"
	allocatedValue    = "true"
	missingSubnet     = "does-not-exist"
	firstRequest      = "nic0"
	secondRequest     = "nic1"
	missingOption     = "missing"
	availableOption   = "available"
	firstIface        = "net7"
	secondIface       = "net8"
	secondDeviceTag   = "-b"
	nicConfigFormat   = `{"apiVersion":"nic.resource.kube-ovn.io/v1alpha1","kind":"NicConfig","interfaceName":%q}`
	subnetSelector    = "device.attributes[%q].subnetName == %q"
)

func init() {
	klog.SetOutput(ginkgo.GinkgoWriter)

	config.CopyFlags(config.Flags, flag.CommandLine)
	k8sframework.RegisterCommonFlags(flag.CommandLine)
	k8sframework.RegisterClusterFlags(flag.CommandLine)
}

func TestE2E(t *testing.T) {
	k8sframework.AfterReadingAllFlags(&k8sframework.TestContext)
	e2e.RunE2ETests(t)
}

// draNicEnabled reports whether kube-ovn-controller runs with --enable-dra-nic=true.
func draNicEnabled(f *framework.Framework) bool {
	ginkgo.GinkgoHelper()

	deploy, err := f.ClientSet.AppsV1().Deployments(framework.KubeOvnNamespace).Get(context.Background(), controllerName, metav1.GetOptions{})
	framework.ExpectNoError(err)
	for _, container := range deploy.Spec.Template.Spec.Containers {
		if slices.Contains(container.Args, enableDRANicFlag) {
			return true
		}
	}
	return false
}

// publishDevices creates the device class and a static ResourceSlice for the driver on the node,
// with one device per name, each naming the subnet.
func publishDevices(ctx context.Context, f *framework.Framework, deviceClassName, sliceName, nodeName, subnetName string, deviceNames ...string) {
	ginkgo.GinkgoHelper()
	resource := f.ClientSet.ResourceV1()
	_, err := resource.DeviceClasses().Create(ctx, &resourcev1.DeviceClass{
		Name: deviceClassName,
		Spec: resourcev1.DeviceClassSpec{Selectors: []resourcev1.DeviceSelector{{
			CEL: &resourcev1.CELDeviceSelector{Expression: fmt.Sprintf("device.driver == %q", driverName)},
		}}},
	}, metav1.CreateOptions{})
	framework.ExpectNoError(err)
	devices := make([]resourcev1.Device, 0, len(deviceNames))
	for _, name := range deviceNames {
		devices = append(devices, resourcev1.Device{
			Name: name,
			Attributes: map[resourcev1.QualifiedName]resourcev1.DeviceAttribute{
				resourcev1.QualifiedName(driverName + "/" + attrSubnetName): {StringValue: new(subnetName)},
			},
		})
	}
	_, err = resource.ResourceSlices().Create(ctx, &resourcev1.ResourceSlice{
		Name: sliceName,
		Spec: resourcev1.ResourceSliceSpec{
			Driver:   driverName,
			NodeName: new(nodeName),
			Pool:     resourcev1.ResourcePool{Name: sliceName, ResourceSliceCount: 1},
			Devices:  devices,
		},
	}, metav1.CreateOptions{})
	framework.ExpectNoError(err)
}

// createClaimPod creates a pod that uses the claim.
func createClaimPod(podClient *framework.PodClient, namespaceName, podName, claimName string) {
	ginkgo.GinkgoHelper()
	pod := framework.MakePod(namespaceName, podName, nil, nil, "", nil, nil)
	pod.Spec.ResourceClaims = []corev1.PodResourceClaim{{Name: podClaimName, ResourceClaimName: new(claimName)}}
	pod.Spec.Containers[0].Resources.Claims = []corev1.ResourceClaim{{Name: podClaimName}}
	_ = podClient.Create(pod)
}

// waitAllocated waits until kube-ovn-controller allocated the nic with the provider key.
func waitAllocated(ctx context.Context, f *framework.Framework, namespaceName, podName, providerKey string) {
	ginkgo.GinkgoHelper()
	allocatedKey := fmt.Sprintf(util.AllocatedAnnotationTemplate, providerKey)
	gomega.Eventually(func(g gomega.Gomega) {
		pod, err := f.ClientSet.CoreV1().Pods(namespaceName).Get(ctx, podName, metav1.GetOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(pod.Annotations).To(gomega.HaveKeyWithValue(allocatedKey, allocatedValue))
	}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(gomega.Succeed())
}

// expectRecorded checks that kube-ovn-controller recorded the subnet and provider of the claim's
// devices on the claim.
func expectRecorded(ctx context.Context, f *framework.Framework, namespaceName, claimName, pool, subnetName, provider string, deviceNames ...string) {
	ginkgo.GinkgoHelper()
	claim, err := f.ClientSet.ResourceV1().ResourceClaims(namespaceName).Get(ctx, claimName, metav1.GetOptions{})
	framework.ExpectNoError(err)
	records := map[string]struct {
		Subnet   string `json:"subnet"`
		Provider string `json:"provider"`
	}{}
	framework.ExpectNoError(json.Unmarshal([]byte(claim.Annotations[util.DRAProvidersAnnotation]), &records))
	framework.ExpectHaveLen(records, len(deviceNames))
	for _, name := range deviceNames {
		record, ok := records[pool+"/"+name]
		framework.ExpectTrue(ok, "no record for device %s in %v", name, records)
		framework.ExpectEqual(record.Subnet, subnetName)
		framework.ExpectEqual(record.Provider, provider)
	}
}

// These tests cover only the kube-ovn-controller side of DRA nics, not the data plane. They publish
// a static ResourceSlice for the kube-ovn DRA NIC driver and let the scheduler allocate a claim from
// it. No kubelet plugin serves the driver, so the claim is never prepared and the pod never starts:
// no nic is plugged into its netns. Everything kube-ovn-controller does for a DRA nic happens before
// that, so the tests check its annotations, ip CR and release with a real scheduler, API and RBAC.
var _ = framework.SerialDescribe("[group:dra]", func() {
	f := framework.NewDefaultFramework(frameworkBaseName)

	var podClient *framework.PodClient
	var subnetClient *framework.SubnetClient
	var ipClient *framework.IPClient
	var namespaceName, suffix, subnetName, provider, deviceClassName, sliceName, claimName, podName string
	var skipped bool
	ginkgo.BeforeEach(func() {
		podClient = f.PodClient()
		subnetClient = f.SubnetClient()
		ipClient = f.IPClient()
		namespaceName = f.Namespace.Name
		suffix = framework.RandomSuffix()
		subnetName = subnetPrefix + suffix
		provider = fmt.Sprintf("%s.%s.%s", subnetName, namespaceName, util.OvnProvider)
		deviceClassName = draNicPrefix + suffix
		sliceName = draNicPrefix + suffix
		claimName = claimPrefix + suffix
		podName = podPrefix + suffix

		// Cleanup only applies once the checks below passed and the spec created resources.
		skipped = true
		f.SkipVersionPriorTo(1, 17, "DRA NIC support was introduced in v1.17")
		if !draNicEnabled(f) {
			ginkgo.Skip("kube-ovn-controller does not run with --enable-dra-nic=true")
		}
		skipped = false
	})
	ginkgo.AfterEach(func() {
		if skipped {
			return
		}
		ctx := context.Background()
		resource := f.ClientSet.ResourceV1()
		ginkgo.By("Deleting pod " + podName)
		err := f.ClientSet.CoreV1().Pods(namespaceName).Delete(ctx, podName, metav1.DeleteOptions{GracePeriodSeconds: new(int64(0))})
		if err != nil && !apierrors.IsNotFound(err) {
			framework.ExpectNoError(err)
		}
		podClient.WaitForNotFound(podName)
		ginkgo.By("Deleting resource claim, slice and device class")
		for _, err := range []error{
			resource.ResourceClaims(namespaceName).Delete(ctx, claimName, metav1.DeleteOptions{}),
			resource.ResourceSlices().Delete(ctx, sliceName, metav1.DeleteOptions{}),
			resource.DeviceClasses().Delete(ctx, deviceClassName, metav1.DeleteOptions{}),
		} {
			if err != nil && !apierrors.IsNotFound(err) {
				framework.ExpectNoError(err)
			}
		}
		ginkgo.By("Deleting subnet " + subnetName)
		subnetClient.DeleteSync(subnetName)
	})

	ginkgo.It("should allocate a DRA nic from the claimed subnet and release it with the pod", func() {
		ctx := context.Background()
		resource := f.ClientSet.ResourceV1()

		ginkgo.By("Creating subnet " + subnetName + " with provider " + provider)
		cidr := framework.RandomCIDR(f.ClusterIPFamily)
		subnet := subnetClient.CreateSync(framework.MakeSubnet(subnetName, "", cidr, "", "", provider, nil, nil, nil))

		nodes, err := e2enode.GetReadySchedulableNodes(ctx, f.ClientSet)
		framework.ExpectNoError(err)
		nodeName := nodes.Items[0].Name

		deviceName := devicePrefix + subnetName
		ginkgo.By("Publishing device " + deviceName + " on node " + nodeName)
		publishDevices(ctx, f, deviceClassName, sliceName, nodeName, subnetName, deviceName)

		ginkgo.By("Creating resource claim " + claimName + " and pod " + podName)
		_, err = resource.ResourceClaims(namespaceName).Create(ctx, &resourcev1.ResourceClaim{
			Name: claimName,
			Spec: resourcev1.ResourceClaimSpec{Devices: resourcev1.DeviceClaim{Requests: []resourcev1.DeviceRequest{{
				Name: requestName,
				Exactly: &resourcev1.ExactDeviceRequest{
					DeviceClassName: deviceClassName,
					Selectors: []resourcev1.DeviceSelector{{CEL: &resourcev1.CELDeviceSelector{
						Expression: fmt.Sprintf(subnetSelector, driverName, subnetName),
					}}},
				},
			}}}},
		}, metav1.CreateOptions{})
		framework.ExpectNoError(err)

		createClaimPod(podClient, namespaceName, podName, claimName)

		ginkgo.By("Waiting for kube-ovn-controller to allocate the DRA nic")
		waitAllocated(ctx, f, namespaceName, podName, provider)

		ginkgo.By("Checking the subnet and provider recorded on the claim")
		expectRecorded(ctx, f, namespaceName, claimName, sliceName, subnetName, provider, deviceName)

		pod := podClient.GetPod(podName)
		framework.ExpectHaveKeyWithValue(pod.Annotations, fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider), subnetName)
		framework.ExpectHaveKeyWithValue(pod.Annotations, fmt.Sprintf(util.CidrAnnotationTemplate, provider), subnet.Spec.CIDRBlock)
		ipStr := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, provider)]
		framework.ExpectTrue(util.CIDRContainIP(subnet.Spec.CIDRBlock, ipStr), "%s should be in %s", ipStr, subnet.Spec.CIDRBlock)

		ginkgo.By("Checking the ip CR of the DRA nic")
		ipName := ovs.PodNameToPortName(podName, namespaceName, provider)
		ip := ipClient.Get(ipName)
		framework.ExpectEqual(ip.Spec.Subnet, subnetName)
		framework.ExpectEqual(ip.Spec.NodeName, nodeName)

		ginkgo.By("Deleting pod " + podName + " and checking the DRA nic is released")
		framework.ExpectNoError(f.ClientSet.CoreV1().Pods(namespaceName).Delete(ctx, podName, metav1.DeleteOptions{GracePeriodSeconds: new(int64(0))}))
		podClient.WaitForNotFound(podName)
		framework.ExpectNoError(ipClient.WaitToDisappear(ipName, 2*time.Second, 2*time.Minute))
		gomega.Eventually(func(g gomega.Gomega) {
			subnet := subnetClient.Get(subnetName)
			g.Expect(subnet.Status.V4UsingIPs.Add(subnet.Status.V6UsingIPs).EqualInt64(0)).To(gomega.BeTrue())
		}).WithTimeout(time.Minute).WithPolling(2 * time.Second).Should(gomega.Succeed())
	})

	ginkgo.It("should allocate DRA nics from firstAvailable subrequests and name them after their parent request's config", func() {
		ctx := context.Background()

		ginkgo.By("Creating subnet " + subnetName + " with provider " + provider)
		cidr := framework.RandomCIDR(f.ClusterIPFamily)
		subnetClient.CreateSync(framework.MakeSubnet(subnetName, "", cidr, "", "", provider, nil, nil, nil))

		nodes, err := e2enode.GetReadySchedulableNodes(ctx, f.ClientSet)
		framework.ExpectNoError(err)
		firstDevice, secondDevice := devicePrefix+subnetName, devicePrefix+subnetName+secondDeviceTag
		ginkgo.By("Publishing devices " + firstDevice + " and " + secondDevice + " of subnet " + subnetName)
		publishDevices(ctx, f, deviceClassName, sliceName, nodes.Items[0].Name, subnetName, firstDevice, secondDevice)

		// Each request prefers a subnet that does not exist and falls back to the claimed subnet,
		// which has a device for each; the interface name comes from a config of the parent request.
		request := func(name string) resourcev1.DeviceRequest {
			return resourcev1.DeviceRequest{Name: name, FirstAvailable: []resourcev1.DeviceSubRequest{
				{Name: missingOption, DeviceClassName: deviceClassName, Selectors: []resourcev1.DeviceSelector{{CEL: &resourcev1.CELDeviceSelector{
					Expression: fmt.Sprintf(subnetSelector, driverName, missingSubnet),
				}}}},
				{Name: availableOption, DeviceClassName: deviceClassName, Selectors: []resourcev1.DeviceSelector{{CEL: &resourcev1.CELDeviceSelector{
					Expression: fmt.Sprintf(subnetSelector, driverName, subnetName),
				}}}},
			}}
		}
		config := func(request, iface string) resourcev1.DeviceClaimConfiguration {
			return resourcev1.DeviceClaimConfiguration{
				Requests: []string{request},
				Opaque:   &resourcev1.OpaqueDeviceConfiguration{Driver: driverName, Parameters: runtime.RawExtension{Raw: fmt.Appendf(nil, nicConfigFormat, iface)}},
			}
		}
		ginkgo.By("Creating resource claim " + claimName + " with firstAvailable requests and pod " + podName)
		_, err = f.ClientSet.ResourceV1().ResourceClaims(namespaceName).Create(ctx, &resourcev1.ResourceClaim{
			Name: claimName,
			Spec: resourcev1.ResourceClaimSpec{Devices: resourcev1.DeviceClaim{
				Requests: []resourcev1.DeviceRequest{request(firstRequest), request(secondRequest)},
				Config:   []resourcev1.DeviceClaimConfiguration{config(firstRequest, firstIface), config(secondRequest, secondIface)},
			}},
		}, metav1.CreateOptions{})
		framework.ExpectNoError(err)
		createClaimPod(podClient, namespaceName, podName, claimName)

		ginkgo.By("Waiting for both nics, keyed by the interface names of their parent requests")
		firstKey, secondKey := provider+"."+firstIface, provider+"."+secondIface
		waitAllocated(ctx, f, namespaceName, podName, firstKey)
		waitAllocated(ctx, f, namespaceName, podName, secondKey)

		claim, err := f.ClientSet.ResourceV1().ResourceClaims(namespaceName).Get(ctx, claimName, metav1.GetOptions{})
		framework.ExpectNoError(err)
		for _, result := range claim.Status.Allocation.Devices.Results {
			framework.ExpectTrue(strings.HasSuffix(result.Request, "/"+availableOption), "request %s was not allocated by its fallback subrequest", result.Request)
		}
		expectRecorded(ctx, f, namespaceName, claimName, sliceName, subnetName, provider, firstDevice, secondDevice)
		ginkgo.By("Checking the ip CRs of both nics")
		for _, key := range []string{firstKey, secondKey} {
			framework.ExpectEqual(ipClient.Get(ovs.PodNameToPortName(podName, namespaceName, key)).Spec.Subnet, subnetName)
		}
	})
})
