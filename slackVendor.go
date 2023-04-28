package main

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	gometrics "github.com/rcrowley/go-metrics"
	"github.com/slack-go/slack"
)

type SlackVendor struct {
	*Vendor
	slackClient *slack.Client
}

// NewSlackVendor create new SlackVendor instance
func NewSlackVendor(dbClient *DbClient, config *Config, logger *Logger, metricsRegistry *gometrics.Registry) *SlackVendor {

	vendor := NewVendor(dbClient, config, logger, "slack")
	// initialize slack client
	client := &http.Client{}
	if config.OutboundProxyParsedURL != nil {
		client.Transport = &http.Transport{
			Proxy: http.ProxyURL(config.OutboundProxyParsedURL),
		}
	}
	slackClient := slack.New(config.SlackAPIKey, slack.OptionDebug(config.Debug), slack.OptionHTTPClient(client))

	slackVendor := SlackVendor{
		Vendor:      vendor,
		slackClient: slackClient,
	}

	if appConfig.MetricsDryRun == false && metricsRegistry != nil {
		slackVendor.Vendor.RegisterMetrics(*metricsRegistry)
	}

	return &slackVendor
}

func (s *SlackVendor) SendMessage(messageQueueItem MessageQueueItem) bool {
	var attachment slack.Attachment
	// if notification is from an incident add the required attachment
	if messageQueueItem.incidentID != 0 {
		attachment = s.constructIncidentAttachment(messageQueueItem)
	}

	// TODO: handle custom notification block rendering

	if err := s.sendMessage(messageQueueItem, attachment); err != nil {
		errorText := err.Error()
		if errorText == "channel_not_found" || errorText == "not_in_channel" {
			status := fmt.Sprintf("failed with error: %s", errorText)
			s.Vendor.persistMsg(messageQueueItem, "", status)
			// return true to avoid retries
			return true
		}

		if messageQueueItem.retries >= s.Vendor.config.MaxMessageRetries {
			status := fmt.Sprintf("failed with error: %s", errorText)
			s.Vendor.persistMsg(messageQueueItem, "", status)
		}
		s.Vendor.logger.Errorf("Failed sending slack message with error %s. message: %+v", errorText, messageQueueItem)
		s.Vendor.counterFailures.Inc(1)
		return false
	}
	s.Vendor.counterSuccesses.Inc(1)

	// store message and status in the db
	s.Vendor.persistMsg(messageQueueItem, "", "sent to vendor")

	return true
}

//  post message to slack api
func (s *SlackVendor) sendMessage(messageQueueItem MessageQueueItem, attachment slack.Attachment) error {

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(time.Second*s.config.SlackTimeout))
	defer cancel()

	destination := messageQueueItem.message.Destination
	// if destination does not have a # or @ prefix assume it is a user and add a @ prefix so slack can resolve it
	if !(strings.HasPrefix(destination, "#") || strings.HasPrefix(destination, "@")) {
		destination = "@" + destination
	}

	_, _, err := s.slackClient.PostMessageContext(
		ctx,
		destination,
		slack.MsgOptionText(messageQueueItem.message.Body, false),
		slack.MsgOptionAttachments(attachment),
		slack.MsgOptionAsUser(true),
		//slack.MsgOptionBlocks(), //TODO add support for blocks
	)
	return err

}

// build incident message attachment
func (s *SlackVendor) constructIncidentAttachment(messageQueueItem MessageQueueItem) slack.Attachment {

	attachment := slack.Attachment{
		Title:      fmt.Sprintf("Iris incident %v", messageQueueItem.incidentID),
		TitleLink:  fmt.Sprintf("%s/%d", s.config.SlackTitleURL, messageQueueItem.incidentID),
		CallbackID: strconv.FormatUint(messageQueueItem.incidentID, 10),
		Color:      "danger",
		Fallback:   "Iris Alert Fired!",
		Actions: []slack.AttachmentAction{
			slack.AttachmentAction{
				Name:  "claim",
				Text:  "Claim Incident",
				Type:  "button",
				Value: "claimed",
			},
			slack.AttachmentAction{
				Name:  "claim all",
				Text:  "Claim All",
				Style: "danger",
				Type:  "button",
				Value: "claimed all",
				Confirm: &slack.ConfirmationField{
					Title:       "Are you sure?",
					Text:        "This will claim all active incidents targeting you.",
					OkText:      "Yes",
					DismissText: "No",
				},
			},
		},
	}

	return attachment

}
