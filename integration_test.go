package main

import (
	"bytes"

	"github.com/jarcoal/httpmock"
	"github.com/stretchr/testify/require"
	apiv2010 "github.com/twilio/twilio-go/rest/api/v2010"

	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIntegration(t *testing.T) {
	appConfig := Config{
		RunLoopDuration:    1,
		IrisBatchSize:      100,
		TwilioAccountSid:   "FakeSid",
		TwilioAuthToken:    "faketokenaaaaaaaaaaaaaaaaaaaaaaa",
		TwilioNumbers:      []string{"555-555-5555"},
		TestHttpClient:     &http.Client{},
		VendorRateLimitMap: map[string]int{"smtp": 1000, "twilio": 1000, "slack": 1000, "fcm": 1000, "drop": 1000, "error": 1000, "dummyVendor": 1000},
	}
	logger := NewLogger("main", appConfig.LogPath, appConfig.Debug)

	//
	// Mock iris http server
	//
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		var ret []byte
		var err error

		if r.Method == http.MethodGet && r.URL.Path == "/v0/internal/priority_mode_map" {
			fixturePath := filepath.Join("testData", "priorityModeMap.json")
			ret, err = ioutil.ReadFile(fixturePath)
			require.Nil(t, err)
		} else if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v0/internal/sender_heartbeat/") {
		} else if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v0/internal/incidents/") {
			ret = []byte(`[5376371,5376376]`)
		} else if r.Method == http.MethodGet && r.URL.Path == "/v0/plans/72838" {
			fixturePath := filepath.Join("testData", "plan.json")
			ret, err = ioutil.ReadFile(fixturePath)
			require.Nil(t, err)
		} else if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v0/internal/incidents/") {
			payload := &IncidentIDList{}
			err = json.NewDecoder(r.Body).Decode(&payload)
			require.Nil(t, err)
			require.Equal(t, []uint64{5376371, 5376376}, payload.IDs)
			fixturePath := filepath.Join("testData", "incidents.json")
			ret, err = ioutil.ReadFile(fixturePath)
			require.Nil(t, err)
		} else if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v0/internal/incident/") {
			payload := &IncidentStep{}
			err = json.NewDecoder(r.Body).Decode(&payload)
			require.Nil(t, err)
		} else if r.Method == http.MethodGet && r.URL.Path == "/v0/internal/build_message" {
			fixturePath := filepath.Join("testData", "message.json")
			ret, err = ioutil.ReadFile(fixturePath)
			require.Nil(t, err)
		} else if r.Method == http.MethodGet && r.URL.Path == "/v0/internal/sender_peer_count" {
			fixturePath := filepath.Join("testData", "peer_count.json")
			ret, err = ioutil.ReadFile(fixturePath)
			require.Nil(t, err)
		} else {
			t.Errorf("Unknown method/path: %v %v", r.Method, r.URL.Path)
		}

		if len(ret) > 0 {
			w.Write(ret)
		}

	}))
	defer ts.Close()

	metricsCollector := NewMetricsCollector(MetricsCollectorConfig{
		AppName: appConfig.AppName,
		FQDN:    appConfig.FQDN,
		DryRun:  appConfig.MetricsDryRun,
	}, logger.Clone("metrics_collector"))

	// Overshadow global vars
	var mainWg sync.WaitGroup
	quit := make(chan int)
	dbClient := NewDbClient(&appConfig, logger.Clone("dbClient"), &metricsCollector.Registry)

	irisClient := NewIrisClient(ts.URL, appConfig.IrisAppName, appConfig.IrisKey, &metricsCollector.Registry, &appConfig, logger.Clone("irisClient"))

	//
	// Mock twilio endpoints. Cannot use a httptest.Server as twilio client
	// does not let us customize the base url
	//
	httpmock.ActivateNonDefault(appConfig.TestHttpClient)
	defer httpmock.DeactivateAndReset()

	textsSent := 0
	callsSent := 0
	twilioRecipientsCnt := map[string]int{}

	httpmock.RegisterResponder("POST", "https://api.twilio.com/2010-04-01/Accounts/FakeSid/Calls.json",
		func(req *http.Request) (*http.Response, error) {
			callsSent++

			stringMap := getTwilioQuery(req)

			if stringMap["From"] != "555-555-5555" {
				t.Fatalf("From number not correctly set")
			}

			if stringMap["To"] != "+1 123-456-0000" {
				t.Fatalf("To number not correctly set")
			}
			if _, ok := twilioRecipientsCnt[stringMap["To"]]; !ok {
				twilioRecipientsCnt[stringMap["To"]] = 0
			}
			twilioRecipientsCnt[stringMap["To"]] += 1

			sid := "FakeCallSid"
			payload := &apiv2010.ApiV2010Call{Sid: &sid}
			return httpmock.NewJsonResponse(200, payload)
		},
	)

	httpmock.RegisterResponder("POST", "https://api.twilio.com/2010-04-01/Accounts/FakeSid/Messages.json",
		func(req *http.Request) (*http.Response, error) {
			textsSent++

			stringMap := getTwilioQuery(req)

			if stringMap["From"] != "555-555-5555" {
				t.Fatalf("From number not correctly set")
			}
			if _, ok := twilioRecipientsCnt[stringMap["To"]]; !ok {
				twilioRecipientsCnt[stringMap["To"]] = 0
			}
			twilioRecipientsCnt[stringMap["To"]] += 1

			sid := "FakeSmsSid"
			payload := &apiv2010.ApiV2010Message{Sid: &sid}
			return httpmock.NewJsonResponse(200, payload)
		},
	)

	aggregationManager := NewAggregationManager(irisClient, &appConfig, logger.Clone("aggregationManager"), quit, &mainWg, &metricsCollector.Registry)
	messageQueueManager := NewMessageQueueManager(aggregationManager, dbClient, irisClient, &appConfig, logger.Clone("messageQueueManager"), quit, &mainWg, &metricsCollector.Registry)

	incidentManager := NewIncidentManager(irisClient, messageQueueManager, aggregationManager, &appConfig, logger.Clone("incidentManager"), quit, &mainWg, &metricsCollector.Registry)

	err := incidentManager.loadPriorityMap()
	require.Nil(t, err)

	var vendorWg sync.WaitGroup
	messageQueueManager.initVendors(&vendorWg)

	// At this point, lots of messages are queued
	incidentManager.RunOnce()

	// Process queue
	messageQueueManager.RunOnce()

	// Wait for messages to be sent
	time.Sleep(10 * time.Second)

	close(quit)
	close(messageQueueManager.vendorQuit)
	for _, v := range messageQueueManager.vendorMap {
		v.QuitVendor()
	}
	vendorWg.Wait()
	// make sure vendors have shut down gracefully
	mainWg.Wait()

	// assert all messages got queued
	got := len(messageQueueManager.messageTrackerMap)
	want := 42
	if got != want {
		t.Fatalf("mismatched number of messages in queue got = %v; want %v", got, want)
	}

	// run incident manager again to clean up incidents if all the messages were sent
	incidentManager.RunOnce()

	// assert message queue got cleaned up
	got = len(messageQueueManager.messageTrackerMap)
	want = 0
	if got != want {
		t.Fatalf("mismatched number of messages in queue got = %v; want %v", got, want)
	}

	// assert that we sent the expected number of calls and sms messages
	got = callsSent
	want = 2
	if got != want {
		t.Fatalf("mismatched number of calls sent = %v; want %v", got, want)
	}
	got = textsSent
	want = 18
	if got != want {
		t.Fatalf("mismatched number of sms messages sent = %v; want %v", got, want)
	}

	// assert that we messaged each number the correct number of times
	want = 2
	for target, msgCnt := range twilioRecipientsCnt {
		if msgCnt != want {
			t.Fatalf("Sent %v messages to %v, expected %v", msgCnt, target, want)
		}
	}

}

func getTwilioQuery(req *http.Request) map[string]string {
	// create a buffer to store the request
	var buffer bytes.Buffer

	// Write the request body to the buffer
	requestBody, err := ioutil.ReadAll(req.Body)
	if err == nil {
		buffer.Write(requestBody)
	}

	// Reset the request body so it can be read again if necessary
	req.Body = ioutil.NopCloser(bytes.NewBuffer(requestBody))

	// Return the formatted request as a string
	bufStr := buffer.String()

	values, err := url.ParseQuery(bufStr)
	if err != nil {
		panic(err)
	}

	// Convert the url.Values struct into a map of strings
	stringMap := make(map[string]string)
	for key, val := range values {
		stringMap[key] = val[0]
	}

	return stringMap
}
