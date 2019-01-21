package controller

import (
	"encoding/json"
	"fmt"
	"github.com/emicklei/go-restful"
	"github.com/fatih/structs"
	"github.com/oilbeater/libovsdb"
	"k8s.io/klog"
	"net/http"
	"strings"
	"time"
)

type OvnHandler struct {
	Config    *Configuration
	OvsClient *libovsdb.OvsdbClient
}

func CreateOvnHandler(config *Configuration) (*OvnHandler, error) {
	var ovs *libovsdb.OvsdbClient
	var err error
	if config.OvnNbSocket != "" {
		ovs, err = libovsdb.ConnectWithUnixSocket(config.OvnNbSocket)
		if err != nil {
			return nil, err
		}
	} else {
		ovs, err = libovsdb.Connect(config.OvnNbHost, config.OvnNbPort)
		if err != nil {
			return nil, err
		}
	}
	return &OvnHandler{OvsClient: ovs}, nil
}

func (oh *OvnHandler) handleListSwitch(request *restful.Request, response *restful.Response) {
	selectOp := libovsdb.Operation{
		Op:    "select",
		Table: "Logical_Switch",
		Where: []interface{}{},
	}
	reply, err := oh.OvsClient.Transact("OVN_Northbound", selectOp)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(len(reply))
	var logicalSwitches []libovsdb.LogicalSwitch
	for _, r := range reply {
		fmt.Println(r.UUID, r.Count, r.Details)
		logicalSwitches = make([]libovsdb.LogicalSwitch, 0, len(r.Rows))
		for _, i := range r.Rows {
			fmt.Println(i)
			ls := libovsdb.LogicalSwitch{}
			bs, _ := json.Marshal(i)
			json.Unmarshal(bs, &ls)
			logicalSwitches = append(logicalSwitches, ls)
		}
	}
	response.WriteHeaderAndEntity(http.StatusOK, logicalSwitches)
	return
}

func (oh *OvnHandler) handleGetSwitch(request *restful.Request, response *restful.Response) {
	return
}

type CreateSwitchRequest struct {
	Name       string   `json:"name"`
	Subnet     string   `json:"subnet"`
	ExcludeIps []string `json:"exclude_ips"`
}

func (oh *OvnHandler) handleCreateSwitch(request *restful.Request, response *restful.Response) {
	payload := CreateSwitchRequest{}
	err := request.ReadEntity(&payload)
	if err != nil {
		response.WriteHeaderAndEntity(http.StatusBadRequest, err)
		return
	}

	ls := libovsdb.LogicalSwitch{Name: payload.Name, OtherConfig: libovsdb.OvsMap{GoMap: map[interface{}]interface{}{}}}
	if payload.Subnet != "" {
		ls.OtherConfig.GoMap["subnet"] = payload.Subnet
	}
	if payload.ExcludeIps != nil {
		excludeIps := strings.Join(payload.ExcludeIps, " ")
		ls.OtherConfig.GoMap["exclude_ips"] = excludeIps
	}

	insertOp := libovsdb.Operation{
		Op:       "insert",
		Table:    "Logical_Switch",
		Row:      structs.Map(ls),
		UUIDName: "insertSwitch",
	}
	raw, err := insertOp.MarshalJSON()
	fmt.Println(string(raw), err)
	_, err = oh.OvsClient.Transact("OVN_Northbound", insertOp)
	if err != nil {
		response.WriteHeaderAndEntity(http.StatusInternalServerError, err)
		return
	}

	return
}

func (oh *OvnHandler) handleUpdateSwitch(request *restful.Request, response *restful.Response) {
	return
}

func (oh *OvnHandler) handleDeleteSwitch(request *restful.Request, response *restful.Response) {
	return
}

func (oh *OvnHandler) handleListPort(request *restful.Request, response *restful.Response) {
	return
}

func (oh *OvnHandler) handleGetPort(request *restful.Request, response *restful.Response) {
	return
}

type CreatePortRequest struct {
	Name   string `json:"name"`
	Switch string `json:"switch"`
}

