package libovsdb

import (
	"encoding/json"
	"log"
	"testing"
)

func TestOpRowSerialization(t *testing.T) {
	operation := Operation{
		Op:    "insert",
		Table: "Bridge",
	}

	operation.Row = make(map[string]interface{})
	operation.Row["name"] = "docker-ovs"

	str, err := json.Marshal(operation)

	if err != nil {
		log.Fatal("serialization error:", err)
	}

	expected := `{"op":"insert","table":"Bridge","row":{"name":"docker-ovs"}}`

	if string(str) != expected {
		t.Error("Expected: ", expected, "Got", string(str))
	}
}

func TestOpRowsSerialization(t *testing.T) {
	operation := Operation{
		Op:    "insert",
		Table: "Interface",
	}

	iface1 := make(map[string]interface{})
	iface1["name"] = "test-iface1"
	iface1["mac"] = "0000ffaaaa"
	iface1["ofport"] = 1

	iface2 := make(map[string]interface{})
	iface2["name"] = "test-iface2"
	iface2["mac"] = "0000ffaabb"
	iface2["ofport"] = 2

	operation.Rows = []map[string]interface{}{iface1, iface2}

	str, err := json.Marshal(operation)

	if err != nil {
		log.Fatal("serialization error:", err)
	}

	expected := `{"op":"insert","table":"Interface","rows":[{"mac":"0000ffaaaa","name":"test-iface1","ofport":1},{"mac":"0000ffaabb","name":"test-iface2","ofport":2}]}`

	if string(str) != expected {
		t.Error("Expected: ", expected, "Got", string(str))
	}
}

func TestValidateOvsSet(t *testing.T) {
	goSlice := []int{1, 2, 3, 4}
	oSet, err := NewOvsSet(goSlice)
	if err != nil {
		t.Error("Error creating OvsSet ", err)
	}
	data, err := json.Marshal(oSet)
	if err != nil {
		t.Error("Error Marshalling OvsSet", err)
	}
	expected := `["set",[1,2,3,4]]`
	if string(data) != expected {
		t.Error("Expected: ", expected, "Got", string(data))
	}
	// Negative condition test
	integer := 5
	oSet, err = NewOvsSet(integer)
	if err == nil {
		t.Error("OvsSet must fail for anything other than Slices")
		t.Error("Expected: ", expected, "Got", string(data))
	}
}

func TestValidateOvsMap(t *testing.T) {
	myMap := make(map[int]string)
	myMap[1] = "hello"
	myMap[2] = "world"
	oMap, err := NewOvsMap(myMap)
	if err != nil {
		t.Error("Error creating OvsMap ", err)
	}
	data, err := json.Marshal(oMap)
	if err != nil {
		t.Error("Error Marshalling OvsMap", err)
	}
	expected1 := `["map",[[1,"hello"],[2,"world"]]]`
	expected2 := `["map",[[2,"world"],[1,"hello"]]]`
	if string(data) != expected1 && string(data) != expected2 {
		t.Error("Expected: ", expected1, "Got", string(data))
	}
	// Negative condition test
	integer := 5
	oMap, err = NewOvsMap(integer)
	if err == nil {
		t.Error("OvsMap must fail for anything other than Maps")
	}
}

func TestValidateUuid(t *testing.T) {
	uuid1 := UUID{"this is a bad uuid"}                   // Bad
	uuid2 := UUID{"alsoabaduuid"}                         // Bad
	uuid3 := UUID{"550e8400-e29b-41d4-a716-446655440000"} // Good
	uuid4 := UUID{"thishoul-dnot-pass-vali-dationchecks"} // Bad

	err := uuid1.validateUUID()

	if err == nil {
		t.Error(uuid1, " is not a valid UUID")
	}

	err = uuid2.validateUUID()

	if err == nil {
		t.Error(uuid2, " is not a valid UUID")
	}

	err = uuid3.validateUUID()

	if err != nil {
		t.Error(uuid3, " is a valid UUID")
	}

	err = uuid4.validateUUID()

	if err == nil {
		t.Error(uuid4, " is not a valid UUID")
	}
}

func TestNewUUID(t *testing.T) {
	uuid := UUID{"550e8400-e29b-41d4-a716-446655440000"}
	uuidStr, _ := json.Marshal(uuid)
	expected := `["uuid","550e8400-e29b-41d4-a716-446655440000"]`
	if string(uuidStr) != expected {
		t.Error("uuid is not correctly formatted")
	}
}

func TestNewNamedUUID(t *testing.T) {
	uuid := UUID{"test-uuid"}
	uuidStr, _ := json.Marshal(uuid)
	expected := `["named-uuid","test-uuid"]`
	if string(uuidStr) != expected {
		t.Error("uuid is not correctly formatted")
	}
}

func TestNewCondition(t *testing.T) {
	cond := NewCondition("uuid", "==", "550e8400-e29b-41d4-a716-446655440000")
	condStr, _ := json.Marshal(cond)
	expected := `["uuid","==","550e8400-e29b-41d4-a716-446655440000"]`
	if string(condStr) != expected {
		t.Error("condition is not correctly formatted")
	}
}

func TestNewMutation(t *testing.T) {
	mutation := NewMutation("column", "+=", 1)
	mutationStr, _ := json.Marshal(mutation)
	expected := `["column","+=",1]`
	if string(mutationStr) != expected {
		t.Error("mutation is not correctly formatted")
	}
}
