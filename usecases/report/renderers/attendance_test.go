package report_renderers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vozko/domain/attendance"
	"vozko/domain/report"
)

type identityLabels struct {
	missing map[string]bool
}

func (l identityLabels) For(string, string) Translate {
	return func(key string) string {
		if l.missing[key] {
			return humanizeKey(key)
		}
		return key
	}
}

type overviewStub struct {
	overview    *attendance.Overview
	err         error
	workspaceID string
	filter      attendance.OverviewFilter
}

func (s *overviewStub) Execute(workspaceID string, filter attendance.OverviewFilter) (*attendance.Overview, error) {
	s.workspaceID = workspaceID
	s.filter = filter
	return s.overview, s.err
}

func decodeOverview(t *testing.T, raw string) *attendance.Overview {
	t.Helper()
	var overview attendance.Overview
	if err := json.Unmarshal([]byte(raw), &overview); err != nil {
		t.Fatalf("decode overview fixture: %v", err)
	}
	return &overview
}

func withStages(t *testing.T, raw string, stages string) *attendance.Overview {
	t.Helper()
	overview := decodeOverview(t, raw)
	var block attendance.OverviewStages
	if err := json.Unmarshal([]byte(stages), &block); err != nil {
		t.Fatalf("decode stage fixture: %v", err)
	}
	overview.Stages = block
	return overview
}

func stageBlockWith(stage string) string {
	return strings.ReplaceAll(stageBlockJSON, "__STAGE__", stage)
}

func executiveOverview(t *testing.T) *attendance.Overview {
	t.Helper()
	raw := executiveOverviewJSON
	for _, dimension := range []string{"origin", "assignee", "age", "tenure", "returning"} {
		raw = strings.ReplaceAll(
			raw,
			"__XRAY_"+dimension+"__",
			strings.ReplaceAll(xrayDimensionJSON, "__NAME__", dimension),
		)
	}
	return decodeOverview(t, raw)
}

var baseParams = AttendanceParams{
	DateFrom:        "2026-08-01",
	DateTo:          "2026-08-17",
	DepartmentLabel: "Suporte",
	MemberLabel:     "Todos",
	ChannelLabel:    "WhatsApp",
	WorkspaceName:   "Vozko",
}

var executiveParams = AttendanceParams{
	DateFrom:        "2026-09-01",
	DateTo:          "2026-09-28",
	DepartmentLabel: "Todos",
	MemberLabel:     "Todos",
	ChannelLabel:    "Todos",
	WorkspaceName:   "Vozko",
}

func renderCSV(
	t *testing.T,
	overview *attendance.Overview,
	params AttendanceParams,
	labels LabelResolver,
	generatedAt time.Time,
) report.Artifact {
	t.Helper()

	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("encode params: %v", err)
	}

	renderer := NewAttendanceRenderer(&overviewStub{overview: overview}, labels)
	renderer.now = func() time.Time { return generatedAt }

	artifact, err := renderer.Render(
		context.Background(),
		report.Job{
			ID:          "job-1",
			WorkspaceID: "ws-1",
			Kind:        report.KindAttendanceOverview,
			Format:      report.FormatCSV,
			Locale:      "pt",
			Params:      encoded,
		},
		func(int) {},
	)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return artifact
}

func buildBase(t *testing.T, overview *attendance.Overview) string {
	t.Helper()
	return string(renderCSV(
		t, overview, baseParams, identityLabels{},
		time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC),
	).Data)
}

func buildExecutive(t *testing.T, overview *attendance.Overview) string {
	t.Helper()
	return string(renderCSV(
		t, overview, executiveParams, identityLabels{},
		time.Date(2026, time.September, 28, 15, 36, 0, 0, time.UTC),
	).Data)
}

func mustContain(t *testing.T, csv string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(csv, fragment) {
			t.Errorf("csv is missing %q", fragment)
		}
	}
}

func mustNotContain(t *testing.T, csv string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if strings.Contains(csv, fragment) {
			t.Errorf("csv should not contain %q", fragment)
		}
	}
}

