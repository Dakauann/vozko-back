package notification_usecase

import (
	"context"
	"encoding/json"

	"vozko/domain/messaging"
	"vozko/domain/notification"
)

type queue struct {
	publisher messaging.MessageQueuePub
}

func NewQueue(publisher messaging.MessageQueuePub) notification.Queue {
	return &queue{publisher: publisher}
}

func (q *queue) EnqueueFanout(_ context.Context, notificationID string) error {
	return q.publish(notification.FanoutTopic, notification.FanoutMessage{NotificationID: notificationID})
}

func (q *queue) EnqueueBroadcast(_ context.Context, broadcastID string) error {
	return q.publish(notification.BroadcastTopic, notification.BroadcastMessage{BroadcastID: broadcastID})
}

func (q *queue) publish(topic string, message interface{}) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return q.publisher.Publish(topic, payload)
}
