package main

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	gometrics "github.com/rcrowley/go-metrics"
	uuid "github.com/satori/go.uuid"
)

var (
	counterNewIncidents                 = gometrics.NewCounter()
	counterIncidentsFailedToProcess     = gometrics.NewCounter()
	counterNotificationsFailedToProcess = gometrics.NewCounter()
	counterFailedHeartbeat              = gometrics.NewCounter()
	gaugeEscalateTime                   = gometrics.NewGauge()
	gaugeLastEmptyQueueTime             = gometrics.NewGauge()
	gaugeTotalIncidents                 = gometrics.NewGauge()

	priorityMap        = map[string]int{}
	modeIDMap          = map[string]int{}
	defaultPriorityMap = map[string]string{}
)

type IncidentManager struct {
	Quit                chan int // send signal to shutdown
	config              *Config
	irisClient          *IrisClient
	messageQueueManager *MessageQueueManager
	aggregationManager  *AggregationManager
	logger              *Logger
	mainWg              *sync.WaitGroup

	escalationMap      map[uint64]Escalation
	escalationMapMutex sync.RWMutex
	nodeUUID           string

	queueEmptyTime time.Time
}

type Escalation struct {
	incident        Incident
	plan            Plan
	notificationMap map[int](map[int]Notification)
	messageMap      map[int](map[int][]Message)
	messageIDList   []uuid.UUID
}

func (i *IncidentManager) RegisterMetrics(reg gometrics.Registry) {
	reg.Register("incidentManager.internal.new_incidents", counterNewIncidents)
	reg.Register("incidentManager.internal.incidents_failed_to_process", counterIncidentsFailedToProcess)
	reg.Register("incidentManager.internal.notifications_failed_to_process", counterNotificationsFailedToProcess)
	reg.Register("incidentManager.internal.failed_heartbeats", counterFailedHeartbeat)
	reg.Register("incidentManager.internal.incident_escalation_time", gaugeEscalateTime)
	reg.Register("incidentManager.internal.time_since_empty_queue", gaugeLastEmptyQueueTime)
	reg.Register("incidentManager.internal.total_incidents", gaugeTotalIncidents)
}

// NewIncidentManager create new IncidentManager instance
func NewIncidentManager(irisClient *IrisClient, messageQueueManager *MessageQueueManager, aggregationManager *AggregationManager, config *Config, logger *Logger, quit chan int, wg *sync.WaitGroup, metricsRegistry *gometrics.Registry) *IncidentManager {

	// node id is just the md5 hash of the hostname
	nodeIDHash := md5.Sum([]byte(config.FQDN))

	incidentManager := IncidentManager{
		Quit:                quit, // send signal to shutdown
		config:              config,
		irisClient:          irisClient,
		messageQueueManager: messageQueueManager,
		aggregationManager:  aggregationManager,
		logger:              logger,
		mainWg:              wg,
		escalationMap:       make(map[uint64]Escalation),
		nodeUUID:            hex.EncodeToString(nodeIDHash[:]),
	}

	if appConfig.MetricsDryRun == false && metricsRegistry != nil {
		incidentManager.RegisterMetrics(*metricsRegistry)
	}

	return &incidentManager
}

