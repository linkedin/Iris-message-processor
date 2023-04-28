package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	gometrics "github.com/rcrowley/go-metrics"
)

var (
	counterDbWriteFailures  = gometrics.NewCounter()
	counterDbWriteSuccesses = gometrics.NewCounter()
	counterDbReadFailures   = gometrics.NewCounter()
	counterDbReadSuccesses  = gometrics.NewCounter()
	errBadMYSQLWhereClause  = errors.New("Query too broad, no correctly configured filters for WHERE clause found")
)

type SQLDB struct {
	db *sql.DB
	// store allowable fields and type for each field
	messageFields map[string]string
}

func NewSQLDB(config *Config, logger *Logger, metricsRegistry *gometrics.Registry) *SQLDB {
	// Capture connection properties.
	cfg := mysql.NewConfig()
	cfg.User = config.MySQLUser
	cfg.Passwd = config.MySQLPassword
	cfg.Net = "tcp"
	cfg.Addr = config.MySQLAddress
	cfg.DBName = config.MySQLDBName
	cfg.ParseTime = true
	cfg.AllowNativePasswords = true
	cfg.CheckConnLiveness = true
	cfg.RejectReadOnly = true
	cfg.Timeout = config.MySQLConnTimeout * time.Second
	cfg.ReadTimeout = config.MySQLReadTimeout * time.Second
	cfg.WriteTimeout = config.MySQLWriteTimeout * time.Second

	// Get a database handle.
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		logger.Errorf("Failed to get db handle with error: %w", err)
	}
	db.SetMaxOpenConns(config.MySQLMaxOpenConns)
	db.SetMaxIdleConns(config.MySQLMaxIdleConns)
	db.SetConnMaxLifetime(config.MySQLMaxConnLifeTime * time.Second)
	db.SetConnMaxIdleTime(config.MySQLMaxConnIdleTime * time.Second)

	sqlDB := SQLDB{
		db: db,
		// store types as strings so they can be compared in an simple if statement instead of type switch, these are not directly compared to or used as actual types just to map what params can use what filters
		messageFields: map[string]string{
			"id":          "string",
			"batch":       "string",
			"sent":        "time.Time",
			"application": "string",
			"target":      "string",
			"destination": "string",
			"mode":        "string",
			"plan":        "string",
			"body":        "string",
			"priority":    "string",
			"subject":     "string",
			"incident_id": "uint64",
		},
	}

	if config.MetricsDryRun == false && metricsRegistry != nil {
		sqlDB.registerMetrics(*metricsRegistry)
	}

	return &sqlDB

}

func (s *SQLDB) registerMetrics(reg gometrics.Registry) {
	reg.Register("mysql.internal.db_write_failures_total", counterDbWriteFailures)
	reg.Register("mysql.internal.db_write_successes_total", counterDbWriteSuccesses)
	reg.Register("mysql.internal.db_read_failures_total", counterDbReadFailures)
	reg.Register("mysql.internal.db_read_successes_total", counterDbReadSuccesses)
}

