package mercadolivre

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type Topic string

const (
	TopicQuestions Topic = "questions"
	TopicMessages  Topic = "messages"
	TopicOrders    Topic = "orders_v2"
	TopicShipments Topic = "shipments"
	TopicItems     Topic = "items"
)

func SubscribedTopics() []Topic {
	return []Topic{TopicQuestions, TopicMessages, TopicOrders, TopicShipments, TopicItems}
}

const (
	ActionCreated = "created"
	ActionRead    = "read"
)

type Notification struct {
	ID            string   `json:"id,omitempty"`
	Resource      string   `json:"resource"`
	UserID        string   `json:"user_id"`
	Topic         string   `json:"topic"`
	ApplicationID string   `json:"application_id"`
	Attempts      int      `json:"attempts"`
	Actions       []string `json:"actions,omitempty"`
	Sent          string   `json:"sent,omitempty"`
	Received      string   `json:"received,omitempty"`
}

type notificationWire struct {
	UnderscoreID  string     `json:"_id"`
	ID            string     `json:"id"`
	Resource      string     `json:"resource"`
	UserID        FlexibleID `json:"user_id"`
	Topic         string     `json:"topic"`
	ApplicationID FlexibleID `json:"application_id"`
	Attempts      int        `json:"attempts"`
	Actions       []string   `json:"actions"`
	Sent          string     `json:"sent"`
	Received      string     `json:"received"`
	Recieved      string     `json:"recieved"`
}

func Decode(raw []byte) (Notification, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return Notification{}, ErrInvalidNotification
	}
	var w notificationWire
	if err := json.Unmarshal(trimmed, &w); err != nil {
		return Notification{}, fmt.Errorf("%w: %v", ErrInvalidNotification, err)
	}
	n := Notification{
		ID:            strings.TrimSpace(w.UnderscoreID),
		Resource:      strings.TrimSpace(w.Resource),
		UserID:        w.UserID.String(),
		Topic:         strings.TrimSpace(w.Topic),
		ApplicationID: w.ApplicationID.String(),
		Attempts:      w.Attempts,
		Actions:       w.Actions,
		Sent:          strings.TrimSpace(w.Sent),
		Received:      strings.TrimSpace(w.Received),
	}
	if n.ID == "" {
		n.ID = strings.TrimSpace(w.ID)
	}
	if n.Received == "" {
		n.Received = strings.TrimSpace(w.Recieved)
	}
	return n, nil
}

type JobKind string

const (
	JobFetchMessage  JobKind = "fetch_message"
	JobFetchThread   JobKind = "fetch_thread"
	JobFetchQuestion JobKind = "fetch_question"
	JobFetchOrder    JobKind = "fetch_order"
	JobFetchShipment JobKind = "fetch_shipment"
	JobFetchItem     JobKind = "fetch_item"
	JobRefreshPack   JobKind = "refresh_pack"
)

type Job struct {
	Kind           JobKind `json:"kind"`
	Topic          string  `json:"topic"`
	SellerID       string  `json:"sellerId"`
	ResourceID     string  `json:"resourceId"`
	Resource       string  `json:"resource"`
	Read           bool    `json:"read,omitempty"`
	NotificationID string  `json:"notificationId,omitempty"`
	Received       string  `json:"received,omitempty"`
	Sent           string  `json:"sent,omitempty"`
	Marker         string  `json:"marker,omitempty"`
}

func (j Job) IdempotencyKey() string {
	prefix := "ml:" + j.Topic + ":" + j.Resource + ":"
	switch {
	case j.NotificationID != "":
		return prefix + j.NotificationID
	case j.Received != "":
		return prefix + "r:" + j.Received
	case j.Sent != "":
		return prefix + "s:" + j.Sent
	case j.Marker != "":
		return prefix + "m:" + j.Marker
	}
	return ""
}

