package util

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestNodeMatchesSelector(t *testing.T) {
	tests := []struct {
		name     string
		node     *v1.Node
		selector *metav1.LabelSelector
		want     bool
		wantErr  bool
	}{
		{
			name: "nil selector should match any node",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						"env": "test",
					},
				},
			},
			selector: nil,
			want:     true,
			wantErr:  false,
		},
		{
			name: "node matches simple label selector",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						"env":  "test",
						"zone": "us-east-1",
					},
				},
			},
			selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"env": "test",
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "node does not match label selector",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						"env": "prod",
					},
				},
			},
			selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"env": "test",
				},
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "node matches multiple labels",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						"env":      "test",
						"zone":     "us-east-1",
						"nodeType": "worker",
					},
				},
			},
			selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"env":  "test",
					"zone": "us-east-1",
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "node matches with match expressions",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						"env":      "test",
						"nodeType": "worker",
					},
				},
			},
			selector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "env",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"test", "staging"},
					},
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "node does not match with match expressions",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						"env": "prod",
					},
				},
			},
			selector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "env",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"test", "staging"},
					},
				},
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "node with no labels and empty selector",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
			},
			selector: &metav1.LabelSelector{},
			want:     true,
			wantErr:  false,
		},
		{
			name: "node with no labels does not match label selector",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
			},
			selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"env": "test",
				},
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "invalid label selector should return error",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
			},
			selector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "env",
						Operator: "InvalidOperator",
						Values:   []string{"test"},
					},
				},
			},
			want:    false,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NodeMatchesSelector(tt.node, tt.selector)
			if (err != nil) != tt.wantErr {
				t.Errorf("NodeMatchesSelector() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("NodeMatchesSelector() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsNodeExcludedFromProviderNetwork(t *testing.T) {
	tests := []struct {
		name    string
		node    *v1.Node
		pn      *kubeovnv1.ProviderNetwork
		want    bool
		wantErr bool
	}{
		{
			name: "node matches nodeSelector - should not be excluded",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "worker-1",
					Labels: map[string]string{
						"env":      "test",
						"nodeType": "worker",
					},
				},
			},
			pn: &kubeovnv1.ProviderNetwork{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-provider",
				},
				Spec: kubeovnv1.ProviderNetworkSpec{
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"env": "test",
						},
					},
					ExcludeNodes: []string{"worker-1"}, // Should be ignored when nodeSelector is present
				},
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "node does not match nodeSelector - should be excluded",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "worker-1",
					Labels: map[string]string{
						"env": "prod",
					},
				},
			},
			pn: &kubeovnv1.ProviderNetwork{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-provider",
				},
				Spec: kubeovnv1.ProviderNetworkSpec{
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"env": "test",
						},
					},
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "no nodeSelector and node in excludeNodes - should be excluded",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "worker-1",
				},
			},
			pn: &kubeovnv1.ProviderNetwork{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-provider",
				},
				Spec: kubeovnv1.ProviderNetworkSpec{
					ExcludeNodes: []string{"worker-1", "worker-2"},
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "no nodeSelector and node not in excludeNodes - should not be excluded",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "worker-3",
				},
			},
			pn: &kubeovnv1.ProviderNetwork{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-provider",
				},
				Spec: kubeovnv1.ProviderNetworkSpec{
					ExcludeNodes: []string{"worker-1", "worker-2"},
				},
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "no nodeSelector and empty excludeNodes - should not be excluded",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "worker-1",
				},
			},
			pn: &kubeovnv1.ProviderNetwork{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-provider",
				},
				Spec: kubeovnv1.ProviderNetworkSpec{},
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "invalid nodeSelector should return error",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "worker-1",
				},
			},
			pn: &kubeovnv1.ProviderNetwork{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-provider",
				},
				Spec: kubeovnv1.ProviderNetworkSpec{
					NodeSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "env",
								Operator: "InvalidOperator",
								Values:   []string{"test"},
							},
						},
					},
				},
			},
			want:    false,
			wantErr: true,
		},
		{
			name: "nodeSelector with match expressions - node matches",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "worker-1",
					Labels: map[string]string{
						"env":      "test",
						"nodeType": "worker",
					},
				},
			},
			pn: &kubeovnv1.ProviderNetwork{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-provider",
				},
				Spec: kubeovnv1.ProviderNetworkSpec{
					NodeSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "env",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"test", "staging"},
							},
							{
								Key:      "nodeType",
								Operator: metav1.LabelSelectorOpExists,
							},
						},
					},
				},
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "nodeSelector with match expressions - node does not match",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "worker-1",
					Labels: map[string]string{
						"env": "prod",
					},
				},
			},
			pn: &kubeovnv1.ProviderNetwork{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-provider",
				},
				Spec: kubeovnv1.ProviderNetworkSpec{
					NodeSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "env",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"test", "staging"},
							},
						},
					},
				},
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "complex nodeSelector with both matchLabels and matchExpressions",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "worker-1",
					Labels: map[string]string{
						"env":      "test",
						"zone":     "us-east-1",
						"nodeType": "worker",
					},
				},
			},
			pn: &kubeovnv1.ProviderNetwork{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-provider",
				},
				Spec: kubeovnv1.ProviderNetworkSpec{
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"env": "test",
						},
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "zone",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"us-east-1", "us-west-1"},
							},
						},
					},
				},
			},
			want:    false,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := IsNodeExcludedFromProviderNetwork(tt.node, tt.pn)
			if (err != nil) != tt.wantErr {
				t.Errorf("IsNodeExcludedFromProviderNetwork() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("IsNodeExcludedFromProviderNetwork() = %v, want %v", got, tt.want)
			}
		})
	}
}
