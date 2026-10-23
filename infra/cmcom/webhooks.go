package cmcom

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"mime"
	"strings"
	"time"
	_ "time/tzdata"

	"vozko/domain/carriermessaging"
)

const (
	ReportTimeLayout   = "2006-01-02T15:04:05"
	reportTimeZone     = "Europe/Amsterdam"
	eventSuggestion    = "SuggestionClick"
	postbackCustomKey  = "postbackdata"
	labelCustomKey     = "label"
	contentTypeXML     = "application/xml"
	contentTypeTextXML = "text/xml"
	contentTypeTextJS  = "text/json"
)

var ErrPayloadInvalid = errors.New("cmcom: invalid webhook payload")

var reportLocation = mustLoadLocation(reportTimeZone)

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func ReportLocation() *time.Location {
	return reportLocation
}

var statusForCode = map[string]carriermessaging.Status{
	"0": carriermessaging.StatusAccepted,
	"1": carriermessaging.StatusRejected,
	"2": carriermessaging.StatusDelivered,
	"3": carriermessaging.StatusFailed,
	"4": carriermessaging.StatusRead,
}

func StatusCode(s carriermessaging.Status) (string, error) {
	for code, status := range statusForCode {
		if status == s {
			return code, nil
		}
	}
	return "", fmt.Errorf("cmcom: status %q has no status report code", s)
}

type statusFields struct {
	Received         string
	Reference        string
	Code             string
	ErrorCode        string
	ErrorDescription string
	Operator         string
}

type jsonStatusEnvelope struct {
	Messages *struct {
		Msg json.RawMessage `json:"msg"`
	} `json:"messages"`
}

type jsonStatusMessage struct {
	Received  *string `json:"received"`
	Reference *string `json:"reference"`
	Status    *struct {
		Code             *string `json:"code"`
		ErrorCode        *string `json:"errorCode"`
		ErrorDescription *string `json:"errorDescription"`
	} `json:"status"`
	Operator *string `json:"operator"`
}

type xmlStatusEnvelope struct {
	XMLName xml.Name `xml:"MESSAGES"`
	Msg     []struct {
		Received  string `xml:"RECEIVED,attr"`
		Reference string `xml:"REFERENCE"`
		Status    struct {
			Code             string `xml:"CODE"`
			ErrorCode        string `xml:"ERRORCODE"`
			ErrorDescription string `xml:"ERRORDESCRIPTION"`
		} `xml:"STATUS"`
		Operator string `xml:"OPERATOR"`
	} `xml:"MSG"`
}

func DecodeStatusReports(contentType string, body []byte) ([]carriermessaging.StatusReport, error) {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, fmt.Errorf("%w: content type %q", ErrPayloadInvalid, contentType)
	}
	var fields []statusFields
	switch mediaType {
	case contentTypeJSON, contentTypeTextJS:
		fields, err = jsonStatusFields(body)
	case contentTypeXML, contentTypeTextXML:
		fields, err = xmlStatusFields(body)
	default:
		return nil, fmt.Errorf("%w: content type %q", ErrPayloadInvalid, contentType)
	}
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("%w: no status report", ErrPayloadInvalid)
	}
	out := make([]carriermessaging.StatusReport, len(fields))
	for i, f := range fields {
		if out[i], err = statusReportFrom(f); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func jsonStatusFields(body []byte) ([]statusFields, error) {
	var envelope jsonStatusEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPayloadInvalid, err)
	}
	if envelope.Messages == nil {
		return nil, fmt.Errorf("%w: no messages object", ErrPayloadInvalid)
	}
	raw := bytes.TrimSpace(envelope.Messages.Msg)
	var messages []jsonStatusMessage
	switch {
	case len(raw) > 0 && raw[0] == '{':
		var one jsonStatusMessage
		if err := json.Unmarshal(raw, &one); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrPayloadInvalid, err)
		}
		messages = []jsonStatusMessage{one}
	case len(raw) > 0 && raw[0] == '[':
		if err := json.Unmarshal(raw, &messages); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrPayloadInvalid, err)
		}
	default:
		return nil, fmt.Errorf("%w: msg must be an object or a list", ErrPayloadInvalid)
	}
	out := make([]statusFields, len(messages))
	for i, m := range messages {
		if m.Status == nil {
			return nil, fmt.Errorf("%w: a status report without status", ErrPayloadInvalid)
		}
		out[i] = statusFields{
			Received:         deref(m.Received),
			Reference:        deref(m.Reference),
			Code:             deref(m.Status.Code),
			ErrorCode:        deref(m.Status.ErrorCode),
			ErrorDescription: deref(m.Status.ErrorDescription),
			Operator:         deref(m.Operator),
		}
	}
	return out, nil
}