// send a message to each unique destination in a plan alerting them of aggregation and providing them with a batchID to look up aggregated messages
func (i *IncidentManager) triggerAggregationMessages(batchID uuid.UUID, plan Plan) {

	// map of mode target and destination
	destinationMap := make(map[string]map[string]string)

	// go through all the plan steps and identify every unique destination so a notification about aggregation can be sent to it
	for _, step := range plan.Steps {
		for _, notification := range step {

			// get recipients for this notification step
			userInfoMap, err := i.irisClient.GetRoleTarget(notification.Role, notification.Target)
			if err != nil {
				counterNotificationsFailedToProcess.Inc(1)
				i.logger.Warnf("Failed to get role and target for aggregation message for plan %s with error:%v.", plan, err)
				continue
			}

			// get appropriate destinations for each recipient and add them to the map
			for target, userInfo := range userInfoMap {
				var mode string
				// check if user has a preference for this priority and if not use default
				if userMode, ok := userInfo.Modes[notification.Priority]; ok {
					mode = userMode
				} else {
					mode = defaultPriorityMap[notification.Priority]
				}

				// get destination for mode or default to email
				if destination, ok := userInfo.Contacts[mode]; ok {
					if _, ok := destinationMap[mode]; !ok {
						destinationMap[mode] = make(map[string]string)
					}
					destinationMap[mode][target] = destination
				} else {
					// there shouldn't be a way to not have an email destination but check just in case
					if destinationBackup, ok := userInfo.Contacts[EmailMode]; ok {
						if _, ok := destinationMap[EmailMode]; !ok {
							destinationMap[EmailMode] = make(map[string]string)
						}
						destinationMap[EmailMode][target] = destinationBackup
					}
				}
			}

		}

	}

	// create message queue items and add them to the queue
	for mode, targetMap := range destinationMap {
		for target, destination := range targetMap {
			// construct message to inform of aggregation
			body := fmt.Sprintf("Plan %s has crossed its configured notification frequency threshold and triggered message aggregation. You can find the messages from this aggregation batch by checking the Iris web UI and searching for batch ID: %s.\n", plan.Name, batchID)

			muuid := uuid.NewV4()
			message := Message{
				MessageUUID: muuid,
				Application: i.config.IrisAppName,
				Target:      target,
				Destination: destination,
				Mode:        mode,
				ModeID:      modeIDMap[mode],
				Subject:     "Iris plan notification batching triggered",
				Body:        body,
			}

			msgQueueItem := MessageQueueItem{
				isOOB:   true,
				active:  true,
				queued:  false,
				message: message,
				batchID: batchID,
			}

			// queue message for sending
			i.messageQueueManager.AddMessage(msgQueueItem)
		}
	}

}

// fetch plans for and initialize new escalations
func (i *IncidentManager) initEscalation(incidentID uint64, wg *sync.WaitGroup) {
	defer wg.Done()

	i.escalationMapMutex.RLock()
	var escalation Escalation
	if esc, ok := i.escalationMap[incidentID]; !ok {
		i.escalationMapMutex.RUnlock()
		i.logger.Errorf("Failed to init incident %d, not in escalation map", escalation.incident.IncidentID)
		counterIncidentsFailedToProcess.Inc(1)
		return
	} else {
		escalation = esc
	}
	i.escalationMapMutex.RUnlock()

	plan, err := i.irisClient.GetPlan(strconv.FormatUint(escalation.incident.PlanID, 10))
	if err != nil {
		i.logger.Errorf("Failed to fetch plan %s for incident %d with error:%v. Dropping this incident this loop.", escalation.incident.Plan, escalation.incident.IncidentID, err)
		counterIncidentsFailedToProcess.Inc(1)
		i.deleteIncident(incidentID)
		return
	}

	// count incident for aggregation purposes, if we trip the aggregation notify the targets of the plan
	batchID, triggeredAggregation := i.aggregationManager.countIncident(plan.Name)
	if triggeredAggregation {
		go i.triggerAggregationMessages(batchID, plan)
	}

	escalation.notificationMap = make(map[int](map[int]Notification))
	escalation.messageMap = make(map[int](map[int][]Message))
	escalation.messageIDList = make([]uuid.UUID, 0)
	escalation.plan = plan
	i.updateIncident(escalation)
}

