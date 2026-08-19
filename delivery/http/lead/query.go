package lead

import (
	"net/url"
	"strconv"
	"strings"

	"vozko/delivery/http/httpx"
	"vozko/domain/crmfilter"
	leaddomain "vozko/domain/lead"
	"vozko/domain/shared"
)

// leadListMaxPageSize caps a page. The list carries derived columns (activity
// across five tables, campaign and memory counts), so an uncapped pageSize is a
// way to ask one HTTP request to do an unbounded amount of database work.
const leadListMaxPageSize = 200

// sortKeyAliases maps the client's sort vocabulary onto the domain's.
//
// It is derived from leaddomain.AllSortKeys() rather than written out, so a new
// sort key becomes sortable over HTTP the moment the domain declares it — the
// old hand-maintained map is why `lastActivity` was orderable in the UI's mind
// and silently ignored by the server.
func sortKeyAliases() map[string]string {
	keys := leaddomain.AllSortKeys()
	allowed := make(map[string]string, len(keys)+2)
	for _, key := range keys {
		allowed[strings.ToLower(string(key))] = string(key)
	}
	// Spellings the UI and older clients use.
	allowed["lastactivity"] = string(leaddomain.SortLastActivityAt)
	allowed["created"] = string(leaddomain.SortCreatedAt)
	return allowed
}

