package ovs

import (
	"errors"
	"fmt"

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

	if c.Database == nil {
		return nil, errors.New("underlying ovsdb call layer is nil")
	}

	return getIndexedFmt(c.Database, &ovnnb.Meter{Name: name}, ignoreNotFound, func(err error) error {
		return fmt.Errorf("get meter %s: %w", name, err)
	})
}

// ListAllMeters retrieves all meters from the database for debugging
func (c *OVNNbClient) ListAllMeters() ([]*ovnnb.Meter, error) {
	meterList, err := filterAll[ovnnb.Meter](c.Database, &ovnnb.Meter{}, func(err error) error {
		return fmt.Errorf("failed to list meters: %w", err)
	})
	if err != nil {
		return nil, err
	}
	return pointersOf(meterList), nil
}

func (c *OVNNbClient) MeterExists(name string) (bool, error) {
	return existsByGet(c.GetMeter, name)
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
	bandOps, err := c.Database.Table(&ovnnb.MeterBand{}).CreateOps(band)
	if err != nil {
		return fmt.Errorf("build meter band ops %s: %w", name, err)
	}
	ops = append(ops, bandOps...)

	meterOps, err := c.Database.Table(&ovnnb.Meter{}).CreateOps(meter)
	if err != nil {
		return fmt.Errorf("build meter ops %s: %w", name, err)
	}
	ops = append(ops, meterOps...)

	return c.transactGenerated("meter-create", ops, nil, nil, wrapErr("create meter %s: %w", name))
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
		band, err := getIndexed(c.Database, &ovnnb.MeterBand{UUID: bandUUID}, true)
		if err != nil {
			return fmt.Errorf("get meter band %s for %s: %w", bandUUID, meter.Name, err)
		}
		if band != nil {
			band.Rate = rate
			band.BurstSize = burst
			bandUpdateOps, err = c.Database.Table(&ovnnb.MeterBand{}).UpdateOps(band, band, &band.Rate, &band.BurstSize)
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
		createBandOps, err := c.Database.Table(&ovnnb.MeterBand{}).CreateOps(band)
		if err != nil {
			return fmt.Errorf("build meter band ops %s: %w", meter.Name, err)
		}
		ops = append(ops, createBandOps...)

		mutateOps, err := c.Database.Table(&ovnnb.Meter{}).MutateOps(meter, model.Mutation{
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
	updateMeterOps, err := c.Database.Table(&ovnnb.Meter{}).UpdateOps(meter, meter, &meter.Unit)
	if err != nil {
		return fmt.Errorf("update meter %s: %w", meter.Name, err)
	}
	ops = append(ops, updateMeterOps...)

	if len(ops) == 0 {
		return nil
	}

	return c.transactGenerated("meter-update", ops, nil, nil, wrapErr("update meter %s: %w", meter.Name))
}

// DeleteMeter removes the meter and its bands if present.
func (c *OVNNbClient) DeleteMeter(name string) error {
	meter, err := getIndexedFmt(c.Database, &ovnnb.Meter{Name: name}, true, func(err error) error {
		return fmt.Errorf("failed to get meter %s: %w", name, err)
	})
	if err != nil {
		return err
	}
	if meter == nil {
		return nil
	}

	ops, err := c.Database.Table(&ovnnb.Meter{}).DeleteOps(meter)
	if err != nil {
		return fmt.Errorf("failed to build delete operations for meter %s: %w", name, err)
	}

	for _, bandUUID := range meter.Bands {
		band := &ovnnb.MeterBand{UUID: bandUUID}
		bandOps, err := c.Database.Table(&ovnnb.MeterBand{}).DeleteOps(band)
		if err != nil {
			return fmt.Errorf("failed to remove meter band %s for %s: %w", bandUUID, name, err)
		}
		ops = append(ops, bandOps...)
	}

	return c.transactGenerated("meter-del", ops, nil, nil, wrapErr("failed to delete meter %s: %w", name))
}
