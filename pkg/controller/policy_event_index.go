package controller

import (
	"maps"
	"slices"
	"sync"

	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	v1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"
	v1alpha2 "sigs.k8s.io/network-policy-api/apis/v1alpha2"
)

type policyEventKind uint8

const (
	policyEventNetworkPolicy policyEventKind = iota
	policyEventAdminNetworkPolicy
	policyEventBaselineAdminNetworkPolicy
	policyEventClusterNetworkPolicy
)

type policyEventRef struct {
	kind policyEventKind
	key  string
}

type policyEventSelector struct {
	namespace *metav1.LabelSelector
	pod       *metav1.LabelSelector
}

type policyEventIndexEntry struct {
	namespaceTokens []string
	podTokens       []string
	namespaceAll    bool
	podAll          bool
}

type policyEventIndex struct {
	mu    sync.RWMutex
	ready bool

	entries      map[policyEventRef]policyEventIndexEntry
	namespace    map[string]map[policyEventRef]struct{}
	pod          map[string]map[policyEventRef]struct{}
	namespaceAll map[policyEventRef]struct{}
	podAll       map[policyEventRef]struct{}
}

func (index *policyEventIndex) markReady() {
	index.mu.Lock()
	index.ready = true
	index.mu.Unlock()
}

func (index *policyEventIndex) isReady() bool {
	index.mu.RLock()
	ready := index.ready
	index.mu.RUnlock()
	return ready
}

func policyEventLabelToken(key, value string) string {
	return "label\x00" + key + "\x00" + value
}

func policyEventKeyToken(key string) string {
	return "key\x00" + key
}

// policyEventSelectorTokens indexes only selectors whose constraints can be
// safely narrowed by a label key/value. Operators such as In and DoesNotExist
// remain in the all set because a label removal can change their result without
// leaving a value token in the new object.
func policyEventSelectorTokens(selector *metav1.LabelSelector) (tokens []string, all bool) {
	if selector == nil || (len(selector.MatchLabels) == 0 && len(selector.MatchExpressions) == 0) {
		return nil, true
	}
	if _, err := metav1.LabelSelectorAsSelector(selector); err != nil {
		return nil, true
	}

	for key, value := range selector.MatchLabels {
		tokens = append(tokens, policyEventLabelToken(key, value))
	}
	for _, requirement := range selector.MatchExpressions {
		if requirement.Operator != metav1.LabelSelectorOpExists {
			return nil, true
		}
		tokens = append(tokens, policyEventKeyToken(requirement.Key))
	}
	slices.Sort(tokens)
	return slices.Compact(tokens), false
}

func policyEventIndexEntryFor(selectors []policyEventSelector) policyEventIndexEntry {
	var entry policyEventIndexEntry
	for _, selector := range selectors {
		if selector.namespace != nil {
			tokens, all := policyEventSelectorTokens(selector.namespace)
			entry.namespaceTokens = append(entry.namespaceTokens, tokens...)
			entry.namespaceAll = entry.namespaceAll || all
		}
		if selector.pod != nil {
			tokens, all := policyEventSelectorTokens(selector.pod)
			entry.podTokens = append(entry.podTokens, tokens...)
			entry.podAll = entry.podAll || all
		}
	}
	slices.Sort(entry.namespaceTokens)
	entry.namespaceTokens = slices.Compact(entry.namespaceTokens)
	slices.Sort(entry.podTokens)
	entry.podTokens = slices.Compact(entry.podTokens)
	return entry
}

func (index *policyEventIndex) upsert(ref policyEventRef, selectors []policyEventSelector) {
	index.mu.Lock()
	defer index.mu.Unlock()
	index.removeLocked(ref)
	if index.entries == nil {
		index.entries = make(map[policyEventRef]policyEventIndexEntry)
	}
	entry := policyEventIndexEntryFor(selectors)
	index.entries[ref] = entry
	index.addEntryLocked(ref, entry)
}

func (index *policyEventIndex) remove(ref policyEventRef) {
	index.mu.Lock()
	defer index.mu.Unlock()
	index.removeLocked(ref)
}

