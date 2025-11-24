package ovs

import (
	"context"
	"errors"
	"fmt"
	"strings"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"

	"github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func (c *OVNNbClient) AddQos(vpcName, externalSubnetName, v4Eip string, burstMax, rateMax int, direction string) error {
	qos := c.newQos(vpcName, externalSubnetName, v4Eip, burstMax, rateMax, direction)
	return c.CreateQos(vpcName, externalSubnetName, qos)
}

func (c *OVNNbClient) newQos(vpcName, externalSubnetName, v4Eip string, burstMax, rateMax int, direction string) *ovnnb.QoS {
	externalPort := fmt.Sprintf("%s-%s", externalSubnetName, vpcName)
	routerCrPort := fmt.Sprintf("cr-%s-%s", vpcName, externalSubnetName)
	qos := &ovnnb.QoS{
		UUID:      ovsclient.NamedUUID(),
		Action:    map[string]int{},
		Bandwidth: map[string]int{"rate": rateMax, "burst": burstMax}, //
		Direction: direction,
		Priority:  2003,
		// Match:       "ip4.src == " + v4Eip + " && " + "inport == \"" + externalPort + "\"",
	}
	if direction == "from-lport" {
		qos.Match = fmt.Sprintf("ip4.src == %s && inport == \"%s\" && is_chassis_resident(\"%s\")", v4Eip, externalPort, routerCrPort)
	}
	if direction == "to-lport" {
		qos.Match = fmt.Sprintf("ip4.dst == %s && outport == \"%s\" && is_chassis_resident(\"%s\")", v4Eip, externalPort, routerCrPort)
	}

	return qos
}

func (c *OVNNbClient) CreateQos(vpcName, lsName string, qosList ...*ovnnb.QoS) error {
	externalPort := fmt.Sprintf("%s-%s", lsName, vpcName)
	models := make([]model.Model, 0, len(qosList))
	qosUUIDs := make([]string, 0, len(qosList))
	for _, qos := range qosList {
		if qos != nil {
			models = append(models, model.Model(qos))
			qosUUIDs = append(qosUUIDs, qos.UUID)
		}
	}

	createQoSOp, err := c.Create(models...)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for creating qosList: %w", err)
	}

	qosAddOps, err := c.QoSOp(lsName, qosUUIDs, ovsdb.MutateOperationInsert)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for adding qos to logicalSwitch %s: %w", lsName, err)
	}

	ops := make([]ovsdb.Operation, 0, len(createQoSOp)+len(qosAddOps))
	ops = append(ops, createQoSOp...)
	ops = append(ops, qosAddOps...)

	// klog.Infof("ops: %v", ops)

	if err = c.Transact("qos-add", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("add qos to %s: %w", externalPort, err)
	}

	return nil
}

func (c *OVNNbClient) UpdateQos(vpcName, externalSubnetName, v4Eip string, burstMax, rateMax int, direction string) error {
	klog.Info("update qos for vpc: ", vpcName, "externalSubnetName:", externalSubnetName, "v4Eip: ", v4Eip, " burstMax: ", burstMax, " rateMax: ", rateMax, " direction: ", direction)
	klog.Infof("before update qos, now delete exists")
	externalPort := fmt.Sprintf("%s-%s", externalSubnetName, vpcName)
	routerCrPort := fmt.Sprintf("cr-%s-%s", vpcName, externalSubnetName)
	err := c.deleteLsQosIfExists(externalSubnetName, externalPort, v4Eip, direction, routerCrPort)
	if err != nil {
		klog.Error("delete qos rule failed: ", err)
		return err
	}

	err = c.AddQos(vpcName, externalSubnetName, v4Eip, burstMax, rateMax, direction)
	if err != nil {
		klog.Error("update qos: add qos rule failed: ", err)
		return err
	}
	return nil
}

func (c *OVNNbClient) GetQos(vpcName, externamSubnet, v4Eip, direction string) ([]*ovnnb.QoS, error) {
	// klog.Info("get qos for vpc: ", vpcName, "externalSubnetName:", externamSubnet, "v4Eip: ", v4Eip, " direction: ", direction)
	lsName := externamSubnet
	externalPort := fmt.Sprintf("%s-%s", externamSubnet, vpcName)
	routerCrPort := fmt.Sprintf("cr-%s-%s", vpcName, externamSubnet)
	qos, err := c.getLogicalSwitchQos(lsName, externalPort, v4Eip, direction, routerCrPort)
	if err != nil {
		return nil, err
	}
	return qos, nil
}

func (c *OVNNbClient) getLogicalSwitchQos(lsName, externalPort, v4Eip, direction, routerCrPort string) ([]*ovnnb.QoS, error) {
	fileterFunc := func(qos *ovnnb.QoS) bool {
		if qos.Direction != direction {
			return false
		}
		if qos.Match != "" {
			klog.Infof("qos match: %s, externalPort: %s, routerCrPort: %s", qos.Match, externalPort, routerCrPort)
			// 判断gatewayPort和v4Eip是否在Match字符串中,使用字符串函数来判断
			if strings.Contains(qos.Match, externalPort) && strings.Contains(qos.Match, v4Eip) {
				return true
			}
			return false
		}
		return true
	}
	QoSList, err := c.listLogicalSwitchQosByFilter(lsName, fileterFunc)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("get qos for logicalSwitch %s: %w", lsName, err)
	}
	return QoSList, nil
}

