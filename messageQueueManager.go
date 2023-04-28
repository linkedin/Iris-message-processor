package main

import (
	"strconv"
	"sync"
	"time"

	gometrics "github.com/rcrowley/go-metrics"
	uuid "github.com/satori/go.uuid"
)

var (
	counterMessagesQueued            = gometrics.NewCounter()
	counterAllVendorsSuccessTotal    = gometrics.NewCounter()
	counterAllVendorsFailureTotal    = gometrics.NewCounter()
	counterAllVendorsSuccessOOB      = gometrics.NewCounter()
	counterAllVendorsSuccessIncident = gometrics.NewCounter()
	gaugeQueueingTime                = gometrics.NewGauge()
	gaugeMessagesQueueLength         = gometrics.NewGauge()

	// group all out of band messages under this synthetic plan name
	oobMessageMockPlanName = "oob-message-2b01cd5a-ec0c-4a07-a062-0c0594d4e41a"
)

const (
	maxBodyLen    = 60000
	maxSubjectLen = 254

	// vendor names as used in maps
	EMAIL_VENDOR  = "email"
	SLACK_VENDOR  = "slack"
	TWILIO_VENDOR = "twilio"
	FCM_VENDOR    = "fcm"
	DROP_VENDOR   = "drop"
	ERROR_VENDOR  = "error"
	DUMMY_VENDOR  = "dummy"
)

type MessageQueueItem struct {
	isOOB          bool
	isErrorMessage bool
	active         bool
	queued         bool
	retries        int
	incidentID     uint64
	plan           string
	message        Message
	sentTime       time.Time
	addedTime      time.Time
	batchID        uuid.UUID
}

type VendorInterface interface {
	SendMessage(message MessageQueueItem) bool
	GetNextMessage(plan string) uuid.UUID
	GetNextPlan() string
	AddToVendorQueue(msgQueueItem MessageQueueItem)
	AdvanceQueue(plan string)
	GetVendorName() string
	QuitVendor()
}

type MessageQueueManager struct {
	Quit                chan int // send signal to shutdown
	vendorQuit          chan int
	messageQuit         chan int
	config              *Config
	metricsRegistry     *gometrics.Registry
	logger              *Logger
	mainWg              *sync.WaitGroup
	aggregationManager  *AggregationManager
	dbClient            *DbClient
	irisClient          *IrisClient
	vendorMap           map[string]VendorInterface
	messageTrackerMap   map[uuid.UUID]MessageQueueItem
	messageTrackerMutex sync.RWMutex
	queueFullMutex      sync.RWMutex
	queueFull           bool
	maxQueueLength      int
	peerCount           int
}

func (m *MessageQueueManager) RegisterMetrics(reg gometrics.Registry) {
	reg.Register("messageQueue.internal.messages_added_to_queue", counterMessagesQueued)
	reg.Register("messageQueue.internal.message_queue_length", gaugeMessagesQueueLength)
	reg.Register("messageQueue.internal.all_vendors_success_total", counterAllVendorsSuccessTotal)
	reg.Register("messageQueue.internal.all_vendors_failure_total", counterAllVendorsFailureTotal)
	reg.Register("messageQueue.internal.all_vendors_success_oob", counterAllVendorsSuccessOOB)
	reg.Register("messageQueue.internal.all_vendors_success_incident", counterAllVendorsSuccessIncident)
	reg.Register("messageQueue.internal.message_queueing_time", gaugeQueueingTime)
}

