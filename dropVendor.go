package main

import (
	gometrics "github.com/rcrowley/go-metrics"
	uuid "github.com/satori/go.uuid"
)

type DropVendor struct {
	*Vendor
}

// NewDropVendor create new DropVendor instance
func NewDropVendor(dbClient *DbClient, config *Config, logger *Logger, metricsRegistry *gometrics.Registry) *DropVendor {

	vendor := NewVendor(dbClient, config, logger, "drop")
	dropVendor := DropVendor{Vendor: vendor}

	if appConfig.MetricsDryRun == false && metricsRegistry != nil {
		dropVendor.Vendor.RegisterMetrics(*metricsRegistry)
	}

	return &dropVendor
}

func (d *DropVendor) SendMessage(message MessageQueueItem) bool {

	// check if message is part of aggregated batch
	if message.batchID == uuid.Nil {
		if d.config.StorageDryRun {
			d.logger.Infof("DropVendor dropped message: %s, %s, %s ", message.message.Target, message.message.Mode, message.message.Destination)
		}
		if d.config.PersistDroppedMsgs {
			d.Vendor.persistMsg(message, "", "message dropped by user preference")
		}
	} else {
		if d.config.PersistDroppedMsgs {
			d.Vendor.persistMsg(message, "", "message part of aggregated batch: "+message.batchID.String())
		}
	}
	d.Vendor.counterSuccesses.Inc(1)
	return true
}
