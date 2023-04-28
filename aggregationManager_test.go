package main

import (
	"sync"
	"testing"
	"time"

	uuid "github.com/satori/go.uuid"
)

func TesttriggerAggregationMessages(t *testing.T) {
	logger := NewLogger("tests", "", false)
	defer logger.RedirectStdLogger()()
	defer logger.Close()
	quit := make(chan int)
	var mainWg sync.WaitGroup
	var appConfig Config

	aggregationManager := NewAggregationManager(irisClient, &appConfig, logger.Clone("aggregationManager"), quit, &mainWg, nil)

	planAggregationRespItem := PlanAggregationResp{
		PlanID:            123,
		PlanName:          "plan1",
		ThresholdWindow:   1,
		ThresholdCount:    2,
		AggregationWindow: 1,
		AggregationReset:  1,
	}

	planAggSettingResp := []PlanAggregationResp{
		planAggregationRespItem,
	}

	aggregationManager.updateSettings(planAggSettingResp)

	var got, want int
	got = len(aggregationManager.planAggregationMap)
	want = 1
	if got != want {
		t.Fatalf("len(authMap) = %d; want %d", got, want)
	}

	aggregationManager.countIncident("plan1")
	batchID, triggerBool := aggregationManager.checkAggregation("plan1")
	var defaultUUID uuid.UUID

	if triggerBool != false {
		t.Fatalf("triggerBool = %+v; want %+v", triggerBool, false)
	}

	if batchID != defaultUUID {
		t.Fatalf("batchID = %+v; want %+v", batchID, uuid.UUID{})
	}

	aggregationManager.countIncident("plan1")
	batchID, triggerBool = aggregationManager.checkAggregation("plan1")

	if triggerBool != false {
		t.Fatalf("triggerBool = %+v; want %+v", triggerBool, false)
	}
	if batchID != defaultUUID {
		t.Fatalf("batchID = %+v; want %+v", batchID, uuid.UUID{})
	}

	aggregationManager.countIncident("plan1")
	batchID, triggerBool = aggregationManager.checkAggregation("plan1")

	// aggregation is triggered
	if triggerBool != true {
		t.Fatalf("triggerBool = %+v; want %+v", triggerBool, false)
	}
	if batchID == defaultUUID {
		t.Fatalf("batchID = %+v; want %+v", batchID, uuid.UUID{})
	}

	// aggregation is still in effect but not freshly triggered
	aggregationManager.countIncident("plan1")
	batchID, triggerBool = aggregationManager.checkAggregation("plan1")

	if triggerBool != false {
		t.Fatalf("triggerBool = %+v; want %+v", triggerBool, false)
	}
	if batchID == defaultUUID {
		t.Fatalf("batchID = %+v; want %+v", batchID, uuid.UUID{})
	}

	// wait so that aggregationwindow expires
	time.Sleep(1 * time.Second)

	// reset aggregation
	aggregationManager.resetAggregation()

	aggregationManager.countIncident("plan1")
	batchID, triggerBool = aggregationManager.checkAggregation("plan1")

	// plan is no longer under aggregation
	if triggerBool != false {
		t.Fatalf("triggerBool = %+v; want %+v", triggerBool, false)
	}
	if batchID != defaultUUID {
		t.Fatalf("batchID = %+v; want %+v", batchID, uuid.UUID{})
	}

}
