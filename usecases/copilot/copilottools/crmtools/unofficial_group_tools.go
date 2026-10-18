package crmtools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/shared"
	"vozko/domain/tools"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	uwg "vozko/domain/unofficial_whatsapp_group"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools"
	uwguc "vozko/usecases/unofficial_whatsapp_group"
)

const (
	groupsPageSize     = 20
	groupPeoplePerPage = 20
	groupTopItems      = 5
)

var errGroupsHidden = fmt.Errorf("%w: o usuário não pode ver os grupos do WhatsApp", copilottools.ErrInvalidArgs)

type GroupDirectory interface {
	Dashboard(ctx context.Context, a uwguc.Actor) (*uwguc.DashboardStats, error)
	ListGroups(ctx context.Context, a uwguc.Actor, in uwguc.ListGroupsInput) (*shared.PaginatedResult[*uwguc.GroupRow], error)
	GroupsByID(ctx context.Context, a uwguc.Actor, ids []string) ([]*uwguc.GroupRow, error)
	GetGroup(ctx context.Context, a uwguc.Actor, id string) (*uwguc.GroupDetail, error)
	People(ctx context.Context, a uwguc.Actor, search string, minGroups int, p shared.Pagination) (*shared.PaginatedResult[*uwg.Person], error)
	CreateAnalysis(ctx context.Context, a uwguc.Actor, in uwguc.AnalysisInput) (*uwg.Analysis, error)
	DraftAnalysis(a uwguc.Actor, in uwguc.AnalysisInput) (*uwg.Analysis, error)
}

type UnofficialGroupDeps struct {
	Campaigns   UnofficialCampaignDeps
	Groups      GroupDirectory
	Permissions workspace.CheckAccessUseCase
}

func (d UnofficialGroupDeps) actor(cc copilot.Context) (uwguc.Actor, error) {
	scope, err := d.Campaigns.scope(cc)
	if err != nil {
		return uwguc.Actor{}, err
	}
	return uwguc.Actor{WorkspaceID: cc.WorkspaceID, UserID: cc.UserID, Departments: scope}, nil
}

func (d UnofficialGroupDeps) seesGroups(cc copilot.Context) error {
	if cc.SystemAdmin {
		return nil
	}
	if d.Permissions == nil || d.Permissions.Execute(cc.UserID, cc.WorkspaceID, workspace.ResourceUnofficialWhatsAppGroups, workspace.ActionRead) != nil {
		return errGroupsHidden
	}
	return nil
}

func groupsRead() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceUnofficialWhatsAppGroups, Action: workspace.ActionRead}
}

type unofficialGroupsDashboardTool struct{ deps UnofficialGroupDeps }

func NewUnofficialGroupsDashboardTool(deps UnofficialGroupDeps) copilot.Tool {
	return &unofficialGroupsDashboardTool{deps: deps}
}

func (t *unofficialGroupsDashboardTool) Meta() copilot.Meta { return groupsRead() }

func (t *unofficialGroupsDashboardTool) Definition() tools.Definition {
	return copilottools.Definition("unofficial_groups_dashboard",
		"Painel dos grupos de WhatsApp dos números por QR code que o usuário vê: grupos acompanhados, pessoas, análises, "+
			"sentimento, grupos mais ativos, os que mais cresceram, insights recentes e pessoas em vários grupos. Devolve os "+
			"datasets daily_volume (mensagens por dia, 7 dias) e growth (entradas, saídas e saldo por dia, 30 dias) para render_chart.",
		struct{}{})
}

