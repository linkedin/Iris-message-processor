package main

import (
	"time"

	gometrics "github.com/rcrowley/go-metrics"
)

type MessageRecord struct {
	MessageID        string    `json:"id"`
	Batch            string    `json:"batch"`
	Sent             time.Time `json:"sent_datetime"`
	SentTimestamp    int64     `json:"sent"`
	CreatedTimestamp int64     `json:"created"`
	Application      string    `json:"application"`
	Target           string    `json:"target"`
	Name             string    `json:"name"`
	Destination      string    `json:"destination"`
	Mode             string    `json:"mode"`
	Priority         string    `json:"priority"`
	Plan             string    `json:"plan"`
	Subject          string    `json:"subject"`
	Body             string    `json:"body"`
	IncidentID       uint64    `json:"incident_id"`
	PlanStep         int       `json:"step"`
}

type MessageStatusRecord struct {
	MessageID               string `json:"id"`
	Status                  string `json:"status"`
	VendorMessageIdentifier string `json:"vendor_id"`
	LastUpdated             time.Time
	LastUpdatedTimestamp    int64 `json:"last_updated"`
}

type MessageChangelogRecord struct {
	ChangelogID   uint64 `json:"id"`
	Date          time.Time
	DateTimestamp int64  `json:"date"`
	MessageID     string `json:"message_id"`
	ChangeType    string `json:"change_type"`
	Old           string `json:"old"`
	New           string `json:"new"`
	Description   string `json:"description"`
}

type DBInterface interface {
	SelectMessages(queryParams map[string]interface{}) ([]MessageRecord, error)
	InsertMessage(msg MessageRecord) error
	SelectMessageStatusByMsgID(msgID string) (MessageStatusRecord, error)
	SelectMessageStatusByVendorID(vendorMessageIdentifier string) (MessageStatusRecord, error)
	InsertMessageStatus(status MessageStatusRecord) error
	UpdateMessageStatus(status MessageStatusRecord) error
	SelectMessageChangelogs(msgID string) ([]MessageChangelogRecord, error)
	InsertMessageChangelog(changelog MessageChangelogRecord) error
}

type DbClient struct {
	config *Config
	logger *Logger
	db     DBInterface
}

// write message to db
func (d *DbClient) writeMessage(msgQueueItem MessageQueueItem) error {
	if d.config.StorageDryRun {
		d.logger.Debugf("StorageDryRun enabled pretending to store message in db")
		return nil
	}
	// convert MessageQueueItem to MessageStatusRecord and call db message insert function
	msg := MessageRecord{
		MessageID:   msgQueueItem.message.MessageUUID.String(),
		Batch:       msgQueueItem.batchID.String(),
		Sent:        msgQueueItem.sentTime,
		Application: msgQueueItem.message.Application,
		Target:      msgQueueItem.message.Target,
		Destination: msgQueueItem.message.Destination,
		Mode:        msgQueueItem.message.Mode,
		Plan:        msgQueueItem.plan,
		Subject:     msgQueueItem.message.Subject,
		Body:        msgQueueItem.message.Body,
		IncidentID:  msgQueueItem.incidentID,
		PlanStep:    msgQueueItem.message.PlanStep + 1, // offset step, iris-api uses set 0 to indicate incident hasn't started processing but iris-message-processor has steps in a list so starts at step index 0
		Priority:    msgQueueItem.message.Priority,
	}
	err := d.db.InsertMessage(msg)
	if err != nil {
		d.logger.Errorf("Failed to insert message with error: %w", err)
	}
	return err
}

// write message to db
func (d *DbClient) writeStatus(msgID string, statusStr string, vendorMessageIdentifier string) error {
	if d.config.StorageDryRun {
		d.logger.Debugf("StorageDryRun enabled pretending to store status in db")
		return nil
	}

	newStatusRecord := MessageStatusRecord{
		MessageID:               msgID,
		Status:                  statusStr,
		VendorMessageIdentifier: vendorMessageIdentifier,
		LastUpdated:             time.Now(),
	}

	storedRecord := MessageStatusRecord{}
	var err error
	// if status callback doesn't include message id use vendor message id instead
	if msgID == "" {
		// get old record using the VendorMessageIdentifier
		storedRecord, err = d.db.SelectMessageStatusByVendorID(vendorMessageIdentifier)
		if err != nil {
			d.logger.Errorf("Failed to check message status with error: %w", err)
			return err
		}
		// update record
		newStatusRecord.MessageID = storedRecord.MessageID
		err = d.db.UpdateMessageStatus(newStatusRecord)
		if err != nil {
			d.logger.Errorf("Failed to check message status with error: %w", err)
			return err
		}
		return nil
	}
	// check if status already exists
	storedRecord, err = d.db.SelectMessageStatusByMsgID(msgID)
	err = d.db.UpdateMessageStatus(newStatusRecord)
	if err != nil {
		d.logger.Errorf("Failed to check message status with error: %w", err)
		return err
	}
	if len(storedRecord.MessageID) > 0 {
		// a status already exists update it
		err = d.db.UpdateMessageStatus(newStatusRecord)
		if err != nil {
			d.logger.Errorf("Failed to update message status with error: %w", err)
		}
	} else {
		// insert new status record
		err = d.db.InsertMessageStatus(newStatusRecord)
		if err != nil {
			d.logger.Errorf("Failed to insert message status with error: %w", err)
		}
	}
	return err
}

// write message to db
func (d *DbClient) writeChangelog(msgID string, changeType string, old string, new string, description string) error {
	if d.config.StorageDryRun {
		d.logger.Debugf("StorageDryRun enabled pretending to store status in db")
		return nil
	}

	changelog := MessageChangelogRecord{
		Date:        time.Now(),
		MessageID:   msgID,
		ChangeType:  changeType,
		Old:         old,
		New:         new,
		Description: description,
	}

	err := d.db.InsertMessageChangelog(changelog)
	if err != nil {
		d.logger.Errorf("Failed to insert message changelog with error: %w", err)
	}
	return err
}

func (d *DbClient) getMessages(queryParams map[string]interface{}) ([]MessageRecord, error) {

	messages, err := d.db.SelectMessages(queryParams)
	if err != nil {
		d.logger.Errorf("Failed to retrieve messages with error: %w", err)
	}
	return messages, err
}

func (d *DbClient) getStatus(msgID string) (MessageStatusRecord, error) {

	status, err := d.db.SelectMessageStatusByMsgID(msgID)
	if err != nil {
		d.logger.Errorf("Failed to retrieve message status with error: %w", err)
	}
	return status, err
}

func (d *DbClient) getChangelogs(msgID string) ([]MessageChangelogRecord, error) {

	changelogs, err := d.db.SelectMessageChangelogs(msgID)
	if err != nil {
		d.logger.Errorf("Failed to retrieve message changelog with error: %w", err)
	}
	return changelogs, err
}

// TODO implement the rest of the mysql functions alongside the apis to fetch messages and insert callbacks

// NewDbClient create new DbClient instance
func NewDbClient(config *Config, logger *Logger, metricsRegistry *gometrics.Registry) *DbClient {

	// interface allows the underlying db to be easiuly replaced with something else if needed
	var db DBInterface
	db = NewSQLDB(config, logger.Clone("mysql"), metricsRegistry)

	dbClient := DbClient{
		config: config,
		logger: logger,
		db:     db,
	}

	return &dbClient
}
