package util

import (
	"fmt"
	"slices"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

// NodeMatchesSelector checks if a node matches the given label selector
func NodeMatchesSelector(node *v1.Node, selector *metav1.LabelSelector) (bool, error) {
	if selector == nil {
		return true, nil
	}

	labelSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false, err
	}

	return labelSelector.Matches(labels.Set(node.Labels)), nil
}

// IsNodeExcludedFromProviderNetwork determines if a node should be excluded from a provider network
// Returns true if the node should be excluded, false otherwise
func IsNodeExcludedFromProviderNetwork(node *v1.Node, pn *kubeovnv1.ProviderNetwork) (bool, error) {
	if pn.Spec.NodeSelector != nil {
		matched, err := NodeMatchesSelector(node, pn.Spec.NodeSelector)
		if err != nil {
			return false, fmt.Errorf("failed to check nodeSelector for provider network %s: %w", pn.Name, err)
		}
		return !matched, nil
	}

	return slices.Contains(pn.Spec.ExcludeNodes, node.Name), nil
}
