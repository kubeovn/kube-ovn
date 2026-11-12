package controllerruntime

import (
	"context"
	"errors"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func getSubnet(ctx context.Context, reader client.Reader, name string, timeout time.Duration) (*kubeovnv1.Subnet, error) {
	readContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	subnet := &kubeovnv1.Subnet{}
	if err := reader.Get(readContext, types.NamespacedName{Name: name}, subnet); err != nil {
		return nil, err
	}
	return subnet, readContext.Err()
}

func syncSubnetMetadata(ctx context.Context, destination, source *kubeovnv1.Subnet) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	destination.ObjectMeta = *source.ObjectMeta.DeepCopy()
	return nil
}

func confirmSubnetWrite(ctx context.Context, reader client.Reader, gate SubnetWriterGate, expected *kubeovnv1.Subnet, timeout time.Duration, writeErr error, matches func(*kubeovnv1.Subnet) bool) (*kubeovnv1.Subnet, error) {
	if apierrors.IsConflict(writeErr) {
		return nil, writeErr
	}
	current, err := getSubnet(ctx, reader, expected.Name, timeout)
	if err != nil {
		return nil, errors.Join(writeErr, err)
	}
	if err := gate.WaitForRecovery(ctx); err != nil {
		return nil, errors.Join(writeErr, err)
	}
	if !sameSubnetSpec(expected, current) || !matches(current) {
		return nil, writeErr
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(writeErr, err)
	}
	return current, nil
}
