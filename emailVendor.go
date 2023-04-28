package main

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"time"

	gometrics "github.com/rcrowley/go-metrics"
	"github.com/russross/blackfriday"
)

type EmailVendor struct {
	*Vendor
}

// NewEmailVendor create new EmailVendor instance
func NewEmailVendor(dbClient *DbClient, config *Config, logger *Logger, metricsRegistry *gometrics.Registry) *EmailVendor {

	vendor := NewVendor(dbClient, config, logger, "smtp")
	emailVendor := EmailVendor{Vendor: vendor}

	if appConfig.MetricsDryRun == false && metricsRegistry != nil {
		emailVendor.Vendor.RegisterMetrics(*metricsRegistry)
	}

	return &emailVendor
}

func (e *EmailVendor) SendMessage(messageQueueItem MessageQueueItem) bool {

	if e.config.SMTPServerAddr == "" {
		e.logger.Errorf("Not sending email due to empty smtp server")
		return true
	}

	message := messageQueueItem.message
	timeout := e.config.SMTPTimeout * time.Second
	var from string
	var to string
	var bcc string
	var body string
	// if custom sender address is configured use that instead of the default
	if len(message.From) > 0 {
		from = message.From
	} else {
		from = e.config.DefaultFromEmail
	}
	// check if there is a bcc recipient
	if len(message.BCCDestination) > 0 {
		bcc = (&mail.Address{message.Target, message.BCCDestination}).String()
	}
	if len(message.Destination) > 0 {
		to = (&mail.Address{message.Target, message.Destination}).String()
	}

	body = message.Body + "\n\n"
	if len(message.EmailText) > 0 {
		body += message.EmailText + "\n\n"
	}
	// parse body as markdown and convert to HTML
	body = string(blackfriday.MarkdownCommon([]byte(body)))

	if len(message.EmailHTML) > 0 {
		body += message.EmailHTML
	}

	// send message through unauthenticated SMTP server
	// if authentication is added to local postfix a simple change to this function can be made to accommodate that
	err := SendMailNoAuth(e.config.SMTPServerAddr, (&mail.Address{message.Application, from}).String(), message.Subject, body, to, bcc, timeout)
	if err != nil {
		// if error is bad recipient syntax ignore it (SMTP Error: 501 5.1. 3 means bad recipient address)
		if smtpError, ok := err.(*textproto.Error); ok {
			if smtpError.Code != 501 {
				e.logger.Errorf("Failed to send email with error:%v. Message: %+v", err, messageQueueItem)
				e.Vendor.counterFailures.Inc(1)
			}
		} else {
			e.logger.Errorf("Failed to send email with error:%v. Message: %+v", err, messageQueueItem)
			e.Vendor.counterFailures.Inc(1)
		}
		if messageQueueItem.retries >= e.Vendor.config.MaxMessageRetries {
			status := fmt.Sprintf("failed with error: %s", err)
			e.Vendor.persistMsg(messageQueueItem, "", status)
		}
		return false
	}
	e.Vendor.counterSuccesses.Inc(1)
	e.Vendor.persistMsg(messageQueueItem, "", "sent to vendor")
	return true
}

// SendMailNoAuth send mail to the unauthenticated local postfix
func SendMailNoAuth(addr, from, subject, body string, to string, bcc string, timeout time.Duration) error {

	// establish connection with SMTP server
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, host)
	defer c.Close()

	// construct email
	if err = c.Mail(from); err != nil {
		return err
	}
	if len(to) > 0 {
		if err = c.Rcpt(to); err != nil {
			return err
		}
	}
	if len(bcc) > 0 {
		if err = c.Rcpt(bcc); err != nil {
			return err
		}
	}

	w, err := c.Data()
	if err != nil {
		return err
	}

	msg := "To: " + to + "\r\n" +
		"From: " + from + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"Content-Type: text/html; charset=\"UTF-8\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" + base64.StdEncoding.EncodeToString([]byte(body))

	// send email
	_, err = w.Write([]byte(msg))
	if err != nil {
		return err
	}
	err = w.Close()
	if err != nil {
		return err
	}
	return c.Quit()
}
