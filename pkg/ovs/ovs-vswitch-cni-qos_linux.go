package ovs

import (
	"fmt"
	"strconv"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *VswitchClient) cniQoSState() ([]vswitch.QoS, []vswitch.Queue, error) {
	results, err := c.transactVswitchOperations([]ovsdb.Operation{selectVswitch(vswitch.QoSTable, nil), selectVswitch(vswitch.QueueTable, nil)})
	if err != nil {
		return nil, nil, err
	}
	qos, err := decodeVswitchRows[vswitch.QoS](c.Schema(), vswitch.QoSTable, results[0].Rows)
	if err != nil {
		return nil, nil, err
	}
	queues, err := decodeVswitchRows[vswitch.Queue](c.Schema(), vswitch.QueueTable, results[1].Rows)
	return qos, queues, err
}

// SetCNIBandwidth preserves priority and other queue settings owned by the
// daemon, while using the same rate and burst units as SetInterfaceBandwidth.
func (c *VswitchClient) SetCNIBandwidth(podName, podNamespace, ifaceID, ingress, egress, ingressBurst, egressBurst string) error {
	ingressKPS, err := parseAndScaleBandwidthRate(ingress, 1000)
	if err != nil {
		return err
	}
	egressBPS, err := parseAndScaleBandwidthRate(egress, 1000000)
	if err != nil {
		return err
	}
	ifaces, err := c.cniInterfaces(ifaceID)
	if err != nil {
		return err
	}
	for _, iface := range ifaces {
		iface.IngressPolicingRate = int(ingressKPS)
		iface.IngressPolicingBurst = int(computeIngressPolicingBurstKbit(ingressKPS, ingressBurst))
		if err := c.updateCNIModel(vswitch.InterfaceTable, iface.UUID, &iface, &iface.IngressPolicingRate, &iface.IngressPolicingBurst); err != nil {
			return err
		}
		if err := c.setCNIEgress(podName, podNamespace, ifaceID, iface.Name, egressBPS, computeHtbBurstBytes(egressBPS, egressBurst)); err != nil {
			return err
		}
	}
	return nil
}

func (c *VswitchClient) setCNIEgress(podName, podNamespace, ifaceID, portName string, rate, burst int64) error {
	qosList, queues, err := c.cniQoSState()
	if err != nil {
		return err
	}
	var qos *vswitch.QoS
	var queue *vswitch.Queue
	for i := range qosList {
		if qosList[i].ExternalIDs["iface-id"] == ifaceID {
			qos = &qosList[i]
			break
		}
	}
	for i := range queues {
		if queues[i].ExternalIDs["iface-id"] == ifaceID {
			queue = &queues[i]
			break
		}
	}
	if rate == 0 {
		if qos == nil || qos.Type != util.HtbQos {
			return nil
		}
		// The bound queue may differ from a stale queue with the same iface-id.
		queue = nil
		for i := range queues {
			if queues[i].UUID == qos.Queues[0] {
				queue = &queues[i]
				break
			}
		}
		if queue == nil {
			return nil
		}
		delete(queue.OtherConfig, "max-rate")
		delete(queue.OtherConfig, "burst")
		patch := cniMapPatch(vswitch.QueueTable, queue.UUID, "other_config", nil, []string{"max-rate", "burst"})
		if len(queue.OtherConfig) != 0 {
			_, err := c.transactVswitchOperations([]ovsdb.Operation{patch})
			return err
		}
		bind, err := c.cniQoSBinding(portName, nil)
		if err != nil {
			return err
		}
		// Do not unbind a queue if the daemon added priority concurrently.
		emptyConfig, _ := ovsdb.NewOvsMap(map[string]string{})
		_, err = c.transactVswitchOperations([]ovsdb.Operation{patch, {
			Op: ovsdb.OperationWait, Table: vswitch.QueueTable, Where: uuidWhere(queue.UUID),
			Columns: []string{"other_config"}, Rows: []ovsdb.Row{{"other_config": emptyConfig}}, Until: "==", Timeout: new(0),
		}, bind})
		if err != nil {
			return err
		}
		return c.ClearCNIQoS(podName, podNamespace, ifaceID)
	}
	config := map[string]string{"max-rate": strconv.FormatInt(rate, 10), "burst": strconv.FormatInt(burst, 10)}
	var ops []ovsdb.Operation
	if queue == nil {
		queue = &vswitch.Queue{UUID: ovsclient.NamedUUID(), ExternalIDs: cniExternalIDs(podName, podNamespace, ifaceID), OtherConfig: config}
		ops, err = c.Create(queue)
		if err != nil {
			return err
		}
	} else {
		ops = append(ops, cniMapPatch(vswitch.QueueTable, queue.UUID, "other_config", config, []string{"max-rate", "burst"}))
	}
	if qos == nil {
		qos = &vswitch.QoS{UUID: ovsclient.NamedUUID(), Type: util.HtbQos, ExternalIDs: cniExternalIDs(podName, podNamespace, ifaceID), Queues: map[int]string{0: queue.UUID}}
		create, err := c.Create(qos)
		if err != nil {
			return err
		}
		ops = append(ops, create...)
	} else {
		qos.Type = util.HtbQos
		// Updating key 0 keeps any other daemon-owned queue bindings.
		queueMap, _ := ovsdb.NewOvsMap(map[int]ovsdb.UUID{0: {GoUUID: queue.UUID}})
		keys, _ := ovsdb.NewOvsSet([]int{0})
		ops = append(ops, ovsdb.Operation{Op: ovsdb.OperationUpdate, Table: vswitch.QoSTable, Where: uuidWhere(qos.UUID), Row: ovsdb.Row{"type": qos.Type}},
			ovsdb.Operation{Op: ovsdb.OperationMutate, Table: vswitch.QoSTable, Where: uuidWhere(qos.UUID), Mutations: []ovsdb.Mutation{{Column: "queues", Mutator: ovsdb.MutateOperationDelete, Value: keys}, {Column: "queues", Mutator: ovsdb.MutateOperationInsert, Value: queueMap}}})
	}
	bind, err := c.cniQoSBinding(portName, new(qos.UUID))
	if err != nil {
		return err
	}
	ops = append(ops, bind)
	_, err = c.transactVswitchOperations(ops)
	return err
}