func (t *unofficialGroupsDashboardTool) Execute(ctx context.Context, cc copilot.Context, _ map[string]interface{}) copilot.Result {
	a, err := t.deps.actor(cc)
	if err != nil {
		return groupFailure(err)
	}
	stats, err := t.deps.Groups.Dashboard(ctx, a)
	if err != nil {
		return groupFailure(err)
	}
	data := map[string]interface{}{
		"connected_numbers": stats.Connections.Connected, "numbers": stats.Connections.Total,
		"groups": stats.Groups.Groups, "followed_groups": stats.Groups.Followed,
		"memberships": stats.People.Memberships, "unique_people": stats.People.Unique,
		"analyses": stats.Analyses.Total, "active_analyses": stats.Analyses.Active,
		"cycles": stats.Runs.Runs, "analyzed_messages": stats.Runs.Messages, "high_alerts": stats.Runs.HighAlerts,
		"message_sentiment": sentimentShare(stats.Sentiment),
		"top_groups":        stats.TopGroups, "top_growing": stats.TopGrowing,
		"insights": stats.Insights, "cross_group_people": len(stats.CrossGroupPeople),
	}
	if d, err := dailyVolumeDataset(stats.DailyVolume); err == nil {
		data["daily_volume"] = copilottools.Keep(cc, d).Handle()
	}
	if d, err := growthDataset(stats.Growth); err == nil {
		data["growth"] = copilottools.Keep(cc, d).Handle()
	}
	return copilot.Result{Status: copilot.StatusOK, Data: data}
}

func sentimentShare(t uwg.SentimentTally) map[string]interface{} {
	return map[string]interface{}{"positive": t.Positive, "neutral": t.Neutral, "negative": t.Negative, "total": t.Total()}
}

func dailyVolumeDataset(points []uwg.DailyPoint) (*copilot.Dataset, error) {
	d := copilot.NewDataset("Mensagens por dia nos grupos", []copilot.Column{
		{Key: "day", Label: "Dia", Kind: copilot.ColumnDate},
		{Key: "messages", Label: "Mensagens", Kind: copilot.ColumnNumber},
		{Key: "active_groups", Label: "Grupos ativos", Kind: copilot.ColumnNumber},
	})
	for _, p := range points {
		if err := d.AddRow(p.Day.Format("2006-01-02"), p.Messages, p.ActiveGroups); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func growthDataset(points []uwg.DailyPoint) (*copilot.Dataset, error) {
	d := copilot.NewDataset("Entradas e saídas nos grupos", []copilot.Column{
		{Key: "day", Label: "Dia", Kind: copilot.ColumnDate},
		{Key: "joins", Label: "Entradas", Kind: copilot.ColumnNumber},
		{Key: "leaves", Label: "Saídas", Kind: copilot.ColumnNumber},
		{Key: "net", Label: "Saldo", Kind: copilot.ColumnNumber},
	})
	for _, p := range points {
		if err := d.AddRow(p.Day.Format("2006-01-02"), p.Joins, p.Leaves, p.Net); err != nil {
			return nil, err
		}
	}
	return d, nil
}

type listUnofficialGroupsArgs struct {
	Search   string `json:"search" desc:"parte do nome do grupo"`
	Followed string `json:"followed" enum:"followed,not_followed" desc:"só acompanhados ou só não acompanhados; vazio para todos"`
	Category string `json:"category" desc:"categoria exata"`
	Page     int    `json:"page" desc:"página, a partir de 1"`
}

type listUnofficialGroupsTool struct{ deps UnofficialGroupDeps }

func NewListUnofficialGroupsTool(deps UnofficialGroupDeps) copilot.Tool {
	return &listUnofficialGroupsTool{deps: deps}
}

func (t *listUnofficialGroupsTool) Meta() copilot.Meta { return groupsRead() }

func (t *listUnofficialGroupsTool) Definition() tools.Definition {
	return copilottools.Definition("list_unofficial_groups",
		"Lista os grupos de WhatsApp dos números por QR code que o usuário vê, com membros, mensagens e saldo de membros "+
			"em 30 dias, sentimento das análises e os números presentes (se cada um pode enviar). Use o group_id exato nas outras ferramentas de grupo.",
		listUnofficialGroupsArgs{})
}

func (t *listUnofficialGroupsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var in listUnofficialGroupsArgs
	if err := copilottools.DecodeArgs(args, &in); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	a, err := t.deps.actor(cc)
	if err != nil {
		return groupFailure(err)
	}
	filter := uwguc.ListGroupsInput{Search: strings.TrimSpace(in.Search), Pagination: shared.Pagination{Page: max(in.Page, 1), PageSize: groupsPageSize}}
	if in.Followed != "" {
		followed := in.Followed == "followed"
		filter.Followed = &followed
	}
	if category := strings.TrimSpace(in.Category); category != "" {
		filter.Category = &category
	}
	page, err := t.deps.Groups.ListGroups(ctx, a, filter)
	if err != nil {
		return groupFailure(err)
	}
	groups := make([]map[string]interface{}, 0, len(page.Items))
	for _, row := range page.Items {
		groups = append(groups, groupSummary(row))
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"groups": groups, "total": page.TotalItems, "page": page.Page, "pages": page.TotalPages}}
}

func groupSummary(row *uwguc.GroupRow) map[string]interface{} {
	g := row.Group
	out := map[string]interface{}{
		"group_id": g.ID, "name": g.Subject, "members": g.MemberCount, "followed": g.Followed(), "origin": g.Origin,
		"category": g.Category, "only_admins_post": g.Announce,
		"messages_30d": row.Activity.Messages, "joins_30d": row.Activity.Joins, "leaves_30d": row.Activity.Leaves,
		"numbers": numberSummaries(row.Numbers), "has_analysis": row.HasAnalysis, "running_campaigns": row.RunningCampaigns,
	}
	if row.Activity.LastActivity != nil {
		out["last_activity"] = row.Activity.LastActivity.Format("2006-01-02 15:04")
	}
	if row.Summary != nil {
		out["sentiment"], out["positivity"], out["high_alert_pct"] = row.Summary.Sentiment, row.Summary.Positivity, row.Summary.HighAlertPct
	}
	return out
}

func numberSummaries(seats []uwguc.NumberSeat) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(seats))
	for _, s := range seats {
		out = append(out, map[string]interface{}{"number_id": s.InstanceID, "name": s.Label, "connected": s.Connected, "admin": s.Admin, "can_post": s.CanPost})
	}
	return out
}

