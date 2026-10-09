package node_executors

import (
	"context"
	"log"
	"strings"

	"vozko/domain/address"
	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	lead_usecase "vozko/usecases/lead"
)

type LeadProfileWriter interface {
	UpdateOfEntry(ctx context.Context, ref shared.EntryRef, in lead_usecase.ProfileUpdate) (lead_usecase.ProfileResult, error)
}

type updateLeadExecutor struct {
	profiles LeadProfileWriter
}

func NewUpdateLeadExecutor(profiles LeadProfileWriter) workflow.NodeExecutor {
	return &updateLeadExecutor{profiles: profiles}
}

func (e *updateLeadExecutor) Definition() workflow.NodeDefinition {
	field := func(key, label, placeholder, description string) workflow.ConfigField {
		return workflow.ConfigField{Key: key, Label: label, Type: "text", Placeholder: placeholder, Description: description}
	}
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeActionUpdateLead,
		Category:    workflow.NodeCategoryAction,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Atualizar Lead",
		Description: "Preenche no cadastro do lead da conversa o endereço, o bairro, a data de nascimento e campos personalizados.",
		Icon:        "UserCircle",
		Guidance: workflow.NodeGuidance{
			When: "Depois de um passo que extraiu dados da conversa (agente de IA, requisição HTTP, variável), para gravar no cadastro do lead o CEP, o endereço, o bairro, a data de nascimento ou campos personalizados.",
			Behavior: "Só preenche o que está vazio no cadastro: um valor diferente já salvo nunca é substituído e aparece em conflicts. " +
				"Com CEP, o CEP é conferido e cidade, UF e logradouro vêm dele; sem CEP, informe cidade e UF junto com o bairro ou o logradouro. " +
				"O endereço vira o endereço principal e aguarda localização no mapa. Campos sensíveis nunca são gravados por fluxos. " +
				"Campos vazios depois de trocar as variáveis são ignorados; sem nenhum dado, ou com um dado recusado, segue pela saída Erro sem gravar nada. O fluxo fica registrado como autor da mudança.",
			Examples: []string{
				"bairro {{node.extrair.bairro}}, cidade {{node.extrair.cidade}}, UF {{node.extrair.uf}}",
				"campos personalizados: interesse = {{last.interesse}}",
			},
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "sucesso", Label: "Sucesso"},
			{ID: "erro", Label: "Erro", Optional: true},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "success", Description: "true quando o cadastro foi conferido sem recusa"},
			{Key: "lead_id", Description: "ID do lead da conversa"},
			{Key: "changed", Description: "Campos preenchidos agora: addresses, birthDate ou customFields.<chave>"},
			{Key: "conflicts", Description: "Campos que já tinham outro valor e foram mantidos"},
			{Key: "error", Description: "Motivo quando nada foi gravado"},
		},
		DefaultConfig: map[string]interface{}{workflow.UpdateLeadCustomFields: map[string]interface{}{}},
		ConfigSchema: []workflow.ConfigField{
			field(workflow.UpdateLeadZipCode, "CEP", "{{last.cep}}", "8 dígitos, com ou sem hífen. Aceita variáveis."),
			field(workflow.UpdateLeadStreet, "Logradouro", "{{last.logradouro}}", "Rua ou avenida, sem o número."),
			field(workflow.UpdateLeadNumber, "Número", "{{last.numero}}", ""),
			field(workflow.UpdateLeadComplement, "Complemento", "{{last.complemento}}", ""),
			field(workflow.UpdateLeadDistrict, "Bairro", "{{last.bairro}}", ""),
			field(workflow.UpdateLeadCity, "Cidade", "{{last.cidade}}", "Obrigatória sem CEP."),
			field(workflow.UpdateLeadState, "UF", "{{last.uf}}", "Sigla ou nome do estado. Obrigatória sem CEP."),
			field(workflow.UpdateLeadBirthDate, "Data de nascimento", "{{last.data_nascimento}}", "DD/MM/AAAA ou AAAA-MM-DD."),
			{Key: workflow.UpdateLeadCustomFields, Label: "Campos personalizados", Type: "keyvalue", Placeholder: "chave do campo = valor (suporta {{var.nome}})",
				Description: "Chave de um campo personalizado de lead e o valor a gravar. Seleção aceita só as opções do campo."},
		},
	}
}

func (e *updateLeadExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	edges := ctx.Graph.OutgoingEdges(ctx.Node.ID)
	if e.profiles == nil {
		return failLeadUpdate(edges, "cadastro de leads indisponível neste contexto (ex.: simulação)"), nil
	}
	customFields := map[string]string{}
	for key, raw := range workflow.UpdateLeadCustomFieldsOf(ctx.Node.Config) {
		if value := strings.TrimSpace(workflow.Interpolate(raw, ctx.State, nil)); value != "" {
			customFields[key] = value
		}
	}
	profile := lead.Profile{
		Address: address.Postal{
			ZipCode: nodeText(ctx, workflow.UpdateLeadZipCode), Street: nodeText(ctx, workflow.UpdateLeadStreet),
			Number: nodeText(ctx, workflow.UpdateLeadNumber), Complement: nodeText(ctx, workflow.UpdateLeadComplement),
			District: nodeText(ctx, workflow.UpdateLeadDistrict), City: nodeText(ctx, workflow.UpdateLeadCity),
			State: nodeText(ctx, workflow.UpdateLeadState),
		},
		BirthDate:    nodeText(ctx, workflow.UpdateLeadBirthDate),
		CustomFields: customFields,
	}
	result, err := e.profiles.UpdateOfEntry(context.Background(),
		shared.EntryRef{EntryID: ctx.Run.EntryID, EntryType: shared.EntryType(ctx.Run.EntryType)},
		lead_usecase.ProfileUpdate{WorkspaceID: ctx.Run.WorkspaceID, Actor: workflowActor(ctx.Run.WorkflowID), Source: lead.ProfileFromWorkflow, Profile: profile})
	if err != nil {
		reason, known := lead_usecase.ExplainProfileRefusal(err)
		if !known {
			log.Printf("[workflow][node:%s][run:%s] update_lead failed: %v", ctx.Node.ID, ctx.Run.ID, err)
			reason = "falha ao atualizar o cadastro do lead; nada foi gravado"
		}
		return failLeadUpdate(edges, reason), nil
	}
	return &workflow.NodeResult{
		NextNodeID: resolveEdgeByLabelStrict(edges, "sucesso"),
		Output: map[string]interface{}{
			"success": true, "lead_id": result.LeadID,
			"changed": fieldList(result.Changed), "conflicts": fieldList(result.Conflicts), "error": "",
		},
	}, nil
}

func fieldList(fields []string) []string {
	if fields == nil {
		return []string{}
	}
	return fields
}

func failLeadUpdate(edges []workflow.Edge, reason string) *workflow.NodeResult {
	return &workflow.NodeResult{
		NextNodeID: resolveEdgeByLabelStrict(edges, "erro"),
		Output:     map[string]interface{}{"success": false, "lead_id": "", "changed": []string{}, "conflicts": []string{}, "error": reason},
	}
}
