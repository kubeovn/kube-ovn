package pinger

import (
	"context"
	"errors"
	"testing"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/compat"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
)

type pingerTableHandle struct {
	compat.TableHandle
	interfaces []vswitch.Interface
}

func (h *pingerTableHandle) List(_ context.Context, result any) error {
	rows, ok := result.(*[]vswitch.Interface)
	if !ok {
		return errors.New("unexpected table result type")
	}
	*rows = append(*rows, h.interfaces...)
	return nil
}

type pingerTableProvider struct {
	handle compat.TableHandle
}

func (p *pingerTableProvider) Table(model.Model) compat.TableHandle {
	return p.handle
}

func TestExporterReconnectsGenericVswitchProvider(t *testing.T) {
	attempts := 0
	provider := &pingerTableProvider{handle: &pingerTableHandle{
		interfaces: []vswitch.Interface{{UUID: "interface-1", Name: "eth0"}},
	}}
	exporter := &Exporter{
		newVswitchTables: func() (compat.TableProvider, error) {
			attempts++
			if attempts == 1 {
				return nil, errors.New("OVSDB is not ready")
			}
			return provider, nil
		},
	}

	require.Error(t, exporter.connectVswitchTables())
	interfaces, err := exporter.getInterfaceInfo()
	require.NoError(t, err)
	require.Len(t, interfaces, 1)
	require.Equal(t, "eth0", interfaces[0].Name)
	require.Equal(t, 2, attempts)
}
