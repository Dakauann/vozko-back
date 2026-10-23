package cmfake

import (
	"encoding/json"
	"fmt"
	"time"

	"vozko/domain/carriermessaging"
	"vozko/infra/cmcom"
)

const (
	webhookRCS = "RCS"
	webhookSMS = "SMS"
)

type StatusReport struct {
	Reference        string
	Phone            string
	Status           carriermessaging.Status
	ErrorCode        string
	ErrorDescription string
	Operator         string
	At               time.Time
}

type InboundMessage struct {
	Reference  string
	Phone      string
	Name       string
	Channel    carriermessaging.Channel
	To         string
	Text       string
	MediaURL   string
	MediaType  string
	MediaTitle string
	Postback   string
	InReplyTo  string
	At         time.Time
}

type SuggestionClick struct {
	EventID   string
	Reference string
	Phone     string
	To        string
	Postback  string
	Label     string
	At        time.Time
}

type wireNumber struct {
	Number string `json:"number"`
	Name   string `json:"name,omitempty"`
}

func StatusReportJSON(r StatusReport) ([]byte, error) {
	code, err := cmcom.StatusCode(r.Status)
	if err != nil {
		return nil, err
	}
	to, err := cmcom.GatewayNumber(r.Phone)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"messages": map[string]any{
			"msg": map[string]any{
				"received":  r.At.In(cmcom.ReportLocation()).Format(cmcom.ReportTimeLayout),
				"to":        to,
				"reference": r.Reference,
				"status": map[string]string{
					"code":             code,
					"errorCode":        r.ErrorCode,
					"errorDescription": r.ErrorDescription,
				},
				"operator": r.Operator,
			},
		},
	})
}

func InboundJSON(m InboundMessage) ([]byte, error) {
	from, err := cmcom.GatewayNumber(m.Phone)
	if err != nil {
		return nil, err
	}
	channel, to, err := inboundReceiver(m)
	if err != nil {
		return nil, err
	}
	custom := map[string]string{}
	if m.Postback != "" {
		custom["postbackdata"] = m.Postback
	}
	payload := map[string]any{
		"from": wireNumber{Number: from, Name: m.Name},
		"to":   wireNumber{Number: to},
		"message": map[string]any{
			"text":   m.Text,
			"media":  map[string]string{"mediaUri": m.MediaURL, "contentType": m.MediaType, "title": m.MediaTitle},
			"custom": custom,
		},
		"reference": m.Reference,
		"groupings": []string{"", "", ""},
		"timeUtc":   m.At.UTC().Format(cmcom.ReportTimeLayout),
		"channel":   channel,
	}
	if m.InReplyTo != "" {
		payload["messageContext"] = m.InReplyTo
	}
	return json.Marshal(payload)
}

func SuggestionClickJSON(c SuggestionClick) ([]byte, error) {
	from, err := cmcom.GatewayNumber(c.Phone)
	if err != nil {
		return nil, err
	}
	custom := map[string]string{"postbackdata": c.Postback}
	if c.Label != "" {
		custom["label"] = c.Label
	}
	return json.Marshal(map[string]any{
		"reference":      c.EventID,
		"messageContext": c.Reference,
		"from":           wireNumber{Number: from},
		"to":             wireNumber{Number: cmcom.GatewayAgent(c.To)},
		"event":          map[string]any{"type": "SuggestionClick", "custom": custom},
		"groupings":      []string{"", "", ""},
		"timeUtc":        c.At.UTC().Format(cmcom.ReportTimeLayout),
		"channel":        webhookRCS,
	})
}

func inboundReceiver(m InboundMessage) (string, string, error) {
	switch m.Channel {
	case carriermessaging.ChannelRCS:
		return webhookRCS, cmcom.GatewayAgent(m.To), nil
	case carriermessaging.ChannelSMS:
		return webhookSMS, m.To, nil
	}
	return "", "", fmt.Errorf("cmfake: unknown inbound channel %q", m.Channel)
}
