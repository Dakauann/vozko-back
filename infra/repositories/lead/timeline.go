package lead

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	"vozko/domain/actor"
	"vozko/domain/calls/calllist"
	"vozko/domain/calls/cdr"
	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/infra/database"
	infracrmfilter "vozko/infra/repositories/crmfilter"
	opportunity_repository "vozko/infra/repositories/opportunity"
)

var errTimelineScanInvalid = errors.New("lead timeline: a scan needs a workspace, a lead and a page size")

const timelineColumns = "kind, at, item_id, ref_type, ref_id, entry_type, actor_id, actor_kind, summary, call_direction, call_agent_id, call_answered_at"

type callFacts struct {
	direction string
	agent     string
	answered  string
}

var noCallFacts = callFacts{direction: "NULL::text", agent: "NULL::text", answered: "NULL::timestamptz"}

type timelineBranch struct {
	selects  string
	from     string
	where    string
	args     []interface{}
	at       string
	item     string
	keysetIn bool
}

func quoted(value string) string {
	return "'" + value + "'"
}

func (b timelineBranch) bounded(before *shared.Keyset, limit int) (string, []interface{}) {
	item := "(" + b.item + `) COLLATE "C"`
	where, args := b.where, append([]interface{}{}, b.args...)
	if before != nil && !b.keysetIn {
		where += " AND (" + b.at + ", " + item + ") < (?, ?)"
		args = append(args, before.At, before.ID)
	}
	args = append(args, limit)
	return "SELECT " + b.selects + " FROM " + b.from + " WHERE " + where +
		" ORDER BY " + b.at + " DESC, " + item + " DESC LIMIT ?", args
}

func (b timelineBranch) sql(before *shared.Keyset, limit int) (string, []interface{}) {
	sql, args := b.bounded(before, limit)
	return "(" + sql + ")", args
}

func timelineSelect(kind, at, item, refType, refID, entryType, actorID, actorKind, summary string, facts callFacts) string {
	return kind + " AS kind, " + at + " AS at, (" + item + `) COLLATE "C" AS item_id, ` + refType + " AS ref_type, " + refID + " AS ref_id, " +
		entryType + " AS entry_type, " + actorID + " AS actor_id, " + actorKind + " AS actor_kind, " + summary + " AS summary, " +
		facts.direction + " AS call_direction, " + facts.agent + " AS call_agent_id, " + facts.answered + " AS call_answered_at"
}

const firstMessage = "CROSS JOIN LATERAL (SELECT tcm.created_at AS started_at FROM conversation_messages tcm" +
	" WHERE tcm.entry_id = lead_entries.entry_id AND tcm.entry_type = lead_entries.entry_type AND tcm.deleted_at IS NULL AND "

func conversationBranch(scan lead.TimelineScan) timelineBranch {
	at := "tfirst.started_at"
	item := quoted(string(lead.TimelineConversation)+":") + " || lead_entries.entry_type || ':' || lead_entries.entry_id::text"
	return timelineBranch{
		selects: timelineSelect(quoted(string(lead.TimelineConversation)), at, item, quoted(string(lead.TimelineRefEntry)),
			"lead_entries.entry_id::text", "lead_entries.entry_type::text", "''", "''",
			"jsonb_build_object('channel', lead_entries.entry_type)", noCallFacts),
		from: infracrmfilter.LeadEntriesSource() + " " + firstMessage + database.NotSeedPlaceholderSQL("tcm") +
			" ORDER BY tcm.created_at LIMIT 1) tfirst",
		where: "lead_entries.lead_id = ?",
		args:  []interface{}{scan.LeadID},
		at:    at,
		item:  item,
	}
}

func milestones(alias string) string {
	stamps := []struct {
		kind   lead.TimelineKind
		column string
	}{
		{lead.TimelineCampaignSent, "sent_at"},
		{lead.TimelineCampaignDelivered, "delivered_at"},
		{lead.TimelineCampaignRead, "read_at"},
		{lead.TimelineCampaignFailed, "failed_at"},
	}
	rows := make([]string, 0, len(stamps))
	for _, s := range stamps {
		rows = append(rows, "("+quoted(string(s.kind))+", "+alias+"."+s.column+")")
	}
	return "CROSS JOIN LATERAL (VALUES " + strings.Join(rows, ", ") + ") AS " + alias + "m(kind, at)"
}