// NewMessageQueueManager create new MessageQueueManager instance
func NewMessageQueueManager(aggregationManager *AggregationManager, dbClient *DbClient, irisClient *IrisClient, config *Config, logger *Logger, quit chan int, wg *sync.WaitGroup, metricsRegistry *gometrics.Registry) *MessageQueueManager {

	messageQueueManager := MessageQueueManager{
		Quit:               quit, // send signal to shutdown
		vendorQuit:         make(chan int),
		config:             config,
		metricsRegistry:    metricsRegistry,
		logger:             logger,
		mainWg:             wg,
		aggregationManager: aggregationManager,
		dbClient:           dbClient,
		irisClient:         irisClient,
		messageTrackerMap:  make(map[uuid.UUID]MessageQueueItem),
		maxQueueLength:     config.MaxQueueLength,
		peerCount:          1,
	}

	if appConfig.MetricsDryRun == false && metricsRegistry != nil {
		messageQueueManager.RegisterMetrics(*metricsRegistry)
	}

	return &messageQueueManager
}

func (m *MessageQueueManager) CheckMessage(messageUUID uuid.UUID) (MessageQueueItem, bool) {
	m.messageTrackerMutex.RLock()
	defer m.messageTrackerMutex.RUnlock()
	if msgQueueItem, ok := m.messageTrackerMap[messageUUID]; ok {
		return msgQueueItem, true
	} else {
		return MessageQueueItem{}, false
	}
}

func (m *MessageQueueManager) CheckQueueFull() bool {
	m.queueFullMutex.RLock()
	defer m.queueFullMutex.RUnlock()
	return m.queueFull
}

// AddMessageQueue add message to messageTrackerMap if it does not already exist and return message that is in queue
func (m *MessageQueueManager) AddMessage(msgQueueItem MessageQueueItem) MessageQueueItem {
	m.messageTrackerMutex.Lock()
	defer m.messageTrackerMutex.Unlock()
	if queuedMsg, ok := m.messageTrackerMap[msgQueueItem.message.MessageUUID]; ok {
		return queuedMsg
	}
	msgQueueItem.addedTime = time.Now()
	m.messageTrackerMap[msgQueueItem.message.MessageUUID] = msgQueueItem
	return msgQueueItem

}

// CloneMessage make a duplicate Message
func (m *MessageQueueManager) CloneMessage(msg Message) (Message, error) {

	muuid := uuid.NewV4()

	deviceIDsCopy := append([]string{}, msg.DeviceIDs...)

	clonedMessage := Message{
		MessageUUID:            muuid,
		Application:            msg.Application,
		Target:                 msg.Target,
		Destination:            msg.Destination,
		DeviceIDs:              deviceIDsCopy,
		BCCDestination:         msg.BCCDestination,
		From:                   msg.From,
		EmailHTML:              msg.EmailHTML,
		EmailText:              msg.EmailText,
		Mode:                   msg.Mode,
		ModeID:                 msg.ModeID,
		Priority:               msg.Priority,
		PriorityID:             msg.PriorityID,
		Subject:                msg.Subject,
		Body:                   msg.Body,
		Template:               msg.Template,
		TemplateID:             msg.TemplateID,
		PlanStep:               msg.PlanStep,
		PlanStepNotificationId: msg.PlanStepNotificationId,
		PlanStepIndex:          msg.PlanStepIndex,
	}

	return clonedMessage, nil

}

func (m *MessageQueueManager) UpdateMessage(messageUUID uuid.UUID, msgQueueItem MessageQueueItem) {
	m.messageTrackerMutex.Lock()
	defer m.messageTrackerMutex.Unlock()
	if _, ok := m.messageTrackerMap[messageUUID]; ok {
		m.messageTrackerMap[messageUUID] = msgQueueItem
	}
}

func (m *MessageQueueManager) DeleteMessage(messageUUID uuid.UUID) {
	m.messageTrackerMutex.Lock()
	defer m.messageTrackerMutex.Unlock()
	delete(m.messageTrackerMap, messageUUID)

}

func (m *MessageQueueManager) PersistMessage(messageQueueItem MessageQueueItem, vendorID string, status string) {
	// store message and status in the db
	m.dbClient.writeMessage(messageQueueItem)
	msgID := messageQueueItem.message.MessageUUID.String()
	m.dbClient.writeStatus(msgID, status, vendorID)
}

