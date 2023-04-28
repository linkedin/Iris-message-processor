package main

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestGetApplicationAuthData(t *testing.T) {
	logger := NewLogger("tests", "", false)
	defer logger.RedirectStdLogger()()
	defer logger.Close()
	fixturePath := filepath.Join("testData", "appAuth.json")
	fixtureData, _ := ioutil.ReadFile(fixturePath)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.UserAgent()
		want := "iris-message-processor"
		if got != want {
			t.Fatalf("Request made with UserAgent = %s; want %s", got, want)
		}
		w.Write(fixtureData)
	}))
	defer ts.Close()

	cfg := Config{IrisDryRun: false}
	irisClient := NewIrisClient(ts.URL, "iris-message-processor", "dummykey", nil, &cfg, logger)

	authMap, err := irisClient.GetApplicationAuthData()
	if err != nil {
		t.Fatalf("Failed to turn JSON response to struct: %v", err)
	}
	var got, want int
	got = len(authMap)
	want = 2
	if got != want {
		t.Fatalf("len(authMap) = %d; want %d", got, want)
	}

	gotKey := authMap["app1"]
	wantKey := "key1"
	if gotKey != wantKey {
		t.Fatalf("got: %s; want %s", gotKey, wantKey)
	}

}

func TestGetApplication(t *testing.T) {
	logger := NewLogger("tests", "", false)
	defer logger.RedirectStdLogger()()
	defer logger.Close()
	fixturePath := filepath.Join("testData", "application.json")
	fixtureData, _ := ioutil.ReadFile(fixturePath)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.UserAgent()
		want := "iris-message-processor"
		if got != want {
			t.Fatalf("Request made with UserAgent = %s; want %s", got, want)
		}
		w.Write(fixtureData)
	}))
	defer ts.Close()

	cfg := Config{IrisDryRun: false}

	irisClient := NewIrisClient(ts.URL, "iris-message-processor", "dummykey", nil, &cfg, logger)

	app, err := irisClient.GetApplication("alerts")
	if err != nil {
		t.Fatalf("Failed to turn JSON response to struct: %v   %v", app, err)
	}
	var got, want string
	got = app.Name
	want = "alerts"
	if got != want {
		t.Fatalf("name = %s; want %s", got, want)
	}

	got = app.Categories[0].Name
	want = "cat1"
	if got != want {
		t.Fatalf("name = %s; want %s", got, want)
	}

}

func TestGetTemplate(t *testing.T) {
	logger := NewLogger("tests", "", false)
	defer logger.RedirectStdLogger()()
	defer logger.Close()
	fixturePath := filepath.Join("testData", "template.json")
	fixtureData, _ := ioutil.ReadFile(fixturePath)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.UserAgent()
		want := "iris-message-processor"
		if got != want {
			t.Fatalf("Request made with UserAgent = %s; want %s", got, want)
		}
		w.Write(fixtureData)
	}))
	defer ts.Close()

	cfg := Config{IrisDryRun: false}
	irisClient := NewIrisClient(ts.URL, "iris-message-processor", "dummykey", nil, &cfg, logger)

	template, err := irisClient.GetTemplate("alerts Default")
	if err != nil {
		t.Fatalf("Failed to turn JSON response to struct: %v   %v", template, err)
	}
	var got, want string
	got = template.Name
	want = "alerts Default"
	if got != want {
		t.Fatalf("name = %s; want %s", got, want)
	}

	got = template.Content["iris-message-processor"]["email"]["subject"]
	want = "testsubject"
	if got != want {
		t.Fatalf("name = %s; want %s", got, want)
	}

}

func TestGetPlan(t *testing.T) {
	logger := NewLogger("tests", "", false)
	defer logger.RedirectStdLogger()()
	defer logger.Close()
	fixturePath := filepath.Join("testData", "plan.json")
	fixtureData, _ := ioutil.ReadFile(fixturePath)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.UserAgent()
		want := "iris-message-processor"
		if got != want {
			t.Fatalf("Request made with UserAgent = %s; want %s", got, want)
		}
		w.Write(fixtureData)
	}))
	defer ts.Close()

	cfg := Config{IrisDryRun: false}
	irisClient := NewIrisClient(ts.URL, "iris-message-processor", "dummykey", nil, &cfg, logger)

	plan, err := irisClient.GetPlan("testplan")
	if err != nil {
		t.Fatalf("Failed to turn JSON response to struct: %v   %v", plan, err)
	}
	var got, want string
	got = plan.Name
	want = "iris-test-plan"
	if got != want {
		t.Fatalf("name = %s; want %s", got, want)
	}

	got = plan.Steps[0][0].Target
	want = "testuser"
	if got != want {
		t.Fatalf("name = %s; want %s", got, want)
	}

}

