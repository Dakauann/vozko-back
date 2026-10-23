package workflow

import (
	"errors"
	"fmt"
	"strings"
)

const NotPublishableCode = "workflow_not_publishable"

const genericStructureHint = "Corrija a estrutura do grafo conforme a mensagem."

const resourceRefHint = "Escolha de novo o recurso deste nó: o selecionado não existe mais, é de outro workspace ou não pode ser usado agora (por exemplo, um template ainda não aprovado)."

type validationClass struct {
	err  error
	code LintIssueCode
	hint string
}

var validationClasses = []validationClass{
	{ErrGraphEmpty, LintGraphEmpty, "O grafo está vazio. Adicione ao menos um nó de gatilho (trigger) e um nó 'end'."},
	{ErrGraphTooManyNodes, LintGraphTooManyNodes, "O grafo excede o número máximo de nós. Simplifique o fluxo."},
	{ErrGraphTooManyEdges, LintGraphTooManyEdges, "O grafo excede o número máximo de arestas. Simplifique as conexões."},
	{ErrInvalidWorkflowType, LintInvalidWorkflowType, "Defina um tipo de workflow válido ('messages' ou 'voip') via set_meta."},
	{ErrGraphDuplicateNodeID, LintDuplicateNodeID, "Há ids de nó duplicados. Cada nó precisa de um id único."},
	{ErrInvalidNodeType, LintInvalidNodeType, "Há um nó com tipo inválido. Use apenas tipos do catálogo."},
	{ErrUnknownNodeType, LintInvalidNodeType, "Há um nó com tipo inválido. Use apenas tipos do catálogo."},
	{ErrGraphInvalidEdgeRef, LintInvalidEdgeRef, "Uma aresta referencia um nó inexistente. Remova-a ou corrija os ids de source/target."},
	{ErrGraphNoTrigger, LintNoTrigger, "Adicione um nó de gatilho (trigger), todo workflow precisa de pelo menos um."},
	{ErrGraphMultipleTriggers, LintDuplicateTriggerType, "Mantenha apenas um gatilho de cada tipo."},
	{ErrGraphNoEndNode, LintNoEnd, "Adicione um nó 'end', todo workflow precisa de pelo menos um caminho que termine em 'end'."},
	{ErrGraphTriggerIncompatibleWithType, LintTriggerIncompatible, "O gatilho não é compatível com o tipo do workflow. Troque o gatilho ou o tipo (set_meta)."},
	{ErrGraphTriggerTypeMismatch, LintTriggerIncompatible, "O gatilho não é compatível com o tipo do workflow. Troque o gatilho ou o tipo (set_meta)."},
	{ErrInvalidTriggerType, LintTriggerIncompatible, "Use um gatilho do catálogo compatível com o tipo do workflow."},
	{ErrTriggerNotAllowedForType, LintTriggerIncompatible, "O gatilho não é compatível com o tipo do workflow. Troque o gatilho ou o tipo (set_meta)."},
	{ErrGraphDuplicateTriggerType, LintDuplicateTriggerType, "Há gatilhos duplicados do mesmo tipo. Mantenha apenas um por tipo."},
	{ErrGraphTriggerHasIncoming, LintTriggerHasIncoming, "Um gatilho é sempre o começo do fluxo. Remova a conexão que chega nele."},
	{ErrGraphTriggerNotConnected, LintTriggerNotConnected, "Conecte a saída do gatilho ao primeiro passo do fluxo."},
	{ErrGraphNodeNoIncoming, LintNodeNoIncoming, "Um nó (não-gatilho) não tem aresta de entrada. Conecte-o a partir de um nó anterior."},
	{ErrGraphNodeNoOutgoing, LintNodeNoOutgoing, "Um nó (não-end) não tem aresta de saída. Conecte-o a um próximo nó ou a 'end'."},
	{ErrGraphOrphanNode, LintOrphanNode, "Há nós inalcançáveis a partir do gatilho. Conecte-os ao fluxo ou remova-os."},
	{ErrGraphCycleDetected, LintCycleDetected, "Há um ciclo inválido. Ciclos só são permitidos quando contêm um nó wait_* ou action_loop e conseguem alcançar um nó 'end'."},
	{ErrNodeIncompatibleScope, LintNodeIncompatibleScope, "Este tipo de nó não é permitido neste tipo de workflow. Remova-o ou troque por um nó compatível."},
	{ErrNodeMissingRequiredOutput, LintMissingRequiredOutput, "Conecte uma aresta a partir de cada saída obrigatória deste nó (o rótulo da aresta deve ser o id da saída)."},
	{ErrNodeMissingRequiredField, LintMissingRequiredField, "Preencha o campo de configuração obrigatório (use get_node_spec para ver o schema do nó)."},
	{ErrNodeConfigMissing, LintMissingRequiredField, "Preencha a configuração obrigatória deste nó."},
	{ErrNodeFieldOutOfRange, LintFieldOutOfRange, "Ajuste o valor para dentro do intervalo permitido do campo (veja Min/Max em get_node_spec)."},
	{ErrNodeInvalidTemplateID, LintInvalidResourceRef, resourceRefHint},
	{ErrNodeInvalidAgentID, LintInvalidResourceRef, resourceRefHint},
	{ErrNodeInvalidMediaID, LintInvalidResourceRef, resourceRefHint},
	{ErrNodeInvalidQueueID, LintInvalidResourceRef, "Em Transferir para Fila, escolha uma fila de atendimento deste workspace."},
	{ErrNodeInvalidLabelID, LintInvalidResourceRef, resourceRefHint},
	{ErrNodeInvalidStageID, LintInvalidResourceRef, resourceRefHint},
	{ErrNodeInvalidOpportunityPipeline, LintInvalidResourceRef, resourceRefHint},
	{ErrNodeInvalidDepartmentID, LintInvalidResourceRef, resourceRefHint},
	{ErrNodeInvalidMemberID, LintInvalidResourceRef, resourceRefHint},
	{ErrNodeInvalidBusinessPhoneID, LintInvalidResourceRef, resourceRefHint},
	{ErrNodeInvalidWorkflowID, LintInvalidResourceRef, resourceRefHint},
	{ErrNodeInvalidMCPCollectionID, LintInvalidResourceRef, resourceRefHint},
	{ErrNodeInvalidKnowledgeBaseID, LintInvalidResourceRef, resourceRefHint},
	{ErrNodeInvalidSheetID, LintInvalidResourceRef, "Em Adicionar linha na planilha, escolha uma planilha deste workspace."},
	{ErrNodeSheetMappingInvalid, LintSheetMappingInvalid, "Em Adicionar linha na planilha, preencha toda coluna obrigatória e use só colunas ativas da planilha."},
	{ErrNodeInvalidModelID, LintInvalidResourceRef, "Escolha um modelo de IA da lista (find_resource ai_models)."},
	{ErrNodeReasoningLevelUnsupported, LintReasoningUnsupported, "Escolha um nível de raciocínio que o modelo oferece ou deixe o padrão."},
	{ErrNodeInvalidToolMode, LintInvalidConfigValue, "O modo de ferramentas do agente de IA deve ser 'route' ou 'execute'."},
	{ErrNodeInvalidResponseMode, LintInvalidConfigValue, "O modo de resposta do agente de IA deve ser 'default' ou 'segmented'."},
	{ErrNodeInvalidToolParamType, LintInvalidToolParamType, "O 'type' de cada parâmetro de custom_tools deve ser um dos tipos suportados."},
	{ErrNodeSegmentedSendConflict, LintSegmentedSendConflict, "Remova o nó send_text após um agente de IA em modo segmentado, ele já envia as mensagens."},
	{ErrNodeInvalidInteractiveConfig, LintInvalidInteractiveConfig, "No nó de botões/lista: use até 3 botões OU até 10 linhas de lista; cada opção precisa de id e título únicos."},
	{ErrNodeUnreadableConfig, LintConfigShapeMismatch, "Este campo do nó não pôde ser lido no formato esperado. Abra o nó e preencha as opções de novo pelo editor."},
	{ErrNodeInvalidDTMFConfig, LintInvalidDTMFConfig, "Em Aguardar Tecla, escolha ao menos uma tecla (0-9, * ou #), sem repetir."},
	{ErrConsentGrantNeedsReply, LintConsentGrantNeedsReply, "Registrar consentimento só vale depois da resposta do lead em todos os caminhos. Para opt-out use Retirar."},
	{ErrAgentCallInputsInvalid, LintAgentCallInputsInvalid, "No gatilho Chamado por agente IA, cada campo precisa de um nome único e de um tipo válido."},
	{ErrReplyToAgentWithoutTrigger, LintReplyToAgentWithoutTrigger, "O nó Responder ao agente só funciona em fluxos com o gatilho Chamado por agente IA."},
	{ErrPinInvalid, LintPinInvalid, "Corrija ou remova a saída fixada deste nó."},
	{ErrTestInputInvalid, LintWorkflowInvalid, "Corrija ou remova o cenário de teste salvo neste gatilho: cada um precisa de id e nome únicos e de no máximo 64 KB."},
	{ErrVoiceTrunkRequired, LintVoiceTrunkInvalid, "Escolha uma linha telefônica deste workspace que receba ligações."},
	{ErrVoiceTrunkInvalid, LintVoiceTrunkInvalid, "Escolha uma linha telefônica deste workspace que receba ligações."},
	{ErrNameRequired, LintWorkflowInvalid, "Dê um nome ao workflow."},
	{ErrInvalidStatus, LintWorkflowInvalid, "O status do workflow é inválido."},
}

