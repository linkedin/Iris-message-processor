package main

import (
	"time"

	gometrics "github.com/rcrowley/go-metrics"
)

type MetricsCollector struct {
	Registry gometrics.Registry
	Config   MetricsCollectorConfig
	logger   *Logger
}

type MetricsCollectorConfig struct {
	AppName string
	FQDN    string
	DryRun  bool
}

func NewMetricsCollector(config MetricsCollectorConfig, logger *Logger) *MetricsCollector {
	return &MetricsCollector{
		Registry: gometrics.NewRegistry(),
		Config:   config,
		logger:   logger,
	}
}

// CollectRuntimeMemStats will register and start a goroutine to start collecting runtime memory metrics
func (c *MetricsCollector) CollectRuntimeMemStats() {
	gometrics.RegisterRuntimeMemStats(c.Registry)
	go gometrics.CaptureRuntimeMemStats(c.Registry, 1*time.Minute)
}

// Emit collected metrics
func (c *MetricsCollector) EmitMetrics() {
	c.logger.Infof("Dummy emitt metrics...")

}
