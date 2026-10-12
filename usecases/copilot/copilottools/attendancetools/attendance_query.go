package attendancetools

import (
	"context"
	"errors"
	"log"

	"github.com/google/uuid"

	"vozko/domain/actor"
	"vozko/domain/attendance"
	"vozko/domain/cache"
	"vozko/domain/copilot"
	"vozko/domain/shared"
	"vozko/domain/tools"
	wd "vozko/domain/workspace/workspace_department"
	"vozko/usecases/copilot/copilottools"
)

const allScope = "all"

func attendanceParams() map[string]tools.Parameter {
	return map[string]tools.Parameter{
		"date_from":     {Type: "string", Description: "início YYYY-MM-DD; omita para usar o período da tela (ou os últimos 30 dias). Máximo de 366 dias por consulta."},
		"date_to":       {Type: "string", Description: "fim YYYY-MM-DD (inclusivo); omita para usar o da tela ou hoje"},
		"department_id": {Type: "string", Description: "id de departamento (list_departments); omita para usar o da tela; \"all\" para o workspace inteiro"},
		"member_id":     {Type: "string", Description: "id de um membro (de attendance_team); omita para usar o da tela; \"all\" para todos"},
		"channel":       {Type: "string", Description: "canal; omita para usar o da tela; \"all\" para todos", Enum: channelOptions()},
		"campaign_id":   {Type: "string", Description: "id de uma campanha de disparo (de campaign_dispatch); omita para usar a da tela; \"all\" para todas"},
	}
}

func withParams(base map[string]tools.Parameter, extra map[string]tools.Parameter) map[string]tools.Parameter {
	for k, v := range extra {
		base[k] = v
	}
	return base
}

func onScreen(arg, view string) string {
	switch arg {
	case allScope:
		return ""
	case "":
		return view
	}
	return arg
}

type AttendanceDeps struct {
	Sections    attendance.OverviewSectionsUseCase
	Departments wd.ListDepartmentsUseCase
	Now         copilottools.Clock
}

var (
	errUnknownDepartment = errors.New("departamento inexistente neste workspace: use list_departments e copie o id exatamente, nunca invente")
	errUnknownMember     = errors.New("member_id inválido: use o member_id devolvido por attendance_team, nunca invente")
	errUnknownChannel    = errors.New("canal inválido: use um dos canais listados no parâmetro channel")
	errUnknownCampaign   = errors.New("campaign_id inválido: use o id devolvido por campaign_dispatch, nunca invente")
)

type attendanceQuery struct {
	window attendance.Window
	filter attendance.OverviewFilter
}

func (d AttendanceDeps) resolve(ctx context.Context, cc copilot.Context, args map[string]interface{}) (attendanceQuery, error) {
	window, err := attendance.ParseWindow(
		onScreen(copilottools.ArgString(args, "date_from"), cc.View.DateFrom),
		onScreen(copilottools.ArgString(args, "date_to"), cc.View.DateTo),
		d.Now(),
	)
	if err != nil {
		return attendanceQuery{}, err
	}
	department, err := cc.Departments.ReadScope(onScreen(copilottools.ArgString(args, "department_id"), cc.View.DepartmentID))
	if err != nil {
		return attendanceQuery{}, err
	}
	if err := d.checkDepartment(cc.WorkspaceID, department); err != nil {
		return attendanceQuery{}, err
	}
	member := onScreen(copilottools.ArgString(args, "member_id"), cc.View.MemberID)
	if member != "" && !wellFormedActor(member) {
		return attendanceQuery{}, errUnknownMember
	}
	channel := onScreen(copilottools.ArgString(args, "channel"), cc.View.Channel)
	if channel != "" && !shared.EntryType(channel).SupportsConversationView() {
		return attendanceQuery{}, errUnknownChannel
	}
	campaign := onScreen(copilottools.ArgString(args, "campaign_id"), cc.View.CampaignID)
	if campaign != "" {
		if _, err := uuid.Parse(campaign); err != nil {
			return attendanceQuery{}, errUnknownCampaign
		}
	}
	filter := attendance.OverviewFilter{
		DepartmentID: department,
		MemberID:     member,
		Channel:      channel,
		CampaignID:   campaign,
		CampaignType: campaignTypeFor(campaign, cc.View),
		IncludeAI:    includeAI(args, cc.View),
	}
	window.Apply(&filter)
	return attendanceQuery{window: window, filter: filter}, nil
}

