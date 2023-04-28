package main

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"time"

	gometrics "github.com/rcrowley/go-metrics"
	"github.com/twilio/twilio-go/client"
	apiv2010 "github.com/twilio/twilio-go/rest/api/v2010"
)

const twilioMaxBodyLen = 1500

type TwilioVendor struct {
	*Vendor
	twilioClient client.Client
	vendorName   string
}

// NewTwilioVendor create new TwilioVendor instance
func NewTwilioVendor(dbClient *DbClient, config *Config, logger *Logger, metricsRegistry *gometrics.Registry) *TwilioVendor {

	vendor := NewVendor(dbClient, config, logger, "twilio")
	accountSid := config.TwilioAccountSid
	authToken := config.TwilioAuthToken

	twilioClient := client.Client{
		Credentials: &client.Credentials{
			Username: accountSid,
			Password: authToken,
		},
	}

	if config.TestHttpClient != nil {
		twilioClient.HTTPClient = config.TestHttpClient
	} else {
		twilioClient.HTTPClient = &http.Client{
			Timeout: config.TwilioTimeout * time.Second,
		}
		if config.OutboundProxyParsedURL != nil {
			twilioClient.HTTPClient.Transport = &http.Transport{
				Proxy: http.ProxyURL(config.OutboundProxyParsedURL),
			}
		}
	}

	twilioClient.SetAccountSid(accountSid)

	twilioVendor := TwilioVendor{
		Vendor:       vendor,
		twilioClient: twilioClient,
	}

	if appConfig.MetricsDryRun == false && metricsRegistry != nil {
		twilioVendor.Vendor.RegisterMetrics(*metricsRegistry)
	}

	return &twilioVendor
}

func (t *TwilioVendor) SendMessage(messageQueueItem MessageQueueItem) bool {

	if len(t.Vendor.config.TwilioNumbers) < 1 {
		t.Vendor.logger.Errorf("Failed to send message, no Twilio numbers configured!")
		t.Vendor.counterFailures.Inc(1)
		return false
	}

	if len(messageQueueItem.message.Body) > twilioMaxBodyLen {
		messageQueueItem.message.Body = messageQueueItem.message.Body[:twilioMaxBodyLen]
	}

	var err error
	var sid string
	if messageQueueItem.message.Mode == CallMode {
		sid, err = t.sendVoice(messageQueueItem)
	} else {
		sid, err = t.sendText(messageQueueItem)
	}

	if err != nil {
		t.Vendor.logger.Errorf("Failed sending twilio message with error %s. Message: %+v", err, messageQueueItem)
		t.Vendor.counterFailures.Inc(1)
		if messageQueueItem.retries >= t.Vendor.config.MaxMessageRetries {
			status := fmt.Sprintf("failed with error: %s", err)
			t.Vendor.persistMsg(messageQueueItem, sid, status)
		}
		return false
	}
	t.Vendor.counterSuccesses.Inc(1)

	// store message and status in the db
	t.Vendor.persistMsg(messageQueueItem, sid, "sent to vendor")

	return true
}

func (t *TwilioVendor) getNumber(messageQueueItem MessageQueueItem) string {
	// check if application is using a reserved number
	if num, ok := t.Vendor.config.ReservedNumbersMap[messageQueueItem.message.Application]; ok {
		return num
	}
	// otherwise pick a random number from the pool
	idx := rand.Intn(len(t.Vendor.config.TwilioNumbers))
	return t.Vendor.config.TwilioNumbers[idx]

}

func (t *TwilioVendor) sendText(messageQueueItem MessageQueueItem) (string, error) {

	from := t.getNumber(messageQueueItem)
	to := messageQueueItem.message.Destination
	statusCallbackURL := t.config.IrisRelayBaseURL + t.config.StatusCallbackEndpoint

	body := messageQueueItem.message.Body
	// truncate body to prevent exceeding twilio limit and allow appending of extra text
	if len(body) > 1450 {
		body = body[0:1450]
	}

	// if message is part of an incident add claiming instructions
	if messageQueueItem.incidentID > 0 {
		body = body + fmt.Sprintf("\n\nreply '%d claim' to claim incident %d or 'claim all' to claim all incidents targeting you", messageQueueItem.incidentID, messageQueueItem.incidentID)
	}

	params := &apiv2010.CreateMessageParams{
		To:             &to,
		From:           &from,
		Body:           &body,
		StatusCallback: &statusCallbackURL,
	}

	textMsgSvc := apiv2010.NewApiServiceWithClient(&t.twilioClient)
	resp, err := textMsgSvc.CreateMessage(params)
	if err != nil {
		return "", err
	}
	msgSID := *resp.Sid
	return msgSID, nil
}

func (t *TwilioVendor) sendVoice(messageQueueItem MessageQueueItem) (string, error) {

	from := t.getNumber(messageQueueItem)
	to := messageQueueItem.message.Destination
	statusCallbackURL := t.config.IrisRelayBaseURL + t.config.StatusCallbackEndpoint
	sayURL := t.config.IrisRelayBaseURL + t.config.SayEndpoint
	gatherURL := t.config.IrisRelayBaseURL + t.config.GatherEndpoint
	loop := 3 // how many times message is repeated by twilio voice
	content := messageQueueItem.message.Body
	source := messageQueueItem.message.Application
	// TODO implement app specific instructions
	instruction := "press 2 to claim"

	// build url
	var callURL string
	if messageQueueItem.incidentID > 0 {
		u, err := url.Parse(gatherURL)
		if err != nil {
			return "", err
		}
		q := u.Query()
		q.Set("content", content)
		q.Set("loop", strconv.Itoa(loop))
		q.Set("source", source)
		q.Set("incident_id", strconv.FormatUint(messageQueueItem.incidentID, 10))
		q.Set("message_id", messageQueueItem.message.MessageUUID.String())
		q.Set("target", messageQueueItem.message.Target)
		q.Set("instruction", instruction)
		u.RawQuery = q.Encode()
		callURL = u.String()
	} else {
		u, err := url.Parse(sayURL)
		if err != nil {
			return "", err
		}
		q := u.Query()
		q.Set("content", content)
		q.Set("loop", strconv.Itoa(loop))
		q.Set("source", source)
		q.Set("message_id", messageQueueItem.message.MessageUUID.String())
		q.Set("target", messageQueueItem.message.Target)
		u.RawQuery = q.Encode()
		callURL = u.String()
	}

	params := &apiv2010.CreateCallParams{
		To:             &to,
		From:           &from,
		Url:            &callURL,
		StatusCallback: &statusCallbackURL,
	}

	voiceCallSvc := apiv2010.NewApiServiceWithClient(&t.twilioClient)
	resp, err := voiceCallSvc.CreateCall(params)
	if err != nil {
		return "", err
	}
	msgSID := *resp.Sid
	return msgSID, nil
}
