package main

import (
	gometrics "github.com/rcrowley/go-metrics"
	uuid "github.com/satori/go.uuid"
)

type DummyVendor struct {
	*Vendor
}

// NewDummyVendor create new DummyVendor instance
func NewDummyVendor(dbClient *DbClient, config *Config, logger *Logger, metricsRegistry *gometrics.Registry) *DummyVendor {

	vendor := NewVendor(dbClient, config, logger, "dummyVendor")
	dummyVendor := DummyVendor{Vendor: vendor}

	if appConfig.MetricsDryRun == false && metricsRegistry != nil {
		dummyVendor.Vendor.RegisterMetrics(*metricsRegistry)
	}

	return &dummyVendor
}

func (d *DummyVendor) SendMessage(message MessageQueueItem) bool {

	// check if message is part of aggregated batch
	if message.batchID == uuid.Nil {
		if d.config.StorageDryRun {
			d.logger.Infof("DummyVendor SendMessage: %s, %s, %s ", message.message.Target, message.message.Mode, message.message.Destination)
		}
		d.Vendor.persistMsg(message, "", "DummyVendor pretended to send this message (dropped)")
	} else {
		d.Vendor.persistMsg(message, "", "message part of aggregated batch: "+message.batchID.String())
	}
	d.Vendor.counterSuccesses.Inc(1)
	return true
}
