package service

import (
	"context"

	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/kubeovn/kube-ovn/test/e2e/framework"
)

// These specs cover the admission rules of a Service served by a vpc-nat-gw through nft share
// DNAT. They run in the webhook suite because that is the job which installs the webhook; the
// gateway itself is not needed, since every rule below is decided from the Service objects alone.
var _ = framework.Describe("[group:webhook-service]", func() {
	f := framework.NewDefaultFramework("webhook-service")

	var serviceClient *framework.ServiceClient
	var eipName, gatewayName string

	ginkgo.BeforeEach(func() {
		serviceClient = f.ServiceClient()
		suffix := framework.RandomSuffix()
		eipName = "webhook-eip-" + suffix
		gatewayName = "webhook-gw-" + suffix
	})

	gwService := func(name string, port int32, protocol corev1.Protocol) *corev1.Service {
		return framework.MakeService(name, corev1.ServiceTypeLoadBalancer,
			map[string]string{
				util.VpcNatGatewayAnnotation: gatewayName,
				util.EipAnnotation:           eipName,
			},
			map[string]string{"app": name},
			[]corev1.ServicePort{{
				Name: "http", Protocol: protocol, Port: port, TargetPort: intstr.FromInt32(8080),
			}},
			corev1.ServiceAffinityNone)
	}
	create := func(svc *corev1.Service) error {
		svc.Namespace = f.Namespace.Name
		_, err := f.ClientSet.CoreV1().Services(f.Namespace.Name).Create(context.Background(), svc, metav1.CreateOptions{})
		if err == nil {
			ginkgo.DeferCleanup(func() {
				ginkgo.By("Deleting service " + svc.Name)
				serviceClient.Delete(svc.Name)
			})
		}
		return err
	}

	// One (eip, external port, protocol) identity maps to one backend set in the gateway, so a
	// second Service declaring it would silently fight over it.
	framework.ConformanceIt("rejects a second service on the same eip and port", func() {
		f.SkipVersionPriorTo(1, 17, "gateway nftable LoadBalancer services were introduced in v1.17")

		first := "webhook-svc-a-" + framework.RandomSuffix()
		ginkgo.By("Creating the first service " + first)
		framework.ExpectNoError(create(gwService(first, 80, corev1.ProtocolTCP)))

		ginkgo.By("Creating a second service on the same eip and port")
		err := create(gwService("webhook-svc-b-"+framework.RandomSuffix(), 80, corev1.ProtocolTCP))
		framework.ExpectError(err, "a second service on the same eip:port must be rejected")
		framework.ExpectContainSubstring(err.Error(), "already used by service "+f.Namespace.Name+"/"+first)

		ginkgo.By("Creating a service on the same eip with another port")
		framework.ExpectNoError(create(gwService("webhook-svc-c-"+framework.RandomSuffix(), 81, corev1.ProtocolTCP)),
			"the identity includes the port, so another port is not a conflict")

		ginkgo.By("Creating a service on the same eip and port with another protocol")
		framework.ExpectNoError(create(gwService("webhook-svc-d-"+framework.RandomSuffix(), 80, corev1.ProtocolUDP)),
			"the identity includes the protocol, so another protocol is not a conflict")
	})

	// The gateway publishes the EIP as the ingress IP, the OVN load balancer publishes an address
	// of the annotated subnet as the same field; one Service cannot have both owners.
	framework.ConformanceIt("rejects a gateway service that also takes its external ip from a subnet", func() {
		f.SkipVersionPriorTo(1, 17, "gateway nftable LoadBalancer services were introduced in v1.17")

		svc := gwService("webhook-svc-ext-"+framework.RandomSuffix(), 80, corev1.ProtocolTCP)
		svc.Annotations[util.ServiceExternalIPFromSubnetAnnotation] = "ovn-default"
		err := create(svc)
		framework.ExpectError(err, "two owners of the ingress ip must be rejected")
		framework.ExpectContainSubstring(err.Error(), "both publish the LoadBalancer ingress IP")
	})

	// The binding is immutable, but clearing it is the supported opt-out: the controller then
	// releases everything the Service programmed.
	framework.ConformanceIt("keeps the binding immutable but allows opting out", func() {
		f.SkipVersionPriorTo(1, 17, "gateway nftable LoadBalancer services were introduced in v1.17")

		name := "webhook-svc-bind-" + framework.RandomSuffix()
		ginkgo.By("Creating service " + name)
		framework.ExpectNoError(create(gwService(name, 80, corev1.ProtocolTCP)))
		svc := serviceClient.Get(name)

		ginkgo.By("Rebinding the service to another gateway is rejected")
		modified := svc.DeepCopy()
		modified.Annotations[util.VpcNatGatewayAnnotation] = gatewayName + "-other"
		_, err := f.ClientSet.CoreV1().Services(f.Namespace.Name).Update(context.Background(), modified, metav1.UpdateOptions{})
		framework.ExpectError(err, "the gateway of a service must be immutable")
		framework.ExpectContainSubstring(err.Error(), "vpc nat gateway cannot change")

		ginkgo.By("Rebinding the service to another eip is rejected")
		modified = svc.DeepCopy()
		modified.Annotations[util.EipAnnotation] = eipName + "-other"
		_, err = f.ClientSet.CoreV1().Services(f.Namespace.Name).Update(context.Background(), modified, metav1.UpdateOptions{})
		framework.ExpectError(err, "the eip of a service must be immutable")
		framework.ExpectContainSubstring(err.Error(), "eip cannot change")

		ginkgo.By("Clearing the binding is allowed")
		modified = svc.DeepCopy()
		delete(modified.Annotations, util.EipAnnotation)
		delete(modified.Annotations, util.VpcNatGatewayAnnotation)
		_, err = f.ClientSet.CoreV1().Services(f.Namespace.Name).Update(context.Background(), modified, metav1.UpdateOptions{})
		framework.ExpectNoError(err, "opting out releases the service, it is not a rebinding")
	})

	// A type=share rule is an accounting record of a Service, not forwarding intent an operator
	// may declare by hand.
	framework.ConformanceIt("rejects a hand-created share dnat rule", func() {
		f.SkipVersionPriorTo(1, 17, "share dnat records became controller-owned in v1.17")

		name := "webhook-share-" + framework.RandomSuffix()
		rule := &apiv1.IptablesDnatRule{
			Name: name,
			Spec: apiv1.IptablesDnatRuleSpec{
				EIP: eipName, ExternalPort: "80", Protocol: "tcp",
				InternalIP: "10.0.0.2", InternalPort: "8080", Type: apiv1.DnatRuleTypeShare,
			},
		}
		_, err := f.KubeOVNClientSet.KubeovnV1().IptablesDnatRules().Create(context.Background(), rule, metav1.CreateOptions{})
		if err == nil {
			ginkgo.DeferCleanup(func() {
				ginkgo.By("Deleting dnat rule " + name)
				_ = f.KubeOVNClientSet.KubeovnV1().IptablesDnatRules().Delete(context.Background(), name, metav1.DeleteOptions{})
			})
		}
		framework.ExpectError(err, "a hand-created share rule must be rejected")
		framework.ExpectContainSubstring(err.Error(), "reserved for nftable LB Service accounting records")
	})
})
