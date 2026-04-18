package controller

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

func TestIndexPodByNode(t *testing.T) {
	tests := []struct {
		name string
		obj  any
		want []string
	}{
		{
			name: "pod on node",
			obj:  &v1.Pod{Spec: v1.PodSpec{NodeName: "node-1"}},
			want: []string{"node-1"},
		},
		{
			name: "pod without node assignment",
			obj:  &v1.Pod{Spec: v1.PodSpec{NodeName: ""}},
			want: nil,
		},
		{
			name: "non-pod object",
			obj:  &v1.Service{},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := indexPodByNode(tt.obj)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !equalStringSlice(got, tt.want) {
				t.Fatalf("indexPodByNode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIndexEPSByService(t *testing.T) {
	tests := []struct {
		name string
		obj  any
		want []string
	}{
		{
			name: "eps with service label",
			obj: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Labels:    map[string]string{discoveryv1.LabelServiceName: "my-svc"},
				},
			},
			want: []string{"default/my-svc"},
		},
		{
			name: "eps without service label",
			obj: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Labels:    map[string]string{},
				},
			},
			want: nil,
		},
		{
			name: "eps with nil labels",
			obj:  &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{Namespace: "default"}},
			want: nil,
		},
		{
			name: "non-eps object",
			obj:  &v1.Pod{},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := indexEPSByService(tt.obj)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !equalStringSlice(got, tt.want) {
				t.Fatalf("indexEPSByService() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIndexersLookup(t *testing.T) {
	podIdx := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{IndexPodByNode: indexPodByNode})
	for _, pod := range []*v1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"}, Spec: v1.PodSpec{NodeName: "node-a"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "p2", Namespace: "ns"}, Spec: v1.PodSpec{NodeName: "node-a"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "p3", Namespace: "ns"}, Spec: v1.PodSpec{NodeName: "node-b"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "p4", Namespace: "ns"}},
	} {
		if err := podIdx.Add(pod); err != nil {
			t.Fatalf("add pod: %v", err)
		}
	}
	got, err := podIdx.ByIndex(IndexPodByNode, "node-a")
	if err != nil {
		t.Fatalf("ByIndex: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 pods on node-a, got %d", len(got))
	}

	epsIdx := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{IndexEPSByService: indexEPSByService})
	for _, eps := range []*discoveryv1.EndpointSlice{
		{ObjectMeta: metav1.ObjectMeta{Name: "e1", Namespace: "ns", Labels: map[string]string{discoveryv1.LabelServiceName: "svc-a"}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "e2", Namespace: "ns", Labels: map[string]string{discoveryv1.LabelServiceName: "svc-a"}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "e3", Namespace: "ns", Labels: map[string]string{discoveryv1.LabelServiceName: "svc-b"}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "e4", Namespace: "other", Labels: map[string]string{discoveryv1.LabelServiceName: "svc-a"}}},
	} {
		if err := epsIdx.Add(eps); err != nil {
			t.Fatalf("add eps: %v", err)
		}
	}
	got, err = epsIdx.ByIndex(IndexEPSByService, "ns/svc-a")
	if err != nil {
		t.Fatalf("ByIndex: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 eps for ns/svc-a, got %d", len(got))
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
