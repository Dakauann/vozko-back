package crmfilter

import (
	"fmt"
	"strings"

	"github.com/lib/pq"

	"vozko/domain/crmfilter"
	"vozko/domain/lead"
)

func (d LeadDescriptor) compileOptOutSource(p crmfilter.Predicate) (string, []interface{}, error) {
	sources, triggers, err := optOutOriginColumns(p.Values)
	if err != nil {
		return "", nil, err
	}
	record := d.alias() + ".whatsapp_consent_record"
	var clauses []string
	args := []interface{}{string(lead.ConsentWithdraw)}
	if len(sources) > 0 {
		clauses = append(clauses, "("+record+" ->> 'source') = ANY(?)")
		args = append(args, pq.Array(sources))
	}
	if len(triggers) > 0 {
		ws, err := d.workspace()
		if err != nil {
			return "", nil, err
		}
		clauses = append(clauses, "("+record+" ->> 'eventId') IN (SELECT ler_f.consent_event_id FROM lead_exit_runs ler_f"+
			" WHERE ler_f.workspace_id = ? AND ler_f.exit_trigger = ANY(?) AND ler_f.consent_event_id <> '')")
		args = append(args, ws, pq.Array(triggers))
	}
	match := "(" + d.alias() + ".opted_out_at IS NOT NULL AND (" + record + " ->> 'kind') = ? AND (" + strings.Join(clauses, " OR ") + "))"
	switch p.Operator {
	case crmfilter.OpEquals, crmfilter.OpIn:
		return match, args, nil
	case crmfilter.OpNotEquals, crmfilter.OpNotIn:
		return negate(match), args, nil
	}
	return "", nil, unsupported(p)
}

func optOutOriginColumns(values []string) (sources, triggers []string, err error) {
	for _, v := range trimmedValues(values) {
		origin := lead.OptOutOrigin(v)
		if !origin.Valid() {
			return nil, nil, fmt.Errorf("%w: %q is not a way a lead leaves", crmfilter.ErrInvalidValue, v)
		}
		if trigger, word := origin.WordTrigger(); word {
			triggers = append(triggers, trigger)
			continue
		}
		sources = append(sources, string(origin.Source()))
	}
	if len(sources) == 0 && len(triggers) == 0 {
		return nil, nil, crmfilter.ErrMissingValue
	}
	return sources, triggers, nil
}
