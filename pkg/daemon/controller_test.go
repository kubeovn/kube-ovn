package daemon

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	listerv1 "k8s.io/client-go/listers/core/v1"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	kubevirtv1 "kubevirt.io/api/core/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
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

			result := c.isVMLauncherPodAlive(tt.namespace, tt.vmiName, "test-iface")
			require.Equal(t, tt.expected, result)
		})
	}
}

// parsePatchLabels extracts the metadata.labels map from a merge-patch JSON.
// Each key maps to a *string (nil means the label is being removed).
func parsePatchLabels(t *testing.T, patchBytes []byte) map[string]*string {
	t.Helper()
	var raw struct {
		Metadata struct {
			Labels map[string]*string `json:"labels"`
		} `json:"metadata"`
	}
	require.NoError(t, json.Unmarshal(patchBytes, &raw))
	return raw.Metadata.Labels
}

func TestCleanProviderNetworkPatchIncludesVlanIntLabel(t *testing.T) {
	t.Parallel()

	pnName := "test-pn"
	nodeName := "test-node"

	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Labels: map[string]string{
				fmt.Sprintf(util.ProviderNetworkReadyTemplate, pnName):   "true",
				fmt.Sprintf(util.ProviderNetworkVlanIntTemplate, pnName): "true",
			},
		},
	}
	fakeClient := fake.NewSimpleClientset(node)

	c := &Controller{
		config: &Configuration{KubeClient: fakeClient},
	}
	pn := &kubeovnv1.ProviderNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: pnName},
		Spec:       kubeovnv1.ProviderNetworkSpec{DefaultInterface: "eth0"},
	}

	// PatchLabels is called before ovsCleanProviderNetwork, so the patch is
	// captured even when ovsCleanProviderNetwork fails in a test environment.
	_ = c.cleanProviderNetwork(pn, node)

	vlanIntKey := fmt.Sprintf(util.ProviderNetworkVlanIntTemplate, pnName)
	var patchFound bool
	for _, action := range fakeClient.Actions() {
		pa, ok := action.(k8stesting.PatchAction)
		if !ok {
			continue
		}
		labels := parsePatchLabels(t, pa.GetPatch())
		val, exists := labels[vlanIntKey]
		if exists {
			require.Nil(t, val, "VlanIntTemplate label should be set to null (removal)")
			patchFound = true
			break
		}
	}
	require.True(t, patchFound, "patch should include %s label cleanup", vlanIntKey)
}

func TestCleanProviderNetworkLabelConsistency(t *testing.T) {
	t.Parallel()

	pnName := "test-pn"

	// All labels that handleDeleteProviderNetwork cleans (except ExcludeTemplate
	// which is set to "true" in cleanProviderNetwork instead of nil).
	expectedCleanedLabels := []string{
		fmt.Sprintf(util.ProviderNetworkReadyTemplate, pnName),
		fmt.Sprintf(util.ProviderNetworkInterfaceTemplate, pnName),
		fmt.Sprintf(util.ProviderNetworkMtuTemplate, pnName),
		fmt.Sprintf(util.ProviderNetworkVlanIntTemplate, pnName),
	}

	labels := make(map[string]string)
	for _, key := range expectedCleanedLabels {
		labels[key] = "true"
	}
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: labels}}
	fakeClient := fake.NewSimpleClientset(node)

	c := &Controller{config: &Configuration{KubeClient: fakeClient}}
	pn := &kubeovnv1.ProviderNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: pnName},
		Spec:       kubeovnv1.ProviderNetworkSpec{DefaultInterface: "eth0"},
	}

	_ = c.cleanProviderNetwork(pn, node)

	var patchFound bool
	for _, action := range fakeClient.Actions() {
		pa, ok := action.(k8stesting.PatchAction)
		if !ok {
			continue
		}
		patchFound = true
		patchLabels := parsePatchLabels(t, pa.GetPatch())
		for _, key := range expectedCleanedLabels {
			val, exists := patchLabels[key]
			require.True(t, exists,
				"cleanProviderNetwork should clean label %s (consistent with handleDeleteProviderNetwork)", key)
			require.Nil(t, val,
				"label %s should be set to null (removal)", key)
		}
	}
	require.True(t, patchFound, "expected at least one PatchAction to be recorded")
}
