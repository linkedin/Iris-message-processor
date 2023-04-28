package main

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestCheckAuth(t *testing.T) {
	logger := NewLogger("tests", "", false)
	defer logger.RedirectStdLogger()()
	defer logger.Close()
	var authClient *AuthChecker

	// capture irisClient requests
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		// because Iris-api and iris-message-processor use the same authentication mechanism we can authenticate a request from our irisclient to test it
		authBool, err := authClient.CheckAuth(r)
		if !authBool || err != nil {
			t.Fatalf("failed to authenticate with error: %v", err)
		}

	}))
	defer ts.Close()
	var quitChan chan int
	cfg := Config{
		IrisDryRun:    false,
		MetricsDryRun: false,
		DebugAuth:     false,
	}
	irisClient := NewIrisClient(ts.URL, "iris-message-processor", "dummykey", nil, &cfg, logger)
	var dummyWg sync.WaitGroup

	authClient = NewAuthChecker(irisClient, &cfg, logger, quitChan, &dummyWg, nil)
	authClient.appAuthMap["iris-message-processor"] = "dummykey"

	// iris-api request will be captured and authed with out internal authChecker if auth fails the test fails
	irisClient.PostEscalateIncident(1, 2, false)

}
