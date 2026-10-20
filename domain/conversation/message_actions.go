package conversation

import (
	"errors"
	"strings"
	"time"
)

type MessageAction string

const (
	MessageActionReact  MessageAction = "react"
	MessageActionEdit   MessageAction = "edit"
	MessageActionDelete MessageAction = "delete"
)

var (
	ErrMessageActionUnsupported   = errors.New("conversation: the channel does not support this message action")
	ErrMessageActionNotAllowed    = errors.New("conversation: this message action is not allowed on this message")
	ErrMessageActionWindowExpired = errors.New("conversation: the channel's window for this message action has passed")
)

type MessageCapabilities struct {
	React               bool  `json:"react"`
	Edit                bool  `json:"edit"`
	Delete              bool  `json:"delete"`
	EditWindowSeconds   int64 `json:"edit_window_seconds"`
	DeleteWindowSeconds int64 `json:"delete_window_seconds"`
}

type MessageActions struct {
	React  bool `json:"react"`
	Edit   bool `json:"edit"`
	Delete bool `json:"delete"`
	Resend bool `json:"resend"`
}

func MessageCapabilitiesOf(channel MessageActionChannel) MessageCapabilities {
	var caps MessageCapabilities
	if channel == nil {
		return caps
	}
	if _, ok := channel.(ReactingAdapter); ok {
		caps.React = true
	}
	if editing, ok := channel.(EditingAdapter); ok {
		caps.EditWindowSeconds = windowSeconds(editing.EditWindow())
		caps.Edit = caps.EditWindowSeconds > 0
	}
	if retracting, ok := channel.(RetractingAdapter); ok {
		caps.DeleteWindowSeconds = windowSeconds(retracting.RetractWindow())
		caps.Delete = caps.DeleteWindowSeconds > 0
	}
	return caps
}

func windowSeconds(window time.Duration) int64 {
	if window <= 0 {
		return 0
	}
	return int64(window / time.Second)
}

func AllowedActions(m *Message, caps MessageCapabilities, operatorID string, now time.Time) MessageActions {
	return MessageActions{
		React:  CheckMessageAction(MessageActionReact, m, caps, operatorID, now) == nil,
		Edit:   CheckMessageAction(MessageActionEdit, m, caps, operatorID, now) == nil,
		Delete: CheckMessageAction(MessageActionDelete, m, caps, operatorID, now) == nil,
		Resend: m.resendableBy(operatorID),
	}
}

func CheckMessageAction(action MessageAction, m *Message, caps MessageCapabilities, operatorID string, now time.Time) error {
	switch action {
	case MessageActionReact:
		return checkReact(m, caps)
	case MessageActionEdit:
		return checkOwnChange(m, caps.Edit, caps.EditWindowSeconds, operatorID, now, m.isPlainText)
	case MessageActionDelete:
		return checkOwnChange(m, caps.Delete, caps.DeleteWindowSeconds, operatorID, now, func() bool { return true })
	}
	return ErrMessageActionUnsupported
}

func checkReact(m *Message, caps MessageCapabilities) error {
	if !caps.React {
		return ErrMessageActionUnsupported
	}
	if !m.actionable() {
		return ErrMessageActionNotAllowed
	}
	return nil
}

func checkOwnChange(m *Message, supported bool, window int64, operatorID string, now time.Time, shapeAllows func() bool) error {
	if !supported || window <= 0 {
		return ErrMessageActionUnsupported
	}
	if !m.actionable() || !m.sentByOperator(operatorID) || !shapeAllows() {
		return ErrMessageActionNotAllowed
	}
	if m.CreatedAt.IsZero() || now.Sub(m.CreatedAt) > time.Duration(window)*time.Second {
		return ErrMessageActionWindowExpired
	}
	return nil
}

func (m *Message) actionable() bool {
	return m != nil && !m.IsRetracted() && m.ProviderMessageID() != ""
}

func (m *Message) sentByOperator(operatorID string) bool {
	operator := strings.TrimSpace(operatorID)
	return operator != "" && m.ResolvedDirection().IsOutbound() && m.SenderUserID() == operator
}

func (m *Message) isPlainText() bool {
	return m.MessageType == MessageTypeOperator &&
		strings.TrimSpace(m.Text) != "" &&
		!m.HasMedia() && m.MediaType == "" && len(m.Image) == 0 && len(m.Video) == 0 &&
		!m.HasButtons()
}