// prepare and send messages and manage plan steps
func (i *IncidentManager) escalate(incidentID uint64, wg *sync.WaitGroup) {
	defer wg.Done()

	i.escalationMapMutex.RLock()
	var escalation Escalation
	if esc, ok := i.escalationMap[incidentID]; !ok {
		i.escalationMapMutex.RUnlock()
		i.logger.Errorf("Failed to escalate incident %d , not in escalation map", escalation.incident.IncidentID)
		counterIncidentsFailedToProcess.Inc(1)
		return
	} else {
		escalation = esc
	}
	i.escalationMapMutex.RUnlock()

	currStep := escalation.incident.CurrentStep
	// initialize notifications if new plan step
	if _, ok := escalation.notificationMap[currStep]; !ok {

		// if new incident set step to 1 to indicate incident has started processing
		if currStep == 0 {
			err := i.irisClient.PostEscalateIncident(escalation.incident.IncidentID, 1, true)
			if err != nil {
				counterIncidentsFailedToProcess.Inc(1)
				i.logger.Errorf("Failed to escalate incident %d with error:%v", escalation.incident.IncidentID, err)
				return
			}

			// check if tracking notification is defined for this application
			if _, ok := escalation.plan.TrackingTemplate[escalation.incident.Application]; ok {
				i.createTrackingMessage(escalation)
			}
		}

		escalation.notificationMap[currStep] = make(map[int]Notification)
		escalation.messageMap[currStep] = make(map[int][]Message)

		// iterate through current plan step and initialize notifications for that step
		for planStepIndex, planStepNotification := range escalation.plan.Steps[currStep] {
			notification := Notification{
				Role:                   planStepNotification.Role,
				Target:                 planStepNotification.Target,
				Template:               planStepNotification.Template,
				Context:                escalation.incident.Context,
				Priority:               planStepNotification.Priority,
				PriorityID:             priorityMap[planStepNotification.Priority],
				Plan:                   escalation.plan.Name,
				PlanID:                 escalation.plan.PlanId,
				IncidentID:             escalation.incident.IncidentID,
				Application:            escalation.incident.Application,
				DynamicIndex:           planStepNotification.DynamicIndex,
				LastSent:               time.Time{},
				SentCount:              0,
				PlanStep:               currStep,
				PlanStepNotificationId: planStepNotification.StepId,
				PlanStepIndex:          planStepIndex,
			}

			// add built in iris context vars
			irisContext := make(map[string]interface{})
			irisContext["target"] = notification.Target
			irisContext["priority"] = notification.Priority
			irisContext["application"] = notification.Application
			irisContext["plan"] = notification.Plan
			irisContext["plan_id"] = notification.PlanID
			irisContext["incident_id"] = notification.IncidentID
			irisContext["template"] = notification.Template
			notification.Context["iris"] = irisContext

			escalation.notificationMap[currStep][planStepNotification.StepId] = notification
		}
	}

	// check if any notifications need to be sent for this incident at the moment and send them
	doneWithStep := true
	for notificationIndex, notification := range escalation.notificationMap[currStep] {
		planStepNotification := escalation.plan.Steps[currStep][notification.PlanStepIndex]

		if _, ok := escalation.messageMap[currStep][notification.PlanStepIndex]; ok {
			// check if notification has previously failed to build messages
			if len(escalation.messageMap[currStep][notification.PlanStepIndex]) == 0 {
				// skip processing the bad notification again so the rest of the escalation can proceed
				continue
			}
		}

		// even if all notifications are sent, wait the configured wait time before moving to the next step
		timeSinceLastMessage := time.Now().Sub(notification.LastSent)
		if timeSinceLastMessage < time.Duration(planStepNotification.Wait)*time.Second {
			doneWithStep = false
		}

		// check if we need still need to repeat this notification
		if notification.SentCount <= planStepNotification.Repeat {
			doneWithStep = false

			// check if it has been long enough since we last sent out this notification
			if timeSinceLastMessage >= time.Duration(planStepNotification.Wait)*time.Second {

				//check if messages have already been previously rendered for this notification
				if _, ok := escalation.messageMap[currStep][notification.PlanStepIndex]; !ok {
					// get rendered messages for notification
					messageList, err := i.irisClient.GetBuildMessage(notification)
					if err == nil && len(messageList) == 0 {
						err = errors.New("resolved no messages from this notification")
					}
					if err != nil {
						buildErr := err
						escalation.messageMap[currStep][notification.PlanStepIndex] = make([]Message, 0)
						// only log and emit metrics for failure if notification is not optional
						if planStepNotification.Optional == 0 {
							counterNotificationsFailedToProcess.Inc(1)
							i.logger.Warnf("Failed to retrieve built messages for plan %s for incident %d with error:%v. Notification details: %+v", escalation.incident.Plan, escalation.incident.IncidentID, err, notification)

							// create dummy message with error info to store in the db for error visibility in incident page
							muuid := uuid.NewV4()

							errorMessage := Message{
								MessageUUID:            muuid,
								Application:            notification.Application,
								Target:                 notification.Target,
								Destination:            notification.Target,
								Mode:                   ErrorMode,
								ModeID:                 modeIDMap[ErrorMode],
								Subject:                "Non-optional notification failed to process",
								Body:                   fmt.Sprintf("failed to build messages with error: %v\n\n\n notification details: %+v", buildErr, notification),
								PlanStep:               notification.PlanStep,
								PlanStepNotificationId: notification.PlanStepNotificationId,
								PlanStepIndex:          notification.PlanStepIndex,
							}
							msgQueueItem := MessageQueueItem{
								isOOB:          false,
								isErrorMessage: true,
								active:         true,
								queued:         false,
								retries:        0,
								incidentID:     notification.IncidentID,
								plan:           notification.Plan,
								message:        errorMessage,
							}
							// hand over error message to messageQueueManager
							i.messageQueueManager.AddMessage(msgQueueItem)

						}
						continue
					}

					pushMessages := []Message{}
					// generate UUIDs for each message
					for idx, _ := range messageList {
						muuid := uuid.NewV4()
						messageList[idx].MessageUUID = muuid
						messageList[idx].PlanStep = notification.PlanStep
						messageList[idx].PlanStepIndex = notification.PlanStepIndex
						messageList[idx].PlanStepNotificationId = notification.PlanStepNotificationId
						escalation.messageIDList = append(escalation.messageIDList, muuid)

						// generate accompanying push notifications call and sms mode messages
						if messageList[idx].Mode == CallMode || messageList[idx].Mode == SMSMode {
							clonedMsg, err := i.messageQueueManager.CloneMessage(messageList[idx])
							if err == nil {
								clonedMsg.Mode = FCMMode
								pushMessages = append(pushMessages, clonedMsg)
								escalation.messageIDList = append(escalation.messageIDList, clonedMsg.MessageUUID)
							}
						}

					}

					messageList = append(messageList, pushMessages...)
					escalation.messageMap[currStep][notification.PlanStepIndex] = messageList
				}

				// add messages to the queue if they don't exist and check if they have all been successfully sent
				allMessagesSent := true
				for _, message := range escalation.messageMap[currStep][notification.PlanStepIndex] {

					msgQueueItem := MessageQueueItem{
						isOOB:      false,
						active:     true,
						queued:     false,
						retries:    0,
						incidentID: escalation.incident.IncidentID,
						plan:       escalation.incident.Plan,
						message:    message,
					}

					// if message has already been queued it will return the details of the queued message
					queuedMsg := i.messageQueueManager.AddMessage(msgQueueItem)
					if queuedMsg.active {
						allMessagesSent = false
					}
				}

				if allMessagesSent {
					// update notification escalation data
					notification.SentCount++
					notification.LastSent = time.Now()
					escalation.notificationMap[currStep][notificationIndex] = notification

					// clean up inactive messages
					for idx, message := range escalation.messageMap[currStep][notification.PlanStepIndex] {
						i.messageQueueManager.DeleteMessage(message.MessageUUID)

						// change message uuid in case cached message is used again in a repeating notification
						muuid := uuid.NewV4()
						escalation.messageMap[currStep][notification.PlanStepIndex][idx].MessageUUID = muuid
					}
				}

			}
		}
	}

	if doneWithStep {

		// check if we are at the end of the plan if so deactivate & remove from escalation map
		// step count is off by 1 from iris because step 0 indicates incident hasn't started processing so add 1 to currstep when escalating to compensate
		if currStep+1 >= len(escalation.plan.Steps) {
			err := i.irisClient.PostEscalateIncident(escalation.incident.IncidentID, currStep+1, false)
			if err != nil {
				i.logger.Errorf("Failed to deactivate incident %d with error:%v. Dropping this incident this loop.", escalation.incident.IncidentID, err)
			}
			i.deleteIncident(incidentID)
			return
		}

		// move on to next incident step
		escalation.incident.CurrentStep = currStep + 1
		err := i.irisClient.PostEscalateIncident(escalation.incident.IncidentID, escalation.incident.CurrentStep+1, true)
		if err != nil {
			counterIncidentsFailedToProcess.Inc(1)
			i.logger.Errorf("Failed to escalate incident %d with error:%v", escalation.incident.IncidentID, err)
		}
	}

	// update escalation data
	i.updateIncident(escalation)
}

