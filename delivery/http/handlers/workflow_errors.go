package handlers

import (
	"fmt"
	"log"
	"net/http"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/conversation"
	"vozko/domain/workflow"
	"vozko/domain/workspace"
	workspace_department "vozko/domain/workspace/workspace_department"
	workflow_usecase "vozko/usecases/workflow"
)

const workflowConsentGrantNeedsReplyCode = "workflow_consent_grant_needs_reply"

const draftRevisionRequiredCode = "draft_revision_required"

const invalidEntryKindCode = "invalid_entry_kind"

const workflowInvalidCode = "workflow_invalid"

const workflowInvalidMessage = "O workflow tem uma configuração que precisa ser corrigida. Veja o detalhe e ajuste o fluxo."

type draftConflictDetails struct {
	Revision int `json:"revision" example:"5"`
}

type workflowValidationDetails struct {
	Detail string `json:"detail" example:"workflow: at least one trigger node must be connected to the flow: \"t1\""`
}

type workflowNotPublishableDetails struct {
	Issues []workflow.LintIssue `json:"issues"`
}

func workflowRefusal(status int, code, message string) response.Refusal {
	return response.Refusal{Status: status, Code: code, Message: message}
}

func workflowInvalid(code, message string) response.Refusal {
	return workflowRefusal(http.StatusUnprocessableEntity, code, message)
}

var (
	refusalWorkflowInvalid     = workflowInvalid(workflowInvalidCode, workflowInvalidMessage)
	refusalWorkflowInternal    = workflowRefusal(http.StatusInternalServerError, "internal_error", "Erro interno do servidor")
	refusalDraftConflict       = workflowRefusal(http.StatusConflict, workflow.DraftConflictCode, "Este rascunho foi alterado por outra pessoa. Recarregue para ver a versão atual ou salve por cima dela.")
	refusalNotPublishable      = workflowRefusal(http.StatusUnprocessableEntity, workflow.NotPublishableCode, "Não foi possível publicar: há problemas que impedem a publicação.")
	refusalConsentForbidden    = workflowRefusal(http.StatusForbidden, workspace.CodeConsentGrantForbidden, "Registrar consentimento de leads exige permissão")
	refusalInvalidBody         = workflowRefusal(http.StatusBadRequest, "invalid_body", "Corpo da requisição inválido")
	refusalBodyTooLarge        = workflowRefusal(http.StatusBadRequest, "body_too_large", "O corpo da requisição passa de 2 MB")
	refusalInvalidEntryKind    = workflowRefusal(http.StatusBadRequest, invalidEntryKindCode, "Tipo de entrada desconhecido")
	refusalNameRequired        = workflowRefusal(http.StatusBadRequest, "name_required", "O nome do workflow é obrigatório").Expecting(map[string]string{"name": "O nome do workflow é obrigatório"})
	refusalInvalidTriggerType  = workflowRefusal(http.StatusBadRequest, "invalid_trigger_type", "Tipo de gatilho inválido").Expecting(map[string]string{"triggerType": "Tipo de gatilho inválido"})
	refusalGraphRequired       = workflowRefusal(http.StatusBadRequest, "graph_empty", "O workflow precisa ter pelo menos um nó").Expecting(map[string]string{"graph": "must have at least one node"})
	refusalGraphNodeType       = workflowRefusal(http.StatusBadRequest, "invalid_node_type", "O workflow contém um tipo de nó inválido")
	refusalGraphEdgeRef        = workflowRefusal(http.StatusBadRequest, "invalid_edge_ref", "Uma conexão referencia um nó que não existe").Expecting(map[string]string{"graph": "edge references non-existent node"})
	refusalNodeTestUnavailable = workflowRefusal(http.StatusNotImplemented, "node_test_unavailable", "Teste de nó não configurado")
	refusalPinUnavailable      = workflowRefusal(http.StatusNotImplemented, "pin_unavailable", "Fixar saídas não está disponível")
	refusalConversationHidden  = workflowRefusal(http.StatusNotFound, "conversation_not_found", "Conversa não encontrada")
	refusalConversationRuns    = workflowRefusal(http.StatusInternalServerError, "conversation_runs_failed", "Erro ao listar as execuções da conversa")
	refusalDraftRevision       = workflowRefusal(http.StatusBadRequest, draftRevisionRequiredCode, "Informe a revisão do rascunho que foi editada (campo revision)").Expecting(map[string]string{"revision": "int (required)"})
	refusalDepartmentDenied    = workflowRefusal(http.StatusBadRequest, "department_denied", "departmentId inválido ou sem acesso").Expecting(map[string]string{"departmentId": "departmentId inválido ou sem acesso"})
)

