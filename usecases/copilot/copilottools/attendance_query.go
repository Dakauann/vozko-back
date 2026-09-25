package copilottools

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"

	"vozko/domain/actor"
	"vozko/domain/attendance"
	"vozko/domain/cache"
	"vozko/domain/copilot"
	"vozko/domain/shared"
	"vozko/domain/tools"
	wd "vozko/domain/workspace/workspace_department"
)

type Clock func() time.Time

const allScope = "all"

func attendanceParams() map[string]tools.Parameter {
	return map[string]tools.Parameter{
		"date_from":     {Type: "string", Description: "início YYYY-MM-DD; omita para usar o período da tela (ou os últimos 30 dias). Máximo de 366 dias por consulta."},
		"date_to":       {Type: "string", Description: "fim YYYY-MM-DD (inclusivo); omita para usar o da tela ou hoje"},
		"department_id": {Type: "string", Description: "id de departamento (list_departments); omita para usar o da tela; \"all\" para o workspace inteiro"},
		"member_id":     {Type: "string", Description: "id de um membro (de attendance_team); omita para usar o da tela; \"all\" para todos"},
		"channel":       {Type: "string", Description: "canal; omita para usar o da tela; \"all\" para todos", Enum: channelOptions()},
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
	Now         Clock
}

var (
	errUnknownDepartment = errors.New("departamento inexistente neste workspace: use list_departments e copie o id exatamente, nunca invente")
	errUnknownMember     = errors.New("member_id inválido: use o member_id devolvido por attendance_team, nunca invente")
	errUnknownChannel    = errors.New("canal inválido: use um dos canais listados no parâmetro channel")
)

type attendanceQuery struct {
	window attendance.Window
	filter attendance.OverviewFilter
}

func (d AttendanceDeps) resolve(ctx context.Context, cc copilot.Context, args map[string]interface{}) (attendanceQuery, error) {
	window, err := attendance.ParseWindow(
		onScreen(argString(args, "date_from"), cc.View.DateFrom),
		onScreen(argString(args, "date_to"), cc.View.DateTo),
		d.Now(),
	)
	if err != nil {
		return attendanceQuery{}, err
	}
	department, err := cc.Departments.ReadScope(onScreen(argString(args, "department_id"), cc.View.DepartmentID))
	if err != nil {
		return attendanceQuery{}, err
	}
	if err := d.checkDepartment(cc.WorkspaceID, department); err != nil {
		return attendanceQuery{}, err
	}
	member := onScreen(argString(args, "member_id"), cc.View.MemberID)
	if member != "" && !wellFormedActor(member) {
		return attendanceQuery{}, errUnknownMember
	}
	channel := onScreen(argString(args, "channel"), cc.View.Channel)
	if channel != "" && !shared.EntryType(channel).SupportsConversationView() {
		return attendanceQuery{}, errUnknownChannel
	}
	filter := attendance.OverviewFilter{
		DepartmentID: department,
		MemberID:     member,
		Channel:      channel,
		IncludeAI:    true,
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
	switch {
	case errors.Is(err, attendance.ErrInvalidWindow), errors.Is(err, attendance.ErrUnknownMetric),
		errors.Is(err, errUnknownDepartment), errors.Is(err, errUnknownMember), errors.Is(err, errUnknownChannel):
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	case errors.Is(err, wd.ErrDepartmentAccessDenied):
		return copilot.Result{Status: copilot.StatusDenied, Message: "sem acesso a este departamento; o usuário só vê os próprios departamentos"}
	case errors.Is(err, wd.ErrDepartmentRequired):
		return copilot.Result{Status: copilot.StatusError, Message: "o usuário pertence a mais de um departamento: pergunte qual (list_departments) e passe department_id"}
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

func keep(cc copilot.Context, d *copilot.Dataset) *copilot.Dataset {
	if cc.Datasets == nil {
		return d
	}
	return cc.Datasets.Put(d)
}