type groupIDArgs struct {
	GroupID string `json:"group_id" req:"true" id:"true" desc:"group_id exato de list_unofficial_groups"`
}

type unofficialGroupOverviewTool struct{ deps UnofficialGroupDeps }

func NewUnofficialGroupOverviewTool(deps UnofficialGroupDeps) copilot.Tool {
	return &unofficialGroupOverviewTool{deps: deps}
}

func (t *unofficialGroupOverviewTool) Meta() copilot.Meta { return groupsRead() }

func (t *unofficialGroupOverviewTool) Definition() tools.Definition {
	return copilottools.Definition("unofficial_group_overview",
		"Visão de um grupo de WhatsApp: membros ativos, saídas em 30 dias, membros com lead, crescimento diário (dataset growth "+
			"para render_chart), o resumo das análises de IA (sentimento, temas, menções, alertas) e o último ciclo com insights e ações sugeridas.",
		groupIDArgs{})
}

func (t *unofficialGroupOverviewTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var in groupIDArgs
	if err := copilottools.DecodeArgs(args, &in); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	a, err := t.deps.actor(cc)
	if err != nil {
		return groupFailure(err)
	}
	detail, err := t.deps.Groups.GetGroup(ctx, a, in.GroupID)
	if err != nil {
		return groupFailure(err)
	}
	data := groupSummary(&detail.GroupRow)
	data["active_members"], data["left_30d"], data["members_with_lead"], data["hidden_phones"] =
		detail.Members.Active, detail.Members.Left30d, detail.Members.WithLead, detail.Members.Hidden
	data["analysis_summary"] = map[string]interface{}{
		"cycles": detail.Overview.Completed, "messages": detail.Overview.Messages, "sentiment": detail.Overview.Dominant,
		"positivity": detail.Overview.AvgPositivity, "high_alert_pct": detail.Overview.HighAlertPct,
		"themes": firstN(detail.Overview.Themes, groupTopItems), "mentions": firstN(detail.Overview.Mentions, groupTopItems),
	}
	if run := detail.LatestRun; run != nil && run.Result != nil {
		data["latest_cycle"] = map[string]interface{}{
			"at": run.CompletedAt.Format("2006-01-02 15:04"), "summary": run.Result.Summary, "alert_level": run.Result.AlertLevel,
			"insights": run.Result.Insights, "suggested_actions": run.Result.SuggestedActions,
		}
	}
	analyses := make([]map[string]interface{}, 0, len(detail.Analyses))
	for _, an := range detail.Analyses {
		analyses = append(analyses, map[string]interface{}{"analysis_id": an.ID, "name": an.Name, "status": an.Status, "trigger": an.Trigger, "cycles": an.Runs})
	}
	data["analyses"] = analyses
	if d, err := growthDataset(detail.Growth); err == nil {
		data["growth"] = copilottools.Keep(cc, d).Handle()
	}
	return copilot.Result{Status: copilot.StatusOK, Data: data}
}