func xmlStatusFields(body []byte) ([]statusFields, error) {
	var envelope xmlStatusEnvelope
	if err := xml.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPayloadInvalid, err)
	}
	out := make([]statusFields, len(envelope.Msg))
	for i, m := range envelope.Msg {
		out[i] = statusFields{
			Received:         m.Received,
			Reference:        m.Reference,
			Code:             m.Status.Code,
			ErrorCode:        m.Status.ErrorCode,
			ErrorDescription: m.Status.ErrorDescription,
			Operator:         m.Operator,
		}
	}
	return out, nil
}

func statusReportFrom(f statusFields) (carriermessaging.StatusReport, error) {
	reference := strings.TrimSpace(f.Reference)
	if reference == "" {
		return carriermessaging.StatusReport{}, fmt.Errorf("%w: a status report without reference", ErrPayloadInvalid)
	}
	status, ok := statusForCode[strings.TrimSpace(f.Code)]
	if !ok {
		return carriermessaging.StatusReport{}, fmt.Errorf("%w: unknown status code %q", ErrPayloadInvalid, f.Code)
	}
	errorCode := strings.TrimSpace(f.ErrorCode)
	if !digitsOnly(errorCode) {
		return carriermessaging.StatusReport{}, fmt.Errorf("%w: unknown error code %q", ErrPayloadInvalid, f.ErrorCode)
	}
	received, err := time.ParseInLocation(ReportTimeLayout, strings.TrimSpace(f.Received), reportLocation)
	if err != nil {
		return carriermessaging.StatusReport{}, fmt.Errorf("%w: received %q", ErrPayloadInvalid, f.Received)
	}
	return carriermessaging.StatusReport{
		Reference:   reference,
		Status:      status,
		ErrorCode:   errorCode,
		ErrorDetail: strings.TrimSpace(f.ErrorDescription),
		Operator:    strings.TrimSpace(f.Operator),
		Unreachable: errorCode == UnreachableErrorCode,
		ReceivedAt:  received,
	}, nil
}

func digitsOnly(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

type wireParty struct {
	Number string `json:"number"`
	Name   string `json:"name"`
}

type wireInbound struct {
	From           wireParty `json:"from"`
	To             wireParty `json:"to"`
	Reference      string    `json:"reference"`
	MessageContext string    `json:"messageContext"`
	TimeUTC        string    `json:"timeUtc"`
	Channel        string    `json:"channel"`
	Message        struct {
		Text  string `json:"text"`
		Media *struct {
			MediaURI    string `json:"mediaUri"`
			ContentType string `json:"contentType"`
			Title       string `json:"title"`
		} `json:"media"`
		Custom map[string]json.RawMessage `json:"custom"`
	} `json:"message"`
}

type wireEvent struct {
	From           wireParty `json:"from"`
	To             wireParty `json:"to"`
	Reference      string    `json:"reference"`
	MessageContext string    `json:"messageContext"`
	TimeUTC        string    `json:"timeUtc"`
	Channel        string    `json:"channel"`
	Event          *struct {
		Type   string                     `json:"type"`
		Custom map[string]json.RawMessage `json:"custom"`
	} `json:"event"`
}

type webhookEnvelope struct {
	channel    carriermessaging.Channel
	phone      string
	to         string
	reference  string
	occurredAt time.Time
}

var webhookChannels = map[string]carriermessaging.Channel{
	"RCS": carriermessaging.ChannelRCS,
	"SMS": carriermessaging.ChannelSMS,
}

func receiverFor(channel carriermessaging.Channel, raw string) (string, error) {
	if channel == carriermessaging.ChannelSMS {
		number, err := routeFromGateway(raw)
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrPayloadInvalid, err)
		}
		return number, nil
	}
	agent := agentFromGateway(raw)
	if agent == "" {
		return "", fmt.Errorf("%w: no agent in to.number", ErrPayloadInvalid)
	}
	return agent, nil
}

func decodeEnvelope(from, to wireParty, reference, timeUTC, channel string) (webhookEnvelope, error) {
	ch, ok := webhookChannels[strings.ToUpper(strings.TrimSpace(channel))]
	if !ok {
		return webhookEnvelope{}, fmt.Errorf("%w: channel %q", ErrPayloadInvalid, channel)
	}
	phone, err := PhoneFromGateway(from.Number)
	if err != nil {
		return webhookEnvelope{}, fmt.Errorf("%w: %w", ErrPayloadInvalid, err)
	}
	receiver, err := receiverFor(ch, to.Number)
	if err != nil {
		return webhookEnvelope{}, err
	}
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return webhookEnvelope{}, fmt.Errorf("%w: no reference", ErrPayloadInvalid)
	}
	at, err := time.ParseInLocation(ReportTimeLayout, strings.TrimSpace(timeUTC), time.UTC)
	if err != nil {
		return webhookEnvelope{}, fmt.Errorf("%w: timeUtc %q", ErrPayloadInvalid, timeUTC)
	}
	return webhookEnvelope{channel: ch, phone: phone, to: receiver, reference: reference, occurredAt: at}, nil
}

