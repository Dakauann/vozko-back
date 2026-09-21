package conversation_repository

import (
	"strings"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

func TestCampaignContainerDeclaresItsDepartmentColumns(t *testing.T) {
	for _, q := range channelQueries {
		if q.CampaignCTE == "" {
			continue
		}
		if q.CampaignCTEEntryCol == "" {
			t.Errorf("%s declares a campaign CTE with no entry column", q.EntryType)
		}
		if q.CampaignFilter == "" {
			t.Errorf("%s declares a campaign CTE but no message filter", q.EntryType)
		}
		if q.CampaignDeptColumn == "" || q.CampaignDeptEntryCol == "" {
			t.Errorf("%s declares a campaign container with no department columns", q.EntryType)
		}
	}
}

func TestCampaignFragmentsHaveOnePlaceholderAndOneVerb(t *testing.T) {
	for _, q := range channelQueries {
		if q.CampaignCTE == "" {
			continue
		}
		for name, sql := range map[string]string{
			"CampaignCTE":    q.CampaignCTE,
			"CampaignFilter": q.CampaignFilter,
		} {
			if got := strings.Count(sql, "?"); got != 1 {
				t.Errorf("%s.%s has %d bind params, want exactly 1", q.EntryType, name, got)
			}
			if got := strings.Count(sql, "%[1]s"); got != 1 {
				t.Errorf("%s.%s has %d department verbs, want exactly 1", q.EntryType, name, got)
			}
		}
	}
}

func TestChannelsWithoutACampaignContainerFallBack(t *testing.T) {
	wa, ok := channelQueryFor(shared.EntryTypeWhatsApp)
	if !ok {
		t.Fatal("the whatsapp channel is not registered")
	}
	if wa.usesCampaignContainer(conversation.ContainerKindCampaign) {
		t.Fatal("the Cloud API channel claimed a separate campaign container")
	}

	primary := wa.cteForKind(conversation.ContainerKindAccount, func(string) string { return "" })
	asCampaign := wa.cteForKind(conversation.ContainerKindCampaign, func(string) string { return "" })
	if primary != asCampaign {
		t.Fatal("a campaign-scoped request on the Cloud API produced a different query")
	}
}

func TestUnofficialChannelHasTwoDistinctContainers(t *testing.T) {
	uw, ok := channelQueryFor(shared.EntryTypeUnofficialWhatsApp)
	if !ok {
		t.Fatal("the unofficial whatsapp channel is not registered")
	}
	if !uw.usesCampaignContainer(conversation.ContainerKindCampaign) {
		t.Fatal("the unofficial channel does not declare a campaign container")
	}

	byNumber := uw.cteForKind(conversation.ContainerKindAccount, func(string) string { return "" })
	byCampaign := uw.cteForKind(conversation.ContainerKindCampaign, func(string) string { return "" })
	if byNumber == byCampaign {
		t.Fatal("scoping by number and by campaign produced the same query")
	}
	if !strings.Contains(byCampaign, "unofficial_whatsapp_campaign_entries") {
		t.Fatalf("the campaign CTE does not reach through campaign entries: %s", byCampaign)
	}
	if !strings.Contains(byCampaign, "DISTINCT") {
		t.Fatalf("the campaign CTE can return the same conversation twice: %s", byCampaign)
	}
}

func TestUnknownContainerKindFallsBack(t *testing.T) {
	uw, _ := channelQueryFor(shared.EntryTypeUnofficialWhatsApp)
	if uw.usesCampaignContainer(conversation.ContainerKind("something-else")) {
		t.Fatal("an unknown kind selected the campaign container")
	}
	if conversation.ContainerKind("something-else").Valid() {
		t.Fatal("an unknown kind validated")
	}
}

func TestFilterForKindUsesTheMatchingDepartmentColumns(t *testing.T) {
	uw, _ := channelQueryFor(shared.EntryTypeUnofficialWhatsApp)

	seen := map[string]string{}
	capture := func(column, entryCol string) (string, []interface{}) {
		seen["column"] = column
		seen["entryCol"] = entryCol
		return "", nil
	}

	uw.filterForKind(conversation.ContainerKindAccount, capture)
	byNumberColumn := seen["column"]

	uw.filterForKind(conversation.ContainerKindCampaign, capture)
	byCampaignColumn := seen["column"]

	if byNumberColumn == byCampaignColumn {
		t.Fatalf("both kinds scoped departments through %q", byNumberColumn)
	}
	if !strings.Contains(byCampaignColumn, "uwcamp") {
		t.Fatalf("campaign scope used %q, want the campaign row's department", byCampaignColumn)
	}
}

func TestContainerFiltersDoNotCastEntryIDsToText(t *testing.T) {
	for _, q := range channelQueries {
		for name, sql := range map[string]string{
			"ContainerFilter": q.ContainerFilter,
			"CampaignFilter":  q.CampaignFilter,
			"ContainerCTE":    q.ContainerCTE,
			"CampaignCTE":     q.CampaignCTE,
		} {
			if sql == "" {
				continue
			}
			for _, line := range strings.Split(sql, "\n") {
				trimmed := strings.TrimSpace(line)
				if !strings.HasPrefix(strings.ToUpper(trimmed), "SELECT") {
					continue
				}
				projection := trimmed
				if idx := strings.Index(strings.ToUpper(trimmed), " FROM "); idx > 0 {
					projection = trimmed[:idx]
				}
				if strings.Contains(projection, "::text") {
					t.Errorf("%s.%s projects an entry id as text; conversation_messages.entry_id is a uuid and the comparison raises 42883:\n  %s",
						q.EntryType, name, projection)
				}
			}
		}
	}
}
