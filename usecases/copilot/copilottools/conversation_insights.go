package copilottools

import (
	"context"

	"vozko/domain/attendance"
	"vozko/domain/audience"
	"vozko/domain/copilot"
	"vozko/domain/shared"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

const defaultInsightExamples = 3

type conversationInsightsTool struct {
	stats audience.StatsUseCase
	list  audience.ListUseCase
	now   Clock
}

func NewConversationInsightsTool(stats audience.StatsUseCase, list audience.ListUseCase, now Clock) copilot.Tool {
	return &conversationInsightsTool{stats: stats, list: list, now: now}
}

func (t *conversationInsightsTool) Meta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceAudience, Action: workspace.ActionRead}
}

func (t *conversationInsightsTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "conversation_insights",
		Description: "O que as análises de IA já feitas sobre as conversas dizem no período (a versão mais recente de cada " +
			"conversa): interesse, desfecho, qualificação do lead, próxima ação, sentimento, nota média de qualidade do " +
			"atendimento (0 a 100) e os assuntos mais citados, mais até 5 exemplos resumidos. Use para explicar o PORQUÊ dos " +
			"números de atendimento. Os assuntos ficam em um dataset para render_chart.",
		Parameters: map[string]tools.Parameter{
			"date_from":     {Type: "string", Description: "início YYYY-MM-DD; omita para o período da tela (ou os últimos 30 dias)"},
			"date_to":       {Type: "string", Description: "fim YYYY-MM-DD (inclusivo)"},
			"interest":      {Type: "string", Description: "filtra por interesse", Enum: []string{string(audience.InterestInterested), string(audience.InterestNotInterested), string(audience.InterestUndecided)}},
			"disposition":   {Type: "string", Description: "filtra por desfecho", Enum: []string{string(audience.DispositionSale), string(audience.DispositionFillingInfo), string(audience.DispositionCallback), string(audience.DispositionDeclined), string(audience.DispositionPending)}},
			"qualification": {Type: "string", Description: "filtra por qualificação", Enum: []string{string(audience.QualificationHotLead), string(audience.QualificationWarmLead), string(audience.QualificationColdLead)}},
			"next_action":   {Type: "string", Description: "filtra por próxima ação", Enum: []string{string(audience.NextActionScheduleCallback), string(audience.NextActionSendWhatsApp), string(audience.NextActionClose), string(audience.NextActionEscalate), string(audience.NextActionContinue)}},
			"examples":      {Type: "integer", Description: "quantos exemplos resumidos trazer (0 a 5, padrão 3)"},
		},
	}
}

func (t *conversationInsightsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	if !cc.Departments.SeesWholeWorkspace() {
		return copilot.Result{Status: copilot.StatusDenied, Message: "as análises de conversa não são separadas por departamento; só quem vê o workspace inteiro pode consultá-las"}
	}
	window, err := attendance.ParseWindow(
		onScreen(argString(args, "date_from"), cc.View.DateFrom),
		onScreen(argString(args, "date_to"), cc.View.DateTo),
		t.now(),
	)
	if err != nil {
		return analyticsFailure("conversation_insights", err)
	}
	in := audience.LatestConversationAnalyses(cc.WorkspaceID, &window.From, &window.To)
	if msg := applyInsightFilters(&in, args); msg != "" {
		return copilot.Result{Status: copilot.StatusError, Message: msg}
	}

	stats, err := t.stats.Execute(ctx, in)
	if err != nil {
		return analyticsFailure("conversation_insights", err)
	}
	digest := audience.DigestConversations(stats)
	examples, err := t.examples(ctx, in, exampleCount(args))
	if err != nil {
		return analyticsFailure("conversation_insights", err)
	}
	subjects, err := subjectsDataset(digest.Subjects)
	if err != nil {
		return analyticsFailure("conversation_insights", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"query": map[string]interface{}{
			"date_from": window.From.Format(attendance.DayLayout),
			"date_to":   window.To.Format(attendance.DayLayout),
			"scope":     "workspace",
		},
		"insights":         digest,
		"subjects_dataset": keep(cc, subjects).Handle(),
		"examples":         examples,
	}}
}

func applyInsightFilters(in *audience.ListInput, args map[string]interface{}) string {
	if v := audience.Interest(argString(args, "interest")); v != "" {
		if !v.Valid() {
			return "interest inválido"
		}
		in.Interest = v
	}
	if v := audience.Disposition(argString(args, "disposition")); v != "" {
		if !v.Valid() {
			return "disposition inválido"
		}
		in.Disposition = v
	}
	if v := audience.Qualification(argString(args, "qualification")); v != "" {
		if !v.Valid() {
			return "qualification inválido"
		}
		in.Qualification = v
	}
	if v := audience.NextAction(argString(args, "next_action")); v != "" {
		if !v.Valid() {
			return "next_action inválido"
		}
		in.NextAction = v
	}
	return ""
}

func exampleCount(args map[string]interface{}) int {
	if _, given := args["examples"]; !given {
		return defaultInsightExamples
	}
	n := argInt(args, "examples")
	switch {
	case n < 0:
		return 0
	case n > audience.MaxDigestExamples:
		return audience.MaxDigestExamples
	}
	return n
}

func (t *conversationInsightsTool) examples(ctx context.Context, in audience.ListInput, n int) ([]audience.ConversationExample, error) {
	out := []audience.ConversationExample{}
	if n == 0 {
		return out, nil
	}
	in.Options.Pagination = shared.Pagination{Page: 1, PageSize: n}
	page, err := t.list.Execute(ctx, in)
	if err != nil {
		return nil, err
	}
	if page == nil {
		return out, nil
	}
	for _, a := range page.Items {
		if len(out) == n {
			break
		}
		out = append(out, audience.DigestExample(a))
	}
	return out, nil
}

func subjectsDataset(subjects []audience.SubjectCount) (*copilot.Dataset, error) {
	d := copilot.NewDataset("conversation subjects", []copilot.Column{
		{Key: "subject", Label: "subject", Kind: copilot.ColumnText},
		{Key: "conversations", Label: "conversations", Kind: copilot.ColumnNumber},
	})
	for _, s := range subjects {
		if err := d.AddRow(s.Label, s.Count); err != nil {
			return nil, err
		}
	}
	return d, nil
}
