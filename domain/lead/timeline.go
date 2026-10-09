package lead

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/actor"
	"vozko/domain/calls/cdr"
	"vozko/domain/customfield"
	"vozko/domain/opportunity"
	"vozko/domain/shared"
)

const (
	DefaultTimelinePage = 30
	MaxTimelinePage     = 100
)

var ErrPageQueryInvalid = errors.New("lead: the page asks for a negative size or a cursor this server did not give")

type TimelineKind string

const (
	TimelineConversation      TimelineKind = "conversation"
	TimelineCampaignSent      TimelineKind = "campaign_sent"
	TimelineCampaignDelivered TimelineKind = "campaign_delivered"
	TimelineCampaignRead      TimelineKind = "campaign_read"
	TimelineCampaignFailed    TimelineKind = "campaign_failed"
	TimelineCall              TimelineKind = "call"
	TimelineDeal              TimelineKind = "deal"
	TimelineDealEvent         TimelineKind = "deal_event"
	TimelineMemory            TimelineKind = "memory"
	TimelineRecord            TimelineKind = "record"
)

type TimelineRefType string

const (
	TimelineRefEntry  TimelineRefType = "entry"
	TimelineRefCall   TimelineRefType = "call"
	TimelineRefDeal   TimelineRefType = "deal"
	TimelineRefMemory TimelineRefType = "memory"
	TimelineRefEvent  TimelineRefType = "lead_event"
)

type TimelineRef struct {
	Type      TimelineRefType
	ID        string
	EntryType shared.EntryType
}

func (r TimelineRef) Entry() (shared.EntryRef, bool) {
	if r.Type != TimelineRefEntry || strings.TrimSpace(r.ID) == "" || !r.EntryType.Valid() {
		return shared.EntryRef{}, false
	}
	return shared.EntryRef{EntryID: r.ID, EntryType: r.EntryType}, true
}

type TimelineSummary struct {
	Channel       shared.EntryType
	Status        string
	Title         string
	CampaignID    string
	Event         string
	Fields        []string
	Direction     string
	Source        string
	DurationSec   int
	AnsweredAt    *time.Time
	ValueCents    int64
	Currency      string
	PipelineID    string
	StageID       string
	Category      string
	Text          string
	FromStageID   string
	StageName     string
	FromStageName string
	Disposition   string
	CallbackAt    *time.Time
	CallListID    string
}

func TimelineDealEvents() []opportunity.EventType {
	return []opportunity.EventType{opportunity.EventStageMoved, opportunity.EventWon, opportunity.EventLost, opportunity.EventReopened}
}

type TimelineItem struct {
	ID                string
	Kind              TimelineKind
	At                time.Time
	Actor             string
	ActorName         string
	Ref               TimelineRef
	Summary           TimelineSummary
	Call              *cdr.Call
	CallListAssignees []string
}

func (i TimelineItem) Cursor() shared.Keyset {
	return shared.Keyset{At: i.At, ID: i.ID}
}

type TimelinePage struct {
	Items []TimelineItem
	Next  string
}

type PageQuery struct {
	LeadID string
	Before string
	Limit  int
}

func (q PageQuery) Normalize() (PageQuery, error) {
	q.LeadID = strings.TrimSpace(q.LeadID)
	q.Before = strings.TrimSpace(q.Before)
	if q.LeadID == "" {
		return PageQuery{}, ErrLeadRequired
	}
	switch {
	case q.Limit < 0:
		return PageQuery{}, ErrPageQueryInvalid
	case q.Limit == 0:
		q.Limit = DefaultTimelinePage
	case q.Limit > MaxTimelinePage:
		q.Limit = MaxTimelinePage
	}
	if _, err := q.Cursor(); err != nil {
		return PageQuery{}, err
	}
	return q, nil
}

func (q PageQuery) Cursor() (*shared.Keyset, error) {
	key, paged, err := shared.ParseKeyset(q.Before)
	if err != nil {
		return nil, ErrPageQueryInvalid
	}
	if !paged {
		return nil, nil
	}
	return &key, nil
}