func campaignSummary(entry, campaign string, channel shared.EntryType) string {
	return "jsonb_build_object('channel', " + quoted(string(channel)) + ", 'campaignId', " + entry + ".campaign_id::text, 'title', COALESCE(" +
		campaign + ".name, ''), 'status', " + entry + ".status)"
}

func officialMilestonesBranch(scan lead.TimelineScan) timelineBranch {
	item := "twm.kind || ':' || tw.id::text"
	return timelineBranch{
		selects: timelineSelect("twm.kind", "twm.at", item, quoted(string(lead.TimelineRefEntry)), "tw.id::text",
			quoted(string(shared.EntryTypeWhatsApp)), "''", "''", campaignSummary("tw", "twc", shared.EntryTypeWhatsApp), noCallFacts),
		from:  "whatsapp_campaign_entries tw LEFT JOIN whatsapp_campaigns twc ON twc.id = tw.campaign_id " + milestones("tw"),
		where: "tw.lead_id = ? AND tw.deleted_at IS NULL AND twm.at IS NOT NULL",
		args:  []interface{}{scan.LeadID},
		at:    "twm.at",
		item:  item,
	}
}

func unofficialMilestonesBranch(scan lead.TimelineScan) timelineBranch {
	item := "tum.kind || ':' || tu.id::text"
	return timelineBranch{
		selects: timelineSelect("tum.kind", "tum.at", item, quoted(string(lead.TimelineRefEntry)), "COALESCE(tu.conversation_id::text, '')",
			quoted(string(shared.EntryTypeUnofficialWhatsApp)), "''", "''", campaignSummary("tu", "tuc", shared.EntryTypeUnofficialWhatsApp), noCallFacts),
		from:  "unofficial_whatsapp_campaign_entries tu LEFT JOIN unofficial_whatsapp_campaigns tuc ON tuc.id = tu.campaign_id " + milestones("tu"),
		where: "tu.lead_id = ? AND tu.workspace_id = ? AND tu.deleted_at IS NULL AND tum.at IS NOT NULL",
		args:  []interface{}{scan.LeadID, scan.WorkspaceID},
		at:    "tum.at",
		item:  item,
	}
}

const callItemPrefix = "'call:' || tcl.id::text"

const callSummary = "'direction', tcl.direction, 'status', tcl.status, 'durationSec', tcl.duration_sec, 'answeredAt', tcl.answered_at, 'source', tcl.source"

const stampedItemOutcome = " LEFT JOIN LATERAL (SELECT tci.disposition, tci.refusal, tci.callback_at, tci.list_id," +
	" (SELECT tlst.assignee_ids FROM call_lists tlst WHERE tlst.id = tci.list_id AND tlst.workspace_id = tci.workspace_id) AS assignee_ids" +
	" FROM call_list_items tci WHERE tci.workspace_id = tcl.workspace_id AND tci.lead_id = tcl.lead_id AND tci.last_call_id = tcl.id" +
	" ORDER BY tci.updated_at DESC LIMIT 1) tco ON TRUE"

const stampedItemSummary = ", 'disposition', tco.disposition, 'refusal', tco.refusal, 'callbackAt', tco.callback_at, 'callListId', tco.list_id::text, 'callListAssignees', tco.assignee_ids"

func callsBounded(where string, args []interface{}, before *shared.Keyset, limit int) (string, []interface{}) {
	return timelineBranch{selects: "tcl.*", from: "calls tcl", where: where, args: args, at: "tcl.started_at", item: callItemPrefix}.bounded(before, limit)
}

