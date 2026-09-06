package pinger

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
	"k8s.io/utils/set"

	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnsb"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	sbServiceAddress string
	sbClientMu       sync.Mutex
	sbClient         *ovs.OVNSbClient
	vswitchClientMu  sync.Mutex
	vswitchClient    *ovs.VswitchClient
)

func init() {
	sbHost, sbPort := util.InjectedServiceVariables("ovn-sb")
	sbServiceAddress = ovs.OvsdbServerAddress(sbHost, intstr.FromString(sbPort))
}

func checkOvs(config *Configuration, setMetrics bool) error {
	for component, err := range getOvsStatus() {
		if err != nil {
			klog.Errorf("%s is down", component)
			if setMetrics {
				SetOvsDownMetrics(config.NodeName)
			}
			return err
		}
	}
	klog.Infof("%s and %s are up", ovs.OvsdbServer, ovs.OvsVswitchd)
	if setMetrics {
		SetOvsUpMetrics(config.NodeName)
	}
	return nil
}

func checkOvnController(config *Configuration, setMetrics bool) error {
	_, err := ovs.Appctl(ovs.OvnController, "-T", "1", "version")
	if err != nil {
		klog.Errorf("failed to get status of %s: %v", ovs.OvnController, err)
		if setMetrics {
			SetOvnControllerDownMetrics(config.NodeName)
		}
		return err
	}
	klog.Infof("%s is up", ovs.OvnController)
	if setMetrics {
		SetOvnControllerUpMetrics(config.NodeName)
	}
	return nil
}

func checkPortBindings(config *Configuration, setMetrics bool) error {
	klog.Infof("start to check port binding")
	ovsBindings, err := checkOvsBindings(config)
	if err != nil {
		klog.Error(err)
		return err
	}
	klog.Infof("ports in ovs: %v", strings.Join(ovsBindings.SortedList(), ", "))

	sbBindings, err := checkSBBindings(config)
	if err != nil {
		klog.Error(err)
		return err
	}
	klog.Infof("ports in sb: %v", strings.Join(sbBindings.SortedList(), ", "))

	if misMatch := ovsBindings.Difference(sbBindings); misMatch.Len() > 0 {
		err = fmt.Errorf("%d ports not exist in ovn-sb-bindings: %s", misMatch.Len(), strings.Join(misMatch.SortedList(), ", "))
		klog.Error(err)
		if setMetrics {
			inconsistentPortBindingGauge.WithLabelValues(config.NodeName).Set(float64(misMatch.Len()))
		}
		return err
	}

	klog.Info("ovs and ovn-sb port binding check passed")
	if setMetrics {
		inconsistentPortBindingGauge.WithLabelValues(config.NodeName).Set(0)
	}
	return nil
}

func checkOvsBindings(config *Configuration) (set.Set[string], error) {
	provider, err := getVswitchProvider(config)
	if err != nil {
		return nil, err
	}
	var interfaces []vswitch.Interface
	if err := provider.Table(&vswitch.Interface{}).Filter(context.Background(), func(row *vswitch.Interface) bool {
		return row.ExternalIDs["iface-id"] != ""
	}, &interfaces); err != nil {
		return nil, fmt.Errorf("failed to list OVS interfaces: %w", err)
	}
	result := set.New[string]()
	for iface := range slices.Values(interfaces) {
		if ifaceID := strings.TrimSpace(iface.ExternalIDs["iface-id"]); ifaceID != "" {
			result.Insert(ifaceID)
		}
	}
	return result, nil
}

func getChassis(hostname string, providers ...compat.TableProvider) (string, error) {
	provider, err := firstSBProvider(providers...)
	if err != nil {
		return "", err
	}
	var rows []ovnsb.Chassis
	if err := provider.Table(&ovnsb.Chassis{}).Filter(context.Background(), func(row *ovnsb.Chassis) bool {
		return row.Hostname == hostname
	}, &rows); err != nil {
		return "", fmt.Errorf("failed to list chassis for hostname %q: %w", hostname, err)
	}
	if len(rows) == 0 {
		return "", fmt.Errorf("no chassis found for hostname %q", hostname)
	}
	if len(rows) != 1 {
		return "", fmt.Errorf("unexpected number of chassis rows for hostname %q: %d", hostname, len(rows))
	}
	return rows[0].UUID, nil
}

func getLogicalPort(chassisUUID string, providers ...compat.TableProvider) (set.Set[string], error) {
	provider, err := firstSBProvider(providers...)
	if err != nil {
		return nil, err
	}
	var rows []ovnsb.PortBinding
	if err := provider.Table(&ovnsb.PortBinding{}).Filter(context.Background(), func(row *ovnsb.PortBinding) bool {
		return row.Chassis != nil && *row.Chassis == chassisUUID
	}, &rows); err != nil {
		return nil, fmt.Errorf("failed to list logical ports for chassis UUID %q: %w", chassisUUID, err)
	}

	ports := set.New[string]()
	for row := range slices.Values(rows) {
		if row.LogicalPort != "" {
			ports.Insert(row.LogicalPort)
		}
	}
	return ports, nil
}

func checkSBBindings(config *Configuration) (set.Set[string], error) {
	provider, err := getSBProvider()
	if err != nil {
		return nil, err
	}
	chassisUUID, err := getChassis(config.NodeName, provider)
	if err != nil {
		return nil, err
	}
	return getLogicalPort(chassisUUID, provider)
}

func firstSBProvider(providers ...compat.TableProvider) (compat.TableProvider, error) {
	if len(providers) != 0 && providers[0] != nil {
		return providers[0], nil
	}
	return getSBProvider()
}

func getSBProvider() (compat.TableProvider, error) {
	sbClientMu.Lock()
	defer sbClientMu.Unlock()
	if sbClient != nil && sbClient.Connected() {
		return sbClient, nil
	}
	if sbClient != nil {
		sbClient.Close()
		sbClient = nil
	}
	client, err := ovs.NewOvnSbClient(sbServiceAddress, 10, 10, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("connect to OVN SB: %w", err)
	}
	sbClient = client
	return client, nil
}

func getVswitchProvider(config *Configuration) (compat.TableProvider, error) {
	vswitchClientMu.Lock()
	defer vswitchClientMu.Unlock()
	if vswitchClient != nil && vswitchClient.Connected() {
		return vswitchClient, nil
	}
	if vswitchClient != nil {
		vswitchClient.Close()
		vswitchClient = nil
	}
	address := config.DatabaseVswitchSocketRemote
	if address == "" {
		address = "unix:/var/run/openvswitch/db.sock"
	}
	client, err := ovs.NewVswitchClient(address, 10, 10)
	if err != nil {
		return nil, fmt.Errorf("connect to OVSDB: %w", err)
	}
	vswitchClient = client
	return client, nil
}