func TestGetUser(t *testing.T) {
	logger := NewLogger("tests", "", false)
	defer logger.RedirectStdLogger()()
	defer logger.Close()
	fixturePath := filepath.Join("testData", "user.json")
	fixtureData, _ := ioutil.ReadFile(fixturePath)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.UserAgent()
		want := "iris-message-processor"
		if got != want {
			t.Fatalf("Request made with UserAgent = %s; want %s", got, want)
		}
		w.Write(fixtureData)
	}))
	defer ts.Close()

	cfg := Config{IrisDryRun: false}
	irisClient := NewIrisClient(ts.URL, "iris-message-processor", "dummykey", nil, &cfg, logger)

	user, err := irisClient.GetUser("testuser")
	if err != nil {
		t.Fatalf("Failed to turn JSON response to struct: %v   %v", user, err)
	}
	var got, want string
	got = user.Name
	want = "testuser"
	if got != want {
		t.Fatalf("name = %s; want %s", got, want)
	}

	got = user.Devices[0].Platform
	want = "Android"
	if got != want {
		t.Fatalf("name = %s; want %s", got, want)
	}

}

func TestGetIncidents(t *testing.T) {
	logger := NewLogger("tests", "", false)
	defer logger.RedirectStdLogger()()
	defer logger.Close()
	fixturePath := filepath.Join("testData", "incidents.json")
	fixtureData, _ := ioutil.ReadFile(fixturePath)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.UserAgent()
		want := "iris-message-processor"
		if got != want {
			t.Fatalf("Request made with UserAgent = %s; want %s", got, want)
		}
		if r.Method == "GET" {
			p := []int{5376371, 5376376}
			json.NewEncoder(w).Encode(p)
		} else {
			w.Write(fixtureData)
		}
	}))
	defer ts.Close()

	cfg := Config{IrisDryRun: false}
	irisClient := NewIrisClient(ts.URL, "iris-message-processor", "dummykey", nil, &cfg, logger)
	incidentIDs, err := irisClient.GetIncidentIDs("hashstring")
	if err != nil {
		t.Fatalf("Failed to get incident IDs: %v", err)
	}
	incidents, err := irisClient.PostFetchIncidents(incidentIDs, "hashstring")
	if err != nil {
		t.Fatalf("Failed to turn JSON response to struct: %v   %v", incidents, err)
	}
	var got, want uint64
	got = incidents[0].IncidentID
	want = 5376371
	if got != want {
		t.Fatalf("id = %d; want %d", got, want)
	}

	var got2, want2 int
	got2 = len(incidents)
	want2 = 2
	if got2 != want2 {
		t.Fatalf("len = %d; want %d", got2, want2)
	}

}

func TestGetRoleTarget(t *testing.T) {
	logger := NewLogger("tests", "", false)
	defer logger.RedirectStdLogger()()
	defer logger.Close()
	fixturePath := filepath.Join("testData", "roleTarget.json")
	fixtureData, _ := ioutil.ReadFile(fixturePath)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.UserAgent()
		want := "iris-message-processor"
		if got != want {
			t.Fatalf("Request made with UserAgent = %s; want %s", got, want)
		}
		w.Write(fixtureData)
	}))
	defer ts.Close()

	cfg := Config{IrisDryRun: false}
	irisClient := NewIrisClient(ts.URL, "iris-message-processor", "dummykey", nil, &cfg, logger)

	userMap, err := irisClient.GetRoleTarget("role", "target")
	if err != nil {
		t.Fatalf("Failed to turn JSON response to struct: %v", err)
	}
	var got, want string
	got = userMap["testuser2"].Name
	want = "testuser2"
	if got != want {
		t.Fatalf("name = %s; want %s", got, want)
	}

	got = userMap["testuser"].Devices[0].Platform
	want = "Android"
	if got != want {
		t.Fatalf("name = %s; want %s", got, want)
	}

}

func TestGetBuildMessage(t *testing.T) {
	logger := NewLogger("tests", "", false)
	defer logger.RedirectStdLogger()()
	defer logger.Close()
	fixturePath := filepath.Join("testData", "message.json")
	fixtureData, _ := ioutil.ReadFile(fixturePath)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.UserAgent()
		want := "iris-message-processor"
		if got != want {
			t.Fatalf("Request made with UserAgent = %s; want %s", got, want)
		}
		w.Write(fixtureData)
	}))
	defer ts.Close()

	cfg := Config{IrisDryRun: false}
	irisClient := NewIrisClient(ts.URL, "iris-message-processor", "dummykey", nil, &cfg, logger)

	messageList, err := irisClient.GetBuildMessage(Notification{})
	if err != nil {
		t.Fatalf("Failed to turn JSON response to struct: %v", err)
	}
	var got, want int
	got = len(messageList)
	want = 11
	if got != want {
		t.Fatalf("name = %d; want %d", got, want)
	}

	var got2, want2 string
	got2 = messageList[0].Destination
	want2 = "+1 123-456-0000"
	if got2 != want2 {
		t.Fatalf("destination = %s; want %s", got2, want2)
	}

}

