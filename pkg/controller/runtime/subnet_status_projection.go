package controllerruntime

import (
	"context"
	"errors"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

// SubnetStatusProjection is the desired status produced by a status
// calculator. The snapshot binds the projection to one Subnet identity so a
// future writer cannot apply a result calculated for a replaced object.
type SubnetStatusProjection struct {
	Snapshot SubnetWriteSnapshot
	Status   kubeovnv1.SubnetStatus
}

// NewSubnetStatusProjection copies a calculated status and binds it to the
// immutable Subnet snapshot. It performs no API write.
func NewSubnetStatusProjection(snapshot SubnetWriteSnapshot, status *kubeovnv1.SubnetStatus) (SubnetStatusProjection, error) {
	if snapshot.Name == "" {
		return SubnetStatusProjection{}, errors.New("subnet status projection requires a Subnet snapshot")
	}
	if status == nil {
		return SubnetStatusProjection{}, errors.New("subnet status projection requires status")
	}
	return SubnetStatusProjection{
		Snapshot: snapshot,
		Status:   *status.DeepCopy(),
	}, nil
}

// SubnetStatusProjector calculates desired status without owning the API
// write. The legacy controller remains the only production status writer
// until an explicit subnet-status mode supplies this seam with a complete
// desired-state source.
type SubnetStatusProjector interface {
	ProjectSubnetStatus(context.Context, SubnetWriteSnapshot, SubnetShadowObservation) (SubnetStatusProjection, error)
}

// SubnetStatusProjectorFunc adapts a function to SubnetStatusProjector.
type SubnetStatusProjectorFunc func(context.Context, SubnetWriteSnapshot, SubnetShadowObservation) (SubnetStatusProjection, error)

func (f SubnetStatusProjectorFunc) ProjectSubnetStatus(ctx context.Context, snapshot SubnetWriteSnapshot, observation SubnetShadowObservation) (SubnetStatusProjection, error) {
	if f == nil {
		return SubnetStatusProjection{}, errors.New("subnet status projector is not configured")
	}
	return f(ctx, snapshot, observation)
}
