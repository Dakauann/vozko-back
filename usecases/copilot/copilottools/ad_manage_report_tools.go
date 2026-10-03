package copilottools

import (
	"context"
	"math"
	"strings"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	adsuc "vozko/usecases/advertising"
)

var reportMetricNames = map[advertising.ReportMetric]string{
	advertising.ReportSpend:               "Valor gasto",
	advertising.ReportImpressions:         "Impressões",
	advertising.ReportReach:               "Alcance",
	advertising.ReportFrequency:           "Frequência",
	advertising.ReportClicks:              "Cliques (todos)",
	advertising.ReportLinkClicks:          "Cliques no link",
	advertising.ReportCTR:                 "CTR (taxa de cliques no link)",
	advertising.ReportCPC:                 "CPC (custo por clique no link)",
	advertising.ReportCPM:                 "CPM (custo por mil impressões)",
	advertising.ReportResults:             "Resultados",
	advertising.ReportCostPerResult:       "Custo por resultado",
	advertising.ReportConversations:       "Conversas por mensagem iniciadas",
	advertising.ReportCostPerConversation: "Custo por conversa iniciada",
	advertising.ReportThruPlays:           "ThruPlays",
	advertising.ReportCostPerThruPlay:     "Custo por ThruPlay",
}

var breakdownNames = map[advertising.Breakdown]string{
	advertising.BreakdownAge:       "Idade",
	advertising.BreakdownGender:    "Gênero",
	advertising.BreakdownCountry:   "País",
	advertising.BreakdownRegion:    "Região",
	advertising.BreakdownPlatform:  "Plataforma",
	advertising.BreakdownPosition:  "Posicionamento",
	advertising.BreakdownDeviceOS:  "Dispositivo",
	advertising.BreakdownHourOfDay: "Hora do dia",
}

var breakdownValueNames = map[advertising.Breakdown]map[string]string{
	advertising.BreakdownGender:   {"male": "Masculino", "female": "Feminino", "unknown": "Não informado"},
	advertising.BreakdownDeviceOS: {"mobile_app": "App no celular", "mobile_web": "Navegador no celular", "desktop": "Computador"},
	advertising.BreakdownPlatform: platformNames,
}

var reportObjectNames = map[advertising.Level]string{
	advertising.LevelCampaign: "Nome da campanha",
	advertising.LevelAdSet:    "Nome do conjunto de anúncios",
	advertising.LevelAd:       "Nome do anúncio",
}

var reportViewNames = map[advertising.ReportView]string{
	advertising.ViewPivot: "tabela por item",
	advertising.ViewBars:  "ranking em barras",
	advertising.ViewTrend: "tendência por dia",
}

func exportLabels(level advertising.Level) advertising.ExportLabels {
	return advertising.ExportLabels{
		Object: reportObjectNames[level.OrCampaign()], Day: "Dia", Total: "Total",
		Breakdowns: breakdownNames, Values: breakdownValueNames, Metrics: reportMetricNames,
	}
}

func breakdownValue(b advertising.Breakdown, raw string) string {
	if label := breakdownValueNames[b][raw]; label != "" {
		return label
	}
	return raw
}

func reportValue(metric advertising.ReportMetric, v *float64) interface{} {
	if v == nil {
		return nil
	}
	if metric.Kind() == advertising.KindMoney {
		return advertising.MicrosToAmount(int64(*v))
	}
	return math.Round(*v*100) / 100
}

func reportColumn(metric advertising.ReportMetric, currency string) copilot.Column {
	label := reportMetricNames[metric]
	kind := copilot.ColumnNumber
	switch metric.Kind() {
	case advertising.KindMoney:
		label += " (" + currency + ")"
	case advertising.KindPercent:
		kind = copilot.ColumnPercent
	}
	return copilot.Column{Key: string(metric), Label: label, Kind: kind}
}

var defaultReportMetrics = []advertising.ReportMetric{
	advertising.ReportSpend, advertising.ReportImpressions, advertising.ReportLinkClicks, advertising.ReportResults, advertising.ReportCostPerResult,
}

