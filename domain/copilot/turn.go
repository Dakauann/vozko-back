package copilot

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	TurnEventAssistantDelta = "assistant_delta"
	TurnEventReasoningDelta = "reasoning_delta"
)

var (
	ErrTurnRunning    = errors.New("copilot: this conversation already has an answer running")
	ErrTurnNotRunning = errors.New("copilot: no answer is running in this conversation")
	ErrTurnForbidden  = errors.New("copilot: only the owner of the conversation controls its answer")
)

type TurnEvent struct {
	Type    string
	Payload json.RawMessage
}

type TurnLimits struct {
	MaxEvents        int
	MaxBytes         int
	SubscriberBuffer int
	DetachedGrace    time.Duration
	Retention        time.Duration
}

func DefaultTurnLimits() TurnLimits {
	return TurnLimits{
		MaxEvents:        2000,
		MaxBytes:         4 << 20,
		SubscriberBuffer: 4096,
		DetachedGrace:    60 * time.Second,
		Retention:        30 * time.Second,
	}
}

type turnEntry struct {
	eventType string
	payload   json.RawMessage
	text      *strings.Builder
}

func (e turnEntry) size() int {
	if e.text != nil {
		return e.text.Len()
	}
	return len(e.payload)
}

func (e turnEntry) event() TurnEvent {
	if e.text == nil {
		return TurnEvent{Type: e.eventType, Payload: e.payload}
	}
	payload, _ := json.Marshal(map[string]string{"text": e.text.String()})
	return TurnEvent{Type: e.eventType, Payload: payload}
}

type TurnLog struct {
	limits    TurnLimits
	entries   []turnEntry
	bytes     int
	truncated bool
}

func NewTurnLog(limits TurnLimits) *TurnLog {
	return &TurnLog{limits: limits}
}

func isDelta(eventType string) bool {
	return eventType == TurnEventAssistantDelta || eventType == TurnEventReasoningDelta
}

func deltaText(payload json.RawMessage) string {
	var body struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(payload, &body)
	return body.Text
}

func (l *TurnLog) Append(eventType string, payload json.RawMessage) {
	if isDelta(eventType) {
		text := deltaText(payload)
		if n := len(l.entries); n > 0 && l.entries[n-1].eventType == eventType && l.entries[n-1].text != nil {
			l.entries[n-1].text.WriteString(text)
			l.bytes += len(text)
			l.trim()
			return
		}
		builder := &strings.Builder{}
		builder.WriteString(text)
		l.push(turnEntry{eventType: eventType, text: builder})
		return
	}
	l.push(turnEntry{eventType: eventType, payload: append(json.RawMessage(nil), payload...)})
}

func (l *TurnLog) push(entry turnEntry) {
	l.entries = append(l.entries, entry)
	l.bytes += entry.size()
	l.trim()
}

func (l *TurnLog) trim() {
	for len(l.entries) > 1 && (len(l.entries) > l.limits.MaxEvents || l.bytes > l.limits.MaxBytes) {
		l.bytes -= l.entries[0].size()
		l.entries = l.entries[1:]
		l.truncated = true
	}
}

func (l *TurnLog) Events() []TurnEvent {
	out := make([]TurnEvent, len(l.entries))
	for i, entry := range l.entries {
		out[i] = entry.event()
	}
	return out
}

func (l *TurnLog) Truncated() bool {
	return l.truncated
}
