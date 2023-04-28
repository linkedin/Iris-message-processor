package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	gometrics "github.com/rcrowley/go-metrics"
)

var (
	counterAuthFailures        = gometrics.NewCounter()
	counterAuthSuccesses       = gometrics.NewCounter()
	counterAuthRefreshFailures = gometrics.NewCounter()
	appAuthMapMutex            = sync.RWMutex{}
)

type AuthChecker struct {
	Quit        chan int // send signal to shutdown
	config      *Config
	appAuthMap  map[string]string
	irisClient  *IrisClient
	logger      *Logger
	mainWg      *sync.WaitGroup
	readyStatus bool
}

// AuthHeader compute hmac authorization header and hmac hash
func AuthHeader(method, path string, body []byte, application string, key []byte) (string, string, error) {
	payload := strconv.AppendInt(make([]byte, 0), time.Now().UTC().Unix()/30, 10)
	payload = append(payload, ' ')
	payload = append(payload, []byte(method)...)
	payload = append(payload, ' ')
	payload = append(payload, []byte(path)...)
	payload = append(payload, ' ')
	payload = append(payload, []byte(body)...)
	mac := hmac.New(sha512.New, key)
	_, err := mac.Write(payload)
	hmacHash := base64.URLEncoding.EncodeToString(mac.Sum(nil))
	hmacHeaderValue := "hmac " + application + ":" + hmacHash
	return hmacHeaderValue, hmacHash, err
}

func (a *AuthChecker) RegisterMetrics(reg gometrics.Registry) {
	reg.Register("authChecker.internal.auth_failures_total", counterAuthFailures)
	reg.Register("authChecker.internal.auth_successes_total", counterAuthSuccesses)
	reg.Register("authChecker.internal.auth_refresh_failure", counterAuthRefreshFailures)
}

// CheckStatus indicates if authchecker has successfully fetched auth data from iris
func (a *AuthChecker) CheckStatus() bool {
	appAuthMapMutex.RLock()
	defer appAuthMapMutex.RUnlock()
	return a.readyStatus
}

// CheckAuth evaluates a requests Authorization header
func (a *AuthChecker) CheckAuth(request *http.Request) (bool, error) {

	// if auth debug mode is enabled skip checking authentication
	if a.config.DebugAuth {
		counterAuthSuccesses.Inc(1)
		return true, nil
	}

	authHeader := request.Header.Get("Authorization")
	// no auth header or auth header is too short to be valid, missing headers are returned as ""
	if authHeader == "" || len(authHeader) < len("hmac a:h") {
		counterAuthFailures.Inc(1)
		a.logger.Infof("HMAC check failed: Auth header missing or incorrectly formatted for HMAC authentication")
		return false, nil
	}

	// trim prefix "hmac " from auth header value
	s := authHeader[len("hmac "):]
	// extract application:hmacHash
	ss := strings.Split(s, ":")
	if len(ss) != 2 {
		counterAuthFailures.Inc(1)
		a.logger.Infof("HMAC check failed: Auth header incorrectly formatted for HMAC authentication")
		return false, nil
	}
	application := ss[0]
	hmacHash := ss[1]

	appAuthMapMutex.Lock()
	if _, ok := a.appAuthMap[application]; !ok {
		appAuthMapMutex.Unlock()
		counterAuthFailures.Inc(1)
		a.logger.Infof("HMAC check failed: Application: %v not found", application)
		return false, nil
	}
	key := []byte(a.appAuthMap[application])
	appAuthMapMutex.Unlock()

	bodyBytes, err := ioutil.ReadAll(request.Body)
	if err != nil {
		counterAuthFailures.Inc(1)
		a.logger.Infof("HMAC check failed: Could not read request body")
		return false, nil
	}
	// body can only be read once so create new ReadCloser for consumption by handler
	request.Body = ioutil.NopCloser(bytes.NewReader(bodyBytes))

	// generate HMAC hash from request data and stored API key for the corresponding application
	_, calculatedHmacHash, err := AuthHeader(request.Method, request.URL.Path, bodyBytes, application, key)
	if err != nil {
		a.logger.Errorw("HMAC check failed: Failed to calculate HMAC hash", "error", err)
		counterAuthFailures.Inc(1)
		return false, errors.New("Server failed to calculate HMAC hash")
	}

	// compare the request auth header HMAC hash to the calculated hash
	if calculatedHmacHash != hmacHash {
		counterAuthFailures.Inc(1)
		a.logger.Infof("HMAC check failed: Server calculated HMAC hash did not match the hash provided by the request")
		return false, nil
	}

	counterAuthSuccesses.Inc(1)
	return true, nil
}

// NewAuthChecker create new AuthChecker instance
func NewAuthChecker(irisClient *IrisClient, config *Config, logger *Logger, quit chan int, wg *sync.WaitGroup, metricsRegistry *gometrics.Registry) *AuthChecker {

	authChecker := AuthChecker{
		Quit:        quit, // send signal to shutdown
		config:      config,
		appAuthMap:  make(map[string]string),
		irisClient:  irisClient,
		logger:      logger,
		mainWg:      wg,
		readyStatus: false,
	}

	if appConfig.MetricsDryRun == false && metricsRegistry != nil {
		authChecker.RegisterMetrics(*metricsRegistry)
	}

	return &authChecker
}

// Run update application auth info every 60 seconds
func (a *AuthChecker) Run() {
	// close wg to signal main process we have gracefully terminated
	defer a.mainWg.Done()
	ticker := time.NewTicker(a.config.RunLoopDuration * time.Second)
	defer ticker.Stop()
	for ; true; <-ticker.C {
		// fetch latest iris-api auth data
		keyMap, err := a.irisClient.GetApplicationAuthData()
		if err != nil {
			counterAuthRefreshFailures.Inc(1)
			a.logger.Errorw("Failed to retrieve Iris-API app auth information", "error", err)
			continue
		}

		// replace current application-> api-key mapping with the one we just got from iris-api
		if len(keyMap) > 0 {
			appAuthMapMutex.Lock()
			a.appAuthMap = keyMap
			a.readyStatus = true
			appAuthMapMutex.Unlock()
		}

		a.logger.Infof("Refreshed app auth info...")
		select {
		case <-a.Quit:
			a.logger.Infof("stopped authchecker...")
			return
		default:
			continue
		}
	}

}
