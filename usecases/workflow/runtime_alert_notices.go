package workflow_usecase

import (
	"fmt"
	"net/url"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/notification"
	"vozko/domain/tools"
	"vozko/domain/workflow"
	"vozko/domain/workspace"
)

const (
	workflowsPath = "/dashboard/workflows/"
	agentsPath    = "/dashboard/agents/"
)

var webhookReasonForEditors = map[string]string{
	"webhook_inactive":             "o webhook está desligado",
	"webhook_not_found":            "o webhook não foi encontrado",
	"method_not_allowed":           "o método HTTP não é aceito",
	"unauthorized":                 "a autenticação enviada é inválida",
	"entry_required":               "o corpo não informa a conversa (entry_id e entry_type, ou phone)",
	"entry_not_found":              "nenhuma conversa foi encontrada para o telefone informado",
	"entry_forbidden":              "a conversa informada é de outro workspace",
	"no_trigger_node":              "o fluxo não tem gatilho de webhook",
	"workspace_at_capacity":        "o limite de execuções simultâneas foi atingido",
	workflow.WebhookReasonInternal: "houve um erro interno ao processar a chamada",
}

func runtimeAlertDraft(alert workflow.RuntimeAlert, names alertNames, steps StepNamer) notification.Draft {
	w := names.workflows[alert.Key.WorkflowID]
	name := workflowName(w)
	d := notification.Draft{
		WorkspaceID: alert.Key.WorkspaceID,
		Category:    notification.CategoryAutomations,
		Severity:    notificationSeverity(alert.Severity),
		Audience:    notification.Holding(workspace.ResourceWorkflows, workspace.ActionUpdate),
		Email:       true,
		EpisodeKey:  alert.EpisodeKey(),
		Source:      notification.SourceWorkflowAlert,
		ActionURL:   workflowLink(alert.Key.WorkflowID, alert.RunID()),
	}
	switch alert.Key.Kind {
	case workflow.AlertWorkflowCalledInactive:
		d.Title = fmt.Sprintf("O fluxo %s foi chamado, mas não está ativo", name)
		d.Body = fmt.Sprintf("O fluxo %s recusou %s na última hora (pelo webhook ou por um agente de IA) porque %s. Quem chamou não teve o resultado esperado. Publique o fluxo para que as próximas chamadas sejam executadas.",
			name, plural(alert.Count, "chamada", "chamadas"), inactiveReason(w))
		d.ActionURL = alertLink(alert)
		d.ActionLabel = "Publicar fluxo"
	case workflow.AlertNodeFailing:
		d.Title = fmt.Sprintf("Nó %q falhando no fluxo %s", alert.Key.NodeID, name)
		d.Body = fmt.Sprintf("O nó %s do fluxo %s falhou em %d das últimas %d execuções. Abra a execução com falha mais recente para ver o erro e os parâmetros usados, corrija o nó e publique o fluxo de novo.",
			nodeLabel(w, alert.Key.NodeID, steps), name, alert.Count, workflow.NodeFailingRecentExecutions)
		d.ActionLabel = "Abrir execução"
	case workflow.AlertWebhookRejecting:
		d.Title = fmt.Sprintf("O webhook do fluxo %s está recusando chamadas", name)
		d.Body = fmt.Sprintf("Na última hora o webhook do fluxo %s recusou %s pelo mesmo motivo: %s. Veja as últimas chamadas para entender o que o sistema que chama está enviando.",
			name, plural(alert.Count, "chamada", "chamadas"), webhookReason(alert.Key.Subject))
		d.ActionURL = alertLink(alert)
		d.ActionLabel = "Ver chamadas"
	case workflow.AlertRunStuck:
		d.Title = fmt.Sprintf("Execução parada no fluxo %s", name)
		stuck := "Uma execução do fluxo %s ficou sem avançar por mais de %d minutos e foi encerrada com erro."
		if alert.Count > 1 {
			stuck = fmt.Sprintf("%d execuções do fluxo %%s ficaram sem avançar por mais de %%d minutos e foram encerradas com erro.", alert.Count)
		}
		d.Body = fmt.Sprintf(stuck, name, workflow.StuckRunTimeoutMinutes) + " Abra a execução mais recente para ver em que etapa ela parou."
		d.ActionLabel = "Abrir execução"
	case workflow.AlertAgentToolFailing:
		agentName := "do agente"
		if ag := names.agents[alert.Key.AgentID]; ag != nil && strings.TrimSpace(ag.Name) != "" {
			agentName = fmt.Sprintf("do agente %q", ag.Name)
		}
		d.Title = fmt.Sprintf("Ferramenta %s falhando", agentName)
		d.Body = fmt.Sprintf("A ferramenta %s %s falhou %s na última hora. Abra o agente para revisar a configuração da ferramenta e veja as chamadas registradas nas conversas.",
			toolLabel(alert, name), agentName, plural(alert.Count, "vez", "vezes"))
		d.ActionURL = agentsPath + url.PathEscape(alert.Key.AgentID)
		d.ActionLabel = "Abrir agente"
	case workflow.AlertInboundLookupFailed:
		d.Title = "Mensagens ficaram sem resposta automática"
		waited := "a resposta de 1 mensagem recebida"
		if alert.Count > 1 {
			waited = fmt.Sprintf("as respostas de %d mensagens recebidas", alert.Count)
		}
		d.Body = "Não foi possível verificar se algum fluxo aguardava " + waited + ". Para não responder em dobro, nem o agente de IA nem os fluxos responderam. Abra a conversa mais recente e responda manualmente se for preciso."
		d.ActionURL = ""
		if ref, ok := alert.Conversation(); ok {
			d.ActionURL = conversation.DashboardLink(ref)
		}
		d.ActionLabel = "Abrir conversa"
	}
	d.Title = notification.ClipTitle(d.Title)
	if d.ActionURL == "" {
		d.ActionLabel = ""
	}
	return d
}