func TestAttendanceCSVRecordsTheAppliedFilter(t *testing.T) {
	csv := buildBase(t, decodeOverview(t, baseOverviewJSON))
	mustContain(t, csv,
		"filters.dateFrom;2026-08-01",
		"filters.dateTo;2026-08-17",
		"filters.department;Suporte",
		"filters.channel;WhatsApp",
		"filters.includeAi;yes",
		"filters.workspace;Vozko",
		"filters.generatedAt;2026-08-17T12:00:00.000Z",
	)
}

func TestAttendanceCSVNamesTheFileAfterTheRange(t *testing.T) {
	artifact := renderCSV(
		t, decodeOverview(t, baseOverviewJSON), baseParams, identityLabels{},
		time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC),
	)
	if artifact.Filename != "atendimento-2026-08-01-2026-08-17.csv" {
		t.Fatalf("filename = %q", artifact.Filename)
	}
}

func TestAttendanceCSVWritesKPIsAndStatusPercentages(t *testing.T) {
	csv := buildBase(t, decodeOverview(t, baseOverviewJSON))
	mustContain(t, csv,
		"kpi.engaged;142",
		"kpi.avgHandleMins;12,35",
		"status.finished;90;63,4",
		"columns.total;142;100",
	)
}

func TestAttendanceCSVOmitsUnavailableMetrics(t *testing.T) {
	csv := buildBase(t, decodeOverview(t, baseOverviewJSON))
	mustNotContain(t, csv,
		"kpi.avgRating",
		"sections.queue",
		"sections.ai",
		"kpi.frtSlaPercent",
	)
}

func TestAttendanceCSVKeepsAMeasuredButEmptyMetricAsABlankRow(t *testing.T) {
	overview := decodeOverview(t, baseOverviewJSON)
	overview.KPIs.AvgHandleMins = nil
	csv := buildBase(t, overview)
	mustContain(t, csv, "kpi.avgHandleMins;\r\n")
}

func TestAttendanceCSVUsesLabelsInsteadOfRawEnumKeys(t *testing.T) {
	overview := decodeOverview(t, baseOverviewJSON)
	if err := json.Unmarshal([]byte(`[{"channel":"unofficial_whatsapp","count":1,"pct":100}]`), &overview.ChannelMix); err != nil {
		t.Fatalf("decode channel mix: %v", err)
	}
	if err := json.Unmarshal([]byte(`[{
		"actor_id":"u1","actor_kind":"human","display_name":"Ana","presence":"online",
		"avg_response_mins":1,"rating":null,"resolution_pct":0,"open":1,"pending":0,"resolved":0
	}]`), &overview.ByMember); err != nil {
		t.Fatalf("decode members: %v", err)
	}

	labels := identityLabels{}
	csv := string(renderCSV(
		t, overview, baseParams,
		labelsFor(map[string]string{
			"channel.unofficial_whatsapp": "WhatsApp (não oficial)",
			"actorKind.human":             "Humano",
			"presence.online":             "Online",
		}, labels),
		time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC),
	).Data)

	mustContain(t, csv, "WhatsApp (não oficial);1;100", "Ana;;Humano;Online;")
	mustNotContain(t, csv, "unofficial_whatsapp")
}

func TestAttendanceCSVAppendsDefinitionsUnderTheirLabel(t *testing.T) {
	overview := decodeOverview(t, baseOverviewJSON)
	overview.Definitions.PeriodScope = "Conversas criadas no período"

	csv := string(renderCSV(
		t, overview, baseParams,
		labelsFor(map[string]string{"definitions.period_scope": "Recorte do período"}, identityLabels{}),
		time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC),
	).Data)

	mustContain(t, csv, "sections.definitions", "Recorte do período;Conversas criadas no período")
	mustNotContain(t, csv, "status_mapping;")
}