func (index *policyEventIndex) removeLocked(ref policyEventRef) {
	entry, ok := index.entries[ref]
	if !ok {
		return
	}
	index.removeTokensLocked(index.namespace, entry.namespaceTokens, ref)
	index.removeTokensLocked(index.pod, entry.podTokens, ref)
	if entry.namespaceAll {
		delete(index.namespaceAll, ref)
	}
	if entry.podAll {
		delete(index.podAll, ref)
	}
	delete(index.entries, ref)
}

func (index *policyEventIndex) addEntryLocked(ref policyEventRef, entry policyEventIndexEntry) {
	if entry.namespaceAll {
		if index.namespaceAll == nil {
			index.namespaceAll = make(map[policyEventRef]struct{})
		}
		index.namespaceAll[ref] = struct{}{}
	}
	if entry.podAll {
		if index.podAll == nil {
			index.podAll = make(map[policyEventRef]struct{})
		}
		index.podAll[ref] = struct{}{}
	}
	index.addTokensLocked(&index.namespace, entry.namespaceTokens, ref)
	index.addTokensLocked(&index.pod, entry.podTokens, ref)
}

func (index *policyEventIndex) addTokensLocked(target *map[string]map[policyEventRef]struct{}, tokens []string, ref policyEventRef) {
	if len(tokens) == 0 {
		return
	}
	if *target == nil {
		*target = make(map[string]map[policyEventRef]struct{})
	}
	for _, token := range tokens {
		if (*target)[token] == nil {
			(*target)[token] = make(map[policyEventRef]struct{})
		}
		(*target)[token][ref] = struct{}{}
	}
}

func (index *policyEventIndex) removeTokensLocked(target map[string]map[policyEventRef]struct{}, tokens []string, ref policyEventRef) {
	for _, token := range tokens {
		refs := target[token]
		delete(refs, ref)
		if len(refs) == 0 {
			delete(target, token)
		}
	}
}

func policyEventLabelTokens(labelSets ...map[string]string) []string {
	seen := make(map[string]struct{})
	for _, set := range labelSets {
		for key, value := range set {
			seen[policyEventLabelToken(key, value)] = struct{}{}
			seen[policyEventKeyToken(key)] = struct{}{}
		}
	}
	return slices.Collect(maps.Keys(seen))
}

func (index *policyEventIndex) candidates(kind policyEventKind, namespaceLabels, podLabels []map[string]string) []string {
	index.mu.RLock()
	defer index.mu.RUnlock()

	refs := make(map[policyEventRef]struct{})
	add := func(ref policyEventRef) {
		if ref.kind == kind {
			refs[ref] = struct{}{}
		}
	}
	query := func(table map[string]map[policyEventRef]struct{}, all map[policyEventRef]struct{}, labelSets ...map[string]string) {
		for ref := range all {
			add(ref)
		}
		for _, token := range policyEventLabelTokens(labelSets...) {
			for ref := range table[token] {
				add(ref)
			}
		}
	}
	query(index.namespace, index.namespaceAll, namespaceLabels...)
	query(index.pod, index.podAll, podLabels...)

	result := make([]string, 0, len(refs))
	for ref := range refs {
		result = append(result, ref.key)
	}
	slices.Sort(result)
	return result
}

func (index *policyEventIndex) candidatesForPod(namespaceLabels, podLabels map[string]string) []string {
	return index.candidates(policyEventNetworkPolicy, []map[string]string{namespaceLabels}, []map[string]string{podLabels})
}

func (index *policyEventIndex) candidatesForNamespace(namespaceLabels map[string]string) []string {
	return index.candidates(policyEventNetworkPolicy, []map[string]string{namespaceLabels}, nil)
}

func (index *policyEventIndex) candidatesForChange(kind policyEventKind, change labelChange) []string {
	return index.candidates(kind,
		[]map[string]string{change.oldNsLabels, change.newNsLabels},
		[]map[string]string{change.oldPodLabels, change.newPodLabels},
	)
}