func (oh *OvnHandler) handleCreatePort(request *restful.Request, response *restful.Response) {
	payload := CreatePortRequest{}
	err := request.ReadEntity(&payload)
	if err != nil {
		response.WriteHeaderAndEntity(http.StatusBadRequest, err)
		return
	}
	klog.Infof("create port request %v", payload)

	// TODO: if port exists return old one
	port := make(map[string]interface{})
	port["name"] = payload.Name
	port["addresses"] = "dynamic"
	port["enabled"] = true
	insertOp := libovsdb.Operation{
		Op:       "insert",
		Table:    "Logical_Switch_Port",
		Row:      port,
		UUIDName: "ovntest",
	}
	raw, err := insertOp.MarshalJSON()
	fmt.Println(string(raw), err)

	mutateUUID := []libovsdb.UUID{{GoUUID: "ovntest"}}
	mutateSet, _ := libovsdb.NewOvsSet(mutateUUID)
	mutation := libovsdb.NewMutation("ports", "insert", mutateSet)
	condition := libovsdb.NewCondition("name", "==", payload.Switch)
	mutateOp := libovsdb.Operation{
		Op:        "mutate",
		Table:     "Logical_Switch",
		Mutations: []interface{}{mutation},
		Where:     []interface{}{condition},
	}

	//TODO: should check reply error
	res, err := oh.OvsClient.Transact("OVN_Northbound", insertOp, mutateOp)
	if err != nil {
		response.WriteHeaderAndEntity(http.StatusInternalServerError, err)
		return
	}
	klog.Infof("insert raw data from ovn-nb %v", res)
	time.Sleep(3 * time.Second)
	condition = libovsdb.NewCondition("name", "==", payload.Name)
	selectOp := libovsdb.Operation{
		Op:    "select",
		Table: "Logical_Switch_Port",
		Where: []interface{}{condition},
	}
	res, err = oh.OvsClient.Transact("OVN_Northbound", selectOp)
	if err != nil {
		klog.Errorf("select transaction failed %v", err)
		response.WriteHeaderAndEntity(http.StatusInternalServerError, err)
		return
	}
	klog.Infof("ovn return data %v", res)
	if res[0].Error != "" {
		klog.Errorf("insert into ovn-nb failed %v", res[0].Error)
		response.WriteHeaderAndEntity(http.StatusInternalServerError, err)
		return
	}
	rows := res[0].Rows
	if len(rows) != 1 {
		klog.Errorf("crated port not found")
		response.WriteHeaderAndEntity(http.StatusInternalServerError, err)
		return
	}

	lsp := libovsdb.LogicalSwitchPort{}
	klog.Infof("returned port row %v", rows[0])
	bs, _ := json.Marshal(rows[0])
	err = json.Unmarshal(bs, &lsp)
	if err != nil {
		klog.Errorf("json unmarshal failed %v", err)
		response.WriteHeaderAndEntity(http.StatusInternalServerError, err)
		return
	}

	fil := AddPortResponse{
		IpAddress:  strings.Split(lsp.DynamicAddresses, " ")[1],
		MacAddress: strings.Split(lsp.DynamicAddresses, " ")[0],
		CIDR:       "10.16.0.0/16",
		Gateway:    "10.16.0.1",
	}

	response.WriteHeaderAndEntity(http.StatusOK, fil)
	return
}

func (oh *OvnHandler) handleUpdatePort(request *restful.Request, response *restful.Response) {
	return
}

func (oh *OvnHandler) handleDeletePort(request *restful.Request, response *restful.Response) {
	name := request.PathParameter("name")

	selectCondition := libovsdb.NewCondition("name", "==", name)
	selectOp := libovsdb.Operation{
		Op:    "select",
		Table: "Logical_Switch_Port",
		Where: []interface{}{selectCondition},
	}
	raw, err := selectOp.MarshalJSON()
	fmt.Println(string(raw), err)
	reply, err := oh.OvsClient.Transact("OVN_Northbound", selectOp)
	if err != nil {
		response.WriteHeaderAndEntity(http.StatusInternalServerError, err)
		return
	}
	if len(reply[0].Rows) == 0 {
		response.WriteHeader(http.StatusNoContent)
		return
	}

	// TODO: also need to clean PortGroup table
	portUuid := reply[0].Rows[0]["_uuid"]
	mutationCondition := libovsdb.NewCondition("ports", "includes", portUuid)
	mutation := libovsdb.NewMutation("ports", "delete", portUuid)
	mutateOp := libovsdb.Operation{
		Op:        "mutate",
		Table:     "Logical_Switch",
		Mutations: []interface{}{mutation},
		Where:     []interface{}{mutationCondition},
	}
	raw, err = mutateOp.MarshalJSON()
	fmt.Println(string(raw), err)

	deleteOp := libovsdb.Operation{
		Op:    "delete",
		Table: "Logical_Switch_Port",
		Where: []interface{}{selectCondition},
	}
	raw, err = deleteOp.MarshalJSON()
	fmt.Println(string(raw), err)

	reply, err = oh.OvsClient.Transact("OVN_Northbound", mutateOp, deleteOp)
	if err != nil {
		response.WriteHeaderAndEntity(http.StatusInternalServerError, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
	return
}

type AddPortResponse struct {
	ID         string
	IpAddress  string
	MacAddress string
	CIDR       string
	Gateway    string
}