func (i *IncidentManager) createTrackingMessage(escalation Escalation) {
	// construct tracking message
	muuid := uuid.NewV4()

	context := escalation.incident.Context
	// add built in iris context vars
	irisContext := make(map[string]interface{})
	irisContext["target"] = escalation.plan.TrackingKey
	irisContext["application"] = escalation.incident.Application
	irisContext["plan"] = escalation.incident.Plan
	irisContext["plan_id"] = escalation.incident.PlanID
	irisContext["incident_id"] = escalation.incident.IncidentID
	context["iris"] = irisContext

	trackingTemplate := escalation.plan.TrackingTemplate[escalation.incident.Application]
	trackingType := escalation.plan.TrackingType
	trackingMessage := Message{
		Application: escalation.incident.Application,
		Target:      escalation.plan.TrackingKey,
		Destination: escalation.plan.TrackingKey,
		Mode:        escalation.plan.TrackingType,
		ModeID:      modeIDMap[escalation.plan.TrackingType],
		MessageUUID: muuid,
	}
	if trackingType == SlackMode || trackingType == SMSMode || trackingType == CallMode {
		if body, ok := trackingTemplate["body"]; ok {
			// call iris-api to render tracking template jinja
			body, err := i.irisClient.GetRenderJinja(body, context)
			if err != nil {
				i.logger.Errorf("failed to render tracking message body jinja with err: %s", err)
			}
			trackingMessage.Body = body
		}
	} else if trackingType == EmailMode {
		if subject, ok := trackingTemplate["email_subject"]; ok {
			subject, err := i.irisClient.GetRenderJinja(subject, context)
			if err != nil {
				i.logger.Errorf("failed to render tracking message email_subject jinja with err: %s", err)
			}
			trackingMessage.Subject = subject
		}
		if text, ok := trackingTemplate["email_text"]; ok {
			text, err := i.irisClient.GetRenderJinja(text, context)
			if err != nil {
				i.logger.Errorf("failed to render tracking message email_text jinja with err: %s", err)
			}
			trackingMessage.EmailText = text
		}
		if html, ok := trackingTemplate["email_html"]; ok {
			html, err := i.irisClient.GetRenderJinja(html, context)
			if err != nil {
				i.logger.Errorf("failed to render tracking message jinja email_html with err: %s", err)
			}
			trackingMessage.EmailHTML = html
		}
	} else {
		counterIncidentsFailedToProcess.Inc(1)
		i.logger.Errorf("Could not process tracking notification, unknown tracking type: %s", trackingType)
		return
	}

	msgQueueItem := MessageQueueItem{
		isOOB:   true,
		active:  true,
		queued:  false,
		retries: 0,
		message: trackingMessage,
	}
	// hand over tracking message to messageQueueManager
	i.messageQueueManager.AddMessage(msgQueueItem)

}