func (c *Controller) indexNetworkPolicy(np *netv1.NetworkPolicy) {
	selectors := []policyEventSelector{{pod: &np.Spec.PodSelector}}
	for _, rule := range np.Spec.Ingress {
		for _, peer := range rule.From {
			if peer.IPBlock == nil {
				selectors = append(selectors, policyEventSelector{namespace: peer.NamespaceSelector, pod: peer.PodSelector})
			}
		}
	}
	for _, rule := range np.Spec.Egress {
		for _, peer := range rule.To {
			if peer.IPBlock == nil {
				selectors = append(selectors, policyEventSelector{namespace: peer.NamespaceSelector, pod: peer.PodSelector})
			}
		}
	}
	c.policyEventIndex.upsert(policyEventRef{kind: policyEventNetworkPolicy, key: np.Namespace + "/" + np.Name}, selectors)
}

func (c *Controller) indexAdminNetworkPolicy(anp *v1alpha1.AdminNetworkPolicy) {
	selectors := adminNetworkPolicySelectors(anp.Spec.Subject, anp.Spec.Ingress, anp.Spec.Egress)
	c.policyEventIndex.upsert(policyEventRef{kind: policyEventAdminNetworkPolicy, key: anp.Name}, selectors)
}

func (c *Controller) indexBaselineAdminNetworkPolicy(banp *v1alpha1.BaselineAdminNetworkPolicy) {
	selectors := baselineAdminNetworkPolicySelectors(banp.Spec.Subject, banp.Spec.Ingress, banp.Spec.Egress)
	c.policyEventIndex.upsert(policyEventRef{kind: policyEventBaselineAdminNetworkPolicy, key: banp.Name}, selectors)
}

func (c *Controller) indexClusterNetworkPolicy(cnp *v1alpha2.ClusterNetworkPolicy) {
	selectors := clusterNetworkPolicySelectors(cnp.Spec.Subject, cnp.Spec.Ingress, cnp.Spec.Egress)
	c.policyEventIndex.upsert(policyEventRef{kind: policyEventClusterNetworkPolicy, key: cnp.Name}, selectors)
}

func adminNetworkPolicySelectors(subject v1alpha1.AdminNetworkPolicySubject, ingress []v1alpha1.AdminNetworkPolicyIngressRule, egress []v1alpha1.AdminNetworkPolicyEgressRule) []policyEventSelector {
	selectors := make([]policyEventSelector, 0, len(ingress)+len(egress)+1)
	selectors = append(selectors, adminSubjectSelector(subject))
	for _, rule := range ingress {
		for _, peer := range rule.From {
			selectors = append(selectors, adminIngressPeerSelector(peer))
		}
	}
	for _, rule := range egress {
		for _, peer := range rule.To {
			selectors = append(selectors, adminEgressPeerSelector(peer))
		}
	}
	return selectors
}

func baselineEgressPeerSelector(peer v1alpha1.BaselineAdminNetworkPolicyEgressPeer) policyEventSelector {
	if peer.Namespaces != nil {
		return policyEventSelector{namespace: peer.Namespaces}
	}
	if peer.Pods != nil {
		return policyEventSelector{namespace: &peer.Pods.NamespaceSelector, pod: &peer.Pods.PodSelector}
	}
	return policyEventSelector{}
}

func adminSubjectSelector(subject v1alpha1.AdminNetworkPolicySubject) policyEventSelector {
	if subject.Namespaces != nil {
		return policyEventSelector{namespace: subject.Namespaces}
	}
	if subject.Pods != nil {
		return policyEventSelector{namespace: &subject.Pods.NamespaceSelector, pod: &subject.Pods.PodSelector}
	}
	return policyEventSelector{}
}

func adminIngressPeerSelector(peer v1alpha1.AdminNetworkPolicyIngressPeer) policyEventSelector {
	if peer.Namespaces != nil {
		return policyEventSelector{namespace: peer.Namespaces}
	}
	if peer.Pods != nil {
		return policyEventSelector{namespace: &peer.Pods.NamespaceSelector, pod: &peer.Pods.PodSelector}
	}
	return policyEventSelector{}
}

func adminEgressPeerSelector(peer v1alpha1.AdminNetworkPolicyEgressPeer) policyEventSelector {
	if peer.Namespaces != nil {
		return policyEventSelector{namespace: peer.Namespaces}
	}
	if peer.Pods != nil {
		return policyEventSelector{namespace: &peer.Pods.NamespaceSelector, pod: &peer.Pods.PodSelector}
	}
	return policyEventSelector{}
}

