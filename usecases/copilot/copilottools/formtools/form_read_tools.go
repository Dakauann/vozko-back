package formtools

import (
	"context"
	"sort"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/tools"
	wf "vozko/domain/whatsappform"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

const responsesPageSize = 20

type listWhatsAppFormsArgs struct {
	Search          string `json:"search" desc:"opcional: parte do nome do formulário"`
	IncludeArchived bool   `json:"include_archived" desc:"true inclui os arquivados"`
}

type listWhatsAppFormsTool struct {
	gate
	deps FormDeps
}

func NewListWhatsAppFormsTool(deps FormDeps) copilot.Tool {
	return &listWhatsAppFormsTool{gate: gated(false, workspace.ResourceWhatsAppForms, workspace.ActionRead, wf.CapabilityView), deps: deps}
}

func (t *listWhatsAppFormsTool) Definition() tools.Definition {
	return copilottools.Definition("list_whatsapp_forms",
		"Lista os formulários do WhatsApp (WhatsApp Flows) do workspace: nome, se tem versão publicada, a situação em cada conta do WhatsApp oficial "+
			"(published, throttled, blocked, failed, deprecated), quantos envios e respostas e a última resposta. Use o form_id exato nas outras ferramentas de formulário.",
		listWhatsAppFormsArgs{})
}

func (t *listWhatsAppFormsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var in listWhatsAppFormsArgs
	if err := copilottools.DecodeArgs(args, &in); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	summaries, err := t.deps.Forms.List(ctx, actorOf(cc), strings.TrimSpace(in.Search), in.IncludeArchived)
	if err != nil {
		return failure(err)
	}
	listed := make([]map[string]interface{}, 0, len(summaries))
	for _, s := range summaries {
		listed = append(listed, formSummary(s))
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"forms": listed, "total": len(listed)}}
}

func formSummary(s wf.FormSummary) map[string]interface{} {
	accounts := make([]map[string]interface{}, 0, len(s.Publications))
	for _, p := range s.Publications {
		accounts = append(accounts, map[string]interface{}{"account": p.MetaWABAID, "status": string(p.Status)})
	}
	row := map[string]interface{}{
		"form_id": s.Form.ID, "name": s.Form.Name, "published": s.Form.PublishedVersionID != "",
		"accounts": accounts, "sent": s.Sent, "responses": s.Responses,
	}
	if s.Form.Description != "" {
		row["description"] = s.Form.Description
	}
	if s.Form.ArchivedAt != nil {
		row["archived"] = true
	}
	if s.LastResponseAt != nil {
		row["last_response"] = stamp(*s.LastResponseAt)
	}
	return row
}

type formResultsArgs struct {
	formRef
	periodArgs
}

type whatsAppFormResultsTool struct {
	gate
	deps FormDeps
}

func NewWhatsAppFormResultsTool(deps FormDeps) copilot.Tool {
	return &whatsAppFormResultsTool{gate: gated(false, workspace.ResourceWhatsAppFormResponses, workspace.ActionRead, wf.CapabilityResponses), deps: deps}
}

func (t *whatsAppFormResultsTool) Definition() tools.Definition {
	return copilottools.Definition("whatsapp_form_results",
		"Desempenho de um formulário do WhatsApp no período, só nas conversas que o usuário alcança: envios por status, entregues, lidos, respostas, "+
			"tempo mediano até a resposta em segundos, envios e respostas por origem, quantas respostas escolheram cada opção de cada pergunta de escolha "+
			"e o dataset by_day (envios e respostas por dia) para render_chart. Formulários sem servidor não dizem em qual tela a pessoa parou.",
		formResultsArgs{})
}