var (
	questionResource = regexp.MustCompile(`^/questions/([0-9]{1,20})$`)
	messageResource  = regexp.MustCompile(`^([0-9A-Za-z]{16,64})$`)
	orderResource    = regexp.MustCompile(`^/orders/([0-9]{1,20})$`)
	shipmentResource = regexp.MustCompile(`^/shipments/([0-9]{1,20})$`)
	itemResource     = regexp.MustCompile(`^/items/(MLB[0-9]{1,20})$`)
)

type topicRoute struct {
	kind     JobKind
	resource *regexp.Regexp
}

var topicRoutes = map[Topic]topicRoute{
	TopicQuestions: {JobFetchQuestion, questionResource},
	TopicMessages:  {JobFetchMessage, messageResource},
	TopicOrders:    {JobFetchOrder, orderResource},
	TopicShipments: {JobFetchShipment, shipmentResource},
	TopicItems:     {JobFetchItem, itemResource},
}

func Normalize(n Notification, appID string) (Job, error) {
	if appID == "" || n.ApplicationID != appID {
		return Job{}, fmt.Errorf("%w: %q", ErrWrongApplication, n.ApplicationID)
	}
	if !ValidNumericID(n.UserID) {
		return Job{}, fmt.Errorf("%w: %q", ErrUserIDInvalid, n.UserID)
	}
	route, ok := topicRoutes[Topic(n.Topic)]
	if !ok {
		return Job{}, fmt.Errorf("%w: %q", ErrUnknownTopic, n.Topic)
	}
	m := route.resource.FindStringSubmatch(n.Resource)
	if m == nil {
		return Job{}, fmt.Errorf("%w: %s %q", ErrInvalidResource, n.Topic, n.Resource)
	}
	job := Job{
		Kind:           route.kind,
		Topic:          n.Topic,
		SellerID:       n.UserID,
		ResourceID:     m[1],
		Resource:       n.Resource,
		NotificationID: n.ID,
		Received:       n.Received,
		Sent:           n.Sent,
	}
	if Topic(n.Topic) == TopicMessages {
		read, err := messageReadAction(n.Actions)
		if err != nil {
			return Job{}, err
		}
		job.Read = read
	}
	if job.IdempotencyKey() == "" {
		return Job{}, ErrNotificationIdentity
	}
	return job, nil
}

func messageReadAction(actions []string) (bool, error) {
	read := false
	for _, a := range actions {
		switch a {
		case ActionCreated:
		case ActionRead:
			read = true
		default:
			return false, fmt.Errorf("%w: %q", ErrUnknownAction, a)
		}
	}
	return read, nil
}

func ThreadJob(sellerID, packKey, marker string) (Job, error) {
	if !ValidNumericID(sellerID) {
		return Job{}, ErrUserIDInvalid
	}
	if !ValidNumericID(packKey) {
		return Job{}, fmt.Errorf("%w: pack %q", ErrInvalidResource, packKey)
	}
	if strings.TrimSpace(marker) == "" {
		return Job{}, ErrNotificationIdentity
	}
	return Job{
		Kind:       JobFetchThread,
		Topic:      "thread",
		SellerID:   sellerID,
		ResourceID: packKey,
		Resource:   ThreadResource(packKey, sellerID),
		Marker:     marker,
	}, nil
}

func QuestionJob(sellerID, questionID, marker string) (Job, error) {
	if !ValidNumericID(sellerID) {
		return Job{}, ErrUserIDInvalid
	}
	if !ValidNumericID(questionID) {
		return Job{}, fmt.Errorf("%w: question %q", ErrInvalidResource, questionID)
	}
	if strings.TrimSpace(marker) == "" {
		return Job{}, ErrNotificationIdentity
	}
	return Job{
		Kind:       JobFetchQuestion,
		Topic:      string(TopicQuestions),
		SellerID:   sellerID,
		ResourceID: questionID,
		Resource:   "/questions/" + questionID,
		Marker:     marker,
	}, nil
}
