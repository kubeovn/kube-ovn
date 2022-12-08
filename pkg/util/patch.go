package util

import (
	jsonpatch "github.com/evanphx/json-patch/v5"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
)

func GenerateStrategicMergePatchPayload(original, modified runtime.Object) ([]byte, error) {
	originalJSON, err := json.Marshal(original)
	if err != nil {
		return nil, err
	}

	modifiedJSON, err := json.Marshal(modified)
	if err != nil {
		return nil, err
	}

	data, err := createStrategicMergePatch(originalJSON, modifiedJSON, modified)
	if err != nil {
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
		return nil, err
	}

	modifiedJSON, err := json.Marshal(modified)
	if err != nil {
		return nil, err
	}

	data, err := createMergePatch(originalJSON, modifiedJSON, modified)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func createMergePatch(originalJSON, modifiedJSON []byte, _ interface{}) ([]byte, error) {
	return jsonpatch.CreateMergePatch(originalJSON, modifiedJSON)
}
