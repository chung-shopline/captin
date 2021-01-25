package outgoing

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	captin_errors "github.com/shoplineapp/captin/errors"
	interfaces "github.com/shoplineapp/captin/interfaces"
	documentStores "github.com/shoplineapp/captin/internal/document_stores"
	helpers "github.com/shoplineapp/captin/internal/helpers"
	models "github.com/shoplineapp/captin/models"
	log "github.com/sirupsen/logrus"
)

var dLogger = log.WithFields(log.Fields{"class": "Dispatcher"})
var nullDocumentStore = documentStores.NewNullDocumentStore()

// Dispatcher - Event Dispatcher
type Dispatcher struct {
	destinations   []models.Destination
	senderMapping  map[string]interfaces.EventSenderInterface
	Errors         []captin_errors.ErrorInterface
	targetDocument map[string]interface{}
	filters        []interfaces.DestinationFilter
	middlewares    []interfaces.DestinationMiddleware
}

// NewDispatcherWithDestinations - Create Outgoing event dispatcher with destinations
func NewDispatcherWithDestinations(
	destinations []models.Destination,
	senderMapping map[string]interfaces.EventSenderInterface) *Dispatcher {
	result := Dispatcher{
		destinations:  destinations,
		senderMapping: senderMapping,
		filters:       []interfaces.DestinationFilter{},
		middlewares:   []interfaces.DestinationMiddleware{},
		Errors:        []captin_errors.ErrorInterface{},
	}

	return &result
}

// SetFilters - Add filters before dispatch
func (d *Dispatcher) SetFilters(filters []interfaces.DestinationFilter) {
	d.filters = filters
}

func (d *Dispatcher) SetMiddlewares(middlewares []interfaces.DestinationMiddleware) {
	d.middlewares = middlewares
}

// Dispatch - Dispatch an event to outgoing webhook
func (d *Dispatcher) Dispatch(
	e models.IncomingEvent,
	store interfaces.StoreInterface,
	throttler interfaces.ThrottleInterface,
	documentStoreMappings map[string]interfaces.DocumentStoreInterface,
) captin_errors.ErrorInterface {
	responses := make(chan int, len(d.destinations))
	for _, destination := range d.destinations {
		canTrigger, timeRemain, err := throttler.CanTrigger(getEventKey(store, e, destination), destination.Config.GetThrottleValue())
		documentStore := d.getDocumentStore(destination, documentStoreMappings)

		if err != nil {
			dLogger.WithFields(log.Fields{"event": e.GetTraceInfo(), "destination": destination, "error": err}).Error("Error on getting throttle key")

			// Send without throttling
			go func(e models.IncomingEvent, destination models.Destination, documentStore interfaces.DocumentStoreInterface) {
				d.sendEvent(e, destination, documentStore)
				responses <- 1
			}(e, destination, documentStore)
			continue
		}

		if canTrigger {
			go func(e models.IncomingEvent, destination models.Destination, documentStore interfaces.DocumentStoreInterface) {
				d.sendEvent(e, destination, documentStore)
				responses <- 1
			}(e, destination, documentStore)
		} else if !destination.Config.ThrottleTrailingDisabled {
			responses <- 0
			d.processDelayedEvent(e, timeRemain, destination, store, documentStore)
		} else {
			responses <- 0
		}
	}
	// Wait for destination completion
	for _, _ = range d.destinations {
		<-responses
	}
	return nil
}

// Private Functions

func (d Dispatcher) getDocumentStore(dest models.Destination, documentStoreMappings map[string]interfaces.DocumentStoreInterface) interfaces.DocumentStoreInterface {
	if documentStoreMappings[dest.GetDocumentStore()] != nil {
		return documentStoreMappings[dest.GetDocumentStore()]
	}
	return nullDocumentStore
}

// inject document and sanitize fields in event based on destination
func (d *Dispatcher) customizeEvent(e models.IncomingEvent, destination models.Destination, documentStore interfaces.DocumentStoreInterface) models.IncomingEvent {
	customized := e

	d.customizeDocument(&customized, destination, documentStore)
	d.customizePayload(&customized, destination)

	return customized
}

func (d *Dispatcher) customizeDocument(e *models.IncomingEvent, destination models.Destination, documentStore interfaces.DocumentStoreInterface) {
	if destination.Config.IncludeDocument == false {
		return
	}

	// memoize document to be used across events for diff. destinations
	if d.targetDocument == nil {
		d.targetDocument = documentStore.GetDocument(*e)
	}

	if len(destination.Config.IncludeDocumentAttrs) >= 1 {
		e.TargetDocument = helpers.IncludeFields(d.targetDocument, destination.Config.IncludeDocumentAttrs).(map[string]interface{})
	} else if len(destination.Config.ExcludeDocumentAttrs) >= 1 {
		e.TargetDocument = helpers.ExcludeFields(d.targetDocument, destination.Config.ExcludeDocumentAttrs).(map[string]interface{})
	} else {
		e.TargetDocument = d.targetDocument
	}

	return
}

func (d *Dispatcher) customizePayload(e *models.IncomingEvent, destination models.Destination) {
	if len(destination.Config.IncludePayloadAttrs) >= 1 {
		e.Payload = helpers.IncludeFields(e.Payload, destination.Config.IncludePayloadAttrs).(map[string]interface{})
	} else if len(destination.Config.ExcludePayloadAttrs) >= 1 {
		e.Payload = helpers.ExcludeFields(e.Payload, destination.Config.ExcludePayloadAttrs).(map[string]interface{})
	}

	return
}