func customString(custom map[string]json.RawMessage, key string) (string, error) {
	raw, ok := custom[key]
	if !ok || string(raw) == "null" {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%w: custom.%s must be text", ErrPayloadInvalid, key)
	}
	return value, nil
}

func DecodeInbound(body []byte) (carriermessaging.Inbound, error) {
	var in wireInbound
	if err := json.Unmarshal(body, &in); err != nil {
		return carriermessaging.Inbound{}, fmt.Errorf("%w: %v", ErrPayloadInvalid, err)
	}
	env, err := decodeEnvelope(in.From, in.To, in.Reference, in.TimeUTC, in.Channel)
	if err != nil {
		return carriermessaging.Inbound{}, err
	}
	postback, err := customString(in.Message.Custom, postbackCustomKey)
	if err != nil {
		return carriermessaging.Inbound{}, err
	}
	out := carriermessaging.Inbound{
		ProviderMessageID: env.reference,
		Phone:             env.phone,
		ContactName:       strings.TrimSpace(in.From.Name),
		Channel:           env.channel,
		To:                env.to,
		Text:              in.Message.Text,
		InReplyTo:         strings.TrimSpace(in.MessageContext),
		Postback:          postback,
		ReceivedAt:        env.occurredAt,
	}
	if m := in.Message.Media; m != nil && strings.TrimSpace(m.MediaURI) != "" {
		if strings.TrimSpace(m.ContentType) == "" {
			return carriermessaging.Inbound{}, fmt.Errorf("%w: media without a content type", ErrPayloadInvalid)
		}
		out.Media = &carriermessaging.InboundMedia{URL: strings.TrimSpace(m.MediaURI), MIMEType: strings.TrimSpace(m.ContentType), Name: m.Title}
	}
	if strings.TrimSpace(out.Text) == "" && out.Media == nil && out.Postback == "" {
		return carriermessaging.Inbound{}, fmt.Errorf("%w: the message carries no text, media or postback", ErrPayloadInvalid)
	}
	return out, nil
}

func DecodeEvent(body []byte) (carriermessaging.SuggestionClick, bool, error) {
	var ev wireEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return carriermessaging.SuggestionClick{}, false, fmt.Errorf("%w: %v", ErrPayloadInvalid, err)
	}
	if ev.Event == nil || strings.TrimSpace(ev.Event.Type) == "" {
		return carriermessaging.SuggestionClick{}, false, fmt.Errorf("%w: no event type", ErrPayloadInvalid)
	}
	env, err := decodeEnvelope(ev.From, ev.To, ev.Reference, ev.TimeUTC, ev.Channel)
	if err != nil {
		return carriermessaging.SuggestionClick{}, false, err
	}
	if env.channel != carriermessaging.ChannelRCS {
		return carriermessaging.SuggestionClick{}, false, fmt.Errorf("%w: events only come from RCS, got %s", ErrPayloadInvalid, env.channel)
	}
	if ev.Event.Type != eventSuggestion {
		return carriermessaging.SuggestionClick{}, false, nil
	}
	messageContext := strings.TrimSpace(ev.MessageContext)
	if messageContext == "" {
		return carriermessaging.SuggestionClick{}, false, fmt.Errorf("%w: a suggestion click without messageContext", ErrPayloadInvalid)
	}
	postback, err := customString(ev.Event.Custom, postbackCustomKey)
	if err != nil {
		return carriermessaging.SuggestionClick{}, false, err
	}
	if strings.TrimSpace(postback) == "" {
		return carriermessaging.SuggestionClick{}, false, fmt.Errorf("%w: a suggestion click without postbackdata", ErrPayloadInvalid)
	}
	label, err := customString(ev.Event.Custom, labelCustomKey)
	if err != nil {
		return carriermessaging.SuggestionClick{}, false, err
	}
	return carriermessaging.SuggestionClick{
		EventID:    env.reference,
		Reference:  messageContext,
		Phone:      env.phone,
		To:         env.to,
		Postback:   postback,
		Label:      label,
		OccurredAt: env.occurredAt,
	}, true, nil
}