func firstN[T any](items []T, n int) []T {
	if len(items) > n {
		return items[:n]
	}
	return items
}

type unofficialGroupPeopleArgs struct {
	Search    string `json:"search" desc:"nome ou telefone"`
	MinGroups int    `json:"min_groups" desc:"mínimo de grupos acompanhados ao mesmo tempo; padrão 2"`
	Page      int    `json:"page" desc:"página, a partir de 1"`
}

type unofficialGroupPeopleTool struct{ deps UnofficialGroupDeps }

func NewUnofficialGroupPeopleTool(deps UnofficialGroupDeps) copilot.Tool {
	return &unofficialGroupPeopleTool{deps: deps}
}

func (t *unofficialGroupPeopleTool) Meta() copilot.Meta { return groupsRead() }

func (t *unofficialGroupPeopleTool) Definition() tools.Definition {
	return copilottools.Definition("unofficial_group_people",
		"Pessoas que estão em vários grupos acompanhados ao mesmo tempo, com os grupos de cada uma e o lead vinculado. Nossos números não entram.",
		unofficialGroupPeopleArgs{})
}

func (t *unofficialGroupPeopleTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var in unofficialGroupPeopleArgs
	if err := copilottools.DecodeArgs(args, &in); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	a, err := t.deps.actor(cc)
	if err != nil {
		return groupFailure(err)
	}
	page, err := t.deps.Groups.People(ctx, a, strings.TrimSpace(in.Search), in.MinGroups, shared.Pagination{Page: max(in.Page, 1), PageSize: groupPeoplePerPage})
	if err != nil {
		return groupFailure(err)
	}
	people := make([]map[string]interface{}, 0, len(page.Items))
	for _, p := range page.Items {
		groups := make([]string, 0, len(p.Groups))
		for _, g := range p.Groups {
			groups = append(groups, g.Subject)
		}
		person := map[string]interface{}{"name": p.Name, "phone": p.Phone, "group_count": p.GroupCount, "groups": groups}
		if p.LeadID != "" {
			person["lead_id"], person["lead_name"] = p.LeadID, p.LeadName
		}
		people = append(people, person)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"people": people, "total": page.TotalItems}}
}

type groupCampaignArgs struct {
	Name              string   `json:"name" req:"true" desc:"nome da campanha"`
	NumberID          string   `json:"number_id" req:"true" id:"true" desc:"number_id de um número presente em todos os grupos e que pode enviar neles (list_unofficial_groups)"`
	GroupIDs          []string `json:"group_ids" req:"true" id:"true" desc:"group_id exatos de list_unofficial_groups"`
	Message           string   `json:"message" desc:"o texto exato enviado em cada grupo (legenda, quando houver anexo)"`
	AttachmentMediaID string   `json:"attachment_media_id" id:"true" desc:"media_id de um anexo da conversa (imagem, vídeo, áudio ou documento) para enviar junto"`
	DepartmentID      string   `json:"department_id" id:"true" desc:"departamento (list_departments); só quando o usuário tem mais de um"`
}

func (a groupCampaignArgs) message(deps UnofficialGroupDeps, cc copilot.Context) (uwc.MessageSpec, error) {
	return unofficialMessageArgs{Message: a.Message, AttachmentMediaID: a.AttachmentMediaID}.message(deps.Campaigns, cc)
}

