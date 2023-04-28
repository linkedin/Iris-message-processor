package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	uuid "github.com/satori/go.uuid"
)

// admin http endpoint used for healthchecking
func admin(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// don't return good until we've received auth info from iris and are ready serve requests
	if authChecker.CheckStatus() {
		fmt.Fprint(w, "GOOD\r\n")
		return
	}
	fmt.Fprint(w, "BAD\r\n")
	return

}

// postNotification handle out of bound notification creation requests
func postNotification(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {

	allowed, err := authChecker.CheckAuth(req)
	if err != nil {
		http.Error(w, "Error checking HMAC authentication", 500)
		return
	} else if !allowed {
		http.Error(w, "HMAC authentication failed", 401)
		return
	}

	decoder := json.NewDecoder(req.Body)
	var notification Notification
	err = decoder.Decode(&notification)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse request body with error: %s", err), 400)
		return
	}

	// if queue is already full reject out of band notification requests
	if messageQueueManager.CheckQueueFull() {
		http.Error(w, "Sender queue for this node is full", 429)
		return
	}

	// render messages from notification and then queue them for sending
	messages, err := irisClient.GetBuildMessage(notification)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to build notification messages with error: %s", err), 400)
		return
	}
	for _, message := range messages {

		muuid := uuid.NewV4()
		message.MessageUUID = muuid

		// construct queue item from message and add it to the message queue
		msgQueueItem := MessageQueueItem{
			isOOB:   true,
			active:  true,
			queued:  false,
			retries: 0,
			message: message,
		}

		messageQueueManager.AddMessage(msgQueueItem)
	}

	w.WriteHeader(http.StatusOK)
}

