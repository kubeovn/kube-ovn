package controllerruntime

import (
	"context"
	"errors"
	"fmt"
	"slices"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/internal"
)

// SubnetStatusCalculator calculates a status projection from a reconstructed
// Subnet object. The calculator must remain read-only; this adapter owns no
// API persistence.
type SubnetStatusCalculator func(*kubeovnv1.Subnet) (*kubeovnv1.Subnet, error)

// NewSubnetStatusCalculatorProjector adapts the legacy read-only calculator to
// the snapshot-bound runtime projector contract.
func NewSubnetStatusCalculatorProjector(calculator SubnetStatusCalculator) SubnetStatusProjector {
	return SubnetStatusProjectorFunc(func(_ context.Context, snapshot SubnetWriteSnapshot, observation SubnetShadowObservation) (SubnetStatusProjection, error) {
		if calculator == nil {
			return SubnetStatusProjection{}, ErrSubnetStatusProjectorMissing
		}
		subnet, err := subnetFromStatusObservation(snapshot, observation)
		if err != nil {
			return SubnetStatusProjection{}, err
		}
		projected, err := calculator(subnet)
		if err != nil {
			return SubnetStatusProjection{}, err
		}
		if projected == nil {
			return SubnetStatusProjection{}, errors.New("subnet status calculator returned nil")
		}
		return NewSubnetStatusProjection(snapshot, &projected.Status)
	})
}

func subnetFromStatusObservation(snapshot SubnetWriteSnapshot, observation SubnetShadowObservation) (*kubeovnv1.Subnet, error) {
	status := kubeovnv1.SubnetStatus{
		Conditions:         slices.Clone(observation.Status.Conditions),
		V4AvailableIPRange: observation.Status.V4AvailableIPRange,
		V4UsingIPRange:     observation.Status.V4UsingIPRange,
		V6AvailableIPRange: observation.Status.V6AvailableIPRange,
		V6UsingIPRange:     observation.Status.V6UsingIPRange,
		GatewayMAC:         observation.Status.GatewayMAC,
	}
	var err error
	if status.V4AvailableIPs, err = parseObservedBigInt(observation.Status.V4AvailableIPs); err != nil {
		return nil, fmt.Errorf("parse subnet %s v4 available IPs: %w", snapshot.Name, err)
	}
	if status.V4UsingIPs, err = parseObservedBigInt(observation.Status.V4UsingIPs); err != nil {
		return nil, fmt.Errorf("parse subnet %s v4 using IPs: %w", snapshot.Name, err)
	}
	if status.V6AvailableIPs, err = parseObservedBigInt(observation.Status.V6AvailableIPs); err != nil {
		return nil, fmt.Errorf("parse subnet %s v6 available IPs: %w", snapshot.Name, err)
	}
	if status.V6UsingIPs, err = parseObservedBigInt(observation.Status.V6UsingIPs); err != nil {
		return nil, fmt.Errorf("parse subnet %s v6 using IPs: %w", snapshot.Name, err)
	}
	return &kubeovnv1.Subnet{
		Name:            snapshot.Name,
		UID:             snapshot.UID,
		Generation:      snapshot.Generation,
		ResourceVersion: snapshot.ResourceVersion,
		Spec:            *snapshot.Spec.DeepCopy(),
		Status:          status,
	}, nil
}

func parseObservedBigInt(value string) (internal.BigInt, error) {
	if value == "" {
		return internal.BigInt{}, nil
	}
	var result internal.BigInt
	if _, ok := result.SetString(value, 10); !ok {
		return internal.BigInt{}, fmt.Errorf("invalid integer %q", value)
	}
	return result, nil
}
