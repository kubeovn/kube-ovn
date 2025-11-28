package ovs

import (
	"context"
	"errors"
	"fmt"

	"github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// GetMeter get meter by name
func (c *OVNNbClient) GetMeter(name string, ignoreNotFound bool) (*ovnnb.Meter, error) {
	if name == "" {
		return nil, errors.New("meter name is empty")
	}

	if c.Client == nil {
		return nil, errors.New("underlying libovsdb client is nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	meter := &ovnnb.Meter{Name: name}

	if err := c.Get(ctx, meter); err != nil {
		if ignoreNotFound && errors.Is(err, client.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get meter %s: %w", name, err)
	}

	return meter, nil
}

// ListAllMeters retrieves all meters from the database for debugging
func (c *OVNNbClient) ListAllMeters() ([]*ovnnb.Meter, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	var meters []*ovnnb.Meter

	// Use cached listing to retrieve all meters
	meterList := make([]ovnnb.Meter, 0)
	if err := c.ovsDbClient.WhereCache(func(_ *ovnnb.Meter) bool {
		return true
	}).List(ctx, &meterList); err != nil {
		return nil, fmt.Errorf("failed to list meters: %w", err)
	}

	for i := range meterList {
		meters = append(meters, &meterList[i])
	}

	return meters, nil
}

// MeterExists check meter exists by name
func (c *OVNNbClient) MeterExists(name string) (bool, error) {
	meter, err := c.GetMeter(name, true)
	return meter != nil, err
}

// CreateOrUpdateMeter ensures a single-band meter exists with the given rate/burst.
// If the meter exists, it updates the first band; otherwise it creates a new meter and band.
func (c *OVNNbClient) CreateOrUpdateMeter(name string, unit ovnnb.MeterUnit, rate, burst int) error {
	if rate <= 0 {
		return c.DeleteMeter(name)
	}

	exists, err := c.MeterExists(name)
	if err != nil {
		return fmt.Errorf("check meter exists %s: %w", name, err)
	}

	if exists {
		meter, err := c.GetMeter(name, false)
		if err != nil {
			return fmt.Errorf("get meter %s: %w", name, err)
		}
		return c.updateMeterAndBand(meter, unit, rate, burst)
	}

	return c.createMeterWithBand(name, unit, rate, burst)
}

func (c *OVNNbClient) createMeterWithBand(name string, unit ovnnb.MeterUnit, rate, burst int) error {
	band := &ovnnb.MeterBand{
		UUID:        ovsclient.NamedUUID(),
		Action:      ovnnb.MeterBandActionDrop,
		Rate:        rate,
		BurstSize:   burst,
		ExternalIDs: map[string]string{"vendor": util.CniTypeName},
	}
	meter := &ovnnb.Meter{
		UUID:        ovsclient.NamedUUID(),
		Name:        name,
		Unit:        unit,
		Bands:       []string{band.UUID},
		ExternalIDs: map[string]string{"vendor": util.CniTypeName},
	}

	ops := make([]ovsdb.Operation, 0, 2)
	bandOps, err := c.Create(band)
	if err != nil {
		return fmt.Errorf("build meter band ops %s: %w", name, err)
	}
	ops = append(ops, bandOps...)

	meterOps, err := c.Create(meter)
	if err != nil {
		return fmt.Errorf("build meter ops %s: %w", name, err)
	}
	ops = append(ops, meterOps...)

	if err := c.Transact("meter-create", ops); err != nil {
		return fmt.Errorf("create meter %s: %w", name, err)
	}

	return nil
}

func (c *OVNNbClient) updateMeterAndBand(meter *ovnnb.Meter, unit ovnnb.MeterUnit, rate, burst int) error {
	ops := make([]ovsdb.Operation, 0, 3)

	// update or create band
	var bandUUID string
	if len(meter.Bands) > 0 {
		bandUUID = meter.Bands[0]
	}

	var (
		bandUpdateOps []ovsdb.Operation
		err           error
	)
	if bandUUID != "" {
		band := &ovnnb.MeterBand{UUID: bandUUID}
		ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
		defer cancel()
		if err := c.Get(ctx, band); err != nil {
			if !errors.Is(err, client.ErrNotFound) {
				return fmt.Errorf("get meter band %s for %s: %w", bandUUID, meter.Name, err)
			}
		} else {
			band.Rate = rate
			band.BurstSize = burst
			bandUpdateOps, err = c.Where(band).Update(band, &band.Rate, &band.BurstSize)
			if err != nil {
				return fmt.Errorf("update meter band %s for %s: %w", bandUUID, meter.Name, err)
			}
			ops = append(ops, bandUpdateOps...)
		}
	}

	if bandUUID == "" || len(bandUpdateOps) == 0 {
		bandUUID = ovsclient.NamedUUID()
		band := &ovnnb.MeterBand{
			UUID:        bandUUID,
			Action:      ovnnb.MeterBandActionDrop,
			Rate:        rate,
			BurstSize:   burst,
			ExternalIDs: map[string]string{"vendor": util.CniTypeName},
		}
		createBandOps, err := c.Create(band)
		if err != nil {
			return fmt.Errorf("build meter band ops %s: %w", meter.Name, err)
		}
		ops = append(ops, createBandOps...)

		mutateOps, err := c.Where(meter).Mutate(meter, model.Mutation{
			Field:   &meter.Bands,
			Value:   []string{bandUUID},
			Mutator: ovsdb.MutateOperationInsert,
		})
		if err != nil {
			return fmt.Errorf("append band %s to meter %s: %w", bandUUID, meter.Name, err)
		}
		ops = append(ops, mutateOps...)
	}

	meter.Unit = unit
	updateMeterOps, err := c.Where(meter).Update(meter, &meter.Unit)
	if err != nil {
		return fmt.Errorf("update meter %s: %w", meter.Name, err)
	}
	ops = append(ops, updateMeterOps...)

	if len(ops) == 0 {
		return nil
	}

	if err := c.Transact("meter-update", ops); err != nil {
		return fmt.Errorf("update meter %s: %w", meter.Name, err)
	}

	return nil
}

// DeleteMeter removes the meter and its bands if present.
func (c *OVNNbClient) DeleteMeter(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	meter := &ovnnb.Meter{Name: name}
	if err := c.Get(ctx, meter); err != nil {
		if errors.Is(err, client.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("failed to get meter %s: %w", name, err)
	}

	ops, err := c.Where(meter).Delete()
	if err != nil {
		return fmt.Errorf("failed to build delete operations for meter %s: %w", name, err)
	}

	for _, bandUUID := range meter.Bands {
		band := &ovnnb.MeterBand{UUID: bandUUID}
		bandOps, err := c.Where(band).Delete()
		if err != nil {
			return fmt.Errorf("failed to remove meter band %s for %s: %w", bandUUID, name, err)
		}
		ops = append(ops, bandOps...)
	}

	if err := c.Transact("meter-del", ops); err != nil {
		return fmt.Errorf("failed to delete meter %s: %w", name, err)
	}

	return nil
}