var workflowRequestRefusals = []response.Refusal{
	refusalWorkflowInvalid,
	refusalWorkflowInternal,
	refusalDraftConflict,
	refusalNotPublishable,
	refusalConsentForbidden,
	refusalInvalidBody,
	refusalBodyTooLarge,
	refusalInvalidEntryKind,
	refusalGraphRequired,
	refusalGraphNodeType,
	refusalGraphEdgeRef,
	refusalPinUnavailable,
	refusalConversationRuns,
	httpx.RefusalWorkflowUnreadable,
}

var workflowDomainRefusals = append(append([]response.ErrorRefusal{}, httpx.WorkflowAccessRefusals...), workflowOwnRefusals...)

var workflowOwnRefusals = []response.ErrorRefusal{
	{Err: workflow.ErrDraftRevisionRequired, Refusal: refusalDraftRevision},
	{Err: workflow.ErrNodeNotFound, Refusal: workflowRefusal(http.StatusNotFound, "node_not_found", "Nó não encontrado no rascunho")},
	{Err: workflow_usecase.ErrNodeTestUnavailable, Refusal: refusalNodeTestUnavailable},
	{Err: workflow.ErrRunNotFound, Refusal: workflowRefusal(http.StatusNotFound, "run_not_found", "Execução do workflow não encontrada")},
	{Err: conversation.ErrNotVisible, Refusal: refusalConversationHidden},
	{Err: workflow.ErrVersionNotFound, Refusal: workflowRefusal(http.StatusNotFound, "version_not_found", "Versão do workflow não encontrada")},
	{Err: workflow.ErrInvalidVersion, Refusal: workflowRefusal(http.StatusBadRequest, "invalid_version", "Número de versão inválido")},
	{Err: workflow.ErrVersionNumberTaken, Refusal: workflowRefusal(http.StatusConflict, "version_number_taken", "Outra publicação aconteceu ao mesmo tempo. Tente publicar de novo.")},
	{Err: workflow.ErrNameRequired, Refusal: refusalNameRequired},
	{Err: workflow.ErrWorkspaceIDRequired, Refusal: workflowRefusal(http.StatusUnauthorized, "workspace_required", "ID do workspace é obrigatório")},
	{Err: workflow.ErrInvalidTriggerType, Refusal: refusalInvalidTriggerType},
	{Err: workspace_department.ErrDepartmentRequired, Refusal: workflowRefusal(http.StatusBadRequest, "department_required", "departmentId é obrigatório quando o usuário pertence a múltiplos departamentos").Expecting(map[string]string{"departmentId": "departmentId é obrigatório quando o usuário pertence a múltiplos departamentos"})},
	{Err: workspace_department.ErrDepartmentAccessDenied, Refusal: refusalDepartmentDenied},
	{Err: workspace_department.ErrDepartmentNotFound, Refusal: refusalDepartmentDenied},
	{Err: workflow.ErrWorkflowNotDraft, Refusal: workflowRefusal(http.StatusConflict, "workflow_not_draft", "O workflow precisa estar em rascunho para editar")},
	{Err: workflow.ErrWorkflowNotActive, Refusal: workflowRefusal(http.StatusConflict, "workflow_not_active", "O workflow não está ativo")},
	{Err: workflow.ErrCannotActivate, Refusal: workflowRefusal(http.StatusConflict, "cannot_activate", "Não é possível ativar o workflow")},
	{Err: workflow.ErrInvalidWorkflowType, Refusal: workflowRefusal(http.StatusBadRequest, "invalid_workflow_type", "Tipo de workflow inválido")},
	{Err: workflow.ErrTriggerNotAllowedForType, Refusal: workflowRefusal(http.StatusBadRequest, "trigger_not_allowed", "O gatilho selecionado não é compatível com o tipo do workflow")},
	{Err: workflow.ErrRunAlreadyExists, Refusal: workflowRefusal(http.StatusConflict, "run_already_exists", "Já existe uma execução ativa para esta entrada")},
	{Err: workflow.ErrRunTerminal, Refusal: workflowRefusal(http.StatusConflict, "run_terminal", "A execução já foi finalizada")},
	{Err: workflow.ErrInsufficientBalance, Refusal: workflowRefusal(http.StatusPaymentRequired, "insufficient_balance", "Saldo insuficiente")},
	{Err: workflow.ErrVoiceTrunkTaken, Refusal: workflowRefusal(http.StatusConflict, "voice_trunk_taken", "Esta linha telefônica já atende ligações com outro fluxo de voz ativo. Pause o outro fluxo antes de ativar este.")},
}

