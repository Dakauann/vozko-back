package workflow

import (
	"fmt"
	"strings"
	"time"

	"vozko/domain/calendar"
)

const (
	calendarDefaultTimezone = "America/Sao_Paulo"
	calendarDateHint        = "Use YYYY-MM-DD para consultar um dia, ou data/hora ISO/RFC3339. O fim precisa ser posterior ao início. Eventos precisam incluir a hora."
)

func lintCalendarConfig(g *Graph, add func(LintIssue)) {
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Type != NodeTypeActionCheckCalendarAvailability && n.Type != NodeTypeActionCreateCalendarEvent {
			continue
		}
		invalid := func(field string, err error) {
			add(LintIssue{Code: LintInvalidConfigValue, Severity: SeverityBlocking, NodeID: n.ID, Field: field, Message: fmt.Sprintf("nó %q: %s", n.ID, err), Hint: calendarDateHint})
		}
		zone := configText(n, "timezone")
		if zone == "" {
			zone = calendarDefaultTimezone
		}
		if isTemplated(zone) {
			continue
		}
		loc, err := time.LoadLocation(zone)
		if err != nil {
			invalid("timezone", err)
			continue
		}
		if n.Type == NodeTypeActionCheckCalendarAvailability {
			lintAvailabilityRange(n, loc, invalid)
		} else {
			lintEventTimes(n, loc, invalid)
		}
	}
}

func lintAvailabilityRange(n *Node, loc *time.Location, invalid func(string, error)) {
	from, to := configText(n, "date_from"), configText(n, "date_to")
	if from == "" || isTemplated(from) || isTemplated(to) {
		return
	}
	start := calendar.ParseClock(configText(n, "working_start"), calendar.DefaultWorkStart)
	end := calendar.ParseClock(configText(n, "working_end"), calendar.DefaultWorkEnd)
	if _, _, err := calendar.AvailabilityRange(from, to, start, end, loc); err != nil {
		invalid("date_from", err)
	}
}

func lintEventTimes(n *Node, loc *time.Location, invalid func(string, error)) {
	parsed := map[string]time.Time{}
	for _, field := range []string{"start_time", "end_time"} {
		raw := configText(n, field)
		if raw == "" || isTemplated(raw) {
			continue
		}
		at, err := calendar.ParseDateTime(raw, loc)
		if err != nil {
			invalid(field, err)
			continue
		}
		parsed[field] = at
	}
	start, hasStart := parsed["start_time"]
	end, hasEnd := parsed["end_time"]
	if hasStart && hasEnd && !end.After(start) {
		invalid("end_time", calendar.ErrRangeNotForward)
	}
}

func isTemplated(value string) bool {
	return strings.Contains(value, "{{")
}
