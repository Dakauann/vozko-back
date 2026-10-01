package copilottools

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"vozko/domain/calls/callhistory"
	"vozko/domain/calls/cdr"
	"vozko/domain/copilot"
	"vozko/domain/shared"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	callhistory_usecase "vozko/usecases/callhistory"
)

const maxCallPageSize = 50

type CallHistory interface {
	List(ctx context.Context, input callhistory_usecase.ListInput) (*shared.PaginatedResult[callhistory.Summary], error)
	Get(ctx context.Context, viewer callhistory_usecase.Viewer, callID string) (*callhistory.Detail, error)
}

type CallHistoryDeps struct {
	History     CallHistory
	Permissions callhistory_usecase.Permissions
}

func (d CallHistoryDeps) viewer(cc copilot.Context) callhistory_usecase.Viewer {
	return callhistory_usecase.ViewerFor(d.Permissions, cc.WorkspaceID, cc.UserID)
}

func callHistoryMeta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceCallHistory, Action: workspace.ActionRead}
}

type listCallsArgs struct {
	Direction string `json:"direction" enum:"inbound,outbound" desc:"recebidas (inbound) ou feitas (outbound); omita para as duas"`
	Channel   string `json:"channel" enum:"phone,whatsapp" desc:"pela linha telefônica (phone) ou pelo WhatsApp; omita para os dois"`
	Result    string `json:"result" enum:"answered,unanswered" desc:"atendidas ou não atendidas; omita para todas"`
	MemberID  string `json:"member_id" id:"true" desc:"user_id de list_workspace_members para ver as ligações de que essa pessoa participou; só vale para quem vê as ligações da equipe"`
	DateFrom  string `json:"date_from" desc:"início YYYY-MM-DD"`
	DateTo    string `json:"date_to" desc:"fim YYYY-MM-DD (inclui o dia todo)"`
	Number    string `json:"number" desc:"parte do número do contato, só se o usuário der o número"`
	Page      int    `json:"page" desc:"página, começa em 1"`
	PageSize  int    `json:"page_size" desc:"ligações por página, até 50 (padrão 20)"`
}

func (a listCallsArgs) input(viewer callhistory_usecase.Viewer) (callhistory_usecase.ListInput, error) {
	input := callhistory_usecase.ListInput{
		Viewer:   viewer,
		Page:     a.Page,
		PageSize: min(a.PageSize, maxCallPageSize),
		MemberID: strings.TrimSpace(a.MemberID),
		Number:   strings.TrimSpace(a.Number),
	}
	if a.Direction != "" {
		direction := cdr.Direction(a.Direction)
		input.Direction = &direction
	}
	if a.Channel != "" {
		channel := callhistory.Channel(a.Channel)
		input.Channel = &channel
	}
	if a.Result != "" {
		answered := a.Result == "answered"
		input.Answered = &answered
	}
	var err error
	if input.From, err = dayBound(a.DateFrom, false); err != nil {
		return input, err
	}
	input.To, err = dayBound(a.DateTo, true)
	return input, err
}

func dayBound(raw string, endOfDay bool) (*time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	bound := shared.ParseDateBound(raw, endOfDay)
	if bound == nil {
		return nil, errors.New("data inválida: use YYYY-MM-DD")
	}
	return bound, nil
}

type listCallsTool struct{ deps CallHistoryDeps }

func NewListCallsTool(deps CallHistoryDeps) copilot.Tool { return &listCallsTool{deps: deps} }

func (t *listCallsTool) Meta() copilot.Meta { return callHistoryMeta() }

func (t *listCallsTool) Definition() tools.Definition {
	return definition("list_calls",
		"Lista as ligações mais recentes, pela linha telefônica e pelo WhatsApp: quem ligou, quem atendeu, se foi atendida, "+
			"quanto durou, quantas transferências teve e o valor cobrado. Quem não vê as ligações da equipe recebe só as de que participou.",
		listCallsArgs{})
}