var workflowValidationRefusals = []response.ErrorRefusal{
	{Err: workflow.ErrPinInvalid, Refusal: workflowInvalid("pin_invalid", "Saída fixada inválida")},
	{Err: workflow.ErrGraphEmpty, Refusal: workflowInvalid("graph_empty", "O workflow precisa ter pelo menos um nó")},
	{Err: workflow.ErrGraphNoTrigger, Refusal: workflowInvalid("graph_no_trigger", "O workflow precisa ter um nó de gatilho")},
	{Err: workflow.ErrGraphMultipleTriggers, Refusal: workflowInvalid("graph_multiple_triggers", "O workflow deve ter apenas um nó de gatilho")},
	{Err: workflow.ErrGraphDuplicateTriggerType, Refusal: workflowInvalid("graph_duplicate_trigger_type", "O workflow não pode ter dois nós de gatilho do mesmo tipo")},
	{Err: workflow.ErrGraphTriggerIncompatibleWithType, Refusal: workflowInvalid("trigger_incompatible_with_type", "O nó de gatilho não é compatível com o tipo do workflow")},
	{Err: workflow.ErrGraphTriggerNotConnected, Refusal: workflowInvalid("trigger_not_connected", "Conecte o gatilho ao primeiro passo do fluxo")},
	{Err: workflow.ErrGraphTriggerHasIncoming, Refusal: workflowInvalid("trigger_has_incoming_edge", "Um gatilho não pode receber conexões, ele é sempre o começo do fluxo")},
	{Err: workflow.ErrGraphNoEndNode, Refusal: workflowInvalid("graph_no_end", "O workflow precisa ter pelo menos um nó de Fim")},
	{Err: workflow.ErrGraphOrphanNode, Refusal: workflowInvalid("graph_orphan_node", "O workflow tem nós que não estão conectados")},
	{Err: workflow.ErrGraphCycleDetected, Refusal: workflowInvalid("graph_cycle_detected", "O workflow contém um ciclo (loop infinito)")},
	{Err: workflow.ErrGraphTooManyNodes, Refusal: workflowInvalid("graph_too_many_nodes", "O workflow excede o número máximo de nós")},
	{Err: workflow.ErrGraphTooManyEdges, Refusal: workflowInvalid("graph_too_many_edges", "O workflow excede o número máximo de conexões")},
	{Err: workflow.ErrGraphInvalidEdgeRef, Refusal: workflowInvalid("invalid_edge_ref", "Uma conexão referencia um nó que não existe")},
	{Err: workflow.ErrGraphDuplicateNodeID, Refusal: workflowInvalid("graph_duplicate_node_id", "O workflow tem nós com IDs duplicados")},
	{Err: workflow.ErrGraphTriggerTypeMismatch, Refusal: workflowInvalid("trigger_type_mismatch", "O tipo do nó de gatilho não corresponde ao tipo de gatilho do workflow")},
	{Err: workflow.ErrGraphNodeNoOutgoing, Refusal: workflowInvalid("node_no_outgoing", "Todos os nós (exceto Fim) precisam ter pelo menos uma conexão de saída")},
	{Err: workflow.ErrGraphNodeNoIncoming, Refusal: workflowInvalid("node_no_incoming", "Todos os nós (exceto gatilho) precisam ter pelo menos uma conexão de entrada")},
	{Err: workflow.ErrNodeMissingRequiredField, Refusal: workflowInvalid("node_missing_required_field", "Campo obrigatório não preenchido em um nó do workflow")},
	{Err: workflow.ErrNodeMissingRequiredOutput, Refusal: workflowInvalid("node_missing_required_output", "Uma saída obrigatória de um nó não está conectada")},
	{Err: workflow.ErrNodeFieldOutOfRange, Refusal: workflowInvalid("node_field_out_of_range", "Um valor numérico em um nó está fora do intervalo permitido.")},
	{Err: workflow.ErrNodeInvalidTemplateID, Refusal: workflowInvalid("invalid_template", "O template selecionado é inválido ou não foi encontrado")},
	{Err: workflow.ErrNodeInvalidAgentID, Refusal: workflowInvalid("invalid_agent", "O agente selecionado é inválido ou não foi encontrado")},
	{Err: workflow.ErrNodeInvalidMCPCollectionID, Refusal: workflowInvalid("invalid_mcp_collection", "Uma coleção MCP selecionada é inválida ou não foi encontrada")},
	{Err: workflow.ErrNodeInvalidKnowledgeBaseID, Refusal: workflowInvalid("invalid_knowledge_base", "Uma base de conhecimento selecionada é inválida ou não foi encontrada")},
	{Err: workflow.ErrNodeInvalidModelID, Refusal: workflowInvalid("invalid_ai_model", "O modelo de IA selecionado é inválido ou não está disponível. Use um modelo da lista (find_resource ai_models).")},
	{Err: workflow.ErrNodeReasoningLevelUnsupported, Refusal: workflowInvalid("reasoning_unsupported", "O modelo de IA de um nó não oferece o nível de raciocínio escolhido. Escolha um nível da lista do modelo ou deixe o padrão.")},
	{Err: workflow.ErrNodeInvalidToolParamType, Refusal: workflowInvalid("invalid_tool_param_type", "Um parâmetro de ferramenta (custom_tools) tem um tipo inválido. Use um tipo suportado (string, number, integer, boolean, array, object, date, time, datetime, email, phone, enum).")},
	{Err: workflow.ErrNodeInvalidToolMode, Refusal: workflowInvalid("invalid_tool_mode", "O modo de ferramentas do agente de IA é inválido. Use 'route' ou 'execute'.")},
	{Err: workflow.ErrNodeInvalidResponseMode, Refusal: workflowInvalid("invalid_response_mode", "O modo de resposta do agente de IA é inválido. Use 'default' ou 'segmented'.")},
	{Err: workflow.ErrNodeSegmentedSendConflict, Refusal: workflowInvalid("segmented_send_conflict", "Um agente de IA em modo segmentado já envia as mensagens. Remova o Enviar texto logo depois dele.")},
	{Err: workflow.ErrNodeInvalidInteractiveConfig, Refusal: workflowInvalid("invalid_interactive_config", "As opções de botões ou lista de um nó não seguem os limites do WhatsApp")},
	{Err: workflow.ErrNodeInvalidLabelID, Refusal: workflowInvalid("invalid_label", "A etiqueta selecionada é inválida ou não foi encontrada")},
	{Err: workflow.ErrNodeInvalidStageID, Refusal: workflowInvalid("invalid_stage", "A etapa selecionada é inválida ou não foi encontrada")},
	{Err: workflow.ErrNodeInvalidOpportunityPipeline, Refusal: workflowInvalid("invalid_opportunity_pipeline", "O funil selecionado não é um funil de oportunidades deste workspace")},
	{Err: workflow.ErrNodeInvalidDepartmentID, Refusal: workflowInvalid("invalid_department", "O departamento selecionado é inválido ou não foi encontrado")},
	{Err: workflow.ErrNodeInvalidMemberID, Refusal: workflowInvalid("invalid_member", "O atendente selecionado é inválido ou não faz parte do workspace")},
	{Err: workflow.ErrNodeInvalidBusinessPhoneID, Refusal: workflowInvalid("invalid_business_phone", "O número de WhatsApp selecionado é inválido ou não pertence ao workspace")},
	{Err: workflow.ErrNodeInvalidWorkflowID, Refusal: workflowInvalid("invalid_workflow_reference", "O workflow referenciado é inválido ou não foi encontrado")},
	{Err: workflow.ErrNodeIncompatibleScope, Refusal: workflowInvalid("node_incompatible_scope", "Um nó não é compatível com o tipo deste workflow")},
	{Err: workflow.ErrVoiceTrunkRequired, Refusal: workflowInvalid("voice_trunk_invalid", "Escolha uma linha telefônica deste workspace que receba ligações")},
	{Err: workflow.ErrVoiceTrunkInvalid, Refusal: workflowInvalid("voice_trunk_invalid", "Escolha uma linha telefônica deste workspace que receba ligações")},
	{Err: workflow.ErrNodeInvalidDTMFConfig, Refusal: workflowInvalid("invalid_dtmf_config", "Em Aguardar Tecla, escolha ao menos uma tecla (0-9, * ou #), sem repetir")},
	{Err: workflow.ErrNodeInvalidQueueID, Refusal: workflowInvalid("invalid_queue", "Em Transferir para Fila, escolha uma fila de atendimento deste workspace")},
	{Err: workflow.ErrNodeInvalidMediaID, Refusal: workflowInvalid("invalid_media", "A mídia selecionada é inválida ou não foi encontrada")},
	{Err: workflow.ErrInvalidNodeType, Refusal: workflowInvalid("invalid_node_type", "O workflow contém um tipo de nó inválido")},
	{Err: workflow.ErrConsentGrantNeedsReply, Refusal: workflowInvalid(workflowConsentGrantNeedsReplyCode, "Registrar consentimento só vale depois da resposta do lead em todos os caminhos (Aguardar Resposta, uma opção de botões ou o botão do template). Para opt-out use Retirar.")},
	{Err: workflow.ErrReplyToAgentWithoutTrigger, Refusal: workflowInvalid("reply_to_agent_without_trigger", "O nó Responder ao agente só funciona em fluxos com o gatilho Chamado por agente IA")},
	{Err: workflow.ErrAgentCallInputsInvalid, Refusal: workflowInvalid("agent_call_inputs_invalid", "Os campos do gatilho Chamado por agente IA são inválidos")},
	{Err: workflow.ErrNodeConfigMissing, Refusal: workflowInvalid("node_config_missing", "Configuração obrigatória ausente em um nó do workflow")},
	{Err: workflow.ErrInvalidStatus, Refusal: workflowInvalid("invalid_status", "O status do workflow é inválido")},
}