func TestAttendanceCSVFallsBackToTheRawKeyWhenADefinitionHasNoLabel(t *testing.T) {
	overview := decodeOverview(t, baseOverviewJSON)
	overview.Definitions.PeriodScope = "Explicação nova"

	csv := string(renderCSV(
		t, overview, baseParams,
		identityLabels{missing: map[string]bool{"definitions.period_scope": true}},
		time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC),
	).Data)

	mustContain(t, csv, "Period scope;Explicação nova")
}

func TestAttendanceCSVIncludesADetailSectionOnceAvailable(t *testing.T) {
	overview := decodeOverview(t, baseOverviewJSON)
	if err := json.Unmarshal([]byte(`{
		"enqueued":10,"connected":8,"abandoned":2,"overflow":0,"queue_full":0,
		"cancelled":0,"avg_asa_mins":1.25,"abandon_rate":20,"available":true
	}`), &overview.Queue); err != nil {
		t.Fatalf("decode queue: %v", err)
	}

	csv := buildBase(t, overview)
	mustContain(t, csv, "sections.queue", "queue.enqueued;10", "queue.avgAsaMins;1,25")
}

func TestAttendanceCSVPadsTheHour(t *testing.T) {
	csv := buildBase(t, decodeOverview(t, baseOverviewJSON))
	mustContain(t, csv, "00;3")
}

func TestAttendanceCSVEmitsDepartmentAndMemberTables(t *testing.T) {
	overview := decodeOverview(t, baseOverviewJSON)
	if err := json.Unmarshal([]byte(`[{
		"department_id":"d1","department_name":"Vendas","avg_wait_mins":2,
		"avg_handle_mins":9,"finished":5,"finished_human":5,"finished_ai":0,
		"finished_system":0,"ongoing":1,"pending":0
	}]`), &overview.ByDepartment); err != nil {
		t.Fatalf("decode departments: %v", err)
	}
	if err := json.Unmarshal([]byte(`[{
		"actor_id":"u1","actor_kind":"user","display_name":"Ana, Silva","email":"ana@x.com",
		"presence":"online","avg_response_mins":4.5,"rating":4.8,"resolution_pct":91,
		"open":2,"pending":1,"resolved":12,"total_messages":140,"avg_messages":11.7
	}]`), &overview.ByMember); err != nil {
		t.Fatalf("decode members: %v", err)
	}

	csv := string(renderCSV(
		t, overview, baseParams,
		labelsFor(map[string]string{
			"actorKind.user":  "Humano",
			"presence.online": "Online",
		}, identityLabels{}),
		time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC),
	).Data)

	mustContain(t, csv,
		"Vendas;2;9;5;5;0;0;1;0",
		"Ana, Silva;ana@x.com;Humano;Online;4,5;4,8;91;2;1;12;140;11,7",
	)
}

func TestAttendanceCSVDefusesAFormulaInADepartmentName(t *testing.T) {
	overview := decodeOverview(t, baseOverviewJSON)
	if err := json.Unmarshal([]byte(`[{
		"department_id":"d1","department_name":"=cmd|\"/c calc\"!A1",
		"avg_wait_mins":null,"avg_handle_mins":null,"finished":0,"ongoing":0,"pending":0
	}]`), &overview.ByDepartment); err != nil {
		t.Fatalf("decode departments: %v", err)
	}

	csv := buildBase(t, overview)
	mustContain(t, csv, "'=cmd|")
	for _, line := range strings.Split(csv, "\r\n") {
		if strings.HasPrefix(line, "=cmd") {
			t.Fatalf("a formula reached the start of a line: %q", line)
		}
	}
}

func TestAttendanceCSVStartsWithABOM(t *testing.T) {
	csv := buildBase(t, decodeOverview(t, baseOverviewJSON))
	if !strings.HasPrefix(csv, report.UTF8BOM) {
		t.Fatalf("csv does not start with the UTF-8 BOM")
	}
}

func TestAttendanceCSVNamesTheFunnelOnEveryStageRow(t *testing.T) {
	csv := buildBase(t, withStages(t, baseOverviewJSON, stageBlockWith(stageRowJSON)))
	mustContain(t, csv, "FUNIL UNIFECAF;Inscrição", "NÃO USAR;Inscrição")
}