// postMessageStatus update message status
func postMessageStatus(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {

	allowed, err := authChecker.CheckAuth(req)
	if err != nil {
		http.Error(w, "Error checking HMAC authentication", 500)
		return
	} else if !allowed {
		http.Error(w, "HMAC authentication failed", 401)
		return
	}

	type statusBody struct {
		MsgID            string `json:"id"`
		Status           string `json:"status"`
		VendorIdentifier string `json:"vendor_identifier"`
	}

	decoder := json.NewDecoder(req.Body)
	var body statusBody
	err = decoder.Decode(&body)
	if err != nil {
		http.Error(w, "Failed to parse request body", 400)
		return
	}

	if len(body.MsgID) == 0 && len(body.VendorIdentifier) == 0 {
		http.Error(w, "invalid request, both id and vendor_identifier fields are empty", 400)
		return
	}

	err = dbClient.writeStatus(body.MsgID, body.Status, body.VendorIdentifier)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// postMessageChangelogs update message changelog
func postMessageChangelog(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {

	allowed, err := authChecker.CheckAuth(req)
	if err != nil {
		http.Error(w, "Error checking HMAC authentication", 500)
		return
	} else if !allowed {
		http.Error(w, "HMAC authentication failed", 401)
		return
	}

	type changelogBody struct {
		MsgID       string `json:"id"`
		ChangeType  string `json:"change_type"`
		Old         string `json:"old"`
		New         string `json:"new"`
		Description string `json:"description"`
	}

	decoder := json.NewDecoder(req.Body)
	var body changelogBody
	err = decoder.Decode(&body)
	if err != nil {
		http.Error(w, "Failed to parse request body", 400)
		return
	}

	err = dbClient.writeChangelog(body.MsgID, body.ChangeType, body.Old, body.New, body.Description)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// retrieve message details given query params and filters
func getMessages(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {

	rawParams := make(map[string]interface{})
	parsedParams := make(map[string]interface{})
	values := req.URL.Query()
	for k, v := range values {
		rawParams[k] = v
	}

	// Query() returns map[string][]string so convert into the appropriate types for each legal param
	for k, v := range rawParams {
		stringParams := []string{"id", "batch", "application", "target", "destination", "mode", "plan", "body", "priority", "subject"}
		uint64Params := []string{"incident_id"}
		intParams := []string{"limit"}
		timeParams := []string{"sent", "created"}
		// query filters are delineated with __ split to get parameter name without filter
		paramSplit := strings.Split(k, "__")

		// attempt to convert parameter values to their correct types
		if contains(uint64Params, paramSplit[0]) {
			uintSlice := make([]uint64, 0)
			for _, stringVal := range v.([]string) {
				number, err := strconv.ParseUint(string(stringVal), 10, 64)
				if err != nil {
					errStr := fmt.Sprintf("Error: parameter %s could not be parsed as uint64", paramSplit[0])
					http.Error(w, errStr, 400)
					return
				}
				uintSlice = append(uintSlice, number)

			}
			// if slice is only a single element store it as a simple variable instead
			if len(uintSlice) == 1 {
				parsedParams[k] = uintSlice[0]
			} else if len(uintSlice) > 1 {
				parsedParams[k] = uintSlice
			}
		} else if contains(timeParams, paramSplit[0]) {
			// time params cannot be lists so if it is not a single element slice it is malformed
			if len(v.([]string)) == 1 {
				i, err := strconv.ParseInt(v.([]string)[0], 10, 64)
				if err != nil {
					errStr := fmt.Sprintf("Error: parameter %s could not be parsed as unix timestamp", paramSplit[0])
					http.Error(w, errStr, 400)
					return
				}
				// replace created key with sent since those are not two distinct fields in IMP
				if paramSplit[0] == "created" {
					sentStr := strings.Replace(k, "created", "sent", 1)
					if _, ok := rawParams[sentStr]; ok {
						continue
					}
					k = sentStr
				}
				tm := time.Unix(i, 0)
				parsedParams[k] = tm
			} else {
				errStr := fmt.Sprintf("Error: parameter %s cannot be a list", paramSplit[0])
				http.Error(w, errStr, 400)
				return
			}
		} else if contains(stringParams, paramSplit[0]) {
			// values already correct type just determine if it is a single value or a slice
			if len(v.([]string)) == 1 {
				parsedParams[k] = v.([]string)[0]
			} else {
				parsedParams[k] = v
			}
		} else if contains(intParams, paramSplit[0]) {
			// only int param is limit and it cannot be a list so if it is not a single element slice it is malformed
			if len(v.([]string)) == 1 {
				i, err := strconv.Atoi(v.([]string)[0])
				if err != nil {
					errStr := fmt.Sprintf("Error: parameter %s could not be parsed as int", paramSplit[0])
					http.Error(w, errStr, 400)
					return
				}
				parsedParams[k] = i
			} else {
				errStr := fmt.Sprintf("Error: parameter %s cannot be a list", paramSplit[0])
				http.Error(w, errStr, 400)
				return
			}
		}

	}

	if len(parsedParams) == 0 {
		http.Error(w, "Query too broad, no valid params were found...", 400)
		return
	}

	messages, err := dbClient.getMessages(parsedParams)
	if err != nil {
		http.Error(w, "error: could not fetch messages", 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(messages)
	if err != nil {
		http.Error(w, "error: could not encode message response", 500)
		return
	}
}

// retrieve message status for a message id
func getMessageStatus(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {

	// parse query params
	values := req.URL.Query()
	msgID := values.Get("id")
	if len(msgID) == 0 {
		http.Error(w, "No message ID found...", 400)
		return
	}

	status, err := dbClient.getStatus(msgID)
	if err != nil {
		http.Error(w, "error: could not fetch status", 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(status)
	if err != nil {
		http.Error(w, "error: could not encode status response", 500)
		return
	}
}

// retrieve message status for a message id
func getMessageChangelogs(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {

	// parse query params
	values := req.URL.Query()
	msgID := values.Get("id")
	if len(msgID) == 0 {
		http.Error(w, "No message ID found...", 400)
		return
	}

	status, err := dbClient.getChangelogs(msgID)
	if err != nil {
		http.Error(w, "error: could not fetch changelogs", 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(status)
	if err != nil {
		http.Error(w, "error: could not encode message response", 500)
		return
	}
}

// helper function contains checks if a string is present in a slice
func contains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}

	return false
}
