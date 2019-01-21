package main

import (
	"fmt"
	"os"

	"github.com/oilbeater/libovsdb"
)

func main() {
	ovs, err := libovsdb.Connect("127.0.0.1", 6641)
	if err != nil {
		fmt.Println("Unable to Connect ", err)
		os.Exit(1)
	}
	fmt.Println(createPort("test-ovn", ovs))
}

func createSwitch(name string, ovs *libovsdb.OvsdbClient) error {
	bridge := make(map[string]interface{})
	bridge["name"] = name
	insertOp := libovsdb.Operation{
		Op:       "insert",
		Table:    "Logical_Switch",
		Row:      bridge,
		UUIDName: "ovntest",
	}
	reply, err := ovs.Transact("OVN_Northbound", insertOp)
	if err != nil {
		return err
	}
	for _, r := range reply {
		fmt.Println(r)
	}
	return nil
}

func deleteSwitch(name string, ovs *libovsdb.OvsdbClient) error {
	deleteOp := libovsdb.Operation{
		Op:    "delete",
		Table: "Logical_Switch",
		Where: []interface{}{libovsdb.NewCondition("name", "==", "test-ovn")},
	}
	_, err := ovs.Transact("OVN_Northbound", deleteOp)
	return err
}

func createPort(name string, ovs *libovsdb.OvsdbClient) error {
	bridge := make(map[string]interface{})
	bridge["name"] = "ovn-1"
	insertOp := libovsdb.Operation{
		Op:       "insert",
		Table:    "Logical_Switch_Port",
		Row:      bridge,
		UUIDName: "ovntest",
	}

	mutateUUID := []libovsdb.UUID{{GoUUID: "ovntest"}}
	mutateSet, _ := libovsdb.NewOvsSet(mutateUUID)
	mutation := libovsdb.NewMutation("ports", "insert", mutateSet)
	condition := libovsdb.NewCondition("name", "==", "test-ovn")
	mutateOp := libovsdb.Operation{
		Op:        "mutate",
		Table:     "Logical_Switch",
		Mutations: []interface{}{mutation},
		Where:     []interface{}{condition},
	}
	_, err := ovs.Transact("OVN_Northbound", insertOp, mutateOp)
	return err
}