func (t *whatsAppFormResultsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var in formResultsArgs
	if err := copilottools.DecodeArgs(args, &in); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	period, err := in.period()
	if err != nil {
		return failure(err)
	}
	a := actorOf(cc)
	detail, err := in.resolve(ctx, t.deps.Forms, a)
	if err != nil {
		return failure(err)
	}
	stats, err := t.deps.Forms.Stats(ctx, a, detail.Form.ID, period)
	if err != nil {
		return failure(err)
	}
	choices, err := t.choiceFields(ctx, a, detail, period)
	if err != nil {
		return failure(err)
	}
	sends := make(map[string]int64, len(stats.Sends))
	for status, n := range stats.Sends {
		sends[string(status)] = n
	}
	origins := make([]map[string]interface{}, 0, len(stats.ByOrigin))
	for _, o := range stats.ByOrigin {
		origins = append(origins, map[string]interface{}{"origin": string(o.Origin), "sent": o.Sent, "responses": o.Responses})
	}
	data := map[string]interface{}{
		"form_id": detail.Form.ID, "name": detail.Form.Name, "period": in.shown(),
		"sends": sends, "delivered": stats.Delivered, "read": stats.Read, "responses": stats.Responses,
		"by_origin": origins, "choice_fields": choices,
	}
	if stats.MedianSecondsToRespond != nil {
		data["median_seconds_to_respond"] = *stats.MedianSecondsToRespond
	}
	if d, err := dailyDataset(stats.ByDay); err == nil {
		data["by_day"] = copilottools.Keep(cc, d).Handle()
	}
	return copilot.Result{Status: copilot.StatusOK, Data: data}
}

func (t *whatsAppFormResultsTool) choiceFields(ctx context.Context, a whatsappform_usecase.Actor, detail whatsappform_usecase.FormDetail, p whatsappform_usecase.Period) ([]map[string]interface{}, error) {
	out := []map[string]interface{}{}
	for _, ref := range documentOf(detail).Fields() {
		field := ref.Field
		if !field.Kind.HasOptions() {
			continue
		}
		counts, err := t.deps.Forms.Distribution(ctx, a, detail.Form.ID, field.Key, p)
		if err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{"field_key": field.Key, "question": field.Prompt(), "options": optionCounts(field, counts)})
	}
	return out, nil
}

func optionCounts(field wf.Field, counts map[string]int64) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(counts))
	known := map[string]bool{}
	for _, o := range field.Options {
		known[o.ID] = true
		out = append(out, map[string]interface{}{"option": o.Title, "responses": counts[o.ID]})
	}
	var others []string
	for id := range counts {
		if !known[id] {
			others = append(others, id)
		}
	}
	sort.Strings(others)
	for _, id := range others {
		out = append(out, map[string]interface{}{"option": id, "responses": counts[id], "retired": true})
	}
	return out
}

func dailyDataset(days []wf.DayStat) (*copilot.Dataset, error) {
	d := copilot.NewDataset("Envios e respostas do formulário por dia", []copilot.Column{
		{Key: "day", Label: "Dia", Kind: copilot.ColumnDate},
		{Key: "sent", Label: "Envios", Kind: copilot.ColumnNumber},
		{Key: "responses", Label: "Respostas", Kind: copilot.ColumnNumber},
	})
	for _, day := range days {
		if err := d.AddRow(day.Day, day.Sent, day.Responses); err != nil {
			return nil, err
		}
	}
	return d, nil
}

type formResponsesArgs struct {
	formRef
	periodArgs
	Search string `json:"search" desc:"opcional: nome do lead, número ou texto das respostas"`
	After  string `json:"after" desc:"opcional: o next da página anterior, para continuar"`
}

type whatsAppFormResponsesTool struct {
	gate
	deps FormDeps
}

func NewWhatsAppFormResponsesTool(deps FormDeps) copilot.Tool {
	return &whatsAppFormResponsesTool{gate: gated(false, workspace.ResourceWhatsAppFormResponses, workspace.ActionRead, wf.CapabilityResponses), deps: deps}
}

