package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestInspectVMLSPRecovery(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name      string
		keepVMIP  bool
		missing   bool
		portOwner string
	}{
		{name: "launcher port exists", portOwner: "virt-launcher-vm"},
		{name: "launcher port missing", missing: true, portOwner: "virt-launcher-vm"},
		{name: "VM port exists", keepVMIP: true, portOwner: "vm"},
		{name: "VM port missing", keepVMIP: true, missing: true, portOwner: "vm"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pod := &corev1.Pod{
				Name: "virt-launcher-vm", Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: kubevirtv1.SchemeGroupVersion.String(),
					Kind:       util.KindVirtualMachineInstance,
					Name:       "vm",
				}},
				Annotations: map[string]string{
					util.LogicalSwitchAnnotation: "ovn-default",
					util.IPAddressAnnotation:     "10.0.0.2",
					util.AllocatedAnnotation:     "true",
					util.RoutedAnnotation:        "true",
				},
				Spec:   corev1.PodSpec{NodeName: "node-c"},
				Status: corev1.PodStatus{Phase: corev1.PodRunning},
			}
			subnet := &kubeovnv1.Subnet{
				Name: "ovn-default",
				Spec: kubeovnv1.SubnetSpec{
					Vpc: "ovn-cluster", Provider: util.OvnProvider, GatewayType: kubeovnv1.GWDistributedType,
				},
			}
			fixture, err := newFakeControllerWithOptions(t, &FakeControllerOptions{
				Pods: []*corev1.Pod{pod}, Subnets: []*kubeovnv1.Subnet{subnet},
			})
			require.NoError(t, err)
			ctrl := fixture.fakeController
			ctrl.config.EnableKeepVMIP = tt.keepVMIP
			ctrl.addOrUpdatePodQueue = newTypedRateLimitingQueue[string]("InspectVMRecovery", nil)
			t.Cleanup(ctrl.addOrUpdatePodQueue.ShutDown)
			portName := ovs.PodNameToPortName(tt.portOwner, pod.Namespace, util.OvnProvider)
			fixture.mockOvnClient.EXPECT().LogicalSwitchPortExists(portName).Return(!tt.missing, nil)

			require.NoError(t, ctrl.inspectPod())
			current, err := ctrl.config.KubeClient.CoreV1().Pods(pod.Namespace).Get(t.Context(), pod.Name, metav1.GetOptions{})
			require.NoError(t, err)
			if tt.missing {
				require.NotContains(t, current.Annotations, util.AllocatedAnnotation)
				require.NotContains(t, current.Annotations, util.RoutedAnnotation)
				require.Equal(t, pod.Annotations[util.IPAddressAnnotation], current.Annotations[util.IPAddressAnnotation])
				require.Equal(t, pod.Annotations[util.LogicalSwitchAnnotation], current.Annotations[util.LogicalSwitchAnnotation])
				require.Equal(t, 1, ctrl.addOrUpdatePodQueue.Len())
				key, shutdown := ctrl.addOrUpdatePodQueue.Get()
				require.False(t, shutdown)
				require.Equal(t, pod.Namespace+"/"+pod.Name, key)
				ctrl.addOrUpdatePodQueue.Done(key)
			} else {
				require.Equal(t, pod.Annotations, current.Annotations)
				require.Zero(t, ctrl.addOrUpdatePodQueue.Len())
			}
		})
	}
}
