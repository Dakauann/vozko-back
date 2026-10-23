package workflow

import (
	"errors"
	"fmt"
	"strings"
)

const (
	WebhookStartOnKey = "start_on"
	WebhookEventsKey  = "events"
)

type WebhookStart string

const (
	WebhookStartConversation WebhookStart = "conversation"
	WebhookStartLead         WebhookStart = "lead"
)

var ErrWebhookTriggerInvalid = errors.New("workflow: the webhook trigger settings are not valid")

func WebhookStartOf(config map[string]interface{}) (WebhookStart, bool) {
	raw, present := config[WebhookStartOnKey]
	if !present || raw == nil {
		return WebhookStartConversation, true
	}
	text, ok := raw.(string)
	if !ok {
		return "", false
	}
	switch start := WebhookStart(strings.TrimSpace(text)); start {
	case "", WebhookStartConversation:
		return WebhookStartConversation, true
	case WebhookStartLead:
		return WebhookStartLead, true
	}
	return "", false
}

func webhookAcceptedEvents(config map[string]interface{}) ([]string, bool) {
	raw, present := config[WebhookEventsKey]
	if !present || raw == nil {
		return nil, true
	}
	list, ok := raw.([]interface{})
	if !ok {
		if texts, isTexts := raw.([]string); isTexts {
			list = make([]interface{}, len(texts))
			for i, text := range texts {
				list[i] = text
			}
		} else {
			return nil, false
		}
	}
	events := make([]string, 0, len(list))
	for _, item := range list {
		text, isText := item.(string)
		name := normalizeWebhookEvent(text)
		if !isText || name == "" {
			return nil, false
		}
		events = append(events, name)
	}
	return events, true
}

func normalizeWebhookEvent(event string) string {
	return strings.ToLower(strings.TrimSpace(event))
}

type WebhookTriggerSettings struct {
	Start  WebhookStart
	events []string
}

func ReadWebhookTrigger(config map[string]interface{}) (WebhookTriggerSettings, error) {
	if field, cause := webhookTriggerProblemOf(config); field != "" {
		return WebhookTriggerSettings{}, fmt.Errorf("%w: %s", ErrWebhookTriggerInvalid, cause)
	}
	start, _ := WebhookStartOf(config)
	events, _ := webhookAcceptedEvents(config)
	return WebhookTriggerSettings{Start: start, events: events}, nil
}

func (s WebhookTriggerSettings) Accepts(event string) bool {
	if len(s.events) == 0 {
		return true
	}
	name := normalizeWebhookEvent(event)
	for _, candidate := range s.events {
		if candidate == name {
			return true
		}
	}
	return false
}

func WebhookAcceptsEvent(config map[string]interface{}, event string) bool {
	settings, err := ReadWebhookTrigger(config)
	return err == nil && settings.Accepts(event)
}

func WebhookOccurrence(event, externalID, idempotencyKey, deliveryID string) string {
	name, id := normalizeWebhookEvent(event), strings.TrimSpace(externalID)
	if name != "" && id != "" {
		return name + "/" + id
	}
	if key := strings.TrimSpace(idempotencyKey); key != "" {
		return "key/" + key
	}
	return "delivery/" + strings.TrimSpace(deliveryID)
}

func WebhookLeadEvent(subject LeadSubject, occurrence string, data map[string]interface{}) TriggerEvent {
	if data == nil {
		data = map[string]interface{}{}
	}
	event := subject.event(TriggerWebhook, data)
	event.Occurrence = occurrence
	return event
}

func ValidateWebhookTriggers(g *Graph) error {
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Type != NodeTypeTriggerWebhook {
			continue
		}
		if field, cause := webhookTriggerProblemOf(n.Config); field != "" {
			return webhookTriggerProblem(n.ID, field, cause)
		}
	}
	return nil
}

func webhookTriggerProblem(nodeID, field, cause string) error {
	return fmt.Errorf("%w: %s: node %q field %q", ErrWebhookTriggerInvalid, cause, nodeID, field)
}

func webhookTriggerProblemOf(config map[string]interface{}) (field, cause string) {
	if _, ok := WebhookStartOf(config); !ok {
		return WebhookStartOnKey, "start_on must be conversation or lead"
	}
	if _, ok := webhookAcceptedEvents(config); !ok {
		return WebhookEventsKey, "events must be a list of event names"
	}
	return "", ""
}