type adReportShape struct {
	View       string   `json:"view" enum:"pivot,bars,trend" desc:"pivot (padrão): uma linha por item e detalhamento; bars: ranking pela primeira métrica; trend: um valor por dia"`
	Level      string   `json:"level" enum:"campaign,adset,ad" desc:"campaign (padrão), adset ou ad"`
	Breakdowns []string `json:"breakdowns" desc:"age, gender, country, region, publisher_platform, platform_position, device_platform ou hourly_stats_aggregated_by_advertiser_time_zone; só combinações aceitas pela Meta, como age+gender ou publisher_platform+platform_position; nada em trend"`
	Metrics    []string `json:"metrics" desc:"colunas na ordem: spend, impressions, reach, frequency, clicks, linkClicks, ctr, cpc, cpm, results, costPerResult, conversations, costPerConversation, thruPlays, costPerThruPlay; trend aceita só spend, impressions, linkClicks, results, conversations; vazio usa gasto, impressões, cliques no link, resultados e custo por resultado"`
	ObjectIDs  []string `json:"object_ids" desc:"meta_id de ads_results para limitar a itens específicos"`
	Since      string   `json:"since" desc:"primeiro dia YYYY-MM-DD; sem período, os últimos 30 dias no fuso da conta"`
	Until      string   `json:"until" desc:"último dia YYYY-MM-DD"`
}

func (s adReportShape) definition(r advertising.DateRange) advertising.ReportDefinition {
	view := advertising.ReportView(s.View)
	if view == "" {
		view = advertising.ViewPivot
	}
	metrics := advertising.Typed[advertising.ReportMetric](s.Metrics)
	if len(metrics) == 0 {
		metrics = defaultReportMetrics
		if view == advertising.ViewTrend {
			metrics = advertising.TrendMetrics()
		}
	}
	return advertising.ReportDefinition{
		View: view, Level: advertising.Level(s.Level).OrCampaign(), Breakdowns: advertising.Typed[advertising.Breakdown](s.Breakdowns),
		Metrics: metrics, DatePreset: advertising.PresetCustom,
		Since: r.Since.Format(advertising.DayLayout), Until: r.Until.Format(advertising.DayLayout),
	}
}

func (m adManage) reportRange(account *advertising.AdAccount, since, until string, fallback *advertising.DateRange) (advertising.DateRange, error) {
	if since != "" || until != "" {
		return adsRange(since, until)
	}
	if fallback != nil {
		return *fallback, nil
	}
	loc, err := account.Location()
	if err != nil {
		return advertising.DateRange{}, err
	}
	return advertising.LastDays(30, m.Now(), loc), nil
}

func (m adManage) shapedInput(ctx context.Context, cc copilot.Context, accountID string, shape adReportShape) (*advertising.AdAccount, adsuc.ReportRunInput, error) {
	account, err := m.ads.account(ctx, cc, accountID)
	if err != nil {
		return nil, adsuc.ReportRunInput{}, err
	}
	dates, err := m.reportRange(account, shape.Since, shape.Until, nil)
	if err != nil {
		return nil, adsuc.ReportRunInput{}, err
	}
	ids, err := metaIDs(shape.ObjectIDs)
	if err != nil {
		return nil, adsuc.ReportRunInput{}, err
	}
	def := shape.definition(dates)
	if err := def.Validate(); err != nil {
		return nil, adsuc.ReportRunInput{}, err
	}
	return account, adsuc.ReportRunInput{WorkspaceID: cc.WorkspaceID, AccountID: account.ID, Definition: def, Range: dates, ObjectIDs: ids}, nil
}