func callBranch(from, summary string, args []interface{}) timelineBranch {
	return timelineBranch{
		selects: timelineSelect(quoted(string(lead.TimelineCall)), "tcl.started_at", callItemPrefix, quoted(string(lead.TimelineRefCall)), "tcl.call_id",
			"''", "COALESCE(tcl.agent_id::text, '')", "CASE WHEN tcl.agent_id IS NULL THEN '' ELSE "+quoted(string(actor.KindHuman))+" END",
			"jsonb_build_object("+summary+")",
			callFacts{direction: "tcl.direction::text", agent: "tcl.agent_id::text", answered: "tcl.answered_at"}),
		from:     from,
		where:    "TRUE",
		args:     args,
		at:       "tcl.started_at",
		item:     callItemPrefix,
		keysetIn: true,
	}
}

func leadCalls(scan lead.TimelineScan) timelineBranch {
	inner, innerArgs := callsBounded("tcl.workspace_id = ? AND tcl.deleted_at IS NULL AND tcl.lead_id = ?",
		[]interface{}{scan.WorkspaceID, scan.LeadID}, scan.Before, scan.Limit)
	return callBranch("("+inner+") tcl"+stampedItemOutcome, callSummary+stampedItemSummary, innerArgs)
}

func unlinkedCallsOfNumbers(scan lead.TimelineScan) timelineBranch {
	inner, innerArgs := callsBounded("tcl.workspace_id = ? AND tcl.lead_id IS NULL AND tcl.deleted_at IS NULL AND "+database.CallCounterpartSQL("tcl")+" = tnum.number",
		[]interface{}{scan.WorkspaceID}, scan.Before, scan.Limit)
	return callBranch("unnest(?::text[]) AS tnum(number) CROSS JOIN LATERAL ("+inner+") tcl", callSummary,
		append([]interface{}{pq.Array(scan.NumberForms)}, innerArgs...))
}

func callBranches(scan lead.TimelineScan) []timelineBranch {
	if !scan.Calls {
		return nil
	}
	branches := []timelineBranch{leadCalls(scan)}
	if len(scan.NumberForms) > 0 {
		branches = append(branches, unlinkedCallsOfNumbers(scan))
	}
	return branches
}

func dealBranches(scan lead.TimelineScan) []timelineBranch {
	if scan.Deals == nil {
		return nil
	}
	where, args := opportunity_repository.LeadDealsCondition("td", scan.WorkspaceID, scan.LeadID, *scan.Deals)
	item := quoted(string(lead.TimelineDeal)+":") + " || td.id::text"
	return []timelineBranch{{
		selects: timelineSelect(quoted(string(lead.TimelineDeal)), "td.created_at", item, quoted(string(lead.TimelineRefDeal)), "td.id::text",
			"''", "COALESCE(td.created_by_id::text, '')", "td.created_by_kind",
			"jsonb_build_object('title', td.title, 'status', td.status, 'valueCents', td.value_cents, 'currency', td.currency, 'pipelineId', td.pipeline_id::text, 'stageId', td.stage_id::text)",
			noCallFacts),
		from:  "opportunities td",
		where: where,
		args:  args,
		at:    "td.created_at",
		item:  item,
	}}
}

func dealEventTypes() []string {
	events := lead.TimelineDealEvents()
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, string(event))
	}
	return types
}

func dealEventBranches(scan lead.TimelineScan) []timelineBranch {
	if scan.Deals == nil {
		return nil
	}
	where, whereArgs := opportunity_repository.LeadDealsCondition("td", scan.WorkspaceID, scan.LeadID, *scan.Deals)
	item := quoted(string(lead.TimelineDealEvent)+":") + " || tev.id::text"
	perDeal, perDealArgs := timelineBranch{
		selects: "tev.*",
		from:    "opportunity_events tev",
		where:   "tev.opportunity_id = td.id AND tev.workspace_id = td.workspace_id AND tev.type = ANY(?)",
		args:    []interface{}{pq.Array(dealEventTypes())},
		at:      "tev.created_at",
		item:    item,
	}.bounded(scan.Before, scan.Limit)
	return []timelineBranch{{
		selects: timelineSelect(quoted(string(lead.TimelineDealEvent)), "tev.created_at", item, quoted(string(lead.TimelineRefDeal)), "td.id::text",
			"''", "COALESCE(tev.actor_id::text, '')", "tev.actor_kind",
			"jsonb_build_object('title', td.title, 'event', tev.type, 'valueCents', tev.value_cents, 'currency', tev.currency, 'pipelineId', td.pipeline_id::text, "+
				"'stageId', tev.to_stage_id::text, 'fromStageId', tev.from_stage_id::text, 'stageName', tst.name, 'fromStageName', tsf.name)",
			noCallFacts),
		from: "opportunities td CROSS JOIN LATERAL (" + perDeal + ") tev" +
			" LEFT JOIN stages tst ON tst.id = tev.to_stage_id::text AND tst.workspace_id = tev.workspace_id" +
			" LEFT JOIN stages tsf ON tsf.id = tev.from_stage_id::text AND tsf.workspace_id = tev.workspace_id",
		where:    where,
		args:     append(perDealArgs, whereArgs...),
		at:       "tev.created_at",
		item:     item,
		keysetIn: true,
	}}
}

