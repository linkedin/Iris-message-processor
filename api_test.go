package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// HMACPost generic POST with HMAC authentication for use in testing server endpoints
func HMACPost(url string, jsonBody []byte) (respBody []byte, statusCode int, err error) {

	//create http client
	httpClient := &http.Client{
		Timeout: time.Second * 5,
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return
	}

	req.Header.Set("Content-Type", "application/json")

	auth, _, err := AuthHeader("POST", "/", jsonBody, "iris", []byte("iriskey"))
	if err != nil {
		return
	}
	req.Header.Add("Authorization", auth)

	resp, err := httpClient.Do(req)
	if err != nil {
		return
	}
	statusCode = resp.StatusCode

	defer resp.Body.Close()
	respBody, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}

	return

}

func TestpostNotification(t *testing.T) {
	logger := NewLogger("tests", "", false)
	defer logger.RedirectStdLogger()()
	defer logger.Close()
	buildMessagePath := filepath.Join("testData", "message.json")
	buildMessageData, _ := ioutil.ReadFile(buildMessagePath)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/notification") {
			postNotification(w, r, nil)
		} else if strings.Contains(r.URL.Path, "/build_message") {
			w.Write(buildMessageData)
		}
	}))
	defer ts.Close()

	// configure authchecker
	var quitChan chan int
	cfg := Config{
		IrisDryRun:    false,
		MetricsDryRun: false,
		DebugAuth:     false,
	}
	var dummyWg sync.WaitGroup
	irisClient := NewIrisClient(ts.URL, "iris-message-processor", "dummykey", nil, &cfg, logger)
	authChecker = NewAuthChecker(irisClient, &cfg, logger, quitChan, &dummyWg, nil)
	authChecker.appAuthMap["iris"] = "iriskey"

	payload := Notification{
		Role:     "mailing-list",
		Target:   "monitoring-visualization-and-alerting",
		Subject:  "subject",
		Template: "iris-message-processor-template",
		Context: map[string]interface{}{
			"var": "successfully rendered the template variable",
		},
		Priority:    "High",
		PriorityID:  17,
		Application: "iris-message-processor",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal json with error %v", err)
	}

	body, statusCode, err := HMACPost(ts.URL, b)
	if err != nil {
		t.Fatalf("Failed to post with error %v", err)
	}

	var got, want int
	got = statusCode
	want = 200
	if got != want {
		fmt.Printf(string(body))
		t.Fatalf("response status code = %d; want %d", got, want)
	}

}
