package daemon

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	kubevirtv1 "kubevirt.io/api/core/v1"
)

func newLauncherPod(name, namespace, vmiName string, useNewLabel bool) *v1.Pod {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{},
			Annotations: map[string]string{
				kubevirtv1.DomainAnnotation: vmiName,
			},
		},
	}
	if useNewLabel {
		pod.Labels[kubevirtv1.VirtualMachineInstanceIDLabel] = vmiName
	}
	pod.Labels[kubevirtv1.DeprecatedVirtualMachineNameLabel] = vmiName
	return pod
}

func TestIsVMLauncherPodAlive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		pods      []*v1.Pod
		vmiName   string
		namespace string
		expected  bool
	}{
		{
			name:      "no pods in namespace",
			pods:      nil,
			vmiName:   "test-vm",
			namespace: "default",
			expected:  false,
		},
		{
			name: "found by new label (VirtualMachineInstanceIDLabel)",
			pods: []*v1.Pod{
				newLauncherPod("virt-launcher-test-vm-abc", "default", "test-vm", true),
			},
			vmiName:   "test-vm",
			namespace: "default",
			expected:  true,
		},
		{
			name: "found by deprecated label only (old KubeVirt)",
			pods: []*v1.Pod{
				newLauncherPod("virt-launcher-test-vm-abc", "default", "test-vm", false),
			},
			vmiName:   "test-vm",
			namespace: "default",
			expected:  true,
		},
		{
			name: "found by domain annotation (long VMI name with hashed label)",
			pods: []*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "virt-launcher-long-vm-abc",
						Namespace: "default",
						Labels: map[string]string{
							// Hashed label value doesn't match the full VMI name
							kubevirtv1.VirtualMachineInstanceIDLabel:     "this-is-a-very-long-vmi-name-that-exceeds-63-chars-so-it-abc123",
							kubevirtv1.DeprecatedVirtualMachineNameLabel: "this-is-a-very-long-vmi-name-that-exceeds-63-chars-so-it-abc123",
						},
						Annotations: map[string]string{
							// Domain annotation always has the full VMI name
							kubevirtv1.DomainAnnotation: "this-is-a-very-long-vmi-name-that-exceeds-63-characters-and-gets-hashed-in-the-label",
						},
					},
				},
			},
			vmiName:   "this-is-a-very-long-vmi-name-that-exceeds-63-characters-and-gets-hashed-in-the-label",
			namespace: "default",
			expected:  true,
		},
		{
			name: "not found - different VM name",
			pods: []*v1.Pod{
				newLauncherPod("virt-launcher-other-vm-abc", "default", "other-vm", true),
			},
			vmiName:   "test-vm",
			namespace: "default",
			expected:  false,
		},
		{
			name: "not found - different namespace",
			pods: []*v1.Pod{
				newLauncherPod("virt-launcher-test-vm-abc", "other-ns", "test-vm", true),
			},
			vmiName:   "test-vm",
			namespace: "default",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
			for _, pod := range tt.pods {
				require.NoError(t, indexer.Add(pod))
			}

			c := &Controller{
				podsLister: listerv1.NewPodLister(indexer),
			}

			result := c.isVMLauncherPodAlive(tt.namespace, tt.vmiName, "test-iface", nil)
			require.Equal(t, tt.expected, result)
		})
	}
}
