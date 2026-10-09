package copilottools

import (
	"sort"
	"strconv"
	"strings"

	"vozko/domain/campaign"
	"vozko/domain/copilot"
	"vozko/domain/customfield"
	"vozko/domain/leadaction"
	"vozko/domain/selection"
)

func (t *prepareLeadActionTool) actionLabel(cc copilot.Context, p leadActionPlan) string {
	params := p.request.Params
	switch p.request.Action {
	case leadaction.ActionClassify:
		if params.ClearsValue() {
			return "limpar o campo " + fieldName(p.field)
		}
		return "classificar: " + fieldName(p.field) + " = " + strings.TrimSpace(p.args.Value)
	case leadaction.ActionAssignOwner:
		if *params.OwnerID == "" {
			return "tirar o responsável"
		}
		return "passar para " + t.ownerName(*params.OwnerID)
	case leadaction.ActionBlock:
		label := "desbloquear"
		if *params.Blocked {
			label = "bloquear"
		}
		if params.BusinessPhoneID != "" {
			label += ", também no WhatsApp"
		}
		return label
	case leadaction.ActionExport:
		if params.Addresses {
			return "exportar em CSV, com o endereço completo"
		}
		return "exportar em CSV, sem o endereço completo"
	case leadaction.ActionSendTemplate:
		return "preparar disparo do WhatsApp oficial: " + params.Send.Name + t.templateLabel(cc, params.Send.TemplateID)
	case leadaction.ActionSendUnofficial:
		return "preparar disparo do WhatsApp não oficial: " + params.Send.Name
	}
	return string(p.request.Action)
}

func (t *prepareLeadActionTool) ownerName(id string) string {
	if t.deps.Names != nil {
		if name := strings.TrimSpace(t.deps.Names.Names(id)[id]); name != "" {
			return name
		}
	}
	return "o responsável escolhido"
}

func (t *prepareLeadActionTool) templateLabel(cc copilot.Context, id string) string {
	if t.deps.Templates == nil {
		return ""
	}
	template, err := t.deps.Templates.Get(cc.WorkspaceID, id)
	if err != nil || template == nil {
		return ""
	}
	return " (modelo " + template.Name + ")"
}

func selectionSize(s selection.Selection, p *leadaction.Preview) int {
	if p.Status != leadaction.PreviewRunning {
		return p.Result.Selected
	}
	if s.Mode == selection.ModeIDs {
		return p.Result.Matched
	}
	return p.Result.ExpectedCount
}

func selectionLabel(s selection.Selection, size, matched int) string {
	switch s.Mode {
	case selection.ModeIDs:
		return strconv.Itoa(size) + " leads escolhidos"
	case selection.ModeEveryone:
		return "toda a base: " + strconv.Itoa(size) + " leads"
	case selection.ModeFirstN:
		return "os primeiros " + strconv.Itoa(size) + " de " + strconv.Itoa(matched) + " leads do filtro"
	}
	return strconv.Itoa(size) + " leads do filtro"
}

var editSkipNames = map[leadaction.SkipReason]string{
	leadaction.SkipUnchanged: "já tinham esse valor",
	leadaction.SkipGone:      "não existem mais",
}

func editSkipLabel(skipped map[leadaction.SkipReason]int) string {
	counts := map[string]int{}
	for reason, n := range skipped {
		name, ok := editSkipNames[reason]
		if !ok {
			name = string(reason)
		}
		counts[name] += n
	}
	return countsLabel(counts)
}

func countsLabel(counts map[string]int) string {
	names := make([]string, 0, len(counts))
	for name, n := range counts {
		if n > 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, strconv.Itoa(counts[name])+" "+name)
	}
	return strings.Join(parts, "; ")
}

func (t *prepareLeadActionTool) sendFields(_ copilot.Context, p leadActionPlan, preview *leadaction.Preview) []copilot.Field {
	q := preview.Send
	if !p.request.Action.Sends() || q == nil {
		return nil
	}
	fields := []copilot.Field{{Key: "parts", Value: strconv.Itoa(q.Parts)}}
	if p.request.Action == leadaction.ActionSendTemplate {
		fields = append(fields,
			copilot.Field{Key: "estimatedCost", Value: formatUSD(q.CostMicros)},
			copilot.Field{Key: "balance", Value: formatUSD(q.BalanceMicros)},
		)
	}
	if q.CapRemaining != nil {
		fields = append(fields, copilot.Field{Key: "capRemaining", Value: strconv.FormatInt(*q.CapRemaining, 10)})
	}
	if q.Refusal != "" {
		fields = append(fields, copilot.Field{Key: "budget", Value: "cabem " + strconv.Itoa(q.Fits) + " leads (" + budgetRefusalName(q.Refusal) + ")"})
	}
	if q.DailyCap > 0 {
		fields = append(fields, copilot.Field{Key: "dailyCap", Value: strconv.Itoa(q.DailyCap)}, copilot.Field{Key: "estimatedDays", Value: strconv.Itoa(q.EstimatedDays)})
	}
	return append(fields, copilot.Field{Key: "next", Value: "a campanha nasce parada; os pulados e o custo final aparecem na aprovação do envio"})
}

func budgetRefusalName(code string) string {
	switch code {
	case campaign.ErrorCode(campaign.ErrUnaffordable):
		return "o saldo não cobre todos"
	case campaign.ErrorCode(campaign.ErrOverMonthlyCap):
		return "o limite mensal de envios não cobre todos"
	}
	return code
}

func fieldName(def *customfield.Definition) string {
	if label := strings.TrimSpace(def.Label); label != "" {
		return label + " (" + def.Key + ")"
	}
	return def.Key
}
