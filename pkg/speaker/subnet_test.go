package speaker

import (
	"fmt"
	"testing"

	"github.com/osrg/gobgp/v4/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestCollectPodExpectedPrefixes(t *testing.T) {
	const (
		localNode  = "node1"
		remoteNode = "node2"
	)

	newSubnet := func(name, cidr, bgpPolicy string) *kubeovnv1.Subnet {
		s := &kubeovnv1.Subnet{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       kubeovnv1.SubnetSpec{CIDRBlock: cidr},
		}
		s.Status.SetCondition(kubeovnv1.ConditionType(kubeovnv1.Ready), "Init", "")
		if bgpPolicy != "" {
			s.Annotations = map[string]string{util.BgpAnnotation: bgpPolicy}
		} else {
			s.Annotations = map[string]string{}
		}
		return s
	}

	newPod := func(name, nodeName string, annotations map[string]string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Annotations: annotations,
			},
			Spec: corev1.PodSpec{
				NodeName: nodeName,
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		}
	}

	tests := []struct {
		name       string
		subnets    []*kubeovnv1.Subnet
		pods       []*corev1.Pod
		expectedV4 []string
		expectedV6 []string
	}{
		{
			name: "primary network with pod bgp=true announces IP",
			subnets: []*kubeovnv1.Subnet{
				newSubnet("ovn-default", "10.16.0.0/16", ""),
			},
			pods: []*corev1.Pod{
				newPod("pod1", remoteNode, map[string]string{
					util.BgpAnnotation: "true",
					fmt.Sprintf(util.IPAddressAnnotationTemplate, "ovn"):     "10.16.0.5",
					fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, "ovn"): "ovn-default",
				}),
			},
			expectedV4: []string{"10.16.0.5/32"},
		},
		{
			name: "attachment network with subnet bgp=cluster announces IP",
			subnets: []*kubeovnv1.Subnet{
				newSubnet("attach-subnet", "192.168.1.0/24", "cluster"),
			},
			pods: []*corev1.Pod{
				newPod("pod1", remoteNode, map[string]string{
					fmt.Sprintf(util.IPAddressAnnotationTemplate, "net1.default.ovn"):     "192.168.1.10",
					fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, "net1.default.ovn"): "attach-subnet",
				}),
			},
			expectedV4: []string{"192.168.1.10/32"},
		},
		{
			name: "attachment network with subnet bgp=local on local node announces IP",
			subnets: []*kubeovnv1.Subnet{
				newSubnet("attach-subnet", "192.168.1.0/24", "local"),
			},
			pods: []*corev1.Pod{
				newPod("pod1", localNode, map[string]string{
					fmt.Sprintf(util.IPAddressAnnotationTemplate, "net1.default.ovn"):     "192.168.1.10",
					fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, "net1.default.ovn"): "attach-subnet",
				}),
			},
			expectedV4: []string{"192.168.1.10/32"},
		},
		{
			name: "attachment network with subnet bgp=local on remote node does not announce",
			subnets: []*kubeovnv1.Subnet{
				newSubnet("attach-subnet", "192.168.1.0/24", "local"),
			},
			pods: []*corev1.Pod{
				newPod("pod1", remoteNode, map[string]string{
					fmt.Sprintf(util.IPAddressAnnotationTemplate, "net1.default.ovn"):     "192.168.1.10",
					fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, "net1.default.ovn"): "attach-subnet",
				}),
			},
		},
		{
			name: "no bgp annotation on pod or subnet does not announce",
			subnets: []*kubeovnv1.Subnet{
				newSubnet("ovn-default", "10.16.0.0/16", ""),
			},
			pods: []*corev1.Pod{
				newPod("pod1", localNode, map[string]string{
					fmt.Sprintf(util.IPAddressAnnotationTemplate, "ovn"):     "10.16.0.5",
					fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, "ovn"): "ovn-default",
				}),
			},
		},
		{
			name: "non-primary CNI mode: only attachment annotations, no primary",
			subnets: []*kubeovnv1.Subnet{
				newSubnet("attach-subnet", "192.168.1.0/24", "cluster"),
			},
			pods: []*corev1.Pod{
				newPod("pod1", localNode, map[string]string{
					fmt.Sprintf(util.IPAddressAnnotationTemplate, "net1.ns.ovn"):     "192.168.1.20",
					fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, "net1.ns.ovn"): "attach-subnet",
				}),
			},
			expectedV4: []string{"192.168.1.20/32"},
		},
		{
			name: "pod bgp annotation overrides subnet annotation",
			subnets: []*kubeovnv1.Subnet{
				newSubnet("ovn-default", "10.16.0.0/16", "cluster"),
			},
			pods: []*corev1.Pod{
				newPod("pod1", remoteNode, map[string]string{
					util.BgpAnnotation: "local",
					fmt.Sprintf(util.IPAddressAnnotationTemplate, "ovn"):     "10.16.0.5",
					fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, "ovn"): "ovn-default",
				}),
			},
			// Pod says "local" but pod is on remote node, so nothing announced
		},
		{
			name: "dual-stack pod with bgp=cluster",
			subnets: []*kubeovnv1.Subnet{
				newSubnet("ovn-default", "10.16.0.0/16,fd00::/112", "cluster"),
			},
			pods: []*corev1.Pod{
				newPod("pod1", localNode, map[string]string{
					fmt.Sprintf(util.IPAddressAnnotationTemplate, "ovn"):     "10.16.0.5,fd00::5",
					fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, "ovn"): "ovn-default",
				}),
			},
			expectedV4: []string{"10.16.0.5/32"},
			expectedV6: []string{"fd00::5/128"},
		},
		{
			name: "multiple networks on same pod",
			subnets: []*kubeovnv1.Subnet{
				newSubnet("ovn-default", "10.16.0.0/16", "cluster"),
				newSubnet("attach-subnet", "192.168.1.0/24", "local"),
			},
			pods: []*corev1.Pod{
				newPod("pod1", localNode, map[string]string{
					fmt.Sprintf(util.IPAddressAnnotationTemplate, "ovn"):                  "10.16.0.5",
					fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, "ovn"):              "ovn-default",
					fmt.Sprintf(util.IPAddressAnnotationTemplate, "net1.default.ovn"):     "192.168.1.10",
					fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, "net1.default.ovn"): "attach-subnet",
				}),
			},
			expectedV4: []string{"10.16.0.5/32", "192.168.1.10/32"},
		},
		{
			name: "dead pod is skipped",
			subnets: []*kubeovnv1.Subnet{
				newSubnet("ovn-default", "10.16.0.0/16", "cluster"),
			},
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "dead-pod",
						Annotations: map[string]string{
							fmt.Sprintf(util.IPAddressAnnotationTemplate, "ovn"):     "10.16.0.5",
							fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, "ovn"): "ovn-default",
						},
					},
					Spec: corev1.PodSpec{
						NodeName:      localNode,
						RestartPolicy: corev1.RestartPolicyNever,
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodFailed,
					},
				},
			},
		},
		{
			name: "pod with no annotations is skipped",
			subnets: []*kubeovnv1.Subnet{
				newSubnet("ovn-default", "10.16.0.0/16", "cluster"),
			},
			pods: []*corev1.Pod{
				newPod("pod1", localNode, nil),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subnetByName := make(map[string]*kubeovnv1.Subnet, len(tt.subnets))
			for _, s := range tt.subnets {
				subnetByName[s.Name] = s
			}

			bgpExpected := make(prefixMap)
			collectPodExpectedPrefixes(tt.pods, subnetByName, localNode, bgpExpected)

			var gotV4, gotV6 []string
			for afi, prefixes := range bgpExpected {
				for p := range prefixes {
					switch afi {
					case api.Family_AFI_IP:
						gotV4 = append(gotV4, p)
					case api.Family_AFI_IP6:
						gotV6 = append(gotV6, p)
					}
				}
			}

			require.ElementsMatch(t, tt.expectedV4, gotV4, "IPv4 prefixes mismatch")
			require.ElementsMatch(t, tt.expectedV6, gotV6, "IPv6 prefixes mismatch")
		})
	}
}