// parseLeadSorts reads `sort` (and the legacy sibling `order`).
//
// `?sort=name:asc,createdAt:desc` is the full form. Older clients send
// `?sort=name&order=desc`, where direction lives in its own parameter; that
// shape still works, and applies to every key that did not state its own.
func parseLeadSorts(values url.Values) []shared.Sort {
	sorts := httpx.ParseSort(values, sortKeyAliases())
	if len(sorts) == 0 {
		return nil
	}

	order := strings.ToLower(strings.TrimSpace(values.Get("order")))
	if order != string(shared.SortAsc) && order != string(shared.SortDesc) {
		return sorts
	}

	// Only entries that carried no explicit ":dir" defer to `order`.
	explicit := map[string]struct{}{}
	for _, raw := range values["sort"] {
		for _, entry := range strings.Split(raw, ",") {
			if key, _, found := strings.Cut(strings.TrimSpace(entry), ":"); found {
				explicit[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
			}
		}
	}

	for i := range sorts {
		if _, stated := explicit[strings.ToLower(sorts[i].Field)]; !stated {
			sorts[i].Direction = shared.SortDirection(order)
		}
	}
	return sorts
}

// filterBuilder accumulates predicates as single-predicate AND groups.
//
// Groups combine with AND and predicates within a group with OR, so one
// predicate per group is precisely "every stated constraint must hold" — the
// semantics a flat query string implies.
type filterBuilder struct {
	groups []crmfilter.Group
}

func (b *filterBuilder) add(field crmfilter.Field, op crmfilter.Operator, values ...string) {
	b.groups = append(b.groups, crmfilter.Group{
		Conjunction: crmfilter.And,
		Predicates:  []crmfilter.Predicate{{Field: field, Operator: op, Values: values}},
	})
}

// text adds a substring predicate when the value is non-blank.
func (b *filterBuilder) text(field crmfilter.Field, raw string) {
	if trimmed := strings.TrimSpace(raw); trimmed != "" {
		b.add(field, crmfilter.OpContains, trimmed)
	}
}

// set adds an IN predicate over the non-blank values of a repeated or
// comma-separated parameter.
func (b *filterBuilder) set(field crmfilter.Field, raw []string) {
	if values := httpx.ParseCSVQuery(raw); len(values) > 0 {
		b.add(field, crmfilter.OpIn, values...)
	}
}

// tristate maps `?x=true|false` onto a boolean predicate, and absence onto no
// predicate at all.
func (b *filterBuilder) tristate(field crmfilter.Field, raw string) {
	if v := httpx.ParseBoolQuery(raw); v != nil {
		if *v {
			b.add(field, crmfilter.OpIsTrue)
		} else {
			b.add(field, crmfilter.OpIsFalse)
		}
	}
}

// presence maps `?x=true|false` onto "has any / has none", which for a
// membership field is is_set / is_empty.
func (b *filterBuilder) presence(field crmfilter.Field, raw string) {
	if v := httpx.ParseBoolQuery(raw); v != nil {
		if *v {
			b.add(field, crmfilter.OpIsSet)
		} else {
			b.add(field, crmfilter.OpIsEmpty)
		}
	}
}

// dateBound adds an inclusive range edge, accepting RFC3339 or a bare
// YYYY-MM-DD (whose upper bound covers the whole day).
func (b *filterBuilder) dateBound(field crmfilter.Field, raw string, op crmfilter.Operator) {
	if t := httpx.ParseDateBound(raw, op == crmfilter.OpLessEq); t != nil {
		b.add(field, op, t.Format("2006-01-02T15:04:05Z07:00"))
	}
}

// numberBound adds a numeric range edge.
func (b *filterBuilder) numberBound(field crmfilter.Field, raw string, op crmfilter.Operator) {
	if v := httpx.ParseIntQuery(raw); v != nil && *v >= 0 {
		b.add(field, op, strconv.Itoa(*v))
	}
}

// listInputFromQuery translates the whole query string into one domain read
// query.
//
// Two client shapes reach this function: the flat convenience parameters
// (`?name=&hasWhatsAppCampaign=&ageFrom=`) that the old endpoint defined and
// existing callers still send, and the structured `?filter=` expression the new
// UI builds. Both become crmfilter predicates here, at the edge, so exactly one
// filter model travels inward — the alternative, two parallel filter paths in
// the repository, is what let the flat parameters and the real query drift
// apart in the first place.
func listInputFromQuery(workspaceID string, values url.Values) (leaddomain.ListLeadsInput, error) {
	structured, err := httpx.DecodeFilterParam(values.Get("filter"))
	if err != nil {
		return leaddomain.ListLeadsInput{}, err
	}

	b := &filterBuilder{}

	// Free text: one box over name, number and remembered facts.
	b.text(crmfilter.FieldQuery, firstNonBlank(values.Get("q"), values.Get("search")))

	// Identity.
	b.text(crmfilter.FieldNumber, values.Get("number"))
	b.text(crmfilter.FieldName, values.Get("name"))
	b.presence(crmfilter.FieldName, values.Get("hasName"))
	b.numberBound(crmfilter.FieldAge, values.Get("ageFrom"), crmfilter.OpGreaterEq)
	b.numberBound(crmfilter.FieldAge, values.Get("ageTo"), crmfilter.OpLessEq)

	// Lifecycle and reachability.
	b.tristate(crmfilter.FieldBlocked, values.Get("blocked"))
	b.tristate(crmfilter.FieldWindowOpen, values.Get("windowOpen"))
	b.set(crmfilter.FieldChannel, values["channel"])

	// Campaigns.
	b.presence(crmfilter.FieldCampaign, values.Get("hasWhatsAppCampaign"))
	b.set(crmfilter.FieldCampaign, values["campaignId"])
	b.set(crmfilter.FieldCampaignStatus, values["campaignStatus"])
	b.numberBound(crmfilter.FieldCampaignCount, values.Get("campaignsFrom"), crmfilter.OpGreaterEq)
	b.numberBound(crmfilter.FieldCampaignCount, values.Get("campaignsTo"), crmfilter.OpLessEq)

	// CRM tags, resolved through the lead's conversations on every channel.
	b.set(crmfilter.FieldStage, values["stageId"])
	b.set(crmfilter.FieldLabel, values["labelId"])

	// Memories.
	b.presence(crmfilter.FieldMemoryCategory, values.Get("hasMemory"))
	b.set(crmfilter.FieldMemoryCategory, values["memoryCategory"])
	b.set(crmfilter.FieldMemoryAuthor, values["memoryAuthor"])
	b.text(crmfilter.FieldMemoryText, values.Get("memoryText"))
	b.numberBound(crmfilter.FieldMemoryCount, values.Get("memoriesFrom"), crmfilter.OpGreaterEq)
	b.numberBound(crmfilter.FieldMemoryCount, values.Get("memoriesTo"), crmfilter.OpLessEq)
	b.dateBound(crmfilter.FieldMemoryUpdatedAt, values.Get("memoryFrom"), crmfilter.OpGreaterEq)
	b.dateBound(crmfilter.FieldMemoryUpdatedAt, values.Get("memoryTo"), crmfilter.OpLessEq)

	// Clocks.
	b.dateBound(crmfilter.FieldCreatedAt, values.Get("createdFrom"), crmfilter.OpGreaterEq)
	b.dateBound(crmfilter.FieldCreatedAt, values.Get("createdTo"), crmfilter.OpLessEq)
	b.dateBound(crmfilter.FieldUpdatedAt, values.Get("updatedFrom"), crmfilter.OpGreaterEq)
	b.dateBound(crmfilter.FieldUpdatedAt, values.Get("updatedTo"), crmfilter.OpLessEq)
	b.dateBound(crmfilter.FieldLastActivityAt, values.Get("activityFrom"), crmfilter.OpGreaterEq)
	b.dateBound(crmfilter.FieldLastActivityAt, values.Get("activityTo"), crmfilter.OpLessEq)

	filter := crmfilter.Filter{Groups: append(b.groups, structured.Groups...)}

	pagination := httpx.ParsePagination(values)
	if pagination.PageSize > leadListMaxPageSize {
		pagination.PageSize = leadListMaxPageSize
	}

	return leaddomain.ListLeadsInput{
		WorkspaceID: workspaceID,
		Filter:      filter,
		Options: shared.QueryOptions{
			Pagination: pagination,
			Sorts:      parseLeadSorts(values),
		},
	}, nil
}

func firstNonBlank(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
