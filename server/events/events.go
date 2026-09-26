package events

import (
	"context"

	"ourway/server/webhooks"
)

var publisher *webhooks.Dispatcher

// SetPublisher sets the global event publisher.
func SetPublisher(d *webhooks.Dispatcher) {
	publisher = d
}

// Publish sends an event to all subscribed webhooks.
func Publish(eventType string, data interface{}) {
	if publisher == nil {
		return
	}
	publisher.Publish(context.Background(), webhooks.Event{
		Type: eventType,
		Data: data,
	})
}
