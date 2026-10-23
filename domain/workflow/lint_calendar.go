package workflow

import (
	"fmt"
	"strings"
	"time"
	"vozko/domain/calendar"
)

func lintCalendarConfig(g *Graph, add func(LintIssue)) {
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Type != NodeTypeActionCheckCalendarAvailability && n.Type != NodeTypeActionCreateCalendarEvent {
			continue
		}
		invalid := func(field string, err error) {
			add(LintIssue{Code: LintInvalidConfigValue, Severity: SeverityBlocking, NodeID: n.ID, Field: field, Message: fmt.Sprintf("nó %q: %s", n.ID, err), Hint: "Use YYYY-MM-DD para consultar um dia, ou data/hora ISO/RFC3339. O fim precisa ser posterior ao início. Eventos precisam incluir a hora."})
		}
		zone := configText(n, "timezone")
		if zone == "" {
			zone = "America/Sao_Paulo"
		}
		if strings.Contains(zone, "{{") {
			continue
		}
		loc, err := time.LoadLocation(zone)
		if err != nil {
			invalid("timezone", err)
			continue
		}
		if n.Type == NodeTypeActionCheckCalendarAvailability {
			from, to := configText(n, "date_from"), configText(n, "date_to")
			if from == "" || strings.Contains(from+to+configText(n, "working_start")+configText(n, "working_end"), "{{") {
				continue
			}
			if _, _, err := calendar.AvailabilityRange(from, to, configText(n, "working_start"), configText(n, "working_end"), loc); err != nil {
				invalid("date_from", err)
			}
		} else {
			var start, end time.Time
			for _, field := range []string{"start_time", "end_time"} {
				raw := configText(n, field)
				if raw == "" || strings.Contains(raw, "{{") {
					continue
				}
				parsed, err := calendar.ParseDateTime(raw, loc)
				if err != nil {
					invalid(field, err)
					continue
				}
				if field == "start_time" {
					start = parsed
				} else {
					end = parsed
				}
			}
			if !start.IsZero() && !end.IsZero() && !end.After(start) {
				invalid("end_time", fmt.Errorf("a data de fim precisa ser depois da data de início"))
			}
		}
	}
}
