package main

import (
	gometrics "github.com/rcrowley/go-metrics"
)

type ErrorVendor struct {
	*Vendor
}

// NewErrorVendor create new ErrorVendor instance
func NewErrorVendor(dbClient *DbClient, config *Config, logger *Logger, metricsRegistry *gometrics.Registry) *ErrorVendor {

	vendor := NewVendor(dbClient, config, logger, "errorVendor")
	errorVendor := ErrorVendor{Vendor: vendor}

	if appConfig.MetricsDryRun == false && metricsRegistry != nil {
		errorVendor.Vendor.RegisterMetrics(*metricsRegistry)
	}

	return &errorVendor
}

func (d *ErrorVendor) SendMessage(message MessageQueueItem) bool {

	if d.config.StorageDryRun {
		d.logger.Infof("ErrorVendor SendMessage: %s, %s, %s ", message.message.Target, message.message.Mode, message.message.Destination)
	}
	d.Vendor.persistMsg(message, "", "Notification failed to build")
	d.Vendor.counterSuccesses.Inc(1)
	return true
}