func (m *MessageQueueManager) getVendorRateLimit(vendorName string) time.Duration {
	m.queueFullMutex.RLock()
	defer m.queueFullMutex.RUnlock()
	// if config not set default to 1 qps
	timeBetweenRequests := 1 * time.Second
	if qpsVal, ok := m.config.VendorRateLimitMap[vendorName]; ok {
		// calculate time between requests needed to stay below rate limit given a qps limit and a number of peers
		timeBetweenRequests = (time.Second / time.Duration(qpsVal)) * time.Duration(m.peerCount)
	}
	return timeBetweenRequests
}

// generic function to run every type of vendor
func (m *MessageQueueManager) runVendor(vendorInterface VendorInterface, vendorWg *sync.WaitGroup) {
	defer vendorWg.Done()

	// get time between queries are needed to maintain that qps rate
	timeBetweenRequests := m.getVendorRateLimit(vendorInterface.GetVendorName())

	// ticker must be defined explicitly so that it may be garbage collected when another iris node goes down and the rate limit changes
	ticker := time.NewTicker(timeBetweenRequests)
	defer ticker.Stop()

	// check if rate limit needs to be refreshed once a second when actively sending messages
	rateLimitUpdateTicker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// send new messages via this vendor at a rate no faster that the rate limit
	for ; true; <-ticker.C {
		select {
		case <-m.vendorQuit:
			return
		// pull message off the queue and send
		default:

			select {
			case <-rateLimitUpdateTicker.C:
				// check if rate limit needs adjusting
				currentTimeBetweenRequests := m.getVendorRateLimit(vendorInterface.GetVendorName())
				if currentTimeBetweenRequests != timeBetweenRequests {
					timeBetweenRequests = currentTimeBetweenRequests
					ticker.Reset(timeBetweenRequests)
				}
			default:
			}

			// get next plan up in the round robin order
			nextPlan := vendorInterface.GetNextPlan()
			// channel was closed by queuemanager
			if nextPlan == "" {
				continue
			}

			// get the first element of the message queue for the plan whose turn it is
			msgUUID := vendorInterface.GetNextMessage(nextPlan)

			// get the messageQueueItem that the UUID is referencing
			msgQueueItem, msgExists := m.CheckMessage(msgUUID)
			if !msgExists {
				// incident was claimed and message has been deleted, move on with the queue
				vendorInterface.AdvanceQueue(nextPlan)
				continue
			}

			// send message
			msgQueueItem.sentTime = time.Now()
			sentSuccessfully := vendorInterface.SendMessage(msgQueueItem)

			if sentSuccessfully || msgQueueItem.retries >= m.config.MaxMessageRetries {

				if sentSuccessfully {
					counterAllVendorsSuccessTotal.Inc(1)
					if msgQueueItem.plan == oobMessageMockPlanName {
						counterAllVendorsSuccessOOB.Inc(1)
					} else {
						counterAllVendorsSuccessIncident.Inc(1)
					}
				} else {
					counterAllVendorsFailureTotal.Inc(1)
					m.logger.Errorf("Failed to send message after all retries: %+v", msgQueueItem)
				}

				msgQueueItem.active = false
				m.UpdateMessage(msgQueueItem.message.MessageUUID, msgQueueItem)
				vendorInterface.AdvanceQueue(nextPlan)
				// if message is from a notification delete it now, incident messages get cleaned up by the incidentManager
				if msgQueueItem.isOOB || msgQueueItem.isErrorMessage {
					m.DeleteMessage(msgQueueItem.message.MessageUUID)
				}
			} else {
				// if message wasn't sent successfully retry
				msgQueueItem.retries++
				m.UpdateMessage(msgQueueItem.message.MessageUUID, msgQueueItem)
				vendorInterface.AddToVendorQueue(msgQueueItem)
				vendorInterface.AdvanceQueue(nextPlan)
			}

		}
	}

}