func (s *SQLDB) buildWhereClause(queryParams map[string]interface{}, fieldTypeMap map[string]string) (values []interface{}, where []string, err error) {
	if len(queryParams) == 0 {
		return values, where, errBadMYSQLWhereClause
	}

	for paramName, paramVal := range queryParams {
		// query filters are delineated with __ split to get param and filter
		paramSplit := strings.Split(paramName, "__")
		// check if param corresponds to an allowed message field
		if typeString, ok := fieldTypeMap[paramSplit[0]]; ok {
			// no query filter specified, default to '=='(eq)
			if len(paramSplit) == 1 {
				switch paramVal.(type) {
				case uint64:
					values = append(values, paramVal)
					where = append(where, fmt.Sprintf("%s = ?", paramSplit[0]))
				case string:
					values = append(values, paramVal)
					where = append(where, fmt.Sprintf("%s = ?", paramSplit[0]))
				}
			} else {
				switch filter := paramSplit[1]; filter {
				case "eq":
					switch paramVal.(type) {
					case uint64:
						values = append(values, paramVal)
						where = append(where, fmt.Sprintf("%s = ?", paramSplit[0]))
					case string:
						values = append(values, paramVal)
						where = append(where, fmt.Sprintf("%s = ?", paramSplit[0]))
					}
				case "ne":
					switch paramVal.(type) {
					case uint64:
						values = append(values, paramVal)
						where = append(where, fmt.Sprintf("%s != ?", paramSplit[0]))
					case string:
						values = append(values, paramVal)
						where = append(where, fmt.Sprintf("%s != ?", paramSplit[0]))
					}
				case "in":
					// verify that field is of correct type for this operation and that value is of same type as field
					if typeString != "time.Time" {
						switch paramVal.(type) {
						case []uint64:
							for _, listVal := range paramVal.([]uint64) {
								values = append(values, listVal)
							}
							clause := paramSplit[0] + ` in (?` + strings.Repeat(",?", len(paramVal.([]uint64))-1) + `)`
							where = append(where, clause)
						case []string:
							for _, listVal := range paramVal.([]string) {
								values = append(values, listVal)
							}
							clause := paramSplit[0] + ` in (?` + strings.Repeat(",?", len(paramVal.([]string))-1) + `)`
							where = append(where, clause)
						}
					}
				case "gt":
					// verify that field is of correct type for this operation and that value is of same type as field
					if typeString == "uint64" || typeString == "time.Time" {
						switch paramVal.(type) {
						case uint64:
							values = append(values, paramVal)
							where = append(where, fmt.Sprintf("%s > ?", paramSplit[0]))
						case time.Time:
							values = append(values, paramVal)
							where = append(where, fmt.Sprintf("%s > ?", paramSplit[0]))
						}
					}
				case "ge":
					// verify that field is of correct type for this operation and that value is of same type as field
					if typeString == "uint64" || typeString == "time.Time" {
						switch paramVal.(type) {
						case uint64:
							values = append(values, paramVal)
							where = append(where, fmt.Sprintf("%s >= ?", paramSplit[0]))
						case time.Time:
							values = append(values, paramVal)
							where = append(where, fmt.Sprintf("%s >= ?", paramSplit[0]))
						}
					}
				case "lt":
					// verify that field is of correct type for this operation and that value is of same type as field
					if typeString == "uint64" || typeString == "time.Time" {
						switch paramVal.(type) {
						case uint64:
							values = append(values, paramVal)
							where = append(where, fmt.Sprintf("%s < ?", paramSplit[0]))
						case time.Time:
							values = append(values, paramVal)
							where = append(where, fmt.Sprintf("%s < ?", paramSplit[0]))
						}
					}
				case "le":
					// verify that field is of correct type for this operation and that value is of same type as field
					if typeString == "uint64" || typeString == "time.Time" {
						switch paramVal.(type) {
						case uint64:
							values = append(values, paramVal)
							where = append(where, fmt.Sprintf("%s <= ?", paramSplit[0]))
						case time.Time:
							values = append(values, paramVal)
							where = append(where, fmt.Sprintf("%s <= ?", paramSplit[0]))
						}
					}
				case "contains":
					// verify that field is of correct type for this operation and that value is of same type as field
					if typeString == "string" {
						switch paramVal.(type) {
						case string:
							values = append(values, "%"+paramVal.(string)+"%")
							where = append(where, fmt.Sprintf("%s LIKE ?", paramSplit[0]))
						}
					}
				case "startswith":
					// verify that field is of correct type for this operation and that value is of same type as field
					if typeString == "string" {
						switch paramVal.(type) {
						case string:
							values = append(values, paramVal.(string)+"%")
							where = append(where, fmt.Sprintf("%s LIKE ?", paramSplit[0]))
						}
					}
				case "endswith":
					// verify that field is of correct type for this operation and that value is of same type as field
					if typeString == "string" {
						switch paramVal.(type) {
						case string:
							values = append(values, "%"+paramVal.(string))
							where = append(where, fmt.Sprintf("%s LIKE ?", paramSplit[0]))
						}
					}
				}

			}
		}
	}

	// there were no correctly configured filters
	if len(where) == 0 {
		return values, where, errBadMYSQLWhereClause
	}

	return values, where, nil
}

