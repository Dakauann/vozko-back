package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
	"vozko/domain/shared"
	"vozko/domain/sms"
)

const (
	CarrierChannelKey   = "channel"
	CarrierSenderKey    = "sender_id"
	CarrierRouteKey     = "route_id"
	CarrierContentKey   = "content"
	CarrierTextKey      = "text"
	CarrierMarketingKey = "marketing"

	CarrierHandleSent   = "sent"
	CarrierHandleFailed = "failed"
)

const (
	runtimeResolvedText = "x"
	runtimeResolvedURL  = "https://runtime.invalid"
)

var (
	ErrCarrierSendUnreadable = errors.New("workflow: the RCS or SMS node config cannot be read")
	ErrCarrierSendInvalid    = errors.New("workflow: the RCS or SMS message is not valid")
	errCarrierPositionalSlot = errors.New("numbered variables like {{1}} are filled only by campaigns, use workflow variables such as {{lead_name}}")
)

type CarrierSend struct {
	Channel   carriermessaging.Channel
	SenderID  string
	Content   rcs.Content
	Text      string
	Marketing bool
}

func ReadCarrierSend(config map[string]interface{}) (CarrierSend, error) {
	channel, _ := config[CarrierChannelKey].(string)
	send := CarrierSend{Channel: carriermessaging.Channel(strings.TrimSpace(channel)), Marketing: true}
	if !send.Channel.Valid() {
		return CarrierSend{}, fmt.Errorf("%w: unknown channel", ErrCarrierSendUnreadable)
	}
	senderKey := CarrierSenderKey
	if send.Channel == carriermessaging.ChannelSMS {
		senderKey = CarrierRouteKey
	}
	var ok bool
	if send.SenderID, ok = optionalText(config, senderKey); !ok {
		return CarrierSend{}, fmt.Errorf("%w: %s is not text", ErrCarrierSendUnreadable, senderKey)
	}
	if send.Text, ok = optionalText(config, CarrierTextKey); !ok {
		return CarrierSend{}, fmt.Errorf("%w: text is not text", ErrCarrierSendUnreadable)
	}
	if raw, present := config[CarrierMarketingKey]; present && raw != nil {
		if send.Marketing, ok = raw.(bool); !ok {
			return CarrierSend{}, fmt.Errorf("%w: marketing is not a switch", ErrCarrierSendUnreadable)
		}
	}
	content, err := readRCSContent(config[CarrierContentKey])
	if err != nil {
		return CarrierSend{}, err
	}
	send.Content = content
	send.SenderID = strings.TrimSpace(send.SenderID)
	return send, nil
}

func optionalText(config map[string]interface{}, key string) (string, bool) {
	raw, present := config[key]
	if !present || raw == nil {
		return "", true
	}
	text, ok := raw.(string)
	return text, ok
}

func readRCSContent(raw interface{}) (rcs.Content, error) {
	if raw == nil {
		return rcs.Content{}, nil
	}
	if _, isObject := raw.(map[string]interface{}); !isObject {
		return rcs.Content{}, fmt.Errorf("%w: content is not an object", ErrCarrierSendUnreadable)
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return rcs.Content{}, fmt.Errorf("%w: content: %v", ErrCarrierSendUnreadable, err)
	}
	var content rcs.Content
	if err := json.Unmarshal(encoded, &content); err != nil {
		return rcs.Content{}, fmt.Errorf("%w: content: %v", ErrCarrierSendUnreadable, err)
	}
	return content, nil
}

func (s CarrierSend) Render(fill func(string) string) CarrierSend {
	return s.renderWith(fill, fill)
}

func (s CarrierSend) renderWith(text, link func(string) string) CarrierSend {
	out := s
	out.Text = text(s.Text)
	out.Content = rcs.Content{Text: text(s.Content.Text), Media: s.Content.Media, Suggestions: renderCarrierSuggestions(s.Content.Suggestions, text, link)}
	if len(s.Content.Cards) > 0 {
		out.Content.Cards = make([]rcs.Card, len(s.Content.Cards))
		for i, card := range s.Content.Cards {
			out.Content.Cards[i] = rcs.Card{
				Media:       card.Media,
				Title:       text(card.Title),
				Description: text(card.Description),
				Suggestions: renderCarrierSuggestions(card.Suggestions, text, link),
			}
		}
	}
	return out
}

func renderCarrierSuggestions(suggestions []rcs.Suggestion, text, link func(string) string) []rcs.Suggestion {
	if len(suggestions) == 0 {
		return nil
	}
	out := make([]rcs.Suggestion, len(suggestions))
	for i, s := range suggestions {
		s.Label = text(s.Label)
		s.URL = link(s.URL)
		out[i] = s
	}
	return out
}

func (s CarrierSend) staticShape() CarrierSend {
	return s.renderWith(staticText, staticURL)
}

func staticText(raw string) string {
	stripped := interpolationPattern.ReplaceAllString(raw, "")
	if stripped != raw && strings.TrimSpace(stripped) == "" {
		return runtimeResolvedText
	}
	return stripped
}

func staticURL(raw string) string {
	if !interpolationPattern.MatchString(raw) {
		return raw
	}
	stripped := strings.TrimSpace(interpolationPattern.ReplaceAllString(raw, ""))
	if stripped == "" {
		return runtimeResolvedURL
	}
	return stripped
}

func (s CarrierSend) messageProblem() (string, error) {
	shape := s.staticShape()
	if s.Channel == carriermessaging.ChannelSMS {
		if strings.TrimSpace(s.Text) == "" {
			return "", nil
		}
		if shared.HighestPositionalParameter([]string{s.Text}) > 0 {
			return CarrierTextKey, errCarrierPositionalSlot
		}
		return CarrierTextKey, sms.Validate(shape.Text)
	}
	if s.Content.VariableSlots() > 0 {
		return CarrierContentKey, errCarrierPositionalSlot
	}
	return CarrierContentKey, shape.Content.Validate()
}

func ValidateCarrierSends(g *Graph) error {
	return validateCarrierSends(g, carriermessaging.Channel.Launched)
}

func validateCarrierSends(g *Graph, launched func(carriermessaging.Channel) bool) error {
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Type != NodeTypeActionSendCarrier {
			continue
		}
		send, err := ReadCarrierSend(n.Config)
		if err != nil {
			return carrierProblem(n.ID, CarrierChannelKey, err)
		}
		if !launched(send.Channel) {
			return carrierProblem(n.ID, CarrierChannelKey, carriermessaging.ErrChannelNotLaunched)
		}
		if field, err := send.messageProblem(); err != nil {
			return carrierProblem(n.ID, field, err)
		}
	}
	return nil
}

func carrierProblem(nodeID, field string, cause error) error {
	return fmt.Errorf("%w: %s: node %q field %q", ErrCarrierSendInvalid, strings.ReplaceAll(cause.Error(), `"`, "'"), nodeID, field)
}