func baselineAdminNetworkPolicySelectors(subject v1alpha1.AdminNetworkPolicySubject, ingress []v1alpha1.BaselineAdminNetworkPolicyIngressRule, egress []v1alpha1.BaselineAdminNetworkPolicyEgressRule) []policyEventSelector {
	selectors := make([]policyEventSelector, 0, len(ingress)+len(egress)+1)
	selectors = append(selectors, adminSubjectSelector(subject))
	for _, rule := range ingress {
		for _, peer := range rule.From {
			selectors = append(selectors, adminIngressPeerSelector(peer))
		}
	}
	for _, rule := range egress {
		for _, peer := range rule.To {
			selectors = append(selectors, baselineEgressPeerSelector(peer))
		}
	}
	return selectors
}

func clusterNetworkPolicySelectors(subject v1alpha2.ClusterNetworkPolicySubject, ingress []v1alpha2.ClusterNetworkPolicyIngressRule, egress []v1alpha2.ClusterNetworkPolicyEgressRule) []policyEventSelector {
	selectors := make([]policyEventSelector, 0, len(ingress)+len(egress)+1)
	selectors = append(selectors, clusterSubjectSelector(subject))
	for _, rule := range ingress {
		for _, peer := range rule.From {
			selectors = append(selectors, clusterIngressPeerSelector(peer))
		}
	}
	for _, rule := range egress {
		for _, peer := range rule.To {
			selectors = append(selectors, clusterEgressPeerSelector(peer))
		}
	}
	return selectors
}

func clusterSubjectSelector(subject v1alpha2.ClusterNetworkPolicySubject) policyEventSelector {
	if subject.Namespaces != nil {
		return policyEventSelector{namespace: subject.Namespaces}
	}
	if subject.Pods != nil {
		return policyEventSelector{namespace: &subject.Pods.NamespaceSelector, pod: &subject.Pods.PodSelector}
	}
	return policyEventSelector{}
}

func clusterIngressPeerSelector(peer v1alpha2.ClusterNetworkPolicyIngressPeer) policyEventSelector {
	if peer.Namespaces != nil {
		return policyEventSelector{namespace: peer.Namespaces}
	}
	if peer.Pods != nil {
		return policyEventSelector{namespace: &peer.Pods.NamespaceSelector, pod: &peer.Pods.PodSelector}
	}
	return policyEventSelector{}
}

func clusterEgressPeerSelector(peer v1alpha2.ClusterNetworkPolicyEgressPeer) policyEventSelector {
	if peer.Namespaces != nil {
		return policyEventSelector{namespace: peer.Namespaces}
	}
	if peer.Pods != nil {
		return policyEventSelector{namespace: &peer.Pods.NamespaceSelector, pod: &peer.Pods.PodSelector}
	}
	return policyEventSelector{}
}

func (c *Controller) rebuildPolicyEventIndex() error {
	if c.config.EnableNP {
		policies, err := c.npsLister.List(labels.Everything())
		if err != nil {
			return err
		}
		for _, policy := range policies {
			c.indexNetworkPolicy(policy)
		}
	}
	if c.config.EnableANP {
		anps, err := c.anpsLister.List(labels.Everything())
		if err != nil {
			return err
		}
		for _, policy := range anps {
			c.indexAdminNetworkPolicy(policy)
		}
		banps, err := c.banpsLister.List(labels.Everything())
		if err != nil {
			return err
		}
		for _, policy := range banps {
			c.indexBaselineAdminNetworkPolicy(policy)
		}
		cnps, err := c.cnpsLister.List(labels.Everything())
		if err != nil {
			return err
		}
		for _, policy := range cnps {
			c.indexClusterNetworkPolicy(policy)
		}
	}
	c.policyEventIndex.markReady()
	return nil
}

func (c *Controller) policyEventIndexCandidatesForPod(pod *corev1.Pod, namespace *corev1.Namespace) []string {
	var namespaceLabels map[string]string
	if namespace != nil {
		namespaceLabels = namespace.Labels
	}
	return c.policyEventIndex.candidatesForPod(namespaceLabels, pod.Labels)
}