func notificationSeverity(s workflow.RuntimeAlertSeverity) notification.Severity {
	if s == workflow.RuntimeAlertHigh {
		return notification.SeverityCritical
	}
	return notification.SeverityWarning
}

func workflowLink(workflowID, runID string) string {
	return editorLink(workflowID, "run", runID)
}

func alertLink(alert workflow.RuntimeAlert) string {
	return editorLink(alert.Key.WorkflowID, "alert", alert.ID)
}

func editorLink(workflowID, param, value string) string {
	if workflowID == "" {
		return ""
	}
	link := workflowsPath + url.PathEscape(workflowID)
	if value != "" {
		link += "?" + url.Values{param: {value}}.Encode()
	}
	return link
}

func workflowName(w *workflow.Workflow) string {
	if w == nil || strings.TrimSpace(w.Name) == "" {
		return "sem nome"
	}
	return fmt.Sprintf("%q", w.Name)
}

func inactiveReason(w *workflow.Workflow) string {
	switch {
	case w == nil:
		return "não está ativo"
	case w.PublishedVersion == nil:
		return "ele nunca foi publicado"
	case w.Status == workflow.WorkflowStatusPaused:
		return "ele está pausado"
	case w.Status == workflow.WorkflowStatusArchived:
		return "ele está arquivado"
	}
	return "ele não está ativo"
}

func nodeLabel(w *workflow.Workflow, nodeID string, steps StepNamer) string {
	label := fmt.Sprintf("%q", nodeID)
	if w == nil {
		return label
	}
	if node := w.Graph.FindNode(nodeID); node != nil {
		return label + " (" + steps.StepName(node.Type) + ")"
	}
	return label
}

func webhookReason(reason string) string {
	if text, ok := webhookReasonForEditors[reason]; ok {
		return text
	}
	return reason
}

func toolLabel(alert workflow.RuntimeAlert, workflowName string) string {
	if _, bound := tools.BoundKeyOf(alert.Key.Subject, workflow.AgentRunWorkflowTool); bound {
		return "Executar fluxo " + workflowName
	}
	if strings.EqualFold(alert.Key.Subject, workflow.AgentHTTPRequestTool) {
		return "Requisição HTTP"
	}
	return alert.Key.Subject
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
