package whatsappform

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

const (
	FlowStatusChange         = "FLOW_STATUS_CHANGE"
	FlowClientErrorRate      = "CLIENT_ERROR_RATE"
	FlowEndpointErrorRate    = "ENDPOINT_ERROR_RATE"
	FlowEndpointLatency      = "ENDPOINT_LATENCY"
	FlowEndpointAvailability = "ENDPOINT_AVAILABILITY"
	flowsField               = "flows"
	alertActivated           = "ACTIVATED"
)

var (
	ErrFlowEventInvalid = errors.New("whatsappform: the flows webhook could not be read")

	flowAlerts = []string{FlowClientErrorRate, FlowEndpointErrorRate, FlowEndpointLatency, FlowEndpointAvailability}
)

type FlowEvent struct {
	MetaWABAID string `json:"metaWabaId"`
	MetaFlowID string `json:"metaFlowId"`
	Event      string `json:"event"`
	Message    string `json:"message,omitempty"`
	OldStatus  string `json:"oldStatus,omitempty"`
	NewStatus  string `json:"newStatus,omitempty"`
	AlertState string `json:"alertState,omitempty"`
}

func (e FlowEvent) IsAlert() bool {
	return slices.Contains(flowAlerts, e.Event)
}

func (e FlowEvent) Activated() bool {
	return e.IsAlert() && e.AlertState == alertActivated
}

type flowEnvelope struct {
	Entry []struct {
		ID      string `json:"id"`
		Changes []struct {
			Field string `json:"field"`
			Value struct {
				Event      string `json:"event"`
				Message    string `json:"message"`
				FlowID     string `json:"flow_id"`
				OldStatus  string `json:"old_status"`
				NewStatus  string `json:"new_status"`
				AlertState string `json:"alert_state"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

func ParseFlowWebhook(body []byte) ([]FlowEvent, error) {
	var envelope flowEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFlowEventInvalid, err)
	}
	var events []FlowEvent
	for _, entry := range envelope.Entry {
		for _, change := range entry.Changes {
			if change.Field != flowsField || change.Value.FlowID == "" {
				continue
			}
			v := change.Value
			events = append(events, FlowEvent{
				MetaWABAID: entry.ID, MetaFlowID: v.FlowID, Event: v.Event, Message: v.Message,
				OldStatus: v.OldStatus, NewStatus: v.NewStatus, AlertState: v.AlertState,
			})
		}
	}
	return events, nil
}
