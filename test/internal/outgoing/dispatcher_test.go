package outgoing_test

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	outgoing "github.com/shoplineapp/captin/internal/outgoing"
	models "github.com/shoplineapp/captin/models"
	mocks "github.com/shoplineapp/captin/test/mocks"

	"github.com/stretchr/testify/mock"
)

func setup(path string) (*mocks.StoreMock, *mocks.SenderMock, *outgoing.Dispatcher, *mocks.ThrottleMock) {
	store := new(mocks.StoreMock)
	sender := new(mocks.SenderMock)
	throttler := new(mocks.ThrottleMock)

	data, err := ioutil.ReadFile(path)
	if err != nil {
		panic(err)
	}
	configs := []models.Configuration{}
	json.Unmarshal(data, &configs)
	destinations := []models.Destination{}
	for _, config := range configs {
		destinations = append(destinations, models.Destination{Config: config})
	}

	dispatcher := outgoing.NewDispatcherWithDestinations(destinations, sender)

	return store, sender, dispatcher, throttler
}

func TestDispatchEvents_Error(t *testing.T) {
	store, sender, dispatcher, throttler := setup("fixtures/config.json")

	sender.On("SendEvent", mock.Anything, mock.Anything).Return(errors.New("Mock Error"))
	store.On("Get", mock.Anything).Return("", false, time.Duration(0), nil)
	store.On("Set", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	throttler.On("CanTrigger", mock.Anything, mock.Anything).Return(true, time.Duration(0), nil)

	dispatcher.Dispatch(models.IncomingEvent{
		Key:        "product.update",
		Source:     "core",
		Payload:    map[string]interface{}{"field1": 1},
		TargetType: "Product",
		TargetId:   "product_id",
	}, store, throttler)

	dispatcher.Dispatch(models.IncomingEvent{
		Key:        "product.update",
		Source:     "core",
		Payload:    map[string]interface{}{"field1": 2},
		TargetType: "Product",
		TargetId:   "product_id_2",
	}, store, throttler)

	time.Sleep(50 * time.Millisecond)

	sender.AssertNumberOfCalls(t, "SendEvent", 6)
	assert.Equal(t, 6, len(dispatcher.Errors))
}

func TestDispatchEvents(t *testing.T) {
	store, sender, dispatcher, throttler := setup("fixtures/config.json")

	sender.On("SendEvent", mock.Anything, mock.Anything).Return(nil)
	store.On("Get", mock.Anything).Return("", false, time.Duration(0), nil)
	store.On("Set", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	throttler.On("CanTrigger", mock.Anything, mock.Anything).Return(true, time.Duration(0), nil)

	dispatcher.Dispatch(models.IncomingEvent{
		Key:        "product.update",
		Source:     "core",
		Payload:    map[string]interface{}{"field1": 1},
		TargetType: "Product",
		TargetId:   "product_id",
	}, store, throttler)

	dispatcher.Dispatch(models.IncomingEvent{
		Key:        "product.update",
		Source:     "core",
		Payload:    map[string]interface{}{"field1": 2},
		TargetType: "Product",
		TargetId:   "product_id_2",
	}, store, throttler)

	time.Sleep(50 * time.Millisecond)

	sender.AssertNumberOfCalls(t, "SendEvent", 6)
}

func TestDispatchEvents_Throttled(t *testing.T) {
	store, sender, dispatcher, throttler := setup("fixtures/config.json")

	sender.On("SendEvent", mock.Anything, mock.Anything).Return(errors.New("Mock Error"))
	store.On("Get", mock.Anything).Return("", false, time.Duration(0), nil)
	store.On("Set", mock.Anything, mock.Anything, mock.Anything).Return(true, nil)
	throttler.On("CanTrigger", mock.Anything, mock.Anything).Return(true, time.Duration(0), nil)

	dispatcher.Dispatch(models.IncomingEvent{
		Key:        "product.update",
		Source:     "core",
		Payload:    map[string]interface{}{"field1": 1},
		TargetType: "Product",
		TargetId:   "product_id",
	}, store, throttler)

	dispatcher.Dispatch(models.IncomingEvent{
		Key:        "product.update",
		Source:     "core",
		Payload:    map[string]interface{}{"field1": 2},
		TargetType: "Product",
		TargetId:   "product_id_2",
	}, store, throttler)

	time.Sleep(50 * time.Millisecond)

	sender.AssertNumberOfCalls(t, "SendEvent", 6)
	assert.Equal(t, 6, len(dispatcher.Errors))
}