type createUnofficialGroupCampaignTool struct{ deps UnofficialGroupDeps }

func NewCreateUnofficialGroupCampaignTool(deps UnofficialGroupDeps) copilot.Tool {
	return &createUnofficialGroupCampaignTool{deps: deps}
}

func (t *createUnofficialGroupCampaignTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceUnofficialWhatsAppCampaigns, Action: workspace.ActionCreate}
}

func (t *createUnofficialGroupCampaignTool) Definition() tools.Definition {
	return copilottools.Definition("create_unofficial_group_campaign",
		"Cria uma campanha do WhatsApp por QR code que envia uma mensagem em cada grupo escolhido, pelo número escolhido. A "+
			"campanha nasce parada; para enviar use start_unofficial_campaign. Só depois da aprovação do usuário.",
		groupCampaignArgs{})
}

func (t *createUnofficialGroupCampaignTool) targets(ctx context.Context, cc copilot.Context, a groupCampaignArgs) ([]*uwguc.GroupRow, error) {
	if err := t.deps.seesGroups(cc); err != nil {
		return nil, err
	}
	actor, err := t.deps.actor(cc)
	if err != nil {
		return nil, err
	}
	rows, err := t.deps.Groups.GroupsByID(ctx, actor, a.GroupIDs)
	if err != nil {
		return nil, err
	}
	var blocked []string
	for _, row := range rows {
		if !canPostFrom(row, strings.TrimSpace(a.NumberID)) {
			blocked = append(blocked, row.Group.Subject)
		}
	}
	if len(blocked) > 0 {
		return nil, fmt.Errorf("%w: o número não está ou não pode enviar nestes grupos: %s", copilottools.ErrInvalidArgs, strings.Join(blocked, ", "))
	}
	return rows, nil
}

func canPostFrom(row *uwguc.GroupRow, numberID string) bool {
	for _, seat := range row.Numbers {
		if seat.InstanceID == numberID {
			return seat.CanPost
		}
	}
	return false
}

func (t *createUnofficialGroupCampaignTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := copilottools.ValidateArgs[groupCampaignArgs](nil, cc, args)
	if err != nil {
		return err
	}
	if _, err := t.targets(ctx, cc, a); err != nil {
		return asInvalid(err)
	}
	scope, err := t.deps.Campaigns.scope(cc)
	if err != nil {
		return err
	}
	if _, err := t.deps.Campaigns.Usable.Usable(ctx, cc.WorkspaceID, scope, a.NumberID); err != nil {
		return fmt.Errorf("%w: %s", copilottools.ErrInvalidArgs, unofficialCampaignFailure(err).Message)
	}
	message, err := a.message(t.deps, cc)
	if err != nil {
		return err
	}
	if err := message.Validate(); err != nil {
		return fmt.Errorf("%w: %s", copilottools.ErrInvalidArgs, unofficialCampaignFailure(err).Message)
	}
	return nil
}

func asInvalid(err error) error {
	if errors.Is(err, copilottools.ErrInvalidArgs) {
		return err
	}
	return fmt.Errorf("%w: %s", copilottools.ErrInvalidArgs, groupFailure(err).Message)
}

func (t *createUnofficialGroupCampaignTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a groupCampaignArgs
	copilottools.BindArgs(args, &a)
	fields := []copilot.Field{
		{Key: "name", Value: strings.TrimSpace(a.Name)},
		{Key: "phone", Value: (&createUnofficialCampaignTool{deps: t.deps.Campaigns}).numberLabel(ctx, cc, a.NumberID)},
	}
	rows, err := t.targets(ctx, cc, a)
	if err != nil {
		return append(fields, copilot.Field{Key: "groups", Value: "grupos desconhecidos ou sem acesso"})
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Group.Subject)
	}
	fields = append(fields, copilot.Field{Key: "groups", Value: strings.Join(names, ", ")})
	if message, err := a.message(t.deps, cc); err == nil && message.MediaID != "" {
		fields = append(fields, copilot.Field{Key: "file", Value: message.FileName})
	}
	return fields
}