func cniMapPatch(table, uuid, column string, values map[string]string, keys []string) ovsdb.Operation {
	keySet, _ := ovsdb.NewOvsSet(keys)
	mutations := []ovsdb.Mutation{{Column: column, Mutator: ovsdb.MutateOperationDelete, Value: keySet}}
	if len(values) != 0 {
		mutations = append(mutations, stringMapMutation(column, values, ovsdb.MutateOperationInsert))
	}
	return ovsdb.Operation{Op: ovsdb.OperationMutate, Table: table, Where: uuidWhere(uuid), Mutations: mutations}
}

func (c *VswitchClient) patchCNIMap(table, uuid, column string, values map[string]string, keys []string) error {
	_, err := c.transactVswitchOperations([]ovsdb.Operation{cniMapPatch(table, uuid, column, values, keys)})
	return err
}

func (c *VswitchClient) cniQoSBinding(portName string, qosID *string) (ovsdb.Operation, error) {
	port := &vswitch.Port{QOS: qosID}
	row, err := newVswitchRow(c.Schema(), vswitch.PortTable, port, &port.QOS)
	return ovsdb.Operation{Op: ovsdb.OperationUpdate, Table: vswitch.PortTable, Where: nameWhere(portName), Row: row}, err
}

func (c *VswitchClient) bindCNIQoS(portName string, qosID *string) error {
	op, err := c.cniQoSBinding(portName, qosID)
	if err != nil {
		return err
	}
	_, err = c.transactVswitchOperations([]ovsdb.Operation{op})
	return err
}

func (c *VswitchClient) SetCNINetem(podName, podNamespace, ifaceID, latency, limit, loss, jitter string) error {
	latencyMS, _ := strconv.Atoi(latency)
	jitterMS, _ := strconv.Atoi(jitter)
	limitPkts, _ := strconv.Atoi(limit)
	lossPercent, _ := strconv.ParseFloat(loss, 64)
	config := make(map[string]string)
	if latencyMS > 0 {
		config["latency"] = strconv.Itoa(latencyMS * 1000)
	}
	if jitterMS > 0 {
		config["jitter"] = strconv.Itoa(jitterMS * 1000)
	}
	if limitPkts > 0 {
		config["limit"] = strconv.Itoa(limitPkts)
	}
	if lossPercent > 0 {
		config["loss"] = strconv.FormatFloat(lossPercent, 'f', -1, 64)
	}
	ifaces, err := c.cniInterfaces(ifaceID)
	if err != nil {
		return err
	}
	for _, iface := range ifaces {
		if err := c.setCNINetemPort(podName, podNamespace, ifaceID, iface.Name, config); err != nil {
			return err
		}
	}
	return nil
}