func (h *WorkflowHandler) handleDomainError(w http.ResponseWriter, err error) {
	if httpx.WriteConsentGrantRefusal(w, err) {
		return
	}
	if current, conflict := workflow.CurrentDraftRevisionOf(err); conflict {
		response.WriteErrorWithDetails(w, refusalDraftConflict.Status, refusalDraftConflict.Code, refusalDraftConflict.Message, draftConflictDetails{Revision: current})
		return
	}
	if blocked, ok := workflow.NotPublishableOf(err); ok {
		response.WriteErrorWithDetails(w, refusalNotPublishable.Status, refusalNotPublishable.Code, notPublishableMessage(len(blocked.Issues)), workflowNotPublishableDetails{Issues: blocked.Issues})
		return
	}
	if response.WriteErrorRefusal(w, err, workflowDomainRefusals) {
		return
	}
	if workflow.IsValidationError(err) {
		writeWorkflowValidationError(w, err)
		return
	}
	log.Printf("[workflow_handler] unhandled error: %v", err)
	response.WriteRefusal(w, refusalWorkflowInternal)
}

func writeWorkflowValidationError(w http.ResponseWriter, err error) {
	refusal, ok := response.MatchRefusal(err, workflowValidationRefusals)
	if !ok {
		refusal = refusalWorkflowInvalid
	}
	response.WriteErrorWithDetails(w, refusal.Status, refusal.Code, refusal.Message, workflowValidationDetails{Detail: err.Error()})
}

func notPublishableMessage(issues int) string {
	if issues == 1 {
		return "Não foi possível publicar: 1 problema impede a publicação."
	}
	return fmt.Sprintf("Não foi possível publicar: %d problemas impedem a publicação.", issues)
}