func memoryBranch(scan lead.TimelineScan) timelineBranch {
	item := quoted(string(lead.TimelineMemory)+":") + " || tm.id::text"
	return timelineBranch{
		selects: timelineSelect(quoted(string(lead.TimelineMemory)), "tm.created_at", item, quoted(string(lead.TimelineRefMemory)), "tm.id::text",
			"''", "tm.actor_id", "tm.actor_kind", "jsonb_build_object('category', tm.category, 'text', tm.content)", noCallFacts),
		from:  "lead_memories tm",
		where: "tm.workspace_id = ? AND tm.lead_id = ? AND tm.deleted_at IS NULL",
		args:  []interface{}{scan.WorkspaceID, scan.LeadID},
		at:    "tm.created_at",
		item:  item,
	}
}

const changedFields = "(SELECT COALESCE(jsonb_agg(change -> 'field'), '[]'::jsonb) FROM jsonb_array_elements(" +
	"CASE WHEN jsonb_typeof(te.changes) = 'array' THEN te.changes ELSE '[]'::jsonb END) AS change)"

func recordBranch(scan lead.TimelineScan) timelineBranch {
	item := quoted(string(lead.TimelineRecord)+":") + " || te.id::text"
	return timelineBranch{
		selects: timelineSelect(quoted(string(lead.TimelineRecord)), "te.created_at", item, quoted(string(lead.TimelineRefEvent)), "te.id::text",
			"''", "COALESCE(te.actor_id::text, '')", "te.actor_kind", "jsonb_build_object('event', te.kind, 'fields', "+changedFields+")", noCallFacts),
		from:  "lead_events te",
		where: "te.workspace_id = ? AND te.lead_id = ?",
		args:  []interface{}{scan.WorkspaceID, scan.LeadID},
		at:    "te.created_at",
		item:  item,
	}
}

func timelineQuery(scan lead.TimelineScan) (string, []interface{}) {
	branches := []timelineBranch{conversationBranch(scan), officialMilestonesBranch(scan), unofficialMilestonesBranch(scan)}
	branches = append(branches, callBranches(scan)...)
	branches = append(branches, dealBranches(scan)...)
	branches = append(branches, dealEventBranches(scan)...)
	branches = append(branches, memoryBranch(scan), recordBranch(scan))
	parts := make([]string, 0, len(branches))
	var args []interface{}
	for _, b := range branches {
		sql, branchArgs := b.sql(scan.Before, scan.Limit)
		parts = append(parts, sql)
		args = append(args, branchArgs...)
	}
	args = append(args, scan.Limit)
	return "SELECT " + timelineColumns + " FROM (" + strings.Join(parts, " UNION ALL ") +
		") timeline ORDER BY timeline.at DESC, timeline.item_id DESC LIMIT ?", args
}

type timelineRow struct {
	Kind           string
	At             time.Time
	ItemID         string
	RefType        string
	RefID          string
	EntryType      string
	ActorID        string
	ActorKind      string
	Summary        []byte
	CallDirection  *string
	CallAgentID    *string
	CallAnsweredAt *time.Time
}

