package util

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
)

func TestGenerateStrategicMergePatchPayload(t *testing.T) {
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
		want    v1.Pod
		wantErr bool
	}{
		{
			name: "base",
			args: args{
				original: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
				modified: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"ovn1": "1", "ovn2": "2"}}},
				remote:   &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
			},
			want:    v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"ovn1": "1", "ovn2": "2"}}},
			wantErr: false,
		},
		{
			name: "baseWithRemote",
			args: args{
				original: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
				modified: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"ovn1": "1", "ovn2": "2"}}},
				remote:   &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"calico1": "1"}}},
			},
			want:    v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"calico1": "1", "ovn1": "1", "ovn2": "2"}}},
			wantErr: false,
		},
		{
			name: "baseWithoutAll",
			args: args{
				original: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
				modified: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
				remote:   &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
			},
			want:    v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: nil}},
			wantErr: false,
		},
		{
			name: "baseWithoutModified",
			args: args{
				original: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"calico1": "1"}}},
				modified: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
				remote:   &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"calico1": "1"}}},
			},
			want:    v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: nil}},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateStrategicMergePatchPayload(tt.args.original, tt.args.modified)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateStrategicMergePatchPayload() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			b, _ := json.Marshal(tt.args.remote)
			// apply patch for remote obj
			newB, _ := strategicpatch.StrategicMergePatch(b, got, v1.Pod{})
			patchedPod := v1.Pod{}
			_ = json.Unmarshal(newB, &patchedPod)
			if !assert.Equal(t, tt.want, patchedPod, "patch: %s", got) {
				t.Errorf("patch not correct, got = %+v, want= %+v", patchedPod, tt.want)
			}
		})
	}
}
