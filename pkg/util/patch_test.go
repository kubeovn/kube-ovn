package util

import (
	"context"
	"encoding/json"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/kubernetes/fake"
	clientv1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPatchAnnotations(t *testing.T) {
	client := fake.NewClientset()
	nodeClient := client.CoreV1().Nodes()
	tests := []struct {
		name    string
		cs      clientv1.NodeInterface
		node    string
		patch   KVPatch
		wantErr bool
	}{
		{
			name:  "normal",
			cs:    nodeClient,
			node:  "node1",
			patch: KVPatch{"key1": "value1"},
		},
		{
			name:  "nil patch",
			cs:    nodeClient,
			node:  "node2",
			patch: KVPatch{},
		},
		{
			name:    "patch with unsupported value type",
			cs:      nodeClient,
			node:    "node3",
			patch:   KVPatch{"callback": func() {}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		// create a node
		node, err := client.CoreV1().Nodes().Create(context.Background(), &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: tt.node,
			},
		}, metav1.CreateOptions{})
		require.NoError(t, err)
		require.NotNil(t, node)
		t.Run(tt.name, func(t *testing.T) {
			if err = PatchAnnotations(tt.cs, tt.node, tt.patch); (err != nil) != tt.wantErr {
				t.Errorf("PatchAnnotations() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestPatchLabels(t *testing.T) {
	client := fake.NewClientset()
	nsClient := client.CoreV1().Namespaces()
	tests := []struct {
		name      string
		cs        clientv1.NamespaceInterface
		namespace string
		patch     KVPatch
		wantErr   bool
	}{
		{
			name:      "normal",
			cs:        nsClient,
			namespace: "ns1",
			patch:     KVPatch{"key1": "value1"},
		},
		{
			name:      "nil patch",
			cs:        nsClient,
			namespace: "ns2",
			patch:     nil,
		},
		{
			name:      "patch with unsupported value type",
			cs:        nsClient,
			namespace: "ns3",
			patch:     KVPatch{"callback": func() {}},
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		// create a node
		node, err := client.CoreV1().Namespaces().Create(context.Background(), &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: tt.namespace,
			},
		}, metav1.CreateOptions{})
		require.NoError(t, err)
		require.NotNil(t, node)
		t.Run(tt.name, func(t *testing.T) {
			if err = PatchLabels(tt.cs, tt.namespace, tt.patch); (err != nil) != tt.wantErr {
				t.Errorf("PatchLabels() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestGenerateStrategicMergePatchPayload(t *testing.T) {
	type C chan struct{}
	type unsupportedType struct {
		C
		*v1.Pod
	}

	type args struct {
		// original stands for the original object we seen before we handle
		original runtime.Object
		// modified stands for the modified object
		modified runtime.Object
		// remote stands for the latest object in the server before apply patch
		remote runtime.Object
	}
	tests := []struct {
		name    string
		args    args
		want    runtime.Object
		wantErr bool
	}{
		{
			name: "base",
			args: args{
				original: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
				modified: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"ovn1": "1", "ovn2": "2"}}},
				remote:   &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
			},
			want:    &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"ovn1": "1", "ovn2": "2"}}},
			wantErr: false,
		},
		{
			name: "baseWithRemote",
			args: args{
				original: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
				modified: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"ovn1": "1", "ovn2": "2"}}},
				remote:   &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"calico1": "1"}}},
			},
			want:    &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"calico1": "1", "ovn1": "1", "ovn2": "2"}}},
			wantErr: false,
		},
		{
			name: "baseWithoutAll",
			args: args{
				original: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
				modified: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
				remote:   &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
			},
			want:    &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: nil}},
			wantErr: false,
		},
		{
			name: "baseWithoutModified",
			args: args{
				original: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"calico1": "1"}}},
				modified: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
				remote:   &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"calico1": "1"}}},
			},
			want:    &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: nil}},
			wantErr: false,
		},
		{
			name: "argument original is of unsupported type",
			args: args{
				original: &unsupportedType{},
				modified: &v1.Pod{},
			},
			wantErr: true,
		},
		{
			name: "argument modified is of unsupported type",
			args: args{
				original: &v1.Pod{},
				modified: &unsupportedType{},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateStrategicMergePatchPayload(tt.args.original, tt.args.modified)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateStrategicMergePatchPayload() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}

			b, _ := json.Marshal(tt.args.remote)
			// apply patch for remote obj
			newB, _ := strategicpatch.StrategicMergePatch(b, got, v1.Pod{})
			patchedPod := v1.Pod{}
			_ = json.Unmarshal(newB, &patchedPod)
			if !assert.Equal(t, tt.want, runtime.Object(&patchedPod), "patch: %s", got) {
				t.Errorf("patch not correct, got = %+v, want= %+v", patchedPod, tt.want)
			}
		})
	}
}

func TestGenerateMergePatchPayload(t *testing.T) {
	type C chan struct{}
	type unsupportedType struct {
		C
		*v1.Pod
	}

	type args struct {
		// original stands for the original object we seen before we handle
		original runtime.Object
		// modified stands for the modified object
		modified runtime.Object
		// remote stands for the latest object in the server before apply patch
		remote runtime.Object
	}
	tests := []struct {
		name    string
		args    args
		want    runtime.Object
		wantErr bool
	}{
		{
			name: "base",
			args: args{
				original: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
				modified: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"ovn1": "1", "ovn2": "2"}}},
				remote:   &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
			},
			want:    &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"ovn1": "1", "ovn2": "2"}}},
			wantErr: false,
		},
		{
			name: "baseWithRemote",
			args: args{
				original: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
				modified: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"ovn1": "1", "ovn2": "2"}}},
				remote:   &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"calico1": "1"}}},
			},
			want:    &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"calico1": "1", "ovn1": "1", "ovn2": "2"}}},
			wantErr: false,
		},
		{
			name: "baseWithoutAll",
			args: args{
				original: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
				modified: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
				remote:   &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
			},
			want:    &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: nil}},
			wantErr: false,
		},
		{
			name: "baseWithoutModified",
			args: args{
				original: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"calico1": "1"}}},
				modified: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
				remote:   &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"calico1": "1"}}},
			},
			want:    &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: nil}},
			wantErr: false,
		},
		{
			name: "argument original is of unsupported type",
			args: args{
				original: &unsupportedType{},
				modified: &v1.Pod{},
			},
			wantErr: true,
		},
		{
			name: "argument modified is of unsupported type",
			args: args{
				original: &v1.Pod{},
				modified: &unsupportedType{},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateMergePatchPayload(tt.args.original, tt.args.modified)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateMergePatchPayload() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}

			b, _ := json.Marshal(tt.args.remote)
			// apply patch for remote obj
			newB, _ := strategicpatch.StrategicMergePatch(b, got, v1.Pod{})
			patchedPod := v1.Pod{}
			_ = json.Unmarshal(newB, &patchedPod)
			if !assert.Equal(t, tt.want, runtime.Object(&patchedPod), "patch: %s", got) {
				t.Errorf("patch not correct, got = %+v, want= %+v", patchedPod, tt.want)
			}
		})
	}
}

func TestFailedGenerateStrategicMergePatchPayload(t *testing.T) {
	// test original and modified object are nil
	got, err := GenerateStrategicMergePatchPayload(nil, nil)
	require.Error(t, err)
	require.Nil(t, got)
}
