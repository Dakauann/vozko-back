package carriermessaging

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"vozko/domain/working_hours"
)

const (
	DefaultAttributionDays     = 7
	MaxAttributionDays         = 90
	healthMinimumDelivered     = 200
	healthMaxOptOutPerThousand = 20
)

var ErrSettingsInvalid = errors.New("rcs: invalid RCS settings")

type FrequencyCap struct {
	PerDay  int `json:"per_day"`
	PerWeek int `json:"per_week"`
}

func DefaultFrequencyCap() FrequencyCap {
	return FrequencyCap{PerDay: 2, PerWeek: 6}
}

func (c FrequencyCap) Validate() error {
	if c.PerDay < 1 || c.PerWeek < c.PerDay {
		return fmt.Errorf("%w: the weekly cap must be at least the daily cap, and both at least 1", ErrSettingsInvalid)
	}
	return nil
}

func (c FrequencyCap) Allows(sentToday, sentThisWeek int) bool {
	return sentToday < c.PerDay && sentThisWeek < c.PerWeek
}

func DefaultQuietHours(timezone string) working_hours.Spec {
	window := []working_hours.Window{{Start: "08:00", End: "20:00"}}
	days := map[string][]working_hours.Window{}
	for _, day := range []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"} {
		days[day] = window
	}
	return working_hours.Spec{Timezone: timezone, Days: days}
}

var optOutKeywords = map[string]bool{"sair": true, "parar": true, "stop": true, "cancelar": true}

func IsOptOutKeyword(text string) bool {
	folded := strings.Trim(strings.ToLower(strings.TrimSpace(foldAccents(text))), ".!")
	return optOutKeywords[folded]
}

func foldAccents(s string) string {
	out, _, err := transform.String(transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC), s)
	if err != nil {
		return s
	}
	return out
}

func OptOutRateBreached(delivered, optOuts int) bool {
	return delivered >= healthMinimumDelivered && optOuts*1000 > delivered*healthMaxOptOutPerThousand
}

type Settings struct {
	WorkspaceID     string             `json:"-"`
	QuietHours      working_hours.Spec `json:"quiet_hours"`
	FrequencyCap    FrequencyCap       `json:"frequency_cap"`
	AttributionDays int                `json:"attribution_days"`
}

func DefaultSettings(workspaceID, timezone string) Settings {
	return Settings{
		WorkspaceID:     workspaceID,
		QuietHours:      DefaultQuietHours(timezone),
		FrequencyCap:    DefaultFrequencyCap(),
		AttributionDays: DefaultAttributionDays,
	}
}

func (s Settings) Validate() error {
	if err := s.FrequencyCap.Validate(); err != nil {
		return err
	}
	if s.AttributionDays < 1 || s.AttributionDays > MaxAttributionDays {
		return fmt.Errorf("%w: attribution window must be 1 to %d days", ErrSettingsInvalid, MaxAttributionDays)
	}
	spec := s.QuietHours
	if err := spec.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrSettingsInvalid, err)
	}
	return nil
}

const (
	FloodThreshold = 30
	FloodWindow    = time.Hour
)

func IsFlooding(inboundInWindow int) bool {
	return inboundInWindow > FloodThreshold
}