func (t *whatsAppFormResponsesTool) Definition() tools.Definition {
	return copilottools.Definition("whatsapp_form_responses",
		"Respostas recentes de um formulário do WhatsApp, da mais nova para a mais antiga, só das conversas que o usuário alcança, com o lead, "+
			"a conversa (entry_id e entry_type) e cada pergunta com a resposta. Respostas sensíveis, fotos e documentos aparecem como •••• para quem "+
			"não pode vê-las; nunca tente descobrir o conteúdo. Quando houver next, passe em after para a página seguinte.",
		formResponsesArgs{})
}

func (t *whatsAppFormResponsesTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var in formResponsesArgs
	if err := copilottools.DecodeArgs(args, &in); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	period, err := in.period()
	if err != nil {
		return failure(err)
	}
	a := actorOf(cc)
	detail, err := in.resolve(ctx, t.deps.Forms, a)
	if err != nil {
		return failure(err)
	}
	page, err := t.deps.Forms.ListResponses(ctx, a, wf.ResponseQuery{
		FormID: detail.Form.ID, From: period.From, To: period.To, Search: strings.TrimSpace(in.Search),
		After: strings.TrimSpace(in.After), Limit: responsesPageSize,
	})
	if err != nil {
		return failure(err)
	}
	docs := []wf.Document{documentOf(detail), detail.Draft.Document}
	rows := make([]map[string]interface{}, 0, len(page.Items))
	for _, item := range page.Items {
		rows = append(rows, responseRow(docs, item))
	}
	data := map[string]interface{}{"form_id": detail.Form.ID, "name": detail.Form.Name, "responses": rows}
	if page.Next != "" {
		data["next"] = page.Next
	}
	return copilot.Result{Status: copilot.StatusOK, Data: data}
}

func responseRow(docs []wf.Document, item whatsappform_usecase.ResponseRow) map[string]interface{} {
	r := item.Response
	row := map[string]interface{}{"response_id": r.ID, "submitted_at": stamp(r.SubmittedAt), "contact": r.ContactNumber, "answers": answerRows(docs, r.Answers)}
	if item.LeadName != "" {
		row["lead_name"] = item.LeadName
	}
	if r.LeadID != "" {
		row["lead_id"] = r.LeadID
	}
	if r.EntryID != "" {
		row["entry_id"], row["entry_type"] = r.EntryID, r.EntryType
	}
	if strings.HasPrefix(string(r.Match), "unmatched") {
		row["unmatched"] = true
	}
	return row
}

func fieldIn(docs []wf.Document, key string) (wf.Field, bool) {
	for _, doc := range docs {
		if ref, ok := doc.Field(key); ok {
			return ref.Field, true
		}
	}
	return wf.Field{}, false
}

func answerRows(docs []wf.Document, answers wf.Answers) []map[string]interface{} {
	keys := make([]string, 0, len(answers.Values))
	order := map[string]int{}
	for i, ref := range docs[0].Fields() {
		order[ref.Field.Key] = i
	}
	for key := range answers.Values {
		keys = append(keys, key)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		left, leftKnown := order[keys[i]]
		right, rightKnown := order[keys[j]]
		if leftKnown != rightKnown {
			return leftKnown
		}
		if leftKnown {
			return left < right
		}
		return keys[i] < keys[j]
	})
	out := make([]map[string]interface{}, 0, len(keys)+len(answers.Unknown))
	for _, key := range keys {
		answer := answers.Values[key]
		field, known := fieldIn(docs, key)
		question := key
		if known {
			question = field.Prompt()
		}
		out = append(out, map[string]interface{}{"question": question, "answer": answerText(field, known, answer)})
	}
	for _, line := range answers.UnknownLines() {
		key, value, _ := strings.Cut(line, ": ")
		out = append(out, map[string]interface{}{"question": key, "answer": value})
	}
	return out
}

func answerText(field wf.Field, known bool, a wf.Answer) string {
	switch {
	case a.Masked:
		return a.Text
	case known:
		return a.Display(field)
	case len(a.OptionIDs) > 0:
		return strings.Join(a.OptionIDs, ", ")
	}
	return a.Text
}
