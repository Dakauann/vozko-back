package crmfilter

import (
	"strings"

	"github.com/lib/pq"

	"vozko/domain/crmfilter"
	"vozko/domain/shared"
)

type taggedArm struct {
	entryType shared.EntryType
	join      string
	lead      string
	live      string
}

func taggedArms() []taggedArm {
	arms := []taggedArm{{shared.EntryTypeWhatsApp, "whatsapp_campaign_entries wce_e ON wce_e.id = tg.entry_id", "wce_e.lead_id", "wce_e.deleted_at IS NULL"}}
	for _, c := range contactChannels {
		conv, contact := c.conversationAlias("e"), c.contactAlias("e")
		arms = append(arms, taggedArm{
			entryType: c.entryType,
			join:      c.conversations + " " + conv + " ON " + conv + ".id = tg.entry_id JOIN " + c.contacts + " " + contact + " ON " + contact + ".id = " + conv + "." + c.contactColumn,
			lead:      contact + ".lead_id",
			live:      contact + ".lead_id IS NOT NULL AND " + c.liveRows("e"),
		})
	}
	return arms
}

func (d LeadDescriptor) taggedField(table, column string) FieldMapping {
	return FieldMapping{Style: StyleCompiled, Kind: crmfilter.KindIDSet, Compile: func(p crmfilter.Predicate) (string, []interface{}, error) {
		return d.compileTagged(table, column, p)
	}}
}

func (d LeadDescriptor) compileTagged(table, column string, p crmfilter.Predicate) (string, []interface{}, error) {
	scope, scopeArgs := d.tagScope()
	match, matchArgs, negated := "", []interface{}{}, false
	switch p.Operator {
	case crmfilter.OpIn, crmfilter.OpEquals, crmfilter.OpNotIn, crmfilter.OpNotEquals:
		match, matchArgs = " AND tg."+column+" = ANY(?)", []interface{}{pq.Array(trimmedValues(p.Values))}
		negated = p.Operator == crmfilter.OpNotIn || p.Operator == crmfilter.OpNotEquals
	case crmfilter.OpIsSet, crmfilter.OpIsEmpty:
		negated = p.Operator == crmfilter.OpIsEmpty
	default:
		return "", nil, unsupported(p)
	}
	arms := taggedArms()
	selects := make([]string, 0, len(arms))
	var args []interface{}
	for _, arm := range arms {
		selects = append(selects, "SELECT "+arm.lead+" FROM "+table+" tg JOIN "+arm.join+
			" WHERE tg.entry_type = '"+string(arm.entryType)+"'"+match+" AND "+scope("tg")+" AND "+arm.live)
		args = append(append(args, matchArgs...), scopeArgs...)
	}
	op := " IN ("
	if negated {
		op = " NOT IN ("
	}
	return ComputedOnce(d.id() + op + strings.Join(selects, " UNION ALL ") + ")"), args, nil
}