func (c *VswitchClient) setCNINetemPort(podName, podNamespace, ifaceID, portName string, config map[string]string) error {
	qosList, _, err := c.cniQoSState()
	if err != nil {
		return err
	}
	var qos *vswitch.QoS
	for i := range qosList {
		if qosList[i].ExternalIDs["iface-id"] == ifaceID {
			qos = &qosList[i]
			break
		}
	}
	// HTB has precedence over netem, as in the daemon's reconciliation path.
	if qos != nil && qos.Type != util.NetemQos {
		return nil
	}
	if len(config) == 0 {
		if qos == nil {
			return nil
		}
		if err := c.bindCNIQoS(portName, nil); err != nil {
			return err
		}
		return c.ClearCNIQoS(podName, podNamespace, ifaceID)
	}
	var ops []ovsdb.Operation
	if qos == nil {
		qos = &vswitch.QoS{UUID: ovsclient.NamedUUID(), Type: util.NetemQos, OtherConfig: config, ExternalIDs: cniExternalIDs(podName, podNamespace, ifaceID)}
		ops, err = c.Create(qos)
		if err != nil {
			return err
		}
	} else {
		// Replace the netem fields, including removing settings no longer requested.
		ops = append(ops, cniMapPatch(vswitch.QoSTable, qos.UUID, "other_config", config, []string{"latency", "jitter", "limit", "loss"}))
	}
	bind, err := c.cniQoSBinding(portName, new(qos.UUID))
	if err != nil {
		return err
	}
	ops = append(ops, bind)
	_, err = c.transactVswitchOperations(ops)
	return err
}

// ClearCNIQoS deletes only unreferenced records belonging to this attachment.
// The wait guards protect against a concurrent daemon binding a record after
// the read and before cleanup (kube-ovn issue #1191).
func (c *VswitchClient) ClearCNIQoS(podName, podNamespace, ifaceID string) error {
	operations := []ovsdb.Operation{selectVswitch(vswitch.PortTable, nil), selectVswitch(vswitch.QoSTable, nil), selectVswitch(vswitch.QueueTable, nil)}
	results, err := c.transactVswitchOperations(operations)
	if err != nil {
		return err
	}
	ports, err := decodeVswitchRows[vswitch.Port](c.Schema(), vswitch.PortTable, results[0].Rows)
	if err != nil {
		return err
	}
	qosList, err := decodeVswitchRows[vswitch.QoS](c.Schema(), vswitch.QoSTable, results[1].Rows)
	if err != nil {
		return err
	}
	queues, err := decodeVswitchRows[vswitch.Queue](c.Schema(), vswitch.QueueTable, results[2].Rows)
	if err != nil {
		return err
	}
	usedQoS := make(map[string]bool)
	for _, port := range ports {
		if port.QOS != nil {
			usedQoS[*port.QOS] = true
		}
	}
	usedQueues := make(map[string]bool)
	var deletes []ovsdb.Operation
	for _, qos := range qosList {
		if cniOwned(qos.ExternalIDs, podName, podNamespace, ifaceID) && !usedQoS[qos.UUID] {
			deletes = append(deletes, deleteVswitchRowOperation(vswitch.QoSTable, qos.UUID))
		} else {
			for _, queue := range qos.Queues {
				usedQueues[queue] = true
			}
		}
	}
	for _, queue := range queues {
		if cniOwned(queue.ExternalIDs, podName, podNamespace, ifaceID) && !usedQueues[queue.UUID] {
			deletes = append(deletes, deleteVswitchRowOperation(vswitch.QueueTable, queue.UUID))
		}
	}
	if len(deletes) == 0 {
		return nil
	}
	guards := make([]ovsdb.Operation, 0, len(operations)+len(deletes))
	for i, op := range operations {
		guards = append(guards, ovsdb.Operation{Op: ovsdb.OperationWait, Table: op.Table, Where: op.Where, Until: "==", Timeout: new(0), Rows: results[i].Rows})
	}
	guards = append(guards, deletes...)
	if _, err := c.transactVswitchOperations(guards); err != nil {
		return fmt.Errorf("clean up CNI QoS: %w", err)
	}
	return nil
}