func (d *Dispatcher) processDelayedEvent(e models.IncomingEvent, timeRemain time.Duration, dest models.Destination, store interfaces.StoreInterface, documentStore interfaces.DocumentStoreInterface) {
	defer func() {
		if err := recover(); err != nil {
			d.Errors = append(d.Errors, &captin_errors.DispatcherError{
				Msg:         err.(error).Error(),
				Destination: dest,
				Event:       e,
			})
		}
	}()

	// Check if store have payload
	dataKey := getEventDataKey(store, e, dest)
	storedData, dataExists, _, storeErr := store.Get(dataKey)
	if storeErr != nil {
		panic(storeErr)
	}

	jsonString, jsonErr := json.Marshal(e)
	if jsonErr != nil {
		panic(jsonErr)
	}

	if dataExists {
		storedEvent := models.IncomingEvent{}
		json.Unmarshal([]byte(storedData), &storedEvent)
		if getControlTimestamp(storedEvent, 0) > getControlTimestamp(e, uint64(time.Now().UnixNano())) {
			// Skip updating event data as stored data has newer timestamp
			dLogger.WithFields(log.Fields{
				"storedEvent":  storedEvent,
				"event":        e.GetTraceInfo(),
				"eventDataKey": "dataKey",
			}).Debug("Skipping update on event data")
			return
		}
	}

	if dataExists {
		// Update Value
		_, updateErr := store.Update(dataKey, string(jsonString))
		if updateErr != nil {
			panic(updateErr)
		}
	} else {
		// Create Value
		_, saveErr := store.Set(dataKey, string(jsonString), dest.Config.GetThrottleValue()*2)
		if saveErr != nil {
			panic(saveErr)
		}

		// Schedule send event later
		time.AfterFunc(timeRemain, d.sendAfterEvent(dataKey, store, dest, documentStore))
	}
}

func getControlTimestamp(e models.IncomingEvent, defaultValue uint64) uint64 {
	defer func(d uint64) uint64 {
		if err := recover(); err != nil {
			return d
		}
		return 0
	}(defaultValue)

	value := e.Control["ts"]

	// Type assertion from interface
	switch v := value.(type) {
	case int:
		value = uint64(v)
	case string:
		parsed, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			panic("unable to convert string timestamp")
		}
		value = parsed
	}

	return value.(uint64)
}

func getEventKey(s interfaces.StoreInterface, e models.IncomingEvent, d models.Destination) string {
	return s.DataKey(e, d, "", "")
}

func getEventDataKey(s interfaces.StoreInterface, e models.IncomingEvent, d models.Destination) string {
	return s.DataKey(e, d, "", "-data")
}

func (d *Dispatcher) sendEvent(evt models.IncomingEvent, destination models.Destination, documentStore interfaces.DocumentStoreInterface) {
	callbackLogger := dLogger.WithFields(log.Fields{
		"action":         evt.Key,
		"event":          evt.GetTraceInfo(),
		"hook_name":      destination.Config.Name,
		"callback_url":   destination.GetCallbackURL(),
		"document_store": destination.GetDocumentStore(),
	})

	defer func() {
		if err := recover(); err != nil {
			callbackLogger.Info(fmt.Sprintf("Event failed sending to %s [%s]", destination.Config.Name, destination.GetCallbackURL()))
			d.Errors = append(d.Errors, err.(*captin_errors.DispatcherError))
		}
		return
	}()

	callbackLogger.Debug("Preprocess payload and document")

	customizedEvt := d.customizeEvent(evt, destination, documentStore)

	callbackLogger.Debug("Final sift on dispatcher")

	sifted := Custom{}.Sift(&customizedEvt, []models.Destination{destination}, d.filters, d.middlewares)
	if len(sifted) == 0 {
		callbackLogger.Info("Event interrupted by dispatcher filters")
		return
	}

	callbackLogger.Debug("Ready to send event")

	senderKey := destination.Config.Sender
	if senderKey == "" {
		senderKey = "http"
	}
	sender, senderExists := d.senderMapping[senderKey]
	if senderExists == false {
		panic(&captin_errors.DispatcherError{
			Msg:         fmt.Sprintf("Sender key %s does not exist", senderKey),
			Destination: destination,
			Event:       customizedEvt,
		})
		return
	}

	if destination.Config.GetDelayValue() != time.Duration(0) {
		// Sending message with delay in goroutine, no error will be caught
		callbackLogger.Info(fmt.Sprintf("Event delayed with %d", destination.Config.Delay))
		go time.AfterFunc(destination.Config.GetDelayValue(), func() {
			delayedErr := sender.SendEvent(customizedEvt, destination)
			if delayedErr != nil {
				callbackLogger.WithFields(log.Fields{"error": delayedErr}).Error(fmt.Sprintf("Delayed event failed with error on %s [%s]", destination.Config.Name, destination.GetCallbackURL()))
				return
			}

			callbackLogger.Info(fmt.Sprintf("Event successfully sent to %s [%s]", destination.Config.Name, destination.GetCallbackURL()))
		})
		return
	}

	err := sender.SendEvent(customizedEvt, destination)
	if err != nil {
		panic(&captin_errors.DispatcherError{
			Msg:         err.Error(),
			Destination: destination,
			Event:       customizedEvt,
		})
		return
	}

	callbackLogger.Info(fmt.Sprintf("Event successfully sent to %s [%s]", destination.Config.Name, destination.GetCallbackURL()))
}

func (d *Dispatcher) sendAfterEvent(key string, store interfaces.StoreInterface, dest models.Destination, documentStore interfaces.DocumentStoreInterface) func() {
	dLogger.WithFields(log.Fields{"key": key}).Debug("After event callback")
	payload, _, _, _ := store.Get(key)
	event := models.IncomingEvent{}
	json.Unmarshal([]byte(payload), &event)
	return func() {
		d.sendEvent(event, dest, documentStore)
		store.Remove(key)
	}
}
