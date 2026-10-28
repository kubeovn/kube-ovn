package cni

import (
	"errors"
	"fmt"
	"net"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/request"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// ExecutorConfig contains only execution-time policy. It intentionally has
// no Kubernetes client or informer dependencies so the executor can run in the
// CNI process after the daemon has returned a validated plan.
type ExecutorConfig struct {
	EnableArpDetectIPConflict bool
}

// Executor applies Pod network plans in the process that the runtime starts.
type Executor struct {
	handler executionHandler
}

func NewExecutor(config ExecutorConfig) *Executor {
	return &Executor{handler: executionHandler{Config: &executionConfig{EnableArpDetectIPConflict: config.EnableArpDetectIPConflict}}}
}

// Add applies a prepared plan and returns facts observed after configuration.
func (e *Executor) Add(plan *request.CNIPlan) (*request.CNIExecutionResult, error) {
	if err := validateCNIPlan(plan); err != nil {
		return nil, err
	}
	if plan.IPAMOnly {
		if err := removePlanDefaultRoutes(&e.handler, plan); err != nil {
			return nil, err
		}
		return &request.CNIExecutionResult{Routes: plan.Routes}, nil
	}

	client, err := ovs.NewCNIVswitchClient("unix:/run/openvswitch/db.sock")
	if err != nil {
		return nil, fmt.Errorf("connect CNI to OVSDB: %w", err)
	}
	defer client.Close()
	e.handler.ovsClient = client

	execution := &request.CNIExecutionResult{}
	var routes []request.Route
	if plan.NicType == util.DpdkType {
		if plan.ShortSharedDir == "" || plan.OriginSharedDir == "" {
			return nil, errors.New("DPDK CNI plan has no shared directory paths")
		}
		if err = createShortSharedDirAt(plan.ShortSharedDir, plan.OriginSharedDir, plan.VhostUserSocketConsumption); err != nil {
			return nil, fmt.Errorf("prepare DPDK shared directory: %w", err)
		}
		err = e.handler.configureDpdkNic(plan.PodName, plan.PodNamespace, plan.Provider, plan.NetNs, plan.ContainerID, plan.IfName, plan.MacAddress, plan.MTU, plan.IPAddr, plan.Gateway, plan.Ingress, plan.Egress, plan.IngressBurst, plan.EgressBurst, plan.ShortSharedDir, plan.VhostUserSocketName, plan.VhostUserSocketConsumption)
	} else {
		routes, err = e.handler.configureNic(plan.PodName, plan.PodNamespace, plan.Provider, plan.NetNs, plan.ContainerID, plan.VfDriver, plan.IfName, plan.MacAddress, plan.MTU, plan.IPAddr, plan.Gateway, plan.IsDefaultRoute, plan.VMMigration, plan.Routes, plan.DNS.Nameservers, plan.DNS.Search, plan.Ingress, plan.Egress, plan.IngressBurst, plan.EgressBurst, plan.DeviceID, plan.Latency, plan.Limit, plan.Loss, plan.Jitter, plan.GatewayCheckMode, plan.U2OInterconnectionIP, plan.OldPodName, plan.EncapIP, plan.LocalnetSubnet, plan.AppendIfName, plan.RoutedSubnet, execution)
	}
	if err != nil {
		return nil, fmt.Errorf("apply CNI plan for %s/%s: %w", plan.PodNamespace, plan.PodName, err)
	}

	ifaceID := ovs.PodNameToPortName(plan.PodName, plan.PodNamespace, plan.Provider)
	if err := client.ConfigureCNIMirror(plan.MirrorEnabled, plan.MirrorControl, ifaceID); err != nil {
		return nil, fmt.Errorf("configure interface mirror: %w", err)
	}
	execution.Routes = routes
	return execution, nil
}

// Delete removes only resources identified by the prepared plan.
func (e *Executor) Delete(plan *request.CNIPlan) error {
	if err := validateCNIPlan(plan); err != nil {
		return err
	}
	if plan.IPAMOnly {
		return nil
	}
	client, err := ovs.NewCNIVswitchClient("unix:/run/openvswitch/db.sock")
	if err != nil {
		return fmt.Errorf("connect CNI to OVSDB: %w", err)
	}
	defer client.Close()
	e.handler.ovsClient = client

	if plan.NicType == util.DpdkType && plan.ShortSharedDir != "" {
		if err := removeShortSharedDirAt(plan.ShortSharedDir, plan.VhostUserSocketConsumption); err != nil {
			return fmt.Errorf("remove DPDK shared directory: %w", err)
		}
	}
	if err := e.handler.deleteNic(plan.PodName, plan.PodNamespace, plan.ContainerID, plan.NetNs, plan.DeviceID, plan.IfName, plan.NicType); err != nil {
		return fmt.Errorf("delete CNI plan for %s/%s: %w", plan.PodNamespace, plan.PodName, err)
	}
	return nil
}

func validateCNIPlan(plan *request.CNIPlan) error {
	if plan == nil {
		return errors.New("CNI plan is required")
	}
	if plan.PodName == "" || plan.PodNamespace == "" || plan.ContainerID == "" {
		return errors.New("CNI plan has incomplete Pod identity")
	}
	if !plan.Delete && plan.NetNs == "" {
		return errors.New("CNI plan has no network namespace")
	}
	if plan.IfName == "" {
		return errors.New("CNI plan has no interface name")
	}
	if plan.MTU < 0 {
		return fmt.Errorf("CNI plan has invalid MTU %d", plan.MTU)
	}
	if plan.MacAddress != "" {
		if _, err := net.ParseMAC(plan.MacAddress); err != nil {
			return fmt.Errorf("CNI plan has invalid MAC %q: %w", plan.MacAddress, err)
		}
	}
	return nil
}

func removePlanDefaultRoutes(handler *executionHandler, plan *request.CNIPlan) error {
	var ipv4, ipv6 bool
	for _, route := range plan.Routes {
		if route.Destination == "" {
			if util.CheckProtocol(route.Gateway) == kubeovnv1.ProtocolIPv6 {
				ipv6 = true
			} else {
				ipv4 = true
			}
			continue
		}
		_, cidr, err := net.ParseCIDR(route.Destination)
		if err != nil {
			return fmt.Errorf("invalid route destination %q: %w", route.Destination, err)
		}
		if ones, _ := cidr.Mask.Size(); ones != 0 {
			continue
		}
		if cidr.IP.To4() != nil {
			ipv4 = true
		} else {
			ipv6 = true
		}
	}
	if ipv4 || ipv6 {
		return handler.removeDefaultRoute(plan.NetNs, ipv4, ipv6)
	}
	return nil
}
