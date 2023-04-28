package main

import (
	"sync"
	"time"

	gometrics "github.com/rcrowley/go-metrics"
	uuid "github.com/satori/go.uuid"
)

var (
	gaugeNumberOfPlans        = gometrics.NewGauge()
	gaugeAggregatedPlansTotal = gometrics.NewGauge()
)

type PlanAggregation struct {
	planName          string
	maxIncidentCount  int
	collectionWindow  time.Duration
	aggregationWindow time.Duration
	resetWindow       time.Duration

	aggregationStatus bool
	incidentCount     int
	collectionTime    time.Time
	aggregationTime   time.Time
	batchID           uuid.UUID
}
type AggregationManager struct {
	Quit       chan int // send signal to shutdown
	config     *Config
	logger     *Logger
	mainWg     *sync.WaitGroup
	irisClient *IrisClient

	planAggregationMap map[string]PlanAggregation
	aggregationMutex   sync.RWMutex
}

func (a *AggregationManager) RegisterMetrics(reg gometrics.Registry) {
	reg.Register("aggregationManager.internal.plan_aggregation_settings_total", gaugeNumberOfPlans)
	reg.Register("aggregationManager.internal.plans_under_aggregation_total", gaugeAggregatedPlansTotal)
}

// NewAggregationManager create new AggregationManager
func NewAggregationManager(irisClient *IrisClient, config *Config, logger *Logger, quit chan int, wg *sync.WaitGroup, metricsRegistry *gometrics.Registry) *AggregationManager {

	aggregationManager := AggregationManager{
		Quit:               quit, // send signal to shutdown
		config:             config,
		irisClient:         irisClient,
		logger:             logger,
		mainWg:             wg,
		planAggregationMap: make(map[string]PlanAggregation),
	}

	if appConfig.MetricsDryRun == false && metricsRegistry != nil {
		aggregationManager.RegisterMetrics(*metricsRegistry)
	}

	return &aggregationManager
}

// check if messages for this plan are under aggregation
func (a *AggregationManager) checkAggregation(plan string) (uuid.UUID, bool) {
	a.aggregationMutex.RLock()
	defer a.aggregationMutex.RUnlock()
	if val, ok := a.planAggregationMap[plan]; ok {
		return val.batchID, val.aggregationStatus
	}
	// plan's aggregation settings have not been initialized for this plan yet, do not aggregate
	return uuid.UUID{}, false

}

// count incidents for a plan and check if it should trigger aggregation
func (a *AggregationManager) countIncident(planName string) (uuid.UUID, bool) {
	a.aggregationMutex.Lock()
	defer a.aggregationMutex.Unlock()
	if planAgg, ok := a.planAggregationMap[planName]; ok {
		planAgg.incidentCount++
		if !planAgg.aggregationStatus {
			if planAgg.incidentCount == 1 {
				// start new collection window if this is the first incident
				planAgg.collectionTime = time.Now()
			} else {
				if planAgg.incidentCount > planAgg.maxIncidentCount {
					// start aggregation window and create a new batch ID for this aggregation
					planAgg.aggregationStatus = true
					planAgg.aggregationTime = time.Now()
					batchID := uuid.NewV4()
					planAgg.batchID = batchID
					a.planAggregationMap[planName] = planAgg
					a.logger.Infof("Started aggregation for plan %s", planAgg.planName)

					// signal caller that new aggregation has been triggered
					return batchID, true
				}
			}
		}
		// did not trigger a new aggregation
		a.planAggregationMap[planName] = planAgg
		return planAgg.batchID, false

	}
	// plan does not exist in the map, do not aggregate
	return uuid.UUID{}, false

}

// check if the correct amount of time has passed to reset timers and counters for each plan
func (a *AggregationManager) resetAggregation() {
	a.aggregationMutex.Lock()
	defer a.aggregationMutex.Unlock()
	var aggregatedPlansTotal int64
	for _, planAgg := range a.planAggregationMap {
		if planAgg.aggregationStatus {
			aggregatedPlansTotal++
			// if aggregationWindow time has elapsed since we started aggregating stop aggregation
			// we don't want to aggregate forever so stop aggregation even if messages are still actively coming in
			// TODO: change iris-api UI to reflect this aggregation behavior change.
			if time.Since(planAgg.aggregationTime) > planAgg.aggregationWindow {
				planAgg.incidentCount = 0
				planAgg.aggregationStatus = false
				a.planAggregationMap[planAgg.planName] = planAgg
				a.logger.Infof("Stopped aggregation for plan %s", planAgg.planName)
			}
		} else {
			if planAgg.incidentCount > 0 {
				// if the collectionwindow has elapsed without triggering aggregation reset it and the count
				if time.Since(planAgg.collectionTime) > planAgg.collectionWindow {
					planAgg.incidentCount = 0
					a.planAggregationMap[planAgg.planName] = planAgg
				}
			}
		}

	}
	gaugeAggregatedPlansTotal.Update(aggregatedPlansTotal)
}

// check if the aggregations settings have changed for each plan and update them if necessary
func (a *AggregationManager) updateSettings(aggSettingResp []PlanAggregationResp) {
	a.aggregationMutex.Lock()
	defer a.aggregationMutex.Unlock()
	gaugeNumberOfPlans.Update(int64(len(aggSettingResp)))
	activePlans := make(map[string]bool)

	for _, resp := range aggSettingResp {
		planName := resp.PlanName
		activePlans[planName] = true

		// convert int number of seconds to durations
		planAgg := PlanAggregation{
			planName:          resp.PlanName,
			maxIncidentCount:  resp.ThresholdCount,
			collectionWindow:  time.Duration(resp.ThresholdWindow) * time.Second,
			aggregationWindow: time.Duration(resp.AggregationWindow) * time.Second,
			resetWindow:       time.Duration(resp.AggregationReset) * time.Second,
			aggregationStatus: false,
			incidentCount:     0,
			collectionTime:    time.Now(),
		}

		if val, ok := a.planAggregationMap[planName]; !ok {
			// plan's aggregation settings had not been initialized yet
			a.planAggregationMap[planName] = planAgg

		} else {

			if planAgg.maxIncidentCount != val.maxIncidentCount || planAgg.collectionWindow != val.collectionWindow || planAgg.aggregationWindow != val.aggregationWindow || planAgg.resetWindow != val.resetWindow {
				// some setting changed, update
				a.planAggregationMap[planName] = planAgg
				a.logger.Infof("Updated aggregation settings for plan %s", planName)
			}
		}
	}

	// clean up plans that no longer exist
	for planName := range a.planAggregationMap {
		if _, ok := activePlans[planName]; !ok {
			//delete non existent plan
			delete(a.planAggregationMap, planName)
		}

	}
}

// periodically fetch plan aggregation settings from Iris and manage existing aggregations
func (a *AggregationManager) Run() {
	defer a.mainWg.Done()

	interval := time.NewTicker(a.config.RunLoopDuration * time.Second)
	defer interval.Stop()
	for ; true; <-interval.C {
		// get aggregation settings from iris-api and update our aggregation map
		aggSettingsResp, err := a.irisClient.GetPlanAggregationSettings()
		if err != nil {
			a.logger.Errorf("Failed to fetch aggregation settings, skipping aggregation settings update...")
		} else {
			a.updateSettings(aggSettingsResp)
		}

		// check if any aggregations need to be reset
		a.resetAggregation()

		a.logger.Infof("Updated aggregation information...")
		select {
		case <-a.Quit:
			a.logger.Infof("stopped aggregationManager...")
			return
		default:
			continue
		}
	}
}
