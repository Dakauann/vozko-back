package quality

import (
	"errors"
	"strings"
	"time"
)

type Subject string

const (
	SubjectTemplate Subject = "template"
	SubjectPhone    Subject = "phone"
)

type Signal string

const (
	SignalQuality        Signal = "quality"
	SignalStatus         Signal = "status"
	SignalCategory       Signal = "category"
	SignalMessagingLimit Signal = "messaging_limit"
)

type Source string

const (
	SourceWebhook Source = "webhook"
	SourcePoll    Source = "poll"
)

const PhonePollWindow = 72 * time.Hour

var ErrInvalidChange = errors.New("quality change needs a subject, an external id, a signal, a source, a current value and when it happened")

type Change struct {
	Subject    Subject
	ExternalID string
	WABAID     string
	Name       string
	Signal     Signal
	Event      string
	Previous   string
	Current    string
	Source     Source
	OccurredAt time.Time
}

func (c Change) Validate() error {
	if c.Subject == "" || strings.TrimSpace(c.ExternalID) == "" || c.Signal == "" || c.Source == "" ||
		strings.TrimSpace(c.Current) == "" || c.OccurredAt.IsZero() {
		return ErrInvalidChange
	}
	return nil
}

func (c Change) Moved(previous, current string) (Change, bool) {
	previous, current = strings.TrimSpace(previous), strings.TrimSpace(current)
	if current == "" || previous == current {
		return Change{}, false
	}
	c.Previous, c.Current = previous, current
	return c, true
}

func OccurredAt(unixSeconds int64, fallback time.Time) time.Time {
	if unixSeconds <= 0 {
		return fallback.UTC()
	}
	return time.Unix(unixSeconds, 0).UTC()
}

type Recorder interface {
	Record(changes ...Change) error
}

type History interface {
	LatestValues(subject Subject, signal Signal, externalIDs []string) (map[string]string, error)
}

type Timeline interface {
	LatestBySignal(subject Subject, externalID string) (map[Signal]Change, error)
}

type ChangeLog interface {
	Recent(subject Subject, externalID string, limit int) ([]Change, error)
}

type Reader interface {
	Timeline
	ChangeLog
}
