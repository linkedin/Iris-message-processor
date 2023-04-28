package main

import (
	"net/http"
	"time"

	fcm "github.com/appleboy/go-fcm"
	gometrics "github.com/rcrowley/go-metrics"
)

type FCMVendor struct {
	*Vendor
	fcmClient *fcm.Client
}

const fcmMaxBodyLen = 64

// NewFCMVendor create new FCMVendor instance
func NewFCMVendor(dbClient *DbClient, config *Config, logger *Logger, metricsRegistry *gometrics.Registry) *FCMVendor {

	vendor := NewVendor(dbClient, config, logger, "fcm")

	httpClient := &http.Client{Timeout: config.FCMTimeout * time.Second}
	if config.OutboundProxyParsedURL != nil {
		httpClient.Transport = &http.Transport{
			Proxy: http.ProxyURL(config.OutboundProxyParsedURL),
		}
	}

	// Create a FCM client
	fcmClient, err := fcm.NewClient(config.FCMAPIKey, fcm.WithHTTPClient(httpClient))
	if err != nil {
		logger.Errorf("Failed to properly initialize FCM client with error: %s", err)
	}

	fcmVendor := FCMVendor{
		Vendor:    vendor,
		fcmClient: fcmClient,
	}

	if appConfig.MetricsDryRun == false && metricsRegistry != nil {
		fcmVendor.Vendor.RegisterMetrics(*metricsRegistry)
	}

	return &fcmVendor
}

func (f *FCMVendor) SendMessage(messageQueueItem MessageQueueItem) bool {

	// if message is not part of an incident skip sending
	if messageQueueItem.incidentID == 0 {
		return true
	}

	// truncate to fit in push notification char length limit
	if len(messageQueueItem.plan) > fcmMaxBodyLen {
		messageQueueItem.plan = messageQueueItem.plan[:fcmMaxBodyLen]
	}
	if len(messageQueueItem.message.Body) > fcmMaxBodyLen {
		messageQueueItem.message.Body = messageQueueItem.message.Body[:fcmMaxBodyLen]
	}

	// send messages to all devices registered to this user
	for _, deviceKey := range messageQueueItem.message.DeviceIDs {

		msg := &fcm.Message{
			To: deviceKey,
			Notification: &fcm.Notification{
				Title: messageQueueItem.plan,
				Body:  messageQueueItem.message.Body,
			},
		}

		// Send the message without retries.
		_, err := f.fcmClient.Send(msg)
		if err != nil {
			f.Vendor.logger.Errorf("Failed sending fcm push notification with error %s. Message: %+v", err, messageQueueItem)
			f.Vendor.counterFailures.Inc(1)
		} else {
			f.Vendor.counterSuccesses.Inc(1)
		}
	}

	// we don't want to retry push messages so always return true even if sending them failed
	return true
}