func (t *createUnofficialGroupCampaignTool) Preview(_ context.Context, _ copilot.Context, args map[string]interface{}) *copilot.Preview {
	var a groupCampaignArgs
	copilottools.BindArgs(args, &a)
	return messagePreview(a.Message, string(shared.EntryTypeUnofficialWhatsApp), "")
}

func (t *createUnofficialGroupCampaignTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a groupCampaignArgs
	if err := copilottools.DecodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	rows, err := t.targets(ctx, cc, a)
	if err != nil {
		return groupFailure(err)
	}
	scope, err := t.deps.Campaigns.scope(cc)
	if err != nil {
		return groupFailure(err)
	}
	scoped, err := creationContext(ctx, a.DepartmentID)
	if err != nil {
		return unofficialCampaignFailure(err)
	}
	message, err := a.message(t.deps, cc)
	if err != nil {
		return unofficialCampaignFailure(err)
	}
	targets := make([]uwc.TargetInput, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, uwc.TargetInput{GroupJID: row.Group.JID, Name: row.Group.Subject})
	}
	created, err := t.deps.Campaigns.Create.Execute(scoped, &uwc.Campaign{
		WorkspaceID: cc.WorkspaceID,
		InstanceID:  strings.TrimSpace(a.NumberID),
		CreatedByID: cc.UserID,
		Name:        a.Name,
		Audience:    uwc.AudienceGroups,
		Message:     message,
		Targets:     targets,
	}, scope)
	if err != nil {
		return unofficialCampaignFailure(err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"campaign_id": created.ID, "name": created.Name, "groups": len(targets)}}
}

type groupAnalysisArgs struct {
	GroupID          string `json:"group_id" req:"true" id:"true" desc:"group_id exato de list_unofficial_groups; o grupo precisa estar acompanhado"`
	Name             string `json:"name" req:"true" desc:"nome da análise"`
	Prompt           string `json:"prompt" req:"true" desc:"o foco da análise, no texto do usuário, por exemplo reclamações sobre entrega"`
	Trigger          string `json:"trigger" req:"true" enum:"message_count,scheduled" desc:"message_count roda a cada messages_per_cycle mensagens novas; scheduled roda uma vez por dia"`
	MessagesPerCycle int    `json:"messages_per_cycle" desc:"com message_count: mensagens novas por ciclo, de 1 a 1000; sem valor usa o padrão da análise"`
	ScheduleHour     int    `json:"schedule_hour" desc:"com scheduled: hora do dia no fuso do workspace, de 0 a 23"`
	ScheduleMinute   int    `json:"schedule_minute" desc:"com scheduled: minuto, de 0 a 59"`
}

func (a groupAnalysisArgs) input() uwguc.AnalysisInput {
	in := uwguc.AnalysisInput{
		GroupID: strings.TrimSpace(a.GroupID), Name: a.Name, Prompt: a.Prompt, Trigger: uwg.Trigger(a.Trigger),
		MessagesPerCycle: a.MessagesPerCycle, ScheduleHour: a.ScheduleHour, ScheduleMinute: a.ScheduleMinute,
	}
	return in
}

func when(a *uwg.Analysis) string {
	if a.Trigger == uwg.TriggerScheduled {
		return fmt.Sprintf("todo dia às %02d:%02d", a.ScheduleHour, a.ScheduleMinute)
	}
	return fmt.Sprintf("a cada %d mensagens novas", a.MessagesPerCycle)
}

type createUnofficialGroupAnalysisTool struct{ deps UnofficialGroupDeps }

func NewCreateUnofficialGroupAnalysisTool(deps UnofficialGroupDeps) copilot.Tool {
	return &createUnofficialGroupAnalysisTool{deps: deps}
}

func (t *createUnofficialGroupAnalysisTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceUnofficialWhatsAppGroups, Action: workspace.ActionConfigure}
}

func (t *createUnofficialGroupAnalysisTool) Definition() tools.Definition {
	return copilottools.Definition("create_unofficial_group_analysis",
		"Cria uma análise de IA contínua de um grupo acompanhado, sobre as mensagens que chegarem depois. Cada ciclo consome "+
			"saldo de IA do workspace e não roda sem saldo. Só depois da aprovação do usuário.",
		groupAnalysisArgs{})
}