// select message with filters
func (s *SQLDB) SelectMessages(queryParams map[string]interface{}) ([]MessageRecord, error) {

	// parse query params and build query
	messages := make([]MessageRecord, 0)
	values, where, err := s.buildWhereClause(queryParams, s.messageFields)
	if err != nil {
		return nil, err
	}

	// add limit if one is specified
	limitStr := ""
	if limit, ok := queryParams["limit"]; ok {
		limitStr = " LIMIT " + strconv.Itoa(limit.(int))
	}

	rows, err := s.db.Query("SELECT `id`, `batch`, `sent`, `application`, `target`, `destination`, `mode`, `priority`, `plan`, `subject`, `body`, `incident_id`, `step` FROM message WHERE "+strings.Join(where, " AND ")+" ORDER BY sent DESC"+limitStr, values...)
	if err != nil {
		counterDbReadFailures.Inc(1)
		return nil, fmt.Errorf("Failed to retrieve messages with error: %w", err)
	}
	defer rows.Close()
	// Loop through rows, using Scan to assign column data to struct fields.
	for rows.Next() {
		var message MessageRecord
		if err := rows.Scan(&message.MessageID, &message.Batch, &message.Sent, &message.Application, &message.Target, &message.Destination, &message.Mode, &message.Priority, &message.Plan, &message.Subject, &message.Body, &message.IncidentID, &message.PlanStep); err != nil {
			if err == sql.ErrNoRows {
				counterDbReadSuccesses.Inc(1)
				return messages, nil
			}
			counterDbReadFailures.Inc(1)
			return nil, fmt.Errorf("Failed to retrieve messages with error: %w", err)
		}
		message.SentTimestamp = message.Sent.Unix()
		message.CreatedTimestamp = message.Sent.Unix()
		message.Name = message.Target
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		counterDbReadFailures.Inc(1)
		return nil, fmt.Errorf("Failed to retrieve messages with error: %w", err)
	}
	counterDbReadSuccesses.Inc(1)
	return messages, nil
}

