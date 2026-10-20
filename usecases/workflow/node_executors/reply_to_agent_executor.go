package node_executors

import (
	"vozko/domain/redact"
	"vozko/domain/tracepayload"
	"vozko/domain/workflow"
)

var agentResultRedactor = redact.New()

type replyToAgentExecutor struct{}

func NewReplyToAgentExecutor() workflow.NodeExecutor {
	return &replyToAgentExecutor{}
}

func (e *replyToAgentExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:         workflow.NodeTypeActionReplyToAgent,
		TestBehavior: workflow.NodeTestPure,
		Category:     workflow.NodeCategoryAction,
		Scopes:       []workflow.NodeScope{workflow.NodeScopeShared},
		Label:        "Responder ao agente",
		Description:  "Devolve um resultado ao agente de IA que executou este fluxo.",
		Icon:         "ArrowBendUpLeft",
		Guidance: workflow.NodeGuidance{
			When: "Em fluxos com o gatilho Chamado por agente IA, para contar ao agente o que aconteceu (por exemplo status e mensagem).",
			Behavior: "Interpola cada campo, mascara dados sensíveis (tokens, senhas, CPF) e grava o mapa como resultado da execução; a última gravação vale. " +
				"O agente vê só esses campos, nunca o estado interno do fluxo. Segue pela saída padrão e disponibiliza os mesmos campos em {{last.<campo>}}.",
			Examples: []string{
				"config: {\"fields\":[{\"key\":\"status\",\"value\":\"{{node.n55.status}}\"},{\"key\":\"mensagem\",\"value\":\"Matrícula feita\"}]}",
			},
		},
		DefaultConfig: map[string]interface{}{workflow.ReplyToAgentFieldsKey: []interface{}{}},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "<campo>", Description: "Cada campo devolvido ao agente, já mascarado"},
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: workflow.ReplyToAgentFieldsKey, Label: "Campos devolvidos ao agente", Type: "agent_reply_fields",
				Description: "Lista de chave e valor; o valor aceita variáveis como {{input.cpf}} ou {{node.<id>.<campo>}}."},
		},
	}
}

func (e *replyToAgentExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	if err := workflow.ValidateReplyToAgentNodes(ctx.Graph); err != nil {
		return &workflow.NodeResult{Error: err.Error(), ErrorKind: workflow.ErrorKindConfig}, nil
	}
	values := workflow.ReplyToAgentValues(workflow.ReplyToAgentFieldsOf(ctx.Node.Config), ctx.State)
	result := tracepayload.Map(agentResultRedactor.Map(values))
	ctx.Run.AgentResult = result
	return routeOutput(&workflow.NodeResult{
		Output: result,
	}, ctx.Graph.OutgoingEdges(ctx.Node.ID), "default"), nil
}
