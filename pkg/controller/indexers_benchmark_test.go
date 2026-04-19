package controller

import (
	"fmt"
	"sort"
	"testing"

	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

// buildPodIndexer populates an indexer with nPods pods spread evenly across
// nNodes nodes. A share of pods are left unscheduled to exercise the empty
// NodeName branch.
func buildPodIndexer(tb testing.TB, nPods, nNodes int) cache.Indexer {
	tb.Helper()
	idx := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{IndexPodByNode: indexPodByNode})
	for i := range nPods {
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%d", i),
				Namespace: "ns",
			},
		}
		// Leave every 20th pod unscheduled.
		if i%20 != 0 {
			pod.Spec.NodeName = fmt.Sprintf("node-%d", i%nNodes)
		}
		if err := idx.Add(pod); err != nil {
			tb.Fatalf("add pod: %v", err)
		}
	}
	return idx
}

// buildEPSIndexer populates an indexer with nEPS endpointslices spread evenly
// across nServices services. A share of slices are left without a service
// label to exercise the orphan branch.
func buildEPSIndexer(tb testing.TB, nEPS, nServices int) cache.Indexer {
	tb.Helper()
	idx := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{IndexEPSByService: indexEPSByService})
	for i := range nEPS {
		eps := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("eps-%d", i),
				Namespace: "ns",
				Labels:    map[string]string{},
			},
		}
		// Leave every 25th slice without a service label.
		if i%25 != 0 {
			eps.Labels[discoveryv1.LabelServiceName] = fmt.Sprintf("svc-%d", i%nServices)
		}
		if err := idx.Add(eps); err != nil {
			tb.Fatalf("add eps: %v", err)
		}
	}
	return idx
}

// listPodsOnNodeFullScan mirrors the pre-indexer logic: list every pod and
// filter client-side by NodeName.
func listPodsOnNodeFullScan(idx cache.Indexer, nodeName string) []*v1.Pod {
	all := idx.List()
	out := make([]*v1.Pod, 0)
	for _, obj := range all {
		pod := obj.(*v1.Pod)
		if pod.Spec.NodeName == nodeName {
			out = append(out, pod)
		}
	}
	return out
}

// listEPSForServiceFullScan mirrors the pre-indexer logic: list every
// endpointslice and filter by the service label.
func listEPSForServiceFullScan(idx cache.Indexer, namespace, service string) []*discoveryv1.EndpointSlice {
	all := idx.List()
	out := make([]*discoveryv1.EndpointSlice, 0)
	for _, obj := range all {
		eps := obj.(*discoveryv1.EndpointSlice)
		if eps.Namespace != namespace {
			continue
		}
		if eps.Labels[discoveryv1.LabelServiceName] == service {
			out = append(out, eps)
		}
	}
	return out
}

func podNames(pods []*v1.Pod) []string {
	out := make([]string, 0, len(pods))
	for _, p := range pods {
		out = append(out, p.Name)
	}
	sort.Strings(out)
	return out
}

func epsNames(epss []*discoveryv1.EndpointSlice) []string {
	out := make([]string, 0, len(epss))
	for _, e := range epss {
		out = append(out, e.Name)
	}
	sort.Strings(out)
	return out
}

// TestIndexersResultParityWithFullScan asserts that the indexer lookup returns
// the same set of objects as a full-scan filter across every key, including
// unscheduled pods and slices without a service label.
func TestIndexersResultParityWithFullScan(t *testing.T) {
	const (
		nPods     = 2000
		nNodes    = 50
		nEPS      = 2000
		nServices = 50
	)

	podIdx := buildPodIndexer(t, nPods, nNodes)
	for i := range nNodes {
		node := fmt.Sprintf("node-%d", i)
		objs, err := podIdx.ByIndex(IndexPodByNode, node)
		if err != nil {
			t.Fatalf("ByIndex: %v", err)
		}
		got := make([]*v1.Pod, 0, len(objs))
		for _, o := range objs {
			got = append(got, o.(*v1.Pod))
		}
		want := listPodsOnNodeFullScan(podIdx, node)
		if a, b := podNames(got), podNames(want); !equalStringSlice(a, b) {
			t.Fatalf("pod parity mismatch for %s: indexer=%v fullscan=%v", node, a, b)
		}
	}

	epsIdx := buildEPSIndexer(t, nEPS, nServices)
	for i := range nServices {
		svc := fmt.Sprintf("svc-%d", i)
		objs, err := epsIdx.ByIndex(IndexEPSByService, "ns/"+svc)
		if err != nil {
			t.Fatalf("ByIndex: %v", err)
		}
		got := make([]*discoveryv1.EndpointSlice, 0, len(objs))
		for _, o := range objs {
			got = append(got, o.(*discoveryv1.EndpointSlice))
		}
		want := listEPSForServiceFullScan(epsIdx, "ns", svc)
		if a, b := epsNames(got), epsNames(want); !equalStringSlice(a, b) {
			t.Fatalf("eps parity mismatch for %s: indexer=%v fullscan=%v", svc, a, b)
		}
	}
}

// BenchmarkPodByNode_Indexer measures lookup cost using the secondary index.
func BenchmarkPodByNode_Indexer(b *testing.B) {
	idx := buildPodIndexer(b, 10000, 200)

	for i := 0; b.Loop(); i++ {
		node := fmt.Sprintf("node-%d", i%200)
		if _, err := idx.ByIndex(IndexPodByNode, node); err != nil {
			b.Fatalf("ByIndex: %v", err)
		}
	}
}

// BenchmarkPodByNode_FullScan measures the pre-indexer approach: list every
// pod and filter client-side.
func BenchmarkPodByNode_FullScan(b *testing.B) {
	idx := buildPodIndexer(b, 10000, 200)

	for i := 0; b.Loop(); i++ {
		node := fmt.Sprintf("node-%d", i%200)
		_ = listPodsOnNodeFullScan(idx, node)
	}
}

// BenchmarkEPSByService_Indexer measures lookup cost using the secondary
// index.
func BenchmarkEPSByService_Indexer(b *testing.B) {
	idx := buildEPSIndexer(b, 10000, 500)

	for i := 0; b.Loop(); i++ {
		svc := fmt.Sprintf("svc-%d", i%500)
		if _, err := idx.ByIndex(IndexEPSByService, "ns/"+svc); err != nil {
			b.Fatalf("ByIndex: %v", err)
		}
	}
}

// BenchmarkEPSByService_FullScan measures the pre-indexer approach for the
// findEndpointSlicesForServices hot path.
func BenchmarkEPSByService_FullScan(b *testing.B) {
	idx := buildEPSIndexer(b, 10000, 500)

	for i := 0; b.Loop(); i++ {
		svc := fmt.Sprintf("svc-%d", i%500)
		_ = listEPSForServiceFullScan(idx, "ns", svc)
	}
}
