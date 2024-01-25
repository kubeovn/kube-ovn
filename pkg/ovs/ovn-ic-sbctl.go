package ovs

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"k8s.io/klog/v2"
)

func (c LegacyClient) ovnIcSbCommand(cmdArgs ...string) (string, error) {
	start := time.Now()
	cmdArgs = append([]string{fmt.Sprintf("--timeout=%d", c.OvnTimeout), fmt.Sprintf("--db=%s", c.OvnICSbAddress)}, cmdArgs...)
	raw, err := exec.Command(OVNIcSbCtl, cmdArgs...).CombinedOutput()
	elapsed := float64((time.Since(start)) / time.Millisecond)
	klog.V(4).Infof("command %s %s in %vms", OVNIcSbCtl, strings.Join(cmdArgs, " "), elapsed)
	method := ""
	for _, arg := range cmdArgs {
		if !strings.HasPrefix(arg, "--") {
			method = arg
			break
		}
	}
	code := "0"
	defer func() {
		ovsClientRequestLatency.WithLabelValues("ovn-ic-sb", method, code).Observe(elapsed)
	}()

	if err != nil {
		code = "1"
		klog.Warningf("ovn-ic-sbctl command error: %s %s in %vms", OVNIcSbCtl, strings.Join(cmdArgs, " "), elapsed)
		return "", fmt.Errorf("%s, %q", raw, err)
	} else if elapsed > 500 {
		klog.Warningf("ovn-ic-sbctl command took too long: %s %s in %vms", OVNIcSbCtl, strings.Join(cmdArgs, " "), elapsed)
	}
	return trimCommandOutput(raw), nil
}

func (c LegacyClient) FindUUIDWithAttrInTable(attribute, value, table string) ([]string, error) {
	key := attribute + "=" + value
	output, err := c.ovnIcSbCommand("--format=csv", "--no-heading", "--data=bare", "--columns=_uuid", "find", table, key)
	if err != nil {
		err := fmt.Errorf("failed to find ovn-ic-sb db, %v", err)
		klog.Error(err)
		return nil, err
	}
	lines := strings.Split(output, "\n")
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		if len(strings.TrimSpace(l)) == 0 {
			continue
		}
		result = append(result, strings.TrimSpace(l))
	}
	return result, nil
}

func (c LegacyClient) DestroyTableWithUUID(uuid, table string) error {
	_, err := c.ovnIcSbCommand("destroy", table, uuid)
	if err != nil {
		klog.Error(err)
		return fmt.Errorf("failed to destroy record %s in table %s: %v", uuid, table, err)
	}
	return nil
}

func (c LegacyClient) GetAzUUID(az string) (string, error) {
	uuids, err := c.FindUUIDWithAttrInTable("name", az, "availability_zone")
	if err != nil {
		klog.Error(err)
		return "", fmt.Errorf("failed to get ovn-ic-sb availability_zone uuid: %v", err)
	}
	if len(uuids) == 1 {
		return uuids[0], nil
	} else if len(uuids) == 0 {
		return "", nil
	}
	return "", fmt.Errorf("two same-name chassises in one db is insane")
}

func (c LegacyClient) GetGatewayUUIDsInOneAZ(uuid string) ([]string, error) {
	gateways, err := c.FindUUIDWithAttrInTable("availability_zone", uuid, "gateway")
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("failed to get ovn-ic-sb gateways with uuid %v: %v", uuid, err)
	}
	return gateways, nil
}

func (c LegacyClient) GetRouteUUIDsInOneAZ(uuid string) ([]string, error) {
	routes, err := c.FindUUIDWithAttrInTable("availability_zone", uuid, "route")
	if err != nil {
		klog.Error(err)
		return nil, fmt.Errorf("failed to get ovn-ic-sb routes with uuid %v: %v", uuid, err)
	}
	return routes, nil
}

func (c LegacyClient) GetPortBindingUUIDsInOneAZ(uuid string) ([]string, error) {
	portBindings, err := c.FindUUIDWithAttrInTable("availability_zone", uuid, "Port_Binding")
	if err != nil {
		return nil, fmt.Errorf("failed to get ovn-ic-sb Port_Binding with uuid %v: %v", uuid, err)
	}
	return portBindings, nil
}

func (c LegacyClient) DestroyGateways(uuids []string) error {
	for _, uuid := range uuids {
		if err := c.DestroyTableWithUUID(uuid, "gateway"); err != nil {
			return fmt.Errorf("failed to delete gateway %v: %v", uuid, err)
		}
	}
	return nil
}

func (c LegacyClient) DestroyRoutes(uuids []string) error {
	for _, uuid := range uuids {
		if err := c.DestroyTableWithUUID(uuid, "route"); err != nil {
			return fmt.Errorf("failed to delete route %v: %v", uuid, err)
		}
	}
	return nil
}

func (c LegacyClient) DestroyPortBindings(uuids []string) error {
	for _, uuid := range uuids {
		if err := c.DestroyTableWithUUID(uuid, "Port_Binding"); err != nil {
			return fmt.Errorf("failed to delete Port_Binding %v: %v", uuid, err)
		}
	}
	return nil
}

func (c LegacyClient) DestroyChassis(uuid string) error {
	if err := c.DestroyTableWithUUID(uuid, "availability_zone"); err != nil {
		return fmt.Errorf("failed to delete chassis %v: %v", uuid, err)
	}
	return nil
}