func (i *IncidentManager) updateIncident(escalation Escalation) {
	i.escalationMapMutex.Lock()
	defer i.escalationMapMutex.Unlock()
	i.escalationMap[escalation.incident.IncidentID] = escalation
}

func (i *IncidentManager) deleteIncident(incidentID uint64) {
	i.escalationMapMutex.Lock()
	defer i.escalationMapMutex.Unlock()

	// delete messages belonging to incident
	escalation := i.escalationMap[incidentID]
	for _, msgID := range escalation.messageIDList {
		msgQueueItem, exists := i.messageQueueManager.CheckMessage(msgID)
		// persist messages that are already enqueued but have not been sent yet for informational purposes
		if exists && msgQueueItem.active {
			msgQueueItem.sentTime = time.Now()
			i.messageQueueManager.PersistMessage(msgQueueItem, "", "dropped (incident deactivated while message still enqueued)")
		}
		i.messageQueueManager.DeleteMessage(msgID)
	}
	// remove incident from escalationMap
	delete(i.escalationMap, incidentID)
}

// add new incidents
func (i *IncidentManager) addNewIncidents(incidentList []Incident) {
	// add all newly created incidents to the escalation map
	newEscalationIDs := make([]uint64, 0)
	i.escalationMapMutex.Lock()
	for _, incident := range incidentList {
		if _, ok := i.escalationMap[incident.IncidentID]; !ok {
			// iris incidents step index are off by one since they use step 0 to indicate incident hasn't begun processing yet. Correct this difference for a new incident that has already started escalation.
			if incident.CurrentStep > 0 {
				incident.CurrentStep--
			}
			i.escalationMap[incident.IncidentID] = Escalation{incident: incident}
			newEscalationIDs = append(newEscalationIDs, incident.IncidentID)
		}
	}
	i.escalationMapMutex.Unlock()

	counterNewIncidents.Inc(int64(len(newEscalationIDs)))
	// fetch plans for and initialize new escalations
	var initWg sync.WaitGroup
	for _, id := range newEscalationIDs {
		initWg.Add(1)
		go i.initEscalation(id, &initWg)
	}
	initWg.Wait()

}