func (m *MessageQueueManager) initVendors(vendorWg *sync.WaitGroup) {
	m.vendorMap = map[string]VendorInterface{}

	dummyVendor := NewDummyVendor(m.dbClient, m.config, m.logger.Clone("dummyvendor"), m.metricsRegistry)
	vendorWg.Add(1)
	go m.runVendor(dummyVendor, vendorWg)
	m.vendorMap[DUMMY_VENDOR] = dummyVendor

	errorVendor := NewErrorVendor(m.dbClient, m.config, m.logger.Clone("errorVendor"), m.metricsRegistry)
	vendorWg.Add(1)
	go m.runVendor(errorVendor, vendorWg)
	m.vendorMap[ERROR_VENDOR] = errorVendor

	dropVendor := NewDropVendor(m.dbClient, m.config, m.logger.Clone("dropVendor"), m.metricsRegistry)
	vendorWg.Add(1)
	go m.runVendor(dropVendor, vendorWg)
	m.vendorMap[DROP_VENDOR] = dropVendor

	emailVendor := NewEmailVendor(m.dbClient, m.config, m.logger.Clone("smtpVendor"), m.metricsRegistry)
	vendorWg.Add(1)
	go m.runVendor(emailVendor, vendorWg)
	m.vendorMap[EMAIL_VENDOR] = emailVendor

	slackVendor := NewSlackVendor(m.dbClient, m.config, m.logger.Clone("slackVendor"), m.metricsRegistry)
	vendorWg.Add(1)
	go m.runVendor(slackVendor, vendorWg)
	m.vendorMap[SLACK_VENDOR] = slackVendor

	twilioVendor := NewTwilioVendor(m.dbClient, m.config, m.logger.Clone("twilioVendor"), m.metricsRegistry)
	vendorWg.Add(1)
	go m.runVendor(twilioVendor, vendorWg)
	m.vendorMap[TWILIO_VENDOR] = twilioVendor

	fcmVendor := NewFCMVendor(m.dbClient, m.config, m.logger.Clone("fcmVendor"), m.metricsRegistry)
	vendorWg.Add(1)
	go m.runVendor(fcmVendor, vendorWg)
	m.vendorMap[FCM_VENDOR] = fcmVendor
}

