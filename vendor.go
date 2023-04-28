package main

import (
	"sync"

	gometrics "github.com/rcrowley/go-metrics"
	uuid "github.com/satori/go.uuid"
)

type Vendor struct {
	logger *Logger

	PlanQueue        chan string
	PlanQueueTracker map[string]bool
	MessageQueue     map[string][]uuid.UUID
	dbClient         *DbClient
	config           *Config
	VendorQueueMutex sync.RWMutex
	counterSuccesses gometrics.Counter
	counterFailures  gometrics.Counter
	vendorName       string
}

// NewVendor create new generic Vendor instance
func NewVendor(dbClient *DbClient, config *Config, logger *Logger, name string) *Vendor {

	vendor := Vendor{
		logger:   logger,
		dbClient: dbClient,
		config:   config,
		// channel buffer size should be large enough that blocking should be extremely unlikely. Only 1 channel per mode per instance of IMP so memory consumption isn't a big concearn
		PlanQueue:        make(chan string, 5000),
		PlanQueueTracker: make(map[string]bool),
		MessageQueue:     make(map[string][]uuid.UUID),
		counterSuccesses: gometrics.NewCounter(),
		counterFailures:  gometrics.NewCounter(),
		vendorName:       name,
	}
	return &vendor
}

func (v *Vendor) RegisterMetrics(reg gometrics.Registry) {
	reg.Register(v.vendorName+".internal.message_failures_total", v.counterFailures)
	reg.Register(v.vendorName+".internal.message_successes_total", v.counterSuccesses)
}

func (v *Vendor) QuitVendor() {
	// do any tasks needed to close out vendors gracefully
	// vendors waiting on messages will be blocked on GetNextPlan, close the channel so vendor can quit
	close(v.PlanQueue)
}

func (v *Vendor) GetVendorName() string {
	return v.vendorName
}

func (v *Vendor) SendMessage(message MessageQueueItem) bool {
	v.logger.Infof("\n Generic Vendor SendMessage: %v \n", message)
	return true
}

func (v *Vendor) GetNextMessage(plan string) uuid.UUID {
	var msgUUID uuid.UUID
	v.VendorQueueMutex.RLock()
	defer v.VendorQueueMutex.RUnlock()
	if len(v.MessageQueue[plan]) > 0 {
		msgUUID = v.MessageQueue[plan][0]
	}

	return msgUUID
}

func (v *Vendor) GetNextPlan() string {
	plan := <-v.PlanQueue
	return plan
}

func (v *Vendor) AddToVendorQueue(msgQueueItem MessageQueueItem) {
	// add message UUID to the vendor message queue under its corresponding plan
	v.VendorQueueMutex.Lock()
	defer v.VendorQueueMutex.Unlock()
	v.MessageQueue[msgQueueItem.plan] = append(v.MessageQueue[msgQueueItem.plan], msgQueueItem.message.MessageUUID)
	// add plan to planqueue if it doesn't already exist, this will be used to evenly distribute the available ratelimit for each vendor across plans
	if !v.PlanQueueTracker[msgQueueItem.plan] {
		v.PlanQueueTracker[msgQueueItem.plan] = true
		v.PlanQueue <- msgQueueItem.plan
	}
}

func (v *Vendor) AdvanceQueue(plan string) {
	v.VendorQueueMutex.Lock()
	defer v.VendorQueueMutex.Unlock()
	// remove message from vendor queue
	if len(v.MessageQueue[plan]) > 1 {
		//if there is more messages in the plan queue just remove the current message and requeue the plan
		v.MessageQueue[plan] = v.MessageQueue[plan][1:]
		v.PlanQueue <- plan

	} else {
		// no more messages in the queue for this plan, don't re-add plan to planqueue
		delete(v.MessageQueue, plan)
		v.PlanQueueTracker[plan] = false
	}
}

func (v *Vendor) persistMsg(messageQueueItem MessageQueueItem, vendorID string, status string) {
	// store message and status in the db
	go v.dbClient.writeMessage(messageQueueItem)
	msgID := messageQueueItem.message.MessageUUID.String()
	go v.dbClient.writeStatus(msgID, status, vendorID)
}
