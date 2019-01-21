package libovsdb

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestNewGetSchemaArgs(t *testing.T) {
	database := "Open_vSwitch"
	args := NewGetSchemaArgs(database)
	argString, _ := json.Marshal(args)
	expected := `["Open_vSwitch"]`
	if string(argString) != expected {
		t.Error("Expected: ", expected, " Got: ", string(argString))
	}
}

func TestNewTransactArgs(t *testing.T) {
	database := "Open_vSwitch"
	operation := Operation{Op: "insert", Table: "Bridge"}
	args := NewTransactArgs(database, operation)
	argString, _ := json.Marshal(args)
	expected := `["Open_vSwitch",{"op":"insert","table":"Bridge"}]`
	if string(argString) != expected {
		t.Error("Expected: ", expected, " Got: ", string(argString))
	}
}

func TestNewMultipleTransactArgs(t *testing.T) {
	database := "Open_vSwitch"
	operation1 := Operation{Op: "insert", Table: "Bridge"}
	operation2 := Operation{Op: "delete", Table: "Bridge"}
	args := NewTransactArgs(database, operation1, operation2)
	argString, _ := json.Marshal(args)
	expected := `["Open_vSwitch",{"op":"insert","table":"Bridge"},{"op":"delete","table":"Bridge"}]`
	if string(argString) != expected {
		t.Error("Expected: ", expected, " Got: ", string(argString))
	}
}

func TestNewCancelArgs(t *testing.T) {
	id := 1
	args := NewCancelArgs(id)
	argString, _ := json.Marshal(args)
	expected := `[1]`
	if string(argString) != expected {
		t.Error("Expected: ", expected, " Got: ", string(argString))
	}
}

func TestNewMonitorArgs(t *testing.T) {
	database := "Open_vSwitch"
	value := 1
	r := MonitorRequest{
		Columns: []string{"name", "ports", "external_ids"},
		Select: MonitorSelect{
			Initial: true,
			Insert:  true,
			Delete:  true,
			Modify:  true,
		},
	}
	requests := make(map[string]MonitorRequest)
	requests["Bridge"] = r

	args := NewMonitorArgs(database, value, requests)
	argString, _ := json.Marshal(args)
	expected := `["Open_vSwitch",1,{"Bridge":{"columns":["name","ports","external_ids"],"select":{"initial":true,"insert":true,"delete":true,"modify":true}}}]`
	if string(argString) != expected {
		t.Error("Expected: ", expected, " Got: ", string(argString))
	}
}

func TestNewMonitorCancelArgs(t *testing.T) {
	value := 1
	args := NewMonitorCancelArgs(value)
	argString, _ := json.Marshal(args)
	expected := `[1]`
	if string(argString) != expected {
		t.Error("Expected: ", expected, " Got: ", string(argString))
	}
}

func TestNewLockArgs(t *testing.T) {
	id := "testId"
	args := NewLockArgs(id)
	argString, _ := json.Marshal(args)
	expected := `["testId"]`
	if string(argString) != expected {
		t.Error("Expected: ", expected, " Got: ", string(argString))
	}
}

func TestEcho(t *testing.T) {
	req := []interface{}{"hi"}
	var reply []interface{}
	echo(nil, req, &reply)
	if !reflect.DeepEqual(req, reply) {
		t.Error("Expected: ", req, " Got: ", reply)
	}
}

func TestUpdate(t *testing.T) {
	var reply interface{}

	// Update notification should fail for arrays of size < 2
	err := update(nil, []interface{}{"hello"}, &reply)
	if err == nil {
		t.Error("Expected: error for a dummy request")
	}

	// Update notification should fail if arg[1] is not map[string]map[string]RowUpdate type
	err = update(nil, []interface{}{"hello", "gophers"}, &reply)
	if err == nil {
		t.Error("Expected: error for a dummy request")
	}

	// Valid dummy update should pass
	validUpdate := make(map[string]interface{})
	validRowUpdate := make(map[string]RowUpdate)
	validRowUpdate["uuid"] = RowUpdate{}
	validUpdate["table"] = validRowUpdate

	err = update(nil, []interface{}{"hello", validUpdate}, &reply)
	if err != nil {
		t.Error(err)
	}
}