func TestAttendanceCSVKeepsShellsInTheirOwnColumn(t *testing.T) {
	csv := buildBase(t, withStages(t, baseOverviewJSON, stageBlockWith(stageRowJSON)))

	var row string
	for _, line := range strings.Split(csv, "\r\n") {
		if strings.HasPrefix(line, "FUNIL UNIFECAF;Inscrição") {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatal("no stage row for the default funnel")
	}
	if !strings.Contains(row, ";600;40;") {
		t.Fatalf("engaged and shell are not separate columns: %q", row)
	}
}

func TestAttendanceCSVExportsWhatIsNotStaged(t *testing.T) {
	csv := buildBase(t, withStages(t, baseOverviewJSON, stageBlockWith(stageRowJSON)))
	mustContain(t, csv, "stages.unstagedEngaged;120", "stages.unstagedShell;10")
}

func TestAttendanceCSVOmitsStagesWhenNothingIsStaged(t *testing.T) {
	csv := buildBase(t, decodeOverview(t, baseOverviewJSON))
	mustNotContain(t, csv, "sections.stages")
}

func TestAttendanceCSVDefusesAFormulaInAStageName(t *testing.T) {
	stage := strings.Replace(
		stageRowJSON,
		`"stage_name": "Inscrição"`,
		`"stage_name": "=HYPERLINK(\"http://x\")"`,
		1,
	)
	csv := buildBase(t, withStages(t, baseOverviewJSON, stageBlockWith(stage)))
	mustContain(t, csv, "'=HYPERLINK")
	mustNotContain(t, csv, ";=HYPERLINK")
}

func TestAttendanceCSVCarriesTheCampaignScope(t *testing.T) {
	params := baseParams
	params.CampaignID = "camp-1"
	params.CampaignType = "whatsapp"

	csv := string(renderCSV(
		t, decodeOverview(t, baseOverviewJSON), params, identityLabels{},
		time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC),
	).Data)

	mustContain(t, csv, "filters.campaign;camp-1", "filters.campaignType;whatsapp")
}

func TestAttendanceCSVExportsThePeriodAndTheCluster(t *testing.T) {
	csv := buildExecutive(t, executiveOverview(t))
	mustContain(t, csv, "sections.period", "executive.openDaysTotal", "22", "executive.cluster")
}

func TestAttendanceCSVExportsEveryProjectionWithItsVerdict(t *testing.T) {
	csv := buildExecutive(t, executiveOverview(t))
	mustContain(t, csv, "sections.projections", "finished", "1786", "at_risk")
}

func TestAttendanceCSVExportsRevenueInCents(t *testing.T) {
	csv := buildExecutive(t, executiveOverview(t))
	mustContain(t, csv, "sections.revenue", "BRL", "12473295")
}

func TestAttendanceCSVExportsTheTrendWithItsFlags(t *testing.T) {
	csv := buildExecutive(t, executiveOverview(t))
	mustContain(t, csv, "sections.trend", "2026-07", "1699")
}

func TestAttendanceCSVExportsTheBacklogXray(t *testing.T) {
	csv := buildExecutive(t, executiveOverview(t))
	mustContain(t, csv, "sections.backlogXray", "origin", "6162")
}

func TestAttendanceCSVExportsReachabilityAndStatesUnsupportedChannels(t *testing.T) {
	csv := buildExecutive(t, executiveOverview(t))
	mustContain(t, csv, "sections.reachability", "whatsapp", "telegram", "no_window_model")
}

func TestAttendanceCSVExportsQualityWithTheTeamTotal(t *testing.T) {
	csv := buildExecutive(t, executiveOverview(t))
	mustContain(t, csv, "sections.quality", "Bella", "Sistema", "columns.total")
}

func TestAttendanceCSVExportsTheTeamRanking(t *testing.T) {
	csv := buildExecutive(t, executiveOverview(t))
	mustContain(t, csv, "sections.teamRanking", "columns.perOnlineHour", "elite")
}

