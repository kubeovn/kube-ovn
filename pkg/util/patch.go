package util

import (
	"context"

	jsonpatch "github.com/evanphx/json-patch/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/klog/v2"
)

type KVPatch map[string]any

type patchClient[T metav1.Object] interface {
	Patch(ctx context.Context, name string, patchType types.PatchType, patch []byte, opt metav1.PatchOptions, subresources ...string) (T, error)
}

func patchMetaKVs[T metav1.Object](cs patchClient[T], name, field string, patch KVPatch) error {
	obj := map[string]map[string]KVPatch{"metadata": {field: patch}}
	patchData, err := json.Marshal(obj)
	if err != nil {
		klog.Errorf("failed to marshal patch %#v for field .metadata.%s: %v", patch, field, err)
		return err
	}

	_, err = cs.Patch(context.Background(), name, types.MergePatchType, patchData, metav1.PatchOptions{})
	if err != nil {
		klog.Errorf("failed to patch resource %s with json merge patch %q: %v", name, string(patchData), err)
		return err
	}
	return nil
}

func PatchLabels[T metav1.Object](cs patchClient[T], name string, patch KVPatch) error {
	return patchMetaKVs(cs, name, "labels", patch)
}

func PatchAnnotations[T metav1.Object](cs patchClient[T], name string, patch KVPatch) error {
	return patchMetaKVs(cs, name, "annotations", patch)
}

func GenerateStrategicMergePatchPayload(original, modified runtime.Object) ([]byte, error) {
	originalJSON, err := json.Marshal(original)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	modifiedJSON, err := json.Marshal(modified)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	data, err := createStrategicMergePatch(originalJSON, modifiedJSON, modified)
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	return data, nil
}

func createStrategicMergePatch(originalJSON, modifiedJSON []byte, dataStruct interface{}) ([]byte, error) {
	return strategicpatch.CreateTwoWayMergePatch(originalJSON, modifiedJSON, dataStruct)
}

func GenerateMergePatchPayload(original, modified runtime.Object) ([]byte, error) {
	originalJSON, err := json.Marshal(original)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	modifiedJSON, err := json.Marshal(modified)
	if err != nil {
		klog.Error(err)
		return nil, err
	}

	data, err := createMergePatch(originalJSON, modifiedJSON, modified)
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	return data, nil
}

func createMergePatch(originalJSON, modifiedJSON []byte, _ interface{}) ([]byte, error) {
	return jsonpatch.CreateMergePatch(originalJSON, modifiedJSON)
}
