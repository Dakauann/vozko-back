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

const leadListMaxPageSize = 200

func sortKeyAliases() map[string]string {
	keys := leaddomain.AllSortKeys()
	allowed := make(map[string]string, len(keys)+2)
	for _, key := range keys {
		allowed[strings.ToLower(string(key))] = string(key)
	}
	allowed["lastactivity"] = string(leaddomain.SortLastActivityAt)
	allowed["created"] = string(leaddomain.SortCreatedAt)
	return allowed
}

func parseLeadSorts(values url.Values) []shared.Sort {
	sorts := httpx.ParseSort(values, sortKeyAliases())
	if len(sorts) == 0 {
		return nil
	}

	order := strings.ToLower(strings.TrimSpace(values.Get("order")))
	if order != string(shared.SortAsc) && order != string(shared.SortDesc) {
		return sorts
	}

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

type filterBuilder struct {
	groups []crmfilter.Group
}

func (b *filterBuilder) add(field crmfilter.Field, op crmfilter.Operator, values ...string) {
	b.groups = append(b.groups, crmfilter.Group{
		Conjunction: crmfilter.And,
		Predicates:  []crmfilter.Predicate{{Field: field, Operator: op, Values: values}},
	})
}

func (b *filterBuilder) text(field crmfilter.Field, raw string) {
	if trimmed := strings.TrimSpace(raw); trimmed != "" {
		b.add(field, crmfilter.OpContains, trimmed)
	}
}

func (b *filterBuilder) set(field crmfilter.Field, raw []string) {
	if values := httpx.ParseCSVQuery(raw); len(values) > 0 {
		b.add(field, crmfilter.OpIn, values...)
	}
}

func (b *filterBuilder) tristate(field crmfilter.Field, raw string) {
	if v := httpx.ParseBoolQuery(raw); v != nil {
		if *v {
			b.add(field, crmfilter.OpIsTrue)
		} else {
			b.add(field, crmfilter.OpIsFalse)
		}
	}
}

func (b *filterBuilder) presence(field crmfilter.Field, raw string) {
	if v := httpx.ParseBoolQuery(raw); v != nil {
		if *v {
			b.add(field, crmfilter.OpIsSet)
		} else {
			b.add(field, crmfilter.OpIsEmpty)
		}
	}
}

func (b *filterBuilder) dateBound(field crmfilter.Field, raw string, op crmfilter.Operator) {
	if t := httpx.ParseDateBound(raw, op == crmfilter.OpLessEq); t != nil {
		b.add(field, op, t.Format("2006-01-02T15:04:05Z07:00"))
	}
}

func (b *filterBuilder) numberBound(field crmfilter.Field, raw string, op crmfilter.Operator) {
	if v := httpx.ParseIntQuery(raw); v != nil && *v >= 0 {
		b.add(field, op, strconv.Itoa(*v))
	}
}

func listInputFromQuery(workspaceID string, values url.Values) (leaddomain.ListLeadsInput, error) {
	structured, err := httpx.DecodeFilterParam(values.Get("filter"))
	if err != nil {
		return leaddomain.ListLeadsInput{}, err
	}

	b := &filterBuilder{}

	b.text(crmfilter.FieldQuery, firstNonBlank(values.Get("q"), values.Get("search")))

	b.text(crmfilter.FieldNumber, values.Get("number"))
	b.text(crmfilter.FieldName, values.Get("name"))
	b.presence(crmfilter.FieldName, values.Get("hasName"))
	b.numberBound(crmfilter.FieldAge, values.Get("ageFrom"), crmfilter.OpGreaterEq)
	b.numberBound(crmfilter.FieldAge, values.Get("ageTo"), crmfilter.OpLessEq)

	b.tristate(crmfilter.FieldBlocked, values.Get("blocked"))
	b.tristate(crmfilter.FieldWindowOpen, values.Get("windowOpen"))
	b.set(crmfilter.FieldChannel, values["channel"])

	b.presence(crmfilter.FieldCampaign, values.Get("hasWhatsAppCampaign"))
	b.set(crmfilter.FieldCampaign, values["campaignId"])
	b.set(crmfilter.FieldCampaignStatus, values["campaignStatus"])
	b.numberBound(crmfilter.FieldCampaignCount, values.Get("campaignsFrom"), crmfilter.OpGreaterEq)
	b.numberBound(crmfilter.FieldCampaignCount, values.Get("campaignsTo"), crmfilter.OpLessEq)

	b.set(crmfilter.FieldStage, values["stageId"])
	b.set(crmfilter.FieldLabel, values["labelId"])

	b.presence(crmfilter.FieldMemoryCategory, values.Get("hasMemory"))
	b.set(crmfilter.FieldMemoryCategory, values["memoryCategory"])
	b.set(crmfilter.FieldMemoryAuthor, values["memoryAuthor"])
	b.text(crmfilter.FieldMemoryText, values.Get("memoryText"))
	b.numberBound(crmfilter.FieldMemoryCount, values.Get("memoriesFrom"), crmfilter.OpGreaterEq)
	b.numberBound(crmfilter.FieldMemoryCount, values.Get("memoriesTo"), crmfilter.OpLessEq)
	b.dateBound(crmfilter.FieldMemoryUpdatedAt, values.Get("memoryFrom"), crmfilter.OpGreaterEq)
	b.dateBound(crmfilter.FieldMemoryUpdatedAt, values.Get("memoryTo"), crmfilter.OpLessEq)

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
