package ovs

import (
	"errors"
	"fmt"

	"k8s.io/klog/v2"
)

func (c LegacyClient) ovnIcSbCommand(cmdArgs ...string) (string, error) {
	return c.ovnIcCommand("ovn-ic-sb", OVNIcSbCtl, c.OvnICSbAddress, cmdArgs...)
}

func (c LegacyClient) FindUUIDWithAttrInTable(attribute, value, table string) ([]string, error) {
	key := attribute + "=" + value
	output, err := c.ovnIcSbCommand("--format=csv", "--no-heading", "--data=bare", "--columns=_uuid", "find", table, key)
	if err != nil {
		return nil, logFmt("failed to find ovn-ic-sb db, %w", err)
	}
	return splitNonEmptyLines(output), nil
}

func (c LegacyClient) DestroyTableWithUUID(uuid, table string) error {
	_, err := c.ovnIcSbCommand("destroy", table, uuid)
	if err != nil {
		return logWrap(err, wrapErr("failed to destroy record %s in table %s: %w", uuid, table))
	}
	return nil
}

func (c LegacyClient) GetAzUUID(az string) (string, error) {
	uuids, err := c.FindUUIDWithAttrInTable("name", az, "availability_zone")
	if err != nil {
		klog.Error(err)
		return "", fmt.Errorf("failed to get ovn-ic-sb availability_zone uuid: %w", err)
	}
	if len(uuids) == 1 {
		return uuids[0], nil
	} else if len(uuids) == 0 {
		return "", nil
	}
	return "", errors.New("two same-name chassises in one db is insane")
}

func (c LegacyClient) uuidsInAZ(uuid, table, kind string) ([]string, error) {
	rows, err := c.FindUUIDWithAttrInTable("availability_zone", uuid, table)
	if err != nil {
		return nil, logWrap(err, wrapErr("failed to get ovn-ic-sb %s with uuid %v: %w", kind, uuid))
	}
	return rows, nil
}

func (c LegacyClient) GetGatewayUUIDsInOneAZ(uuid string) ([]string, error) {
	return c.uuidsInAZ(uuid, "gateway", "gateways")
}

func (c LegacyClient) GetRouteUUIDsInOneAZ(uuid string) ([]string, error) {
	return c.uuidsInAZ(uuid, "route", "routes")
}

func (c LegacyClient) GetPortBindingUUIDsInOneAZ(uuid string) ([]string, error) {
	return c.uuidsInAZ(uuid, "Port_Binding", "Port_Binding")
}

func (c LegacyClient) destroyUUIDs(uuids []string, table, kind string) error {
	for _, uuid := range uuids {
		if err := c.DestroyTableWithUUID(uuid, table); err != nil {
			return logWrap(err, wrapErr("failed to delete %s %v: %w", kind, uuid))
		}
	}
	return nil
}

func (c LegacyClient) DestroyGateways(uuids []string) error {
	return c.destroyUUIDs(uuids, "gateway", "gateway")
}

func (c LegacyClient) DestroyRoutes(uuids []string) error {
	return c.destroyUUIDs(uuids, "route", "route")
}

func (c LegacyClient) DestroyPortBindings(uuids []string) error {
	return c.destroyUUIDs(uuids, "Port_Binding", "Port_Binding")
}

func (c LegacyClient) DestroyChassis(uuid string) error {
	if err := c.DestroyTableWithUUID(uuid, "availability_zone"); err != nil {
		return logWrap(err, wrapErr("failed to delete chassis %v: %w", uuid))
	}
	return nil
}