// Single interval of message queue
func (m *MessageQueueManager) RunOnce() {
	// take any un-queued messages and add them to their corresponding vendor queue
	unqueuedMessages := make([]MessageQueueItem, 0)
	m.queueFullMutex.Lock()
	m.messageTrackerMutex.Lock()
	var msgQueueLength int64
	for uuid, msgQueueItem := range m.messageTrackerMap {
		msgQueueLength++
		if !msgQueueItem.queued {
			msgQueueItem.queued = true
			unqueuedMessages = append(unqueuedMessages, msgQueueItem)
			m.messageTrackerMap[uuid] = msgQueueItem
			counterMessagesQueued.Inc(1)
		}
	}
	gaugeMessagesQueueLength.Update(msgQueueLength)

	// queue backpressure, if too full momentarily pause processing new incidents and accepting out-of-band messages while the queue drains back below max length so queue size is bounded
	if len(m.messageTrackerMap) > m.maxQueueLength {
		m.queueFull = true
	} else {
		m.queueFull = false
	}

	// get current peer count from iris-api
	peerCount, err := m.irisClient.GetPeerCount()
	if err == nil {
		if peerCount < 1 {
			// could potentially query iris for peercount before this node has finished registering, avoid divide by 0
			m.logger.Warn("peerCount less than 1")
			m.peerCount = 1
		} else {
			m.peerCount = peerCount
		}
	} else {
		m.logger.Errorf("Failed retrieving peer count with error: %w", err)
	}

	m.messageTrackerMutex.Unlock()
	m.queueFullMutex.Unlock()

	for _, msgQueueItem := range unqueuedMessages {

		msgChanged := false
		// check if plan is under aggregation
		if !msgQueueItem.isOOB && !msgQueueItem.isErrorMessage {
			if batchID, aggregated := m.aggregationManager.checkAggregation(msgQueueItem.plan); aggregated {
				// plan is currently under aggregation change mode so that it is only stored in the db instead of set out
				msgQueueItem.message.Mode = AggregationMode
				// add batch id so that user can find the aggregated messages
				msgQueueItem.batchID = batchID
				msgChanged = true
			}
		}

		mode := msgQueueItem.message.Mode
		if len(msgQueueItem.plan) == 0 {
			// vendors round robin the available rate limit by plan so group all out-of-band notifications under a fake plan name
			msgQueueItem.plan = oobMessageMockPlanName
			msgQueueItem.message.Body = "[" + msgQueueItem.message.Application + "]\n" + msgQueueItem.message.Body
			msgQueueItem.message.Subject = "[" + msgQueueItem.message.Application + "] " + msgQueueItem.message.Subject
			msgChanged = true
		} else if mode == EmailMode {
			msgQueueItem.message.Subject = strconv.FormatUint(msgQueueItem.incidentID, 10) + " " + msgQueueItem.message.Subject
			msgChanged = true
		}

		// check that user set unbounded fields are not too long for db
		if len(msgQueueItem.message.Body) > maxBodyLen {
			msgQueueItem.message.Body = msgQueueItem.message.Body[:maxBodyLen]
			msgChanged = true
		}
		if len(msgQueueItem.message.Subject) > maxSubjectLen {
			msgQueueItem.message.Subject = msgQueueItem.message.Subject[:maxSubjectLen]
			msgChanged = true
		}

		// update message with changes
		if msgChanged {
			m.UpdateMessage(msgQueueItem.message.MessageUUID, msgQueueItem)
		}

		if m.config.SenderDryRun {
			go m.vendorMap[DUMMY_VENDOR].AddToVendorQueue(msgQueueItem)
		} else {
			switch mode {
			case DropMode:
				go m.vendorMap[DROP_VENDOR].AddToVendorQueue(msgQueueItem)
			case ErrorMode:
				go m.vendorMap[ERROR_VENDOR].AddToVendorQueue(msgQueueItem)
			case EmailMode:
				go m.vendorMap[EMAIL_VENDOR].AddToVendorQueue(msgQueueItem)
			case SlackMode:
				go m.vendorMap[SLACK_VENDOR].AddToVendorQueue(msgQueueItem)
			case SMSMode, CallMode:
				go m.vendorMap[TWILIO_VENDOR].AddToVendorQueue(msgQueueItem)
			case FCMMode:
				go m.vendorMap[FCM_VENDOR].AddToVendorQueue(msgQueueItem)
			default:
				go m.vendorMap[DUMMY_VENDOR].AddToVendorQueue(msgQueueItem)
			}
		}

	}
}

// Run manage the message queue
func (m *MessageQueueManager) Run() {
	// close wg to signal main process we have gracefully terminated
	defer m.mainWg.Done()

	var vendorWg sync.WaitGroup

	// start vendor loops
	m.initVendors(&vendorWg)

	// initialize and run dummy Vendor

	interval := time.NewTicker(m.config.RunLoopDuration * time.Second)
	defer interval.Stop()
	for ; true; <-interval.C {

		startTime := time.Now()

		m.RunOnce()

		diff := time.Since(startTime)
		m.logger.Infof("Message Queueing total time taken %v seconds", diff.Seconds())
		gaugeQueueingTime.Update(int64(diff.Seconds()))
		select {
		case <-m.Quit:
			close(m.vendorQuit)
			for _, v := range m.vendorMap {
				v.QuitVendor()
			}
			// make sure vendors have shut down gracefully
			vendorWg.Wait()
			m.logger.Infof("Stopped messageQueueManager...")
			return

		default:
			continue

		}
	}

}
