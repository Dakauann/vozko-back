package mercadolivre

import (
	"errors"
	"fmt"

	"vozko/domain/webhook"
)

var ErrUnknownJobKind = errors.New("mercado livre job kind is not handled")

var jobTopics = map[JobKind]string{
	JobFetchMessage:  webhook.TopicMercadoLivreMessage,
	JobFetchThread:   webhook.TopicMercadoLivreMessage,
	JobFetchQuestion: webhook.TopicMercadoLivreQuestion,
	JobFetchItem:     webhook.TopicMercadoLivreQuestion,
	JobFetchOrder:    webhook.TopicMercadoLivreOrder,
	JobFetchShipment: webhook.TopicMercadoLivreOrder,
	JobRefreshPack:   webhook.TopicMercadoLivreOrder,
}

func JobKinds() []JobKind {
	return []JobKind{JobFetchMessage, JobFetchThread, JobFetchQuestion, JobFetchOrder, JobFetchShipment, JobFetchItem, JobRefreshPack}
}

func TopicForJob(kind JobKind) (string, error) {
	topic, ok := jobTopics[kind]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownJobKind, kind)
	}
	return topic, nil
}

func QueueTopics() []string {
	return []string{webhook.TopicMercadoLivreMessage, webhook.TopicMercadoLivreQuestion, webhook.TopicMercadoLivreOrder}
}