func (t *listCallsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a listCallsArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	viewer := t.deps.viewer(cc)
	input, err := a.input(viewer)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	page, err := t.deps.History.List(ctx, input)
	if err != nil {
		return callHistoryFailure("list_calls", err)
	}
	calls := make([]map[string]interface{}, 0, len(page.Items))
	for _, summary := range page.Items {
		calls = append(calls, callSummaryData(summary))
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"calls":       calls,
		"page":        page.Page,
		"total_calls": page.TotalItems,
		"total_pages": page.TotalPages,
		"scope":       callScope(viewer),
	}}
}

func callScope(viewer callhistory_usecase.Viewer) string {
	if viewer.SeesEveryone {
		return "ligações de toda a equipe"
	}
	return "só as ligações de que o usuário participou"
}

type getCallArgs struct {
	CallID string `json:"call_id" req:"true" desc:"call_id exato de list_calls"`
}

type getCallTool struct{ deps CallHistoryDeps }

func NewGetCallTool(deps CallHistoryDeps) copilot.Tool { return &getCallTool{deps: deps} }

func (t *getCallTool) Meta() copilot.Meta { return callHistoryMeta() }

func (t *getCallTool) Definition() tools.Definition {
	return definition("get_call",
		"Conta a história de uma ligação: quem participou e a linha do tempo (início, atendimento, cada transferência e o que aconteceu com ela, fim).",
		getCallArgs{})
}

func (t *getCallTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a getCallArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	detail, err := t.deps.History.Get(ctx, t.deps.viewer(cc), strings.TrimSpace(a.CallID))
	if err != nil {
		return callHistoryFailure("get_call", err)
	}
	data := callSummaryData(detail.Summary)
	handlers := make([]string, 0, len(detail.Handlers))
	for _, person := range detail.Handlers {
		handlers = append(handlers, person.Name)
	}
	data["handled_by"] = handlers
	timeline := make([]map[string]interface{}, 0, len(detail.Timeline))
	for _, entry := range detail.Timeline {
		timeline = append(timeline, timelineData(entry))
	}
	data["timeline"] = timeline
	data["has_recording"] = detail.Recording != nil
	return copilot.Result{Status: copilot.StatusOK, Data: data}
}

func callSummaryData(s callhistory.Summary) map[string]interface{} {
	data := map[string]interface{}{
		"call_id":      s.CallID,
		"direction":    s.Direction,
		"channel":      s.Channel,
		"result":       s.Outcome,
		"started_at":   s.StartedAt.Format(time.RFC3339),
		"talk_seconds": s.TalkSeconds,
		"ring_seconds": s.RingSeconds,
		"contact":      contactName(s.Contact),
		"placed_by":    personName(s.PlacedBy),
		"answered_by":  personName(s.AnsweredBy),
		"transfers":    s.Transfers,
		"charged_brl":  nil,
	}
	if s.Contact.LeadID != "" {
		data["lead_id"] = s.Contact.LeadID
	}
	if s.Charge != nil {
		data["charged_brl"] = float64(s.Charge.Micros) / 1_000_000
		data["charge_settled"] = s.Charge.Settled
	}
	return data
}

func timelineData(entry callhistory.NamedEntry) map[string]interface{} {
	data := map[string]interface{}{"event": entry.Kind, "at": entry.At.Format(time.RFC3339)}
	if entry.Actor != nil {
		data["by"] = entry.Actor.Name
	}
	if entry.Target != nil {
		data["to"] = entry.Target.Name
	}
	if entry.QueueName != "" {
		data["queue"] = entry.QueueName
	}
	if entry.Notes != "" {
		data["notes"] = entry.Notes
	}
	if entry.Reason != "" {
		data["reason"] = entry.Reason
	}
	return data
}

func contactName(contact callhistory.Contact) string {
	if name := strings.TrimSpace(contact.Name); name != "" {
		return name
	}
	return contact.Number
}

func personName(person *callhistory.Person) interface{} {
	if person == nil {
		return nil
	}
	return person.Name
}

func callHistoryFailure(tool string, err error) copilot.Result {
	if errors.Is(err, cdr.ErrCallNotFound) {
		return copilot.Result{Status: copilot.StatusError, Message: "ligação não encontrada; use um call_id de list_calls (quem não vê as ligações da equipe só encontra as de que participou)"}
	}
	log.Printf("[copilot] %s failed: %v", tool, err)
	return copilot.Result{Status: copilot.StatusError, Message: "não foi possível ler as ligações agora"}
}