func (m adManage) savedReport(ctx context.Context, cc copilot.Context, raw string) (*advertising.SavedReport, error) {
	id, err := knownID(raw, "report_id", "list_ad_reports")
	if err != nil {
		return nil, err
	}
	reports, err := m.Reports.List(ctx, cc.WorkspaceID)
	if err != nil {
		return nil, err
	}
	for _, r := range reports {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, advertising.ErrReportNotFound
}

func (m adManage) savedInput(ctx context.Context, cc copilot.Context, saved *advertising.SavedReport, since, until string) (*advertising.AdAccount, adsuc.ReportRunInput, error) {
	account, err := m.ads.account(ctx, cc, saved.AdAccountID)
	if err != nil {
		return nil, adsuc.ReportRunInput{}, err
	}
	var fixed *advertising.DateRange
	if def := saved.Definition; def.DatePreset == advertising.PresetCustom {
		r, err := advertising.NewDateRange(def.Since, def.Until)
		if err != nil {
			return nil, adsuc.ReportRunInput{}, err
		}
		fixed = &r
	}
	dates, err := m.reportRange(account, since, until, fixed)
	if err != nil {
		return nil, adsuc.ReportRunInput{}, err
	}
	return account, adsuc.ReportRunInput{WorkspaceID: cc.WorkspaceID, AccountID: account.ID, Definition: saved.Definition, Range: dates}, nil
}

func reportDataset(run *adsuc.ReportRun) (*copilot.Dataset, error) {
	t := run.Table
	if t.View == advertising.ViewTrend {
		columns := []copilot.Column{{Key: "day", Label: "Dia", Kind: copilot.ColumnDate}}
		for _, m := range t.Metrics {
			columns = append(columns, reportColumn(m, run.Currency))
		}
		d := copilot.NewDataset("relatório de anúncios por dia", columns)
		for _, day := range run.Series {
			row := []any{day.Day.Format(advertising.DayLayout)}
			for _, m := range t.Metrics {
				row = append(row, reportValue(m, day.Values[m]))
			}
			if err := d.AddRow(row...); err != nil {
				return nil, err
			}
		}
		return d, nil
	}
	named := len(t.Rows) > 0 && t.Rows[0].ObjectID != ""
	var columns []copilot.Column
	if named {
		columns = append(columns, copilot.Column{Key: "name", Label: "Nome", Kind: copilot.ColumnText})
	}
	for _, b := range t.Breakdowns {
		columns = append(columns, copilot.Column{Key: string(b), Label: breakdownNames[b], Kind: copilot.ColumnText})
	}
	for _, m := range t.Metrics {
		columns = append(columns, reportColumn(m, run.Currency))
	}
	d := copilot.NewDataset("relatório de anúncios", columns)
	for _, r := range t.Rows {
		row := []any{}
		if named {
			row = append(row, r.Name)
		}
		for i, v := range r.Dimensions {
			row = append(row, breakdownValue(t.Breakdowns[i], v))
		}
		for _, m := range t.Metrics {
			row = append(row, reportValue(m, r.Values[m]))
		}
		if err := d.AddRow(row...); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func metricValues(metrics []advertising.ReportMetric, cells advertising.ReportCells) map[string]interface{} {
	out := make(map[string]interface{}, len(metrics))
	for _, m := range metrics {
		out[string(m)] = reportValue(m, cells[m])
	}
	return out
}

func reportRows(run *adsuc.ReportRun) []map[string]interface{} {
	t := run.Table
	rows := make([]map[string]interface{}, 0, min(run.Rows(), maxAdRowsShown))
	if t.View == advertising.ViewTrend {
		for _, day := range run.Series[:min(len(run.Series), maxAdRowsShown)] {
			row := metricValues(t.Metrics, day.Values)
			row["day"] = day.Day.Format(advertising.DayLayout)
			rows = append(rows, row)
		}
		return rows
	}
	for _, r := range t.Rows[:min(len(t.Rows), maxAdRowsShown)] {
		row := metricValues(t.Metrics, r.Values)
		if r.ObjectID != "" {
			row["meta_id"], row["name"] = r.ObjectID, r.Name
		}
		if len(r.Dimensions) > 0 {
			segment := make([]string, 0, len(r.Dimensions))
			for i, v := range r.Dimensions {
				segment = append(segment, breakdownValue(t.Breakdowns[i], v))
			}
			row["segment"] = strings.Join(segment, " · ")
		}
		rows = append(rows, row)
	}
	return rows
}

func reportResult(cc copilot.Context, account *advertising.AdAccount, in adsuc.ReportRunInput, run *adsuc.ReportRun) copilot.Result {
	data := map[string]interface{}{
		"account": account.Name, "currency": run.Currency, "view": string(run.Table.View),
		"since": in.Range.Since.Format(advertising.DayLayout), "until": in.Range.Until.Format(advertising.DayLayout),
		"rows": reportRows(run), "rows_total": run.Rows(),
	}
	if run.Table.View != advertising.ViewTrend {
		data["totals"] = metricValues(run.Table.Metrics, run.Table.Totals)
	}
	if dataset, err := reportDataset(run); err == nil {
		data["dataset"] = keep(cc, dataset).Handle()
	}
	return copilot.Result{Status: copilot.StatusOK, Data: data}
}

type runAdReportArgs struct {
	AdAccountID string `json:"ad_account_id" req:"true" id:"true" desc:"ad_account_id de list_ad_accounts"`
	adReportShape
}

type runAdReportTool struct{ deps adManage }

func (t *runAdReportTool) Meta() copilot.Meta { return adsMeta(workspace.ActionRead, false) }

func (t *runAdReportTool) Definition() tools.Definition {
	return definition("run_ad_report",
		"Monta um relatório de anúncios igual à tela Relatórios: tabela por item (pivot), ranking em barras (bars) ou tendência por dia (trend), "+
			"no nível de campanha, conjunto ou anúncio, com detalhamentos e as métricas escolhidas, com as mesmas contas do gerenciador. "+
			"Valores de dinheiro em unidades da moeda da conta. Devolve até 30 linhas, os totais e um dataset com a tabela inteira "+
			"para query_dataset e render_chart (barras para ranking, linhas para tendência).",
		runAdReportArgs{})
}

func (t *runAdReportTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a runAdReportArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	account, in, err := t.deps.shapedInput(ctx, cc, a.AdAccountID, a.adReportShape)
	if err != nil {
		return adsFailure("run_ad_report", err)
	}
	run, err := t.deps.Runs.Run(ctx, in)
	if err != nil {
		return adsFailure("run_ad_report", err)
	}
	return reportResult(cc, account, in, run)
}

type listAdReportsArgs struct {
	AdAccountID string `json:"ad_account_id" id:"true" desc:"ad_account_id de list_ad_accounts para ver só os relatórios dessa conta"`
}

type listAdReportsTool struct{ deps adManage }

func (t *listAdReportsTool) Meta() copilot.Meta { return adsMeta(workspace.ActionRead, false) }

func (t *listAdReportsTool) Definition() tools.Definition {
	return definition("list_ad_reports",
		"Lista os relatórios de anúncios salvos na tela Relatórios, com a visão, o nível, os detalhamentos, as métricas e o período de cada um. "+
			"Use o report_id em run_saved_ad_report ou export_ad_report.",
		listAdReportsArgs{})
}

func (t *listAdReportsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a listAdReportsArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	reports, err := t.deps.Reports.List(ctx, cc.WorkspaceID)
	if err != nil {
		return adsFailure("list_ad_reports", err)
	}
	out := make([]map[string]interface{}, 0, len(reports))
	for _, r := range reports {
		if a.AdAccountID != "" && r.AdAccountID != a.AdAccountID {
			continue
		}
		def := r.Definition
		row := map[string]interface{}{
			"report_id": r.ID, "name": r.Name, "ad_account_id": r.AdAccountID, "view": string(def.View), "level": string(def.Level),
			"breakdowns": def.Breakdowns, "metrics": def.Metrics, "period": string(def.DatePreset),
		}
		if def.DatePreset == advertising.PresetCustom {
			row["since"], row["until"] = def.Since, def.Until
		}
		out = append(out, row)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"reports": out}}
}

type runSavedAdReportArgs struct {
	ReportID string `json:"report_id" req:"true" id:"true" desc:"report_id de list_ad_reports"`
	Since    string `json:"since" desc:"primeiro dia YYYY-MM-DD; vazio usa as datas fixas do relatório ou, se o período dele é relativo (period), os últimos 30 dias"`
	Until    string `json:"until" desc:"último dia YYYY-MM-DD"`
}

type runSavedAdReportTool struct{ deps adManage }

func (t *runSavedAdReportTool) Meta() copilot.Meta { return adsMeta(workspace.ActionRead, false) }

func (t *runSavedAdReportTool) Definition() tools.Definition {
	return definition("run_saved_ad_report",
		"Roda um relatório salvo de list_ad_reports e devolve as linhas, os totais e um dataset, como run_ad_report. "+
			"Quando o período salvo é relativo (por exemplo thisMonth), mande since e until desse período.",
		runSavedAdReportArgs{})
}

func (t *runSavedAdReportTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a runSavedAdReportArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	saved, err := t.deps.savedReport(ctx, cc, a.ReportID)
	if err != nil {
		return adsFailure("run_saved_ad_report", err)
	}
	account, in, err := t.deps.savedInput(ctx, cc, saved, a.Since, a.Until)
	if err != nil {
		return adsFailure("run_saved_ad_report", err)
	}
	run, err := t.deps.Runs.Run(ctx, in)
	if err != nil {
		return adsFailure("run_saved_ad_report", err)
	}
	result := reportResult(cc, account, in, run)
	result.Data.(map[string]interface{})["report"] = saved.Name
	return result
}

type exportAdReportArgs struct {
	ReportID    string `json:"report_id" id:"true" desc:"report_id de list_ad_reports para exportar um relatório salvo; vazio monta o relatório com os campos abaixo"`
	AdAccountID string `json:"ad_account_id" id:"true" desc:"ad_account_id de list_ad_accounts (obrigatório sem report_id)"`
	Name        string `json:"name" desc:"nome do arquivo em Exportações; vazio usa o nome do relatório salvo ou Relatório de anúncios"`
	adReportShape
}

type exportPlan struct {
	account  *advertising.AdAccount
	input    adsuc.ReportRunInput
	reportID string
	name     string
}

type exportAdReportTool struct{ deps adManage }

func (t *exportAdReportTool) Meta() copilot.Meta { return adsMeta(workspace.ActionRead, true) }

func (t *exportAdReportTool) Definition() tools.Definition {
	return definition("export_ad_report",
		"Exporta um relatório de anúncios para CSV (planilha) em Exportações, na tela Relatórios, com os nomes das colunas em português. "+
			"Serve um relatório salvo (report_id) ou um montado na hora com os mesmos campos de run_ad_report. "+
			"Depois mostra um botão que abre a tela de Relatórios. Só depois da aprovação do usuário.",
		exportAdReportArgs{})
}

func (t *exportAdReportTool) plan(ctx context.Context, cc copilot.Context, args map[string]interface{}) (*exportPlan, error) {
	a, err := validateArgs[exportAdReportArgs](nil, cc, args)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(a.Name)
	if a.ReportID == "" {
		account, in, err := t.deps.shapedInput(ctx, cc, a.AdAccountID, a.adReportShape)
		if err != nil {
			return nil, err
		}
		if name == "" {
			name = "Relatório de anúncios"
		}
		return &exportPlan{account: account, input: in, name: name}, nil
	}
	saved, err := t.deps.savedReport(ctx, cc, a.ReportID)
	if err != nil {
		return nil, err
	}
	account, in, err := t.deps.savedInput(ctx, cc, saved, a.Since, a.Until)
	if err != nil {
		return nil, err
	}
	if err := in.Definition.Validate(); err != nil {
		return nil, err
	}
	if name == "" {
		name = saved.Name
	}
	return &exportPlan{account: account, input: in, reportID: saved.ID, name: name}, nil
}

func (t *exportAdReportTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := t.plan(ctx, cc, args)
	return adsValidation("export_ad_report", err)
}

func (t *exportAdReportTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return []copilot.Field{{Key: "report", Value: "relatório com campos a corrigir"}}
	}
	def := p.input.Definition
	metrics := make([]string, 0, len(def.Metrics))
	for _, m := range def.Metrics {
		metrics = append(metrics, reportMetricNames[m])
	}
	fields := []copilot.Field{
		{Key: "name", Value: p.name},
		{Key: "account", Value: p.account.Name},
		{Key: "period", Value: p.input.Range.Since.Format("02/01/2006") + " a " + p.input.Range.Until.Format("02/01/2006")},
		{Key: "view", Value: reportViewNames[def.View] + ", por " + levelNames[def.Level.OrCampaign()]},
		{Key: "metrics", Value: strings.Join(metrics, ", ")},
	}
	if len(def.Breakdowns) > 0 {
		names := make([]string, 0, len(def.Breakdowns))
		for _, b := range def.Breakdowns {
			names = append(names, breakdownNames[b])
		}
		fields = append(fields, copilot.Field{Key: "breakdowns", Value: strings.Join(names, " + ")})
	}
	return fields
}

func (t *exportAdReportTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	p, err := t.plan(ctx, cc, args)
	if err != nil {
		return adsFailure("export_ad_report", err)
	}
	export, err := t.deps.Runs.Export(ctx, adsuc.ReportExportInput{
		ReportRunInput: p.input, UserID: cc.UserID, Name: p.name, ReportID: p.reportID, Labels: exportLabels(p.input.Definition.Level),
	})
	if err != nil {
		return adsFailure("export_ad_report", err)
	}
	card, _ := copilot.NewNavigationCard(workspace.ScreenAdsReports, "")
	return copilot.Result{Status: copilot.StatusOK, Card: card, Data: map[string]interface{}{
		"export_id": export.ID, "name": export.Name, "rows": export.Rows,
		"since": export.Range.Since.Format(advertising.DayLayout), "until": export.Range.Until.Format(advertising.DayLayout),
	}}
}