func TestRenderJinja(t *testing.T) {
	logger := NewLogger("tests", "", false)
	defer logger.RedirectStdLogger()()
	defer logger.Close()
	fixturePath := filepath.Join("testData", "jinja.json")
	fixtureData, _ := ioutil.ReadFile(fixturePath)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.UserAgent()
		want := "iris-message-processor"
		if got != want {
			t.Fatalf("Request made with UserAgent = %s; want %s", got, want)
		}
		w.Write(fixtureData)
	}))
	defer ts.Close()

	cfg := Config{IrisDryRun: false}
	irisClient := NewIrisClient(ts.URL, "iris-message-processor", "dummykey", nil, &cfg, logger)

	templateStr := "jinja str {{var}}"
	context := make(map[string]interface{})
	context["var"] = "foo"

	renderedStr, err := irisClient.GetRenderJinja(templateStr, context)
	if err != nil {
		t.Fatalf("Failed to turn JSON response to struct: %v", err)
	}
	var got, want string
	got = renderedStr
	want = "jinja str foo"
	if got != want {
		t.Fatalf("got = %s; want %s", got, want)
	}

}

func TestPostRegisterZone(t *testing.T) {
	logger := NewLogger("tests", "", false)
	defer logger.RedirectStdLogger()()
	defer logger.Close()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.UserAgent()
		want := "iris-message-processor"
		if got != want {
			t.Fatalf("Request made with UserAgent = %s; want %s", got, want)
		}
		b, err := ioutil.ReadAll(r.Body)
		if err != nil {
			panic(err)
		}
		gotBody := string(b)
		wantBody := `{"current_step":2,"active":false}`
		if gotBody != wantBody {
			t.Fatalf("Body sent = %s; want %s", b, wantBody)
		}

	}))
	defer ts.Close()

	cfg := Config{IrisDryRun: false}
	irisClient := NewIrisClient(ts.URL, "iris-message-processor", "dummykey", nil, &cfg, logger)

	err := irisClient.PostEscalateIncident(1, 2, false)
	if err != nil {
		t.Fatalf("Failed to post: %v", err)
	}

}

func TestGetPlanAggregationSettings(t *testing.T) {
	logger := NewLogger("tests", "", false)
	defer logger.RedirectStdLogger()()
	defer logger.Close()
	fixturePath := filepath.Join("testData", "aggSettings.json")
	fixtureData, _ := ioutil.ReadFile(fixturePath)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.UserAgent()
		want := "iris-message-processor"
		if got != want {
			t.Fatalf("Request made with UserAgent = %s; want %s", got, want)
		}
		w.Write(fixtureData)
	}))
	defer ts.Close()

	cfg := Config{IrisDryRun: false}
	irisClient := NewIrisClient(ts.URL, "iris-message-processor", "dummykey", nil, &cfg, logger)

	settingsList, err := irisClient.GetPlanAggregationSettings()
	if err != nil {
		t.Fatalf("Failed to turn JSON response to struct: %v", err)
	}
	var got, want int
	got = len(settingsList)
	want = 5
	if got != want {
		t.Fatalf("name = %d; want %d", got, want)
	}

	var got2, want2 string
	got2 = settingsList[0].PlanName
	want2 = "test-1"
	if got2 != want2 {
		t.Fatalf("name = %s; want %s", got2, want2)
	}

}

func TestGetPeerCount(t *testing.T) {
	logger := NewLogger("tests", "", false)
	defer logger.RedirectStdLogger()()
	defer logger.Close()
	fixturePath := filepath.Join("testData", "peerCount.json")
	fixtureData, _ := ioutil.ReadFile(fixturePath)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.UserAgent()
		want := "iris-message-processor"
		if got != want {
			t.Fatalf("Request made with UserAgent = %s; want %s", got, want)
		}
		w.Write(fixtureData)
	}))
	defer ts.Close()

	cfg := Config{IrisDryRun: false}
	irisClient := NewIrisClient(ts.URL, "iris-message-processor", "dummykey", nil, &cfg, logger)

	peerCount, err := irisClient.GetPeerCount()
	if err != nil {
		t.Fatalf("Failed to turn JSON response to struct: %v", err)
	}
	var got, want int
	got = peerCount
	want = 4
	if got != want {
		t.Fatalf("got = %d; want %d", got, want)
	}

}