// insert message
func (s *SQLDB) InsertMessage(msg MessageRecord) error {
	_, err := s.db.Exec("INSERT INTO message (id, batch, sent, application, target, destination, mode, priority, plan, subject, body, incident_id, step) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		msg.MessageID, msg.Batch, msg.Sent, msg.Application, msg.Target, msg.Destination, msg.Mode, msg.Priority, msg.Plan, msg.Subject, msg.Body, msg.IncidentID, msg.PlanStep)
	if err != nil {
		counterDbWriteFailures.Inc(1)
		return fmt.Errorf("Failed with error: %w", err)
	}
	counterDbWriteSuccesses.Inc(1)
	return nil
}

// select message status
func (s *SQLDB) SelectMessageStatusByMsgID(msgID string) (MessageStatusRecord, error) {
	var status MessageStatusRecord

	row := s.db.QueryRow("SELECT `message_id`, `status`, `vendor_message_id`, `last_updated` FROM message_sent_status WHERE message_id = ? LIMIT 1", msgID)
	if err := row.Scan(&status.MessageID, &status.Status, &status.VendorMessageIdentifier, &status.LastUpdated); err != nil {
		if err == sql.ErrNoRows {
			counterDbReadSuccesses.Inc(1)
			return status, nil
		}
		counterDbReadFailures.Inc(1)
		status.LastUpdatedTimestamp = status.LastUpdated.Unix()
		return status, fmt.Errorf("Failed retrieving message status with error: %w", err)
	}
	counterDbReadSuccesses.Inc(1)
	return status, nil
}

// select message status
func (s *SQLDB) SelectMessageStatusByVendorID(vendorMessageIdentifier string) (MessageStatusRecord, error) {
	var status MessageStatusRecord
	row := s.db.QueryRow("SELECT `message_id`, `status`, `vendor_message_id`, `last_updated` FROM message_sent_status WHERE vendor_message_id = ? LIMIT 1", vendorMessageIdentifier)
	if err := row.Scan(&status.MessageID, &status.Status, &status.VendorMessageIdentifier, &status.LastUpdated); err != nil {
		// it is possible for arbitrary vendorMessageIdentifier to not have rows
		if err == sql.ErrNoRows {
			counterDbReadSuccesses.Inc(1)
			return status, nil
		}
		counterDbReadFailures.Inc(1)
		status.LastUpdatedTimestamp = status.LastUpdated.Unix()
		return status, fmt.Errorf("Failed retrieving message status with error: %w", err)
	}
	counterDbReadSuccesses.Inc(1)
	return status, nil
}

// insert message status
func (s *SQLDB) InsertMessageStatus(status MessageStatusRecord) error {
	_, err := s.db.Exec("INSERT INTO message_sent_status (message_id, status, vendor_message_id, last_updated) VALUES (?, ?, ?, ?)",
		status.MessageID, status.Status, status.VendorMessageIdentifier, status.LastUpdated)
	if err != nil {
		counterDbWriteFailures.Inc(1)
		return fmt.Errorf("Failed with error: %w", err)
	}
	counterDbWriteSuccesses.Inc(1)
	return nil
}

// update message status
func (s *SQLDB) UpdateMessageStatus(status MessageStatusRecord) error {
	_, err := s.db.Exec("UPDATE message_sent_status SET status = ?, last_updated = ? WHERE message_id = ?",
		status.Status, status.LastUpdated, status.MessageID)
	if err != nil {
		counterDbWriteFailures.Inc(1)
		return fmt.Errorf("Failed with error: %w", err)
	}
	counterDbWriteSuccesses.Inc(1)
	return nil
}

// select message changelogs
func (s *SQLDB) SelectMessageChangelogs(msgID string) ([]MessageChangelogRecord, error) {
	logs := make([]MessageChangelogRecord, 0)

	rows, err := s.db.Query("SELECT `id`, `date`, `message_id`, `change_type`, `old`, `new`, `description` FROM message_changelog WHERE message_id = ? ORDER BY date DESC", msgID)
	if err != nil {
		counterDbReadFailures.Inc(1)
		return nil, fmt.Errorf("Failed to retrieve msg changelogs with error: %w", err)
	}
	defer rows.Close()
	// Loop through rows, using Scan to assign column data to struct fields.
	for rows.Next() {
		var changelog MessageChangelogRecord
		if err := rows.Scan(&changelog.ChangelogID, &changelog.Date, &changelog.MessageID, &changelog.ChangeType, &changelog.Old, &changelog.New, &changelog.Description); err != nil {
			if err == sql.ErrNoRows {
				counterDbReadSuccesses.Inc(1)
				return logs, nil
			}
			counterDbReadFailures.Inc(1)
			return nil, fmt.Errorf("Failed to retrieve msg changelogs with error: %w", err)
		}
		changelog.DateTimestamp = changelog.Date.Unix()
		logs = append(logs, changelog)
	}
	if err := rows.Err(); err != nil {
		counterDbReadFailures.Inc(1)
		return nil, fmt.Errorf("Failed to retrieve msg changelogs with error: %w", err)
	}
	counterDbReadSuccesses.Inc(1)
	return logs, nil
}

// insert message changelog
func (s *SQLDB) InsertMessageChangelog(changelog MessageChangelogRecord) error {
	_, err := s.db.Exec("INSERT INTO message_changelog (date, message_id, change_type, old, new, description) VALUES (?, ?, ?, ?, ?, ?)",
		changelog.Date, changelog.MessageID, changelog.ChangeType, changelog.Old, changelog.New, changelog.Description)
	if err != nil {
		counterDbWriteFailures.Inc(1)
		return fmt.Errorf("Failed with error: %w", err)
	}
	counterDbWriteSuccesses.Inc(1)
	return nil
}