func TestAttendanceCSVExportsReworkWithItsCost(t *testing.T) {
	csv := buildExecutive(t, executiveOverview(t))
	mustContain(t, csv, "sections.rework", "Bella", "900000", "columns.reopenRate")
}

func TestAttendanceCSVExportsThePerMemberMessageVolume(t *testing.T) {
	csv := buildExecutive(t, executiveOverview(t))
	mustContain(t, csv, "columns.avgMessages")
}

func TestAttendanceCSVLeavesReworkOutWhenNothingWasClosed(t *testing.T) {
	overview := executiveOverview(t)
	overview.Rework = attendance.OverviewRework{Available: false, Reason: "no_finished_closes"}
	csv := buildExecutive(t, overview)
	mustNotContain(t, csv, "sections.rework")
}

func TestAttendanceCSVLeavesUnavailableBlocksOutInsteadOfExportingZeros(t *testing.T) {
	overview := executiveOverview(t)
	overview.Revenue = attendance.Revenue{Available: false, Reason: "revenue_not_department_scoped"}
	overview.Quality = attendance.Quality{Threshold: 30, Available: false, Reason: "outcome_capture_disabled"}

	csv := buildExecutive(t, overview)
	mustNotContain(t, csv, "sections.revenue", "sections.quality")
	mustContain(t, csv, "sections.period")
}

func TestAttendanceRendererPassesTheFilterThrough(t *testing.T) {
	params := baseParams
	params.DepartmentID = "d1"
	params.MemberID = "u1"
	params.Channel = "whatsapp"
	params.RankMetric = "resolved"
	params.TrendBuckets = 6

	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("encode params: %v", err)
	}

	source := &overviewStub{overview: decodeOverview(t, baseOverviewJSON)}
	renderer := NewAttendanceRenderer(source, identityLabels{})
	if _, err := renderer.Render(
		context.Background(),
		report.Job{WorkspaceID: "ws-9", Format: report.FormatCSV, Locale: "pt", Params: encoded},
		func(int) {},
	); err != nil {
		t.Fatalf("render: %v", err)
	}

	if source.workspaceID != "ws-9" {
		t.Fatalf("workspace = %q, want %q", source.workspaceID, "ws-9")
	}
	if source.filter.DepartmentID != "d1" || source.filter.MemberID != "u1" {
		t.Fatalf("scope filter = %+v", source.filter)
	}
	if source.filter.Channel != "whatsapp" || source.filter.RankMetric != "resolved" {
		t.Fatalf("metric filter = %+v", source.filter)
	}
	if source.filter.TrendBuckets != 6 {
		t.Fatalf("trend buckets = %d, want 6", source.filter.TrendBuckets)
	}
	if !source.filter.IncludeAI {
		t.Fatal("IncludeAI should default to true when the caller omits it")
	}
	if source.filter.DateFrom == nil || source.filter.DateTo == nil {
		t.Fatalf("dates were not parsed: %+v", source.filter)
	}
}

func TestAttendanceRendererRefusesAnEmptyOverview(t *testing.T) {
	renderer := NewAttendanceRenderer(&overviewStub{overview: nil}, identityLabels{})
	_, err := renderer.Render(
		context.Background(),
		report.Job{WorkspaceID: "ws-1", Format: report.FormatCSV, Params: json.RawMessage(`{}`)},
		func(int) {},
	)
	if err == nil {
		t.Fatal("a missing overview must fail the render, never produce an empty file")
	}
}

type overrideLabels struct {
	overrides map[string]string
	base      LabelResolver
}

func (l overrideLabels) For(locale, namespace string) Translate {
	inner := l.base.For(locale, namespace)
	return func(key string) string {
		if value, ok := l.overrides[key]; ok {
			return value
		}
		return inner(key)
	}
}

func labelsFor(overrides map[string]string, base LabelResolver) LabelResolver {
	return overrideLabels{overrides: overrides, base: base}
}