func (t *createUnofficialGroupAnalysisTool) analysed(ctx context.Context, cc copilot.Context, a groupAnalysisArgs) (*uwguc.GroupRow, error) {
	actor, err := t.deps.actor(cc)
	if err != nil {
		return nil, err
	}
	rows, err := t.deps.Groups.GroupsByID(ctx, actor, []string{a.GroupID})
	if err != nil {
		return nil, err
	}
	if !rows[0].Group.Followed() {
		return nil, uwg.ErrGroupNotFollowed
	}
	return rows[0], nil
}

func (t *createUnofficialGroupAnalysisTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := copilottools.ValidateArgs[groupAnalysisArgs](nil, cc, args)
	if err != nil {
		return err
	}
	if _, err := t.analysed(ctx, cc, a); err != nil {
		return asInvalid(err)
	}
	if _, err := t.draft(cc, a); err != nil {
		return asInvalid(err)
	}
	return nil
}

func (t *createUnofficialGroupAnalysisTool) draft(cc copilot.Context, a groupAnalysisArgs) (*uwg.Analysis, error) {
	actor, err := t.deps.actor(cc)
	if err != nil {
		return nil, err
	}
	return t.deps.Groups.DraftAnalysis(actor, a.input())
}

func (t *createUnofficialGroupAnalysisTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a groupAnalysisArgs
	copilottools.BindArgs(args, &a)
	group := "grupo desconhecido ou sem acesso"
	if row, err := t.analysed(ctx, cc, a); err == nil {
		group = row.Group.Subject
	}
	fields := []copilot.Field{
		{Key: "group", Value: group},
		{Key: "name", Value: strings.TrimSpace(a.Name)},
		{Key: "description", Value: strings.TrimSpace(a.Prompt)},
	}
	if draft, err := t.draft(cc, a); err == nil {
		fields = append(fields, copilot.Field{Key: "when", Value: when(draft)})
	}
	return append(fields, copilot.Field{Key: "cost", Value: "cada ciclo consome saldo de IA do workspace"})
}

func (t *createUnofficialGroupAnalysisTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a groupAnalysisArgs
	if err := copilottools.DecodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	actor, err := t.deps.actor(cc)
	if err != nil {
		return groupFailure(err)
	}
	analysis, err := t.deps.Groups.CreateAnalysis(ctx, actor, a.input())
	if err != nil {
		return groupFailure(err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"analysis_id": analysis.ID, "name": analysis.Name, "when": when(analysis)}}
}

func groupFailure(err error) copilot.Result {
	switch {
	case errors.Is(err, copilottools.ErrInvalidArgs):
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	case errors.Is(err, uwg.ErrGroupNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: "grupo desconhecido ou de outro departamento; use o group_id de list_unofficial_groups"}
	case errors.Is(err, uwg.ErrGroupNotFollowed):
		return copilot.Result{Status: copilot.StatusError, Message: "o grupo não está acompanhado; peça ao usuário para acompanhar o grupo na tela de grupos"}
	case errors.Is(err, uw.ErrInstanceNotFound), errors.Is(err, uwg.ErrNumberOutOfScope):
		return copilot.Result{Status: copilot.StatusError, Message: "número desconhecido ou de outro departamento"}
	case errors.Is(err, uwg.ErrAnalysisNameRequired), errors.Is(err, uwg.ErrAnalysisPromptRequired),
		errors.Is(err, uwg.ErrMessagesPerCycleRange), errors.Is(err, uwg.ErrScheduleInvalid),
		errors.Is(err, uwg.ErrTriggerInvalid), errors.Is(err, uwg.ErrPromptTooLong):
		return copilot.Result{Status: copilot.StatusError, Message: "a análise não pode ser criada assim: " + err.Error()}
	}
	log.Printf("[copilot] group tool failed: %v", err)
	return copilot.Result{Status: copilot.StatusError, Message: "falha na operação dos grupos"}
}