// IrisHeartbeat triggers the heartbeat call at regular intervals
func (i *IncidentManager) IrisHeartbeat() {
	interval := time.NewTicker(15 * time.Second)
	defer interval.Stop()
	for ; true; <-interval.C {
		err := i.irisClient.GetHeartbeat(i.nodeUUID)
		if err != nil {
			counterFailedHeartbeat.Inc(1)
			i.logger.Errorf("Failed heartbeat with error:%v", err)
		}
		select {
		case <-i.Quit:
			return
		default:
			continue
		}
	}
}

// fetchIncidentBatch takes a list of incident IDs and fetches their details from iris-api
func (i *IncidentManager) fetchIncidentBatch(idList []uint64, incidentChan chan Incident, wg *sync.WaitGroup) {
	defer wg.Done()

	// fetch incidents from iris-api
	activeIncidentList, err := i.irisClient.PostFetchIncidents(idList, i.nodeUUID)
	if err != nil {
		counterIncidentsFailedToProcess.Inc(1)
		i.logger.Errorf("Failed to fetch batch of incidents with error:%v", err)
		return
	}

	// write incidents to chan
	for _, incident := range activeIncidentList {
		incidentChan <- incident
	}

}

// A single iteration of IncidentManager's main loop
func (i *IncidentManager) RunOnce() {
	i.logger.Infof("Fetching new incidents..")
	startTime := time.Now()

	// fetch incident IDs assigned to this node
	incidentIDs, err := i.irisClient.GetIncidentIDs(i.nodeUUID)
	if err != nil {
		counterIncidentsFailedToProcess.Inc(1)
		i.logger.Errorw("Failed to retrieve new incidents from Iris-API", "error", err)
		diff := time.Since(startTime)
		i.logger.Infof("Incident escalation total time taken %v seconds", diff.Seconds())
		gaugeEscalateTime.Update(int64(diff.Seconds()))
		return
	}
	i.logger.Infof("Fetched %d incident IDs", len(incidentIDs))

	irisMaxIncidentsPerRequest := i.config.IrisBatchSize
	incidentChan := make(chan Incident, len(incidentIDs))
	idList := make([]uint64, 0)
	var wg sync.WaitGroup

	// fetch details of assigned incidents in batches of size irisMaxIncidentsPerRequest
	for idx, incID := range incidentIDs {

		// add current id to batch
		idList = append(idList, incID)

		// if batch is full or if this is the last element in incidentIDs fetch batch's incident details
		if len(idList) >= irisMaxIncidentsPerRequest || idx >= (len(incidentIDs)-1) {
			// copy slice to be used to fetch this batch of incidents concurrently
			batch := make([]uint64, len(idList))
			copy(batch, idList)

			// make concurrent call to fetch this batch of incidents
			wg.Add(1)
			go i.fetchIncidentBatch(batch, incidentChan, &wg)

			// empty idList for next batch
			idList = make([]uint64, 0)
		}
	}
	// wait for all the batches to finish fetching incident data and writing it to the channel
	wg.Wait()

	// read all incidents from channel to create activeIncidentList
	activeIncidentList := make([]Incident, 0)
LOOP:
	// read all values currently in the channel and then close it
	for {
		select {
		case incident, ok := <-incidentChan:
			if ok {
				activeIncidentList = append(activeIncidentList, incident)
			} else {
				break LOOP
			}
		default:
			break LOOP
		}
	}
	close(incidentChan)
	i.logger.Infof("Processing %d active incidents", len(activeIncidentList))
	// make a map of new incident ids to more quickly search for incidents that need to be deactivated
	newIncidentIdMap := make(map[uint64]bool)
	for _, incident := range activeIncidentList {
		newIncidentIdMap[incident.IncidentID] = true
	}

	// identify and remove incidents that are no longer active
	i.escalationMapMutex.RLock()
	incidentIDsToDeactivate := make([]uint64, 0)
	for incidentId, _ := range i.escalationMap {
		if _, ok := newIncidentIdMap[incidentId]; !ok {
			// incident is no longer active or assigned to this IMP instance
			incidentIDsToDeactivate = append(incidentIDsToDeactivate, incidentId)

		}
	}
	i.escalationMapMutex.RUnlock()

	// remove inactive incidents
	for _, incidentID := range incidentIDsToDeactivate {
		i.deleteIncident(incidentID)
	}

	if i.messageQueueManager.CheckQueueFull() {
		// if message queue is full do not process any new incidents
		i.logger.Infof("Message manager queue is full! Skipping new incidents this iteration...")
	} else {
		i.queueEmptyTime = time.Now()
		// add new active incidents
		i.addNewIncidents(activeIncidentList)
	}
	// emit how long it has been since queue wasn't full
	gaugeLastEmptyQueueTime.Update(int64(time.Since(i.queueEmptyTime)))

	// prepare and send messages for all incidents concurrently
	var escalateWg sync.WaitGroup
	i.escalationMapMutex.RLock()
	gaugeTotalIncidents.Update(int64(len(i.escalationMap)))
	for id := range i.escalationMap {
		escalateWg.Add(1)
		go i.escalate(id, &escalateWg)
	}
	i.escalationMapMutex.RUnlock()
	escalateWg.Wait()

	diff := time.Since(startTime)
	i.logger.Infof("Incident escalation total time taken %v seconds", diff.Seconds())
	gaugeEscalateTime.Update(int64(diff.Seconds()))
}

func (i *IncidentManager) loadPriorityMap() error {
	var err error
	priorityMap, modeIDMap, defaultPriorityMap, err = i.irisClient.GetPriorityModeMap()
	return err
}

// Run fetch anew incidents and escalate existing ones every 60 seconds
func (i *IncidentManager) Run() {
	// close wg to signal main process we have gracefully terminated
	defer i.mainWg.Done()

	go i.IrisHeartbeat()

	err := i.loadPriorityMap()
	if err != nil {
		// failed basic iris setup, bail
		i.logger.Fatalf("Error failed retrieving priority and mode information from iris: %s", err)
	}

	interval := time.NewTicker(i.config.RunLoopDuration * time.Second)
	i.queueEmptyTime = time.Now()
	defer interval.Stop()
	for range interval.C {
		i.RunOnce()
		select {
		case <-i.Quit:
			i.logger.Infof("Stopped incidentManager...")
			return
		default:
			continue
		}
	}

}