type timelineSummaryRow struct {
	Channel           string     `json:"channel"`
	Status            string     `json:"status"`
	Title             string     `json:"title"`
	CampaignID        string     `json:"campaignId"`
	Event             string     `json:"event"`
	Fields            []*string  `json:"fields"`
	Direction         string     `json:"direction"`
	Source            string     `json:"source"`
	DurationSec       int        `json:"durationSec"`
	AnsweredAt        *time.Time `json:"answeredAt"`
	ValueCents        int64      `json:"valueCents"`
	Currency          string     `json:"currency"`
	PipelineID        string     `json:"pipelineId"`
	StageID           string     `json:"stageId"`
	Category          string     `json:"category"`
	Text              string     `json:"text"`
	FromStageID       string     `json:"fromStageId"`
	StageName         string     `json:"stageName"`
	FromStageName     string     `json:"fromStageName"`
	Disposition       string     `json:"disposition"`
	CallbackAt        *time.Time `json:"callbackAt"`
	CallListID        string     `json:"callListId"`
	Refusal           string     `json:"refusal"`
	CallListAssignees []string   `json:"callListAssignees"`
}

func (s timelineSummaryRow) summary() lead.TimelineSummary {
	var fields []string
	for _, f := range s.Fields {
		if f != nil && *f != "" {
			fields = append(fields, *f)
		}
	}
	summary := lead.TimelineSummary{
		Channel: shared.EntryType(s.Channel), Status: s.Status, Title: s.Title, CampaignID: s.CampaignID,
		Event: s.Event, Fields: fields, Direction: s.Direction, Source: s.Source, DurationSec: s.DurationSec,
		AnsweredAt: s.AnsweredAt, ValueCents: s.ValueCents, Currency: s.Currency, PipelineID: s.PipelineID,
		StageID: s.StageID, Category: s.Category, Text: s.Text, FromStageID: s.FromStageID, StageName: s.StageName, FromStageName: s.FromStageName,
		CallListID: s.CallListID,
	}
	if outcome, shown := calllist.StampedCallOutcome(s.Disposition, s.Refusal, s.CallbackAt); shown {
		summary.Disposition, summary.CallbackAt = outcome.Disposition, outcome.CallbackAt
	}
	return summary
}

func (r timelineRow) call() *cdr.Call {
	if lead.TimelineKind(r.Kind) != lead.TimelineCall || r.CallDirection == nil || r.RefID == "" {
		return nil
	}
	call := &cdr.Call{CallID: r.RefID, Direction: cdr.Direction(*r.CallDirection), AnsweredAt: r.CallAnsweredAt}
	if r.CallAgentID != nil && *r.CallAgentID != "" {
		agent := *r.CallAgentID
		call.AgentID = &agent
	}
	return call
}

func (r timelineRow) item() (lead.TimelineItem, error) {
	var summary timelineSummaryRow
	if len(r.Summary) > 0 {
		if err := json.Unmarshal(r.Summary, &summary); err != nil {
			return lead.TimelineItem{}, fmt.Errorf("timeline item %s: %w", r.ItemID, err)
		}
	}
	return lead.TimelineItem{
		ID:                r.ItemID,
		Kind:              lead.TimelineKind(r.Kind),
		At:                r.At,
		Actor:             opportunity_repository.JoinAuthor(r.ActorID, r.ActorKind),
		Ref:               lead.TimelineRef{Type: lead.TimelineRefType(r.RefType), ID: r.RefID, EntryType: shared.EntryType(r.EntryType)},
		Summary:           summary.summary(),
		Call:              r.call(),
		CallListAssignees: summary.CallListAssignees,
	}, nil
}

func (r *repository) ScanTimeline(ctx context.Context, scan lead.TimelineScan) ([]lead.TimelineItem, error) {
	scan.WorkspaceID, scan.LeadID = strings.TrimSpace(scan.WorkspaceID), strings.TrimSpace(scan.LeadID)
	if scan.WorkspaceID == "" || scan.LeadID == "" || scan.Limit <= 0 {
		return nil, errTimelineScanInvalid
	}
	sql, args := timelineQuery(scan)
	var rows []timelineRow
	if err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]lead.TimelineItem, 0, len(rows))
	for _, row := range rows {
		item, err := row.item()
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}
