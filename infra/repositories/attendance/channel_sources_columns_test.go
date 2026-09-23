package attendance_repository

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	gormschema "gorm.io/gorm/schema"

	"vozko/domain/attendance"

	"vozko/infra/database/schema"
)

var aliasModels = map[string]interface{}{
	"wce":  &schema.WhatsAppCampaignEntry{},
	"wc":   &schema.WhatsAppCampaign{},
	"igc":  &schema.InstagramConversation{},
	"iga":  &schema.InstagramAccount{},
	"igct": &schema.InstagramContact{},
	"tgc":  &schema.TelegramConversation{},
	"tga":  &schema.TelegramAccount{},
	"tgct": &schema.TelegramContact{},
	"uwc":  &schema.UnofficialWhatsAppConversation{},
	"uwi":  &schema.UnofficialWhatsAppInstance{},
	"uwct": &schema.UnofficialWhatsAppContact{},
}

var qualifiedColumn = regexp.MustCompile(`\b([a-z]+[a-z0-9]*)\.([a-z_][a-z0-9_]*)\b`)

func columnsOf(t *testing.T, model interface{}) map[string]struct{} {
	t.Helper()
	parsed, err := gormschema.Parse(model, &sync.Map{}, gormschema.NamingStrategy{})
	if err != nil {
		t.Fatalf("schema.Parse(%T) error = %v", model, err)
	}
	out := make(map[string]struct{}, len(parsed.DBNames))
	for _, name := range parsed.DBNames {
		out[name] = struct{}{}
	}
	return out
}

func assertColumnsExist(t *testing.T, where, sql string) {
	t.Helper()
	for _, match := range qualifiedColumn.FindAllStringSubmatch(sql, -1) {
		alias, column := match[1], match[2]
		model, tracked := aliasModels[alias]
		if !tracked {
			continue
		}
		if _, found := columnsOf(t, model)[column]; !found {
			available := make([]string, 0)
			for name := range columnsOf(t, model) {
				available = append(available, name)
			}
			sort.Strings(available)
			t.Fatalf(
				"%s references %s.%s which %T does not declare; available columns: %s",
				where, alias, column, model, strings.Join(available, ", "),
			)
		}
	}
}

func TestChannelSourceProjectionsOnlyReferenceRealColumns(t *testing.T) {
	for _, src := range channelSources {
		channel := string(src.EntryType)
		assertColumnsExist(t, channel+" projection", src.projection("TRUE"))
		assertColumnsExist(t, channel+" group by", src.groupByColumns())
		assertColumnsExist(t, channel+" lead join", src.LeadJoin)
		assertColumnsExist(t, channel+" container join", src.ContainerJoin)
		assertColumnsExist(t, channel+" status column", src.StatusColumn)
		assertColumnsExist(t, channel+" close source", src.CloseSourceColumn)
		assertColumnsExist(t, channel+" close outcome", src.CloseOutcomeColumn)
		assertColumnsExist(t, channel+" closed at", src.ClosedAtColumn)
		assertColumnsExist(t, channel+" department", src.DepartmentColumn)
		assertColumnsExist(t, channel+" container id", src.ContainerIDColumn)
		assertColumnsExist(t, channel+" container name", src.ContainerNameExpr)
		assertColumnsExist(t, channel+" workspace", src.WorkspaceColumn)
		assertColumnsExist(t, channel+" lead id", src.LeadIDExpr)
	}
}

func TestChannelSourceGroupByCoversEveryProjectedColumn(t *testing.T) {
	for _, src := range channelSources {
		groupBy := src.groupByColumns()
		for _, expr := range src.containerNameGroupBy() {
			if !strings.Contains(groupBy, expr) {
				t.Fatalf(
					"%s group by %q is missing the container name expression %q",
					src.EntryType, groupBy, expr,
				)
			}
		}
		if src.LeadIDExpr != "" && !strings.Contains(groupBy, src.LeadIDExpr) {
			t.Fatalf("%s group by is missing the lead expression %q", src.EntryType, src.LeadIDExpr)
		}
		if src.ClosedAtColumn != "" && !strings.Contains(groupBy, src.ClosedAtColumn) {
			t.Fatalf("%s group by is missing %q", src.EntryType, src.ClosedAtColumn)
		}
		if src.CloseOutcomeColumn != "" && !strings.Contains(groupBy, src.CloseOutcomeColumn) {
			t.Fatalf("%s group by is missing %q", src.EntryType, src.CloseOutcomeColumn)
		}
	}
}

func TestTrendAndBacklogSQLOnlyReferenceRealColumns(t *testing.T) {
	for _, src := range channelSources {
		channel := string(src.EntryType)
		assertColumnsExist(t, channel+" trend joins", trendJoins(src))
		assertColumnsExist(t, channel+" trend engaged", trendEngagedPredicate(src))

		clause, _ := trendScopeClause(src, overviewFilterForTest()).build()
		assertColumnsExist(t, channel+" trend scope", clause)
	}

	union, _ := priorFinishedLeadsUnion("ws", "tmp_msg", overviewFilterForTest())
	assertColumnsExist(t, "prior finished leads union", union)
}

func overviewFilterForTest() attendance.OverviewFilter {
	return attendance.OverviewFilter{
		DepartmentID: "dept-1",
		MemberID:     "user-1",
		CampaignID:   "camp-1",
	}
}