func (q PageQuery) DealsCursor() (*shared.Keyset, error) {
	before, err := q.Cursor()
	if err != nil || before == nil {
		return before, err
	}
	id, err := uuid.Parse(before.ID)
	if err != nil {
		return nil, ErrPageQueryInvalid
	}
	before.ID = id.String()
	return before, nil
}

type TimelineScan struct {
	WorkspaceID string
	LeadID      string
	NumberForms []string
	Before      *shared.Keyset
	Limit       int
	Calls       bool
	Deals       *opportunity.DealScope
}

type TimelineSource interface {
	ScanTimeline(ctx context.Context, scan TimelineScan) ([]TimelineItem, error)
}

type TimelineGrants struct {
	Entries   map[shared.EntryRef]bool
	Calls     map[string]bool
	CallLists map[string]bool
	Deals     bool
	Fields    Viewer
}

func (g TimelineGrants) Visible(item TimelineItem) (TimelineItem, bool) {
	switch item.Kind {
	case TimelineConversation, TimelineCampaignSent, TimelineCampaignDelivered, TimelineCampaignRead, TimelineCampaignFailed:
		ref, ok := item.Ref.Entry()
		return item, ok && g.Entries[ref]
	case TimelineCall:
		return g.visibleCall(item)
	case TimelineDeal, TimelineDealEvent:
		return item, g.Deals && item.Ref.Type == TimelineRefDeal
	case TimelineMemory:
		return item, item.Ref.Type == TimelineRefMemory
	case TimelineRecord:
		return g.visibleRecord(item)
	}
	return item, false
}

func (g TimelineGrants) visibleCall(item TimelineItem) (TimelineItem, bool) {
	item.CallListAssignees = nil
	if item.Ref.Type != TimelineRefCall || !g.Calls[item.Ref.ID] {
		return item, false
	}
	if listID := item.Summary.CallListID; listID == "" || !g.CallLists[listID] {
		item.Summary.CallListID, item.Summary.Disposition, item.Summary.CallbackAt = "", "", nil
	}
	return item, true
}

func TimelineCallLists(items []TimelineItem) map[string][]string {
	lists := map[string][]string{}
	for _, item := range items {
		listID := item.Summary.CallListID
		if item.Kind != TimelineCall || listID == "" {
			continue
		}
		if _, seen := lists[listID]; !seen {
			lists[listID] = item.CallListAssignees
		}
	}
	return lists
}

func (g TimelineGrants) visibleRecord(item TimelineItem) (TimelineItem, bool) {
	if item.Ref.Type != TimelineRefEvent {
		return item, false
	}
	changed := item.Summary.Fields
	item.Summary.Fields = VisibleChangedFields(changed, g.Fields)
	return item, len(changed) == 0 || len(item.Summary.Fields) > 0
}

func VisibleChangedFields(fields []string, v Viewer) []string {
	readable := map[string]bool{}
	for _, def := range v.Definitions {
		if customfield.VisibleTo(def, v.Fields) {
			readable[def.Key] = true
		}
	}
	var visible []string
	for _, field := range fields {
		if key, custom := customFieldKey(field); custom && !readable[key] {
			continue
		}
		visible = append(visible, field)
	}
	return visible
}

func TimelineEntries(items []TimelineItem) []shared.EntryRef {
	seen := map[shared.EntryRef]bool{}
	var refs []shared.EntryRef
	for _, item := range items {
		ref, ok := item.Ref.Entry()
		if !ok || seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	return refs
}

func TimelineCalls(items []TimelineItem) []string {
	seen := map[string]bool{}
	var ids []string
	for _, item := range items {
		if item.Kind != TimelineCall || item.Ref.Type != TimelineRefCall || item.Ref.ID == "" || seen[item.Ref.ID] {
			continue
		}
		seen[item.Ref.ID] = true
		ids = append(ids, item.Ref.ID)
	}
	return ids
}

func NameTimelineActors(namer actor.Namer, items []TimelineItem) {
	if namer == nil || len(items) == 0 {
		return
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item.Actor != "" {
			ids = append(ids, item.Actor)
		}
	}
	if len(ids) == 0 {
		return
	}
	names := namer.Names(ids...)
	for i := range items {
		items[i].ActorName = names[items[i].Actor]
	}
}

func (l *Lead) LegacyCallForms() []string {
	return NumberForms(l.NumberMasks(), true)
}