func validationClassOf(err error) (validationClass, bool) {
	if err == nil {
		return validationClass{}, false
	}
	for _, class := range validationClasses {
		if errors.Is(err, class.err) {
			return class, true
		}
	}
	return validationClass{}, false
}

func IsValidationError(err error) bool {
	if _, ok := NotPublishableOf(err); ok {
		return true
	}
	_, ok := validationClassOf(err)
	return ok
}

func issueInfo(err error) (LintIssueCode, string) {
	class, ok := validationClassOf(err)
	if !ok {
		return LintGraphStructure, genericStructureHint
	}
	if class.hint == "" {
		return class.code, genericStructureHint
	}
	return class.code, class.hint
}

func IssueOf(err error) LintIssue {
	code, hint := issueInfo(err)
	return LintIssue{Code: code, Severity: SeverityBlocking, NodeID: nodeIDFromErr(err), Path: missingOutputOf(err), Message: err.Error(), Hint: hint}
}

type NotPublishableError struct {
	Issues []LintIssue
	causes []error
}

func (e *NotPublishableError) Error() string {
	messages := make([]string, 0, len(e.Issues))
	for _, i := range e.Issues {
		messages = append(messages, i.Message)
	}
	return fmt.Sprintf("workflow: the draft cannot be published, %d blocking problems: %s", len(e.Issues), strings.Join(messages, "; "))
}

func (e *NotPublishableError) Unwrap() []error {
	return e.causes
}

func NotPublishableOf(err error) (*NotPublishableError, bool) {
	var blocked *NotPublishableError
	if errors.As(err, &blocked) {
		return blocked, true
	}
	return nil, false
}

func NotPublishableFrom(report LintReport, problems []error) error {
	issues := report.Blocking()
	causes := append([]error(nil), report.causes...)
	for _, p := range problems {
		issues = append(issues, IssueOf(p))
		causes = append(causes, p)
	}
	if len(issues) == 0 {
		return nil
	}
	return &NotPublishableError{Issues: issues, causes: causes}
}