func (c *OVNNbClient) listLogicalSwitchQosByFilter(lsName string, filter func(qos *ovnnb.QoS) bool) ([]*ovnnb.QoS, error) {
	ls, err := c.GetLogicalSwitch(lsName, false)
	if err != nil {
		klog.Error(err)
		return nil, err
	}
	QoSList := make([]*ovnnb.QoS, 0, len(ls.QOSRules))
	for _, uuid := range ls.QOSRules {
		qos, err := c.getQosByUUID(uuid)
		if err != nil {
			if errors.Is(err, client.ErrNotFound) {
				continue
			}
			klog.Error(err)
			return nil, err
		}
		if filter == nil || filter(qos) {
			QoSList = append(QoSList, qos)
		}
	}

	return QoSList, nil
}

func (c *OVNNbClient) getQosByUUID(uuid string) (*ovnnb.QoS, error) {
	obj := &ovnnb.QoS{} // Ensure the correct model type is used
	conditions := []model.Condition{
		{
			Field:    &obj.UUID, // Reference a valid field from the QoS model
			Function: ovsdb.ConditionEqual,
			Value:    uuid, // Replace `uuid` with the actual value
		},
	}
	var result []*ovnnb.QoS
	cond := c.WhereAll(obj, conditions...)
	err := cond.List(context.Background(), &result)
	if err != nil {
		klog.Error(err)
	}

	if len(result) > 0 {
		return result[0], nil
	}
	return nil, client.ErrNotFound
}

func (c *OVNNbClient) deleteQosByUUIDs(lsName string, qosUUIDs []string) error {
	qosDelOps, err := c.QoSOp(lsName, qosUUIDs, ovsdb.MutateOperationDelete)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("generate operations for adding qos to logicalSwitch %s: %w", lsName, err)
	}

	ops := make([]ovsdb.Operation, 0, len(qosDelOps))
	ops = append(ops, qosDelOps...)
	if err = c.Transact("qos-del", ops); err != nil {
		klog.Error(err)
		return fmt.Errorf("add qos to %s: %w", lsName, err)
	}
	return nil
}

func (c OVNNbClient) deleteLsQosIfExists(lsName, externalPort, v4Eip, direction, routerCrPort string) error {
	klog.Info("delete Qos if exists for logicalSwitch: ", lsName, " externalPort: ", externalPort, " v4Eip: ", v4Eip)
	existQos, err := c.getLogicalSwitchQos(lsName, externalPort, v4Eip, direction, routerCrPort)
	if err != nil {
		klog.Errorf("failed to get qos rules for logical switch %s: %v", lsName, err)
		return err
	}
	if len(existQos) > 0 {
		uuids := []string{}
		for _, qos := range existQos {
			uuids = append(uuids, qos.UUID)
		}
		err := c.deleteQosByUUIDs(lsName, uuids)
		if err != nil {
			klog.Error("delete qos by uuids faild: ", err)
		}
	}
	return nil
}

func (c *OVNNbClient) DeleteQos(vpcName, externalSubnetName, v4Eip, direction string) error {
	klog.Info("delete qos for vpc: ", vpcName, "externalSubnetName:", externalSubnetName, "v4Eip: ", v4Eip, " direction: ", direction)
	externalPort := fmt.Sprintf("%s-%s", externalSubnetName, vpcName)
	routerCrPort := fmt.Sprintf("cr-%s-%s", vpcName, externalSubnetName)
	err := c.deleteLsQosIfExists(externalSubnetName, externalPort, v4Eip, direction, routerCrPort)
	if err != nil {
		klog.Error("delete qos rule faild: ", err)
		return err
	}
	return nil
}

// QoSOp create operations about logical switch qos //模仿router_policy写的
func (c *OVNNbClient) QoSOp(lsName string, qosUUIDs []string, op ovsdb.Mutator) ([]ovsdb.Operation, error) {
	if len(qosUUIDs) == 0 {
		return nil, nil
	}
	mutation := func(ls *ovnnb.LogicalSwitch) *model.Mutation {
		mutation := &model.Mutation{
			Field:   &ls.QOSRules,
			Value:   qosUUIDs,
			Mutator: op,
		}

		return mutation
	}

	return c.LogicalSwitchQosOp(lsName, mutation)
}

// LogicalSwitchQosOp create operations about switch qos
func (c *OVNNbClient) LogicalSwitchQosOp(lsName string, mutationsFunc ...func(ls *ovnnb.LogicalSwitch) *model.Mutation) ([]ovsdb.Operation, error) {
	ls, err := c.GetLogicalSwitch(lsName, false)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("get logical switch %s: %w", lsName, err)
	}
	if len(mutationsFunc) == 0 {
		return nil, nil
	}
	mutations := make([]model.Mutation, 0, len(mutationsFunc))
	for _, f := range mutationsFunc {
		mutation := f(ls)

		if mutation != nil {
			mutations = append(mutations, *mutation)
		}
	}
	ops, err := c.ovsDbClient.Where(ls).Mutate(ls, mutations...)
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("generate operations for mutating logical switch %v: %w", ls, err)
	}
	return ops, nil
}
