package v1

import (
	"encoding/json"
	"testing"

	kotypes "github.com/kubeovn/kube-ovn/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestSubnetStatusBigIntJSONSerialization 验证 SubnetStatus 的 BigInt 字段正确序列化为 JSON 字符串
func TestSubnetStatusBigIntJSONSerialization(t *testing.T) {
	subnet := &Subnet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Subnet",
			APIVersion: "kubeovn.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-subnet",
		},
		Spec: SubnetSpec{
			CIDRBlock: "10.16.0.0/16",
		},
		Status: SubnetStatus{
			V4AvailableIPs: kotypes.NewBigInt(65533),
			V4UsingIPs:     kotypes.NewBigInt(3),
			V6AvailableIPs: kotypes.NewBigInt(0),
			V6UsingIPs:     kotypes.NewBigInt(0),
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(subnet)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	jsonStr := string(data)
	t.Logf("Serialized Subnet JSON (truncated): %s", jsonStr[:min(200, len(jsonStr))])

	// Verify the status field contains quoted strings
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}

	status, ok := decoded["status"].(map[string]interface{})
	if !ok {
		t.Fatal("status field not found or not an object")
	}

	// Check each BigInt field is a string
	checkStringField := func(name string, expected string) {
		value, ok := status[name]
		if !ok {
			t.Errorf("Field %s not found in status", name)
			return
		}
		strValue, ok := value.(string)
		if !ok {
			t.Errorf("Field %s should be string, got %T: %v", name, value, value)
			return
		}
		if strValue != expected {
			t.Errorf("Field %s = %q, want %q", name, strValue, expected)
		}
	}

	checkStringField("v4availableIPs", "65533")
	checkStringField("v4usingIPs", "3")
	checkStringField("v6availableIPs", "0")
	checkStringField("v6usingIPs", "0")

	// Unmarshal back to Subnet
	var decodedSubnet Subnet
	if err := json.Unmarshal(data, &decodedSubnet); err != nil {
		t.Fatalf("Unmarshal to Subnet failed: %v", err)
	}

	// Verify values match
	if !decodedSubnet.Status.V4AvailableIPs.Equal(subnet.Status.V4AvailableIPs) {
		t.Errorf("V4AvailableIPs mismatch after round-trip: got %v, want %v",
			decodedSubnet.Status.V4AvailableIPs.String(), subnet.Status.V4AvailableIPs.String())
	}
}

// TestSubnetStatusPatchJSON 模拟 Kubernetes patch 操作的 JSON 序列化
func TestSubnetStatusPatchJSON(t *testing.T) {
	status := SubnetStatus{
		V4AvailableIPs: kotypes.NewBigInt(253),
		V4UsingIPs:     kotypes.NewBigInt(1),
		V6AvailableIPs: kotypes.NewBigInt(0),
		V6UsingIPs:     kotypes.NewBigInt(0),
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	t.Logf("SubnetStatus JSON: %s", string(data))

	// Parse as generic map to verify field types
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}

	// All BigInt fields must be strings, not numbers
	for fieldName, expectedValue := range map[string]string{
		"v4availableIPs": "253",
		"v4usingIPs":     "1",
		"v6availableIPs": "0",
		"v6usingIPs":     "0",
	} {
		value, ok := m[fieldName]
		if !ok {
			t.Errorf("Field %s not found", fieldName)
			continue
		}
		strValue, ok := value.(string)
		if !ok {
			t.Errorf("Field %s should be string (for K8s CRD validation), got %T: %v",
				fieldName, value, value)
			continue
		}
		if strValue != expectedValue {
			t.Errorf("Field %s = %q, want %q", fieldName, strValue, expectedValue)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