func (d AttendanceDeps) checkDepartment(workspaceID, departmentID string) error {
	if departmentID == "" {
		return nil
	}
	departments, err := d.Departments.Execute(workspaceID)
	if err != nil {
		return err
	}
	for _, dept := range departments {
		if dept.ID == departmentID {
			return nil
		}
	}
	return errUnknownDepartment
}

func wellFormedActor(id string) bool {
	raw, _ := actor.Split(id)
	_, err := uuid.Parse(raw)
	return err == nil
}

func channelOptions() []string {
	types := shared.ConversationViewableEntryTypes()
	out := make([]string, 0, len(types)+1)
	for _, t := range types {
		out = append(out, string(t))
	}
	return append(out, allScope)
}

func (q attendanceQuery) forWindow(w attendance.Window) attendanceQuery {
	next := q
	next.window = w
	w.Apply(&next.filter)
	return next
}

func (q attendanceQuery) describe() map[string]interface{} {
	scope := map[string]interface{}{
		"date_from": q.window.From.Format(attendance.DayLayout),
		"date_to":   q.window.To.Format(attendance.DayLayout),
		"days":      q.window.Days(),
	}
	scope["department_id"] = orAll(q.filter.DepartmentID)
	scope["member_id"] = orAll(q.filter.MemberID)
	scope["channel"] = orAll(q.filter.Channel)
	return scope
}

func orAll(v string) string {
	if v == "" {
		return allScope
	}
	return v
}

func analyticsFailure(tool string, err error) copilot.Result {
	if res, ok := copilottools.DepartmentFailure(err); ok {
		return res
	}
	switch {
	case errors.Is(err, attendance.ErrInvalidWindow), errors.Is(err, attendance.ErrUnknownMetric), errors.Is(err, attendance.ErrUnknownBlock),
		errors.Is(err, errUnknownDepartment), errors.Is(err, errUnknownMember), errors.Is(err, errUnknownChannel), errors.Is(err, errUnknownCampaign):
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	case errors.Is(err, cache.ErrGateBusy):
		return copilot.Result{Status: copilot.StatusError, Message: "as análises estão ocupadas agora; tente novamente em alguns segundos ou responda com o que já tem"}
	case errors.Is(err, context.DeadlineExceeded):
		return copilot.Result{Status: copilot.StatusError, Message: "a consulta demorou demais; use um período menor"}
	case errors.Is(err, context.Canceled):
		return copilot.Result{Status: copilot.StatusError, Message: "consulta cancelada"}
	}
	log.Printf("[copilot] %s failed: %v", tool, err)
	return copilot.Result{Status: copilot.StatusError, Message: "falha ao consultar os dados de atendimento"}
}

func columnKindOf(kind attendance.MetricKind) copilot.ColumnKind {
	switch kind {
	case attendance.MetricKindPercent:
		return copilot.ColumnPercent
	case attendance.MetricKindMinutes:
		return copilot.ColumnMinutes
	case attendance.MetricKindMoney:
		return copilot.ColumnMoney
	}
	return copilot.ColumnNumber
}

func campaignTypeFor(campaignID string, view copilot.View) string {
	if campaignID == "" || campaignID != view.CampaignID {
		return ""
	}
	return view.CampaignType
}

func includeAI(args map[string]interface{}, view copilot.View) bool {
	if explicit := copilottools.ArgBoolPtr(args, "include_ai"); explicit != nil {
		return *explicit
	}
	if view.IncludeAI != nil {
		return *view.IncludeAI
	}
	return true
}
