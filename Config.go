// Package config handles transforming configs from json file to a golang struct
package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"time"
)

// Config represents the config json as a golang struct
type Config struct {
	Port                   string   `json:"Port"`
	FQDN                   string   `json:"FQDN"`
	AppName                string   `json:"AppName"`
	TLSCertPath            string   `json:"TLSCertPath"`
	TLSKeyPath             string   `json:"TLSKeyPath"`
	TLSCAPath              string   `json:"TLSCAPath"`
	MetricsDryRun          bool     `json:"MetricsDryRun"`
	SenderDryRun           bool     `json:"SenderDryRun"`
	IrisDryRun             bool     `json:"IrisDryRun"`
	StorageDryRun          bool     `json:"StorageDryRun"`
	OutboundProxyURL       string   `json:"OutboundProxyURL"`
	OutboundProxyParsedURL *url.URL `json:"-"`

	RunLoopDuration time.Duration `json:"RunLoopDuration"`

	MySQLUser            string        `json:"MySQLUser"`
	MySQLPassword        string        `json:"MySQLPassword"`
	MySQLAddress         string        `json:"MySQLAddress"`
	MySQLDBName          string        `json:"MySQLDBName"`
	MySQLConnTimeout     time.Duration `json:"MySQLConnTimeout"`
	MySQLReadTimeout     time.Duration `json:"MySQLReadTimeout"`
	MySQLWriteTimeout    time.Duration `json:"MySQLWriteTimeout"`
	MySQLMaxConnLifeTime time.Duration `json:"MySQLMaxConnLifeTime"`
	MySQLMaxConnIdleTime time.Duration `json:"MySQLMaxConnIdleTime"`
	MySQLMaxOpenConns    int           `json:"MySQLMaxOpenConns"`
	MySQLMaxIdleConns    int           `json:"MySQLMaxIdleConns"`

	IrisBaseURL string `json:"IrisBaseURL"`
	IrisAppName string `json:"IrisAppName"`
	IrisKey     string `json:"IrisKey"`

	DebugAuth bool `json:"DebugAuth"`

	MaxQueueLength     int            `json:"MaxQueueLength"`
	IrisBatchSize      int            `json:"IrisBatchSize"`
	MaxMessageRetries  int            `json:"MaxMessageRetries"`
	VendorRateLimitMap map[string]int `json:"VendorRateLimitMap"`
	PersistDroppedMsgs bool           `json:"PersistDroppedMsgs"`

	SMTPServerAddr   string        `json:"SMTPServerAddr"`
	DefaultFromEmail string        `json:"DefaultFromEmail"`
	SMTPTimeout      time.Duration `json:"SMTPTimeout"`

	SlackAPIKey   string        `json:"SlackAPIKey"`
	SlackTitleURL string        `json:"SlackTitleURL"`
	SlackTimeout  time.Duration `json:"SlackTimeout"`

	TwilioTimeout          time.Duration     `json:"TwilioTimeout"`
	TwilioAccountSid       string            `json:"AccountSid"`
	TwilioAuthToken        string            `json:"AuthToken"`
	TwilioNumbers          []string          `json:"TwilioNumbers"`
	ReservedNumbersMap     map[string]string `json:"ReservedNumbersMap"`
	IrisRelayBaseURL       string            `json:"IrisRelayBaseURL"`
	StatusCallbackEndpoint string            `json:"StatusCallbackEndpoint"`
	SayEndpoint            string            `json:"SayEndpoint"`
	GatherEndpoint         string            `json:"GatherEndpoint"`

	FCMTimeout time.Duration `json:"FCMTimeout"`
	FCMAPIKey  string        `json:"FCMAPIKey"`

	LogPath    string `json:"LogPath"`
	Debug      bool   `json:"DebugLogging"`
	MaxAgeDays int    `json:"MaxAgeDays"`

	// Smuggle a custom http client into tests
	TestHttpClient *http.Client `json:"-"`
}

// Parse parsees configs from json file
func ParseConfig(jsonConfig string, appConfig *Config) error {
	file, err := os.Open(jsonConfig)
	if err != nil {
		return err
	}
	defer file.Close()
	err = json.NewDecoder(file).Decode(&appConfig)
	if err != nil {
		return err
	}

	return nil
}
