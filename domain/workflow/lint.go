package workflow

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"vozko/domain/redact"
)

type LintSeverity string

const (
	SeverityBlocking LintSeverity = "blocking"
	SeverityAdvisory LintSeverity = "advisory"
	SeverityInfo     LintSeverity = "info"
)

type LintIssueCode string

const (
	LintGraphEmpty               LintIssueCode = "GRAPH_EMPTY"
	LintGraphTooManyNodes        LintIssueCode = "GRAPH_TOO_MANY_NODES"
	LintGraphTooManyEdges        LintIssueCode = "GRAPH_TOO_MANY_EDGES"
	LintInvalidWorkflowType      LintIssueCode = "GRAPH_INVALID_WORKFLOW_TYPE"
	LintDuplicateNodeID          LintIssueCode = "GRAPH_DUPLICATE_NODE_ID"
	LintInvalidNodeType          LintIssueCode = "NODE_INVALID_TYPE"
	LintInvalidEdgeRef           LintIssueCode = "EDGE_INVALID_REF"
	LintNoTrigger                LintIssueCode = "GRAPH_NO_TRIGGER"
	LintNoEnd                    LintIssueCode = "GRAPH_NO_END"
	LintTriggerIncompatible      LintIssueCode = "TRIGGER_INCOMPATIBLE_WITH_TYPE"
	LintDuplicateTriggerType     LintIssueCode = "GRAPH_DUPLICATE_TRIGGER_TYPE"
	LintTriggerHasIncoming       LintIssueCode = "TRIGGER_HAS_INCOMING_EDGE"
	LintTriggerNotConnected      LintIssueCode = "TRIGGER_NOT_CONNECTED"
	LintNodeNoIncoming           LintIssueCode = "NODE_NO_INCOMING"
	LintNodeNoOutgoing           LintIssueCode = "NODE_NO_OUTGOING"
	LintOrphanNode               LintIssueCode = "GRAPH_ORPHAN_NODE"
	LintCycleDetected            LintIssueCode = "GRAPH_CYCLE_DETECTED"
	LintGraphStructure           LintIssueCode = "GRAPH_STRUCTURE"
	LintNodeIncompatibleScope    LintIssueCode = "NODE_INCOMPATIBLE_SCOPE"
	LintMissingRequiredOutput    LintIssueCode = "NODE_MISSING_REQUIRED_OUTPUT"
	LintMissingRequiredField     LintIssueCode = "NODE_MISSING_REQUIRED_FIELD"
	LintFieldOutOfRange          LintIssueCode = "NODE_FIELD_OUT_OF_RANGE"
	LintSegmentedSendConflict    LintIssueCode = "NODE_SEGMENTED_SEND_CONFLICT"
	LintInvalidToolParamType     LintIssueCode = "NODE_INVALID_TOOL_PARAM_TYPE"
	LintInvalidInteractiveConfig LintIssueCode = "NODE_INVALID_INTERACTIVE_CONFIG"
	LintInvalidCarrierMessage    LintIssueCode = "NODE_INVALID_CARRIER_MESSAGE"
	LintInvalidWebhookTrigger    LintIssueCode = "NODE_INVALID_WEBHOOK_TRIGGER"
	LintNodeNeedsConversation    LintIssueCode = "NODE_NEEDS_CONVERSATION"
	LintInvalidDTMFConfig        LintIssueCode = "NODE_INVALID_DTMF_CONFIG"
	LintInvalidResourceRef       LintIssueCode = "NODE_INVALID_RESOURCE_REF"
	LintUnknownCustomField       LintIssueCode = "NODE_UNKNOWN_CUSTOM_FIELD"
	LintSensitiveCustomField     LintIssueCode = "NODE_SENSITIVE_CUSTOM_FIELD"
	LintInvalidConfigValue       LintIssueCode = "NODE_INVALID_CONFIG_VALUE"
	LintReasoningUnsupported     LintIssueCode = "NODE_REASONING_UNSUPPORTED"
	LintPinInvalid               LintIssueCode = "NODE_PIN_INVALID"
	LintSheetMappingInvalid      LintIssueCode = "NODE_SHEET_MAPPING_INVALID"
	LintVoiceTrunkInvalid        LintIssueCode = "WORKFLOW_VOICE_TRUNK_INVALID"
	LintWorkflowInvalid          LintIssueCode = "WORKFLOW_INVALID"

	LintBadHandleLabel      LintIssueCode = "EDGE_BAD_HANDLE_LABEL"
	LintDanglingDataRef     LintIssueCode = "DATA_REF_DANGLING"
	LintUnknownOutputKey    LintIssueCode = "DATA_REF_UNKNOWN_KEY"
	LintMissingResourceRef  LintIssueCode = "NODE_MISSING_RESOURCE_REF"
	LintUnknownConfigKey    LintIssueCode = "NODE_UNKNOWN_CONFIG_KEY"
	LintConfigShapeMismatch LintIssueCode = "NODE_CONFIG_SHAPE_MISMATCH"
	LintCodeReadsTemplate   LintIssueCode = "NODE_CODE_READS_TEMPLATE"
	LintSecretInPlainText   LintIssueCode = "NODE_SECRET_IN_PLAIN_TEXT"
	LintPinnedOutput        LintIssueCode = "NODE_PINNED_OUTPUT"
)

type LintIssue struct {
	Code     LintIssueCode `json:"code"`
	Severity LintSeverity  `json:"severity"`
	NodeID   string        `json:"nodeId,omitempty"`
	Field    string        `json:"field,omitempty"`
	Path     string        `json:"path,omitempty"`
	EdgeRef  string        `json:"edgeRef,omitempty"`
	Message  string        `json:"message"`
	Hint     string        `json:"hint,omitempty"`
}

type LintReport struct {
	Issues []LintIssue `json:"issues"`
	causes []error
}

func (r LintReport) Blocking() []LintIssue {
	out := make([]LintIssue, 0, len(r.Issues))
	for _, i := range r.Issues {
		if i.Severity == SeverityBlocking {
			out = append(out, i)
		}
	}
	return out
}

func (r LintReport) Advisory() []LintIssue {
	out := make([]LintIssue, 0, len(r.Issues))
	for _, i := range r.Issues {
		if i.Severity == SeverityAdvisory {
			out = append(out, i)
		}
	}
	return out
}

func (r LintReport) WithProblems(problems []error) LintReport {
	for _, p := range problems {
		r.Issues = append(r.Issues, IssueOf(p))
		r.causes = append(r.causes, p)
	}
	return r
}

func (r LintReport) IsGreen() bool {
	for _, i := range r.Issues {
		if i.Severity == SeverityBlocking {
			return false
		}
	}
	return true
}

type DynamicHandleResolver func(n Node) (handles []HandleDefinition, ok bool)

type GraphRule struct {
	Code     LintIssueCode
	Hint     string
	Validate func(*Graph) error
}

var PureGraphRules = []GraphRule{
	{
		Code:     LintSegmentedSendConflict,
		Hint:     "Remova o nó send_text após um agente de IA em modo segmentado, ele já envia as mensagens.",
		Validate: ValidateSegmentedSendConflict,
	},
	{
		Code:     LintMissingRequiredField,
		Hint:     "O nó de agente de IA tem dois modos pelo campo 'source': em 'prompt' preencha 'model' (find_resource ai_models) e 'instructions'; em 'agent' (o padrão quando 'source' está vazio) preencha 'agent_id' (find_resource agents). Não misture, model/instructions são ignorados em modo agente, e agent_id é ignorado em modo prompt.",
		Validate: ValidateAIAgentSourceConfig,
	},
	{
		Code:     LintInvalidToolParamType,
		Hint:     "O 'type' de cada parâmetro de custom_tools deve ser um dos tipos suportados: string, number, integer, boolean, array, object, date, time, datetime, email, phone ou enum.",
		Validate: ValidateAIAgentToolParams,
	},
	{
		Code:     LintInvalidInteractiveConfig,
		Hint:     "No nó de botões/lista: use até 3 botões OU até 10 linhas de lista (máx. 10 seções); cada opção precisa de id e título; ids únicos; títulos de botão únicos e com no máximo 20 caracteres.",
		Validate: ValidateInteractivePromptConfig,
	},
	{
		Code:     LintInvalidCarrierMessage,
		Hint:     "No nó action_send_carrier, 'channel' é 'rcs' (com 'sender_id' e 'content' no formato do compositor RCS: text, media, cards e suggestions) ou 'sms' (com 'route_id' e 'text' de até 8 partes). Variáveis {{...}} são aceitas e conferidas de novo no envio.",
		Validate: ValidateCarrierSends,
	},
	{
		Code:     LintInvalidWebhookTrigger,
		Hint:     "No gatilho Webhook, 'start_on' é 'conversation' (o padrão: a conversa existente pelo entry_id ou telefone) ou 'lead' (o lead pelo telefone, criado se não existir, ou pelo e-mail). 'events' é uma lista opcional de nomes de evento aceitos.",
		Validate: ValidateWebhookTriggers,
	},
	{
		Code:     LintInvalidPlatformEmail,
		Hint:     "No nó action_send_platform_email, 'body_mode' é 'text' (com 'text') ou 'design' (com 'design_id' escolhido na lista de designs salvos). 'sender_id' vazio usa o remetente padrão; remetente e design vêm da lista, não de variáveis. Assunto, destinatário e texto aceitam variáveis {{...}}.",
		Validate: ValidatePlatformEmailSends,
	},
	{
		Code:     LintMissingRequiredField,
		Hint:     "O nó action_update_lead precisa de ao menos um dado para gravar: CEP, logradouro, número, complemento, bairro, cidade, UF, data de nascimento ou um campo personalizado em custom_fields (chave do campo e valor, que aceita variáveis).",
		Validate: ValidateUpdateLeadConfig,
	},
	{
		Code:     LintMissingRequiredField,
		Hint:     "No nó action_update_lead, um endereço (logradouro, número, complemento ou bairro) precisa do CEP ou da cidade e UF; sem isso o nó segue pela saída Erro e nada é gravado. O campo número é o número do imóvel, não o telefone: o telefone do lead nunca é alterado por este nó.",
		Validate: ValidateUpdateLeadAddress,
	},
	{
		Code:     LintInvalidDTMFConfig,
		Hint:     "No nó wait_dtmf, 'keys' precisa de ao menos uma tecla, só 0-9, * ou #, sem repetir. Cada tecla vira uma saída com o próprio dígito como rótulo.",
		Validate: ValidateDTMFWaits,
	},
	{
		Code:     LintMissingRequiredField,
		Hint:     "O nó action_consent precisa de 'mode': 'registrar' (grava o consentimento) ou 'retirar' (o lead para de receber).",
		Validate: ValidateConsentModes,
	},
	{
		Code:     LintConsentGrantNeedsReply,
		Hint:     "O nó action_consent em modo 'registrar' só vale depois da resposta do lead em todos os caminhos: a saída 'replied' de wait_for_reply, uma opção de action_send_interactive ou a saída 'true' de uma condição sobre {{selected_option_id}} ou {{selected_option_title}} (botão do template). Para opt-out use o modo 'retirar', que vale em qualquer ponto.",
		Validate: ValidateConsentGrantsFollowAReply,
	},
	{
		Code:     LintAgentCallInputsInvalid,
		Hint:     "No gatilho Chamado por agente IA, cada campo precisa de um nome único com letras minúsculas, dígitos e _, e um tipo entre string, number, boolean, object e array. Sem campos também vale.",
		Validate: ValidateAgentCallTriggers,
	},
	{
		Code:     LintReplyToAgentWithoutTrigger,
		Hint:     "O nó Responder ao agente só funciona em fluxos com o gatilho Chamado por agente IA. Adicione esse gatilho ou remova o nó.",
		Validate: ValidateReplyToAgentNodes,
	},
}

func RunPureGraphRules(g *Graph) error {
	for _, r := range PureGraphRules {
		if err := r.Validate(g); err != nil {
			return err
		}
	}
	return nil
}

func LintGraph(g *Graph, wfType WorkflowType, catalog []NodeDefinition, exempt map[string]bool, resolveHandles DynamicHandleResolver) LintReport {
	report := LintReport{}
	add := func(i LintIssue) { report.Issues = append(report.Issues, i) }
	addProblem := func(err error, i LintIssue) {
		i.Severity = SeverityBlocking
		i.NodeID = nodeIDFromErr(err)
		i.Message = err.Error()
		add(i)
		report.causes = append(report.causes, err)
	}

	for _, err := range GraphProblems(g, wfType, exempt) {
		code, hint := issueInfo(err)
		addProblem(err, LintIssue{Code: code, Hint: hint})
	}

	for _, err := range NodeScopeProblems(g, wfType, catalog) {
		addProblem(err, LintIssue{
			Code: LintNodeIncompatibleScope,
			Hint: fmt.Sprintf("Este tipo de nó não é permitido em workflows do tipo %q. Remova-o ou troque por um nó compatível.", wfType),
		})
	}

	outputProblems := append(RequiredOutputProblems(g, catalog), RequiredDynamicOutputProblems(g, resolveHandles)...)
	for _, err := range outputProblems {
		addProblem(err, LintIssue{
			Code: LintMissingRequiredOutput,
			Path: missingOutputOf(err),
			Hint: "Conecte uma aresta a partir de cada saída obrigatória deste nó (o rótulo da aresta deve ser o id da saída).",
		})
	}

	for _, err := range append(NodeConfigProblems(g, catalog), StructuredConfigProblems(g)...) {
		code, hint := issueInfo(err)
		addProblem(err, LintIssue{Code: code, Field: fieldFromErr(err), Hint: hint})
	}

	for _, rule := range PureGraphRules {
		if err := rule.Validate(g); err != nil {
			addProblem(err, LintIssue{Code: rule.Code, Field: fieldFromErr(err), Hint: rule.Hint})
		}
	}

	defs := NodeCatalogMap(catalog)
	lintEdgeHandles(g, defs, resolveHandles, add)
	lintDataFlow(g, defs, add)
	lintDataRefAvailability(g, defs, add)
	lintFunctionalResourceRefs(g, add)
	lintNodeShape(g, defs, add)
	lintCodeReadsTemplates(g, add)
	lintSecretsInPlainText(g, add)
	lintPinnedOutputs(g, add)
	lintConversationNodesOnLeadRuns(g, defs, add)
	lintSendsOutsideWindow(g, add)

	return report
}

func lintPinnedOutputs(g *Graph, add func(LintIssue)) {
	for _, n := range g.Nodes {
		if n.Pinned == nil {
			continue
		}
		add(LintIssue{
			Code: LintPinnedOutput, Severity: SeverityInfo,
			NodeID:  n.ID,
			Message: "Saída fixada (apenas testes)",
			Hint:    "Nos testes este nó devolve a saída fixada sem executar. Em produção ele sempre executa: a publicação remove as saídas fixadas.",
		})
	}
}

const genericCodeStatePath = "node.<id>.<campo>"

func lintCodeReadsTemplates(g *Graph, add func(LintIssue)) {
	for i := range g.Nodes {
		n := g.Nodes[i]
		if n.Type != NodeTypeActionCode {
			continue
		}
		code, _ := n.Config["code"].(string)
		match := interpolationPattern.FindStringSubmatch(code)
		if match == nil {
			continue
		}
		hint := fmt.Sprintf("Leia a saída do nó anterior com input.<campo> e a de outro nó com state.get('%s').", genericCodeStatePath)
		path := normalizeIndexing(match[1])
		switch {
		case strings.HasPrefix(path, SecretScope+"."):
			hint = fmt.Sprintf("Leia o segredo do workspace com secrets.get('%s').", strings.TrimPrefix(path, SecretScope+"."))
		case !strings.HasPrefix(path, "sys."):
			hint += fmt.Sprintf(" Aqui: state.get('%s').", path)
		}
		add(LintIssue{
			Code: LintCodeReadsTemplate, Severity: SeverityAdvisory,
			NodeID:  n.ID,
			Field:   "code",
			Path:    "code",
			Message: fmt.Sprintf("Código lê template: o nó %q usa %s dentro do JavaScript, mas templates não são interpolados no código, então o script recebe o texto literal", n.ID, match[0]),
			Hint:    hint,
		})
	}
}

var codeCredentialAssignment = regexp.MustCompile("['\"]?([A-Za-z_$][\\w$]*)['\"]?\\s*[:=]\\s*('[^'\\n]+'|\"[^\"\\n]+\"|`[^`\\n]+`)")

var secretNameUnsafe = regexp.MustCompile(`[^a-z0-9_]+`)

func lintSecretsInPlainText(g *Graph, add func(LintIssue)) {
	for i := range g.Nodes {
		n := g.Nodes[i]
		switch n.Type {
		case NodeTypeActionHTTPRequest:
			lintHTTPCredentials(n, add)
		case NodeTypeActionSetVariable:
			name, _ := n.Config["variable"].(string)
			if redact.IsSensitiveKey(name) && isPlainCredential(n.Config["value"]) {
				addPlainSecretIssue(add, n, "value", "config.value", fmt.Sprintf("a variável %q", name), templateSecretHint(name))
			}
		case NodeTypeActionCode:
			code, _ := n.Config["code"].(string)
			if name, ok := codeCredentialLiteral(code); ok {
				addPlainSecretIssue(add, n, "code", "code:"+name, fmt.Sprintf("o valor atribuído a %q no JavaScript", name),
					fmt.Sprintf("Mova o valor para os segredos do workspace e leia com secrets.get('%s').", suggestedSecretName(name)))
			}
		}
		lintTestInputCredentials(n, add)
	}
}

func lintTestInputCredentials(n Node, add func(LintIssue)) {
	for _, scenario := range n.TestInputs {
		path, ok := plainCredentialInJSON(scenario.Input)
		if !ok {
			continue
		}
		key := path[len(path)-1]
		addPlainSecretIssue(add, n, "testInputs", "testInputs."+scenario.Name+"."+strings.Join(path, "."),
			fmt.Sprintf("o campo %q do cenário de teste %q", key, scenario.Name),
			"Apague o valor do cenário salvo e digite-o só na hora do teste; cenários ficam salvos no rascunho.")
	}
}

func lintHTTPCredentials(n Node, add func(LintIssue)) {
	headers := RequestMap(n.Config["headers"])
	for _, key := range sortedKeys(headers) {
		if redact.IsSensitiveKey(key) && isPlainCredential(headers[key]) {
			addPlainSecretIssue(add, n, "headers", "headers."+key, fmt.Sprintf("o cabeçalho %q", key), templateSecretHint(key))
			break
		}
	}
	if isPlainCredential(n.Config["auth_token"]) {
		addPlainSecretIssue(add, n, "auth_token", "auth_token", "o token de autenticação", templateSecretHint("auth_token"))
	}
	body, _ := n.Config["body"].(string)
	if path, ok := plainCredentialInJSON(RequestMap(body)); ok {
		key := path[len(path)-1]
		addPlainSecretIssue(add, n, "body", "body."+strings.Join(path, "."), fmt.Sprintf("o campo %q do corpo", key), templateSecretHint(key))
	}
}

func plainCredentialInJSON(m map[string]interface{}) ([]string, bool) {
	for _, key := range sortedKeys(m) {
		if redact.IsSensitiveKey(key) && isPlainCredential(m[key]) {
			return []string{key}, true
		}
		if nested, ok := m[key].(map[string]interface{}); ok {
			if found, ok := plainCredentialInJSON(nested); ok {
				return append([]string{key}, found...), true
			}
		}
	}
	return nil, false
}

func codeCredentialLiteral(code string) (string, bool) {
	for _, m := range codeCredentialAssignment.FindAllStringSubmatch(code, -1) {
		if !redact.IsSensitiveKey(m[1]) {
			continue
		}
		if strings.HasPrefix(m[2], "`") && strings.Contains(m[2], "${") {
			continue
		}
		return m[1], true
	}
	return "", false
}

func isPlainCredential(v interface{}) bool {
	switch typed := v.(type) {
	case nil:
		return false
	case string:
		trimmed := strings.TrimSpace(typed)
		return trimmed != "" && !strings.Contains(trimmed, "{{")
	case bool:
		return false
	default:
		return true
	}
}

func suggestedSecretName(key string) string {
	name := strings.Trim(secretNameUnsafe.ReplaceAllString(strings.ToLower(key), "_"), "_")
	if len(name) > 64 {
		name = name[:64]
	}
	if name == "" {
		return "nome"
	}
	return name
}

func templateSecretHint(key string) string {
	return fmt.Sprintf("Mova o valor para os segredos do workspace e use {{secret.%s}} no lugar.", suggestedSecretName(key))
}

func addPlainSecretIssue(add func(LintIssue), n Node, field, path, what, hint string) {
	add(LintIssue{
		Code: LintSecretInPlainText, Severity: SeverityAdvisory,
		NodeID:  n.ID,
		Field:   field,
		Path:    path,
		Message: fmt.Sprintf("Segredo em texto no fluxo: o nó %q guarda %s em texto puro, que fica salvo no grafo e visível para quem abre ou exporta o fluxo", n.ID, what),
		Hint:    hint,
	})
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func EdgeHasOutput(outputs []HandleDefinition, label string) bool {
	if label == "" {
		return true
	}
	if len(outputs) == 0 {
		return label == "default"
	}
	for _, o := range outputs {
		if o.ID == label {
			return true
		}
	}
	return false
}

func NodeOutputs(n Node, defs map[NodeType]NodeDefinition, resolveHandles DynamicHandleResolver) ([]HandleDefinition, bool) {
	if resolveHandles != nil {
		if handles, ok := resolveHandles(n); ok {
			return handles, true
		}
	}
	def, ok := defs[n.Type]
	if !ok || def.DynamicHandles {
		return nil, false
	}
	return def.Outputs, true
}

func lintEdgeHandles(g *Graph, defs map[NodeType]NodeDefinition, resolveHandles DynamicHandleResolver, add func(LintIssue)) {
	for i := range g.Nodes {
		n := g.Nodes[i]
		outputs, known := NodeOutputs(n, defs, resolveHandles)
		if !known {
			continue
		}
		for _, e := range g.OutgoingEdges(n.ID) {
			if EdgeHasOutput(outputs, e.Label) {
				continue
			}
			add(LintIssue{
				Code: LintBadHandleLabel, Severity: SeverityBlocking,
				NodeID:  n.ID,
				EdgeRef: e.Source + "->" + e.Target,
				Message: fmt.Sprintf("aresta do nó %q usa o rótulo %q, que não corresponde a nenhuma saída atual do nó", n.ID, e.Label),
				Hint:    "Ligue a conexão de novo a partir de uma das saídas atuais do nó, ou apague-a. Para saídas dinâmicas, defina as opções (cases, custom_tools, botões ou linhas) ANTES de conectar e use o id exato da saída como rótulo da aresta.",
			})
		}
	}
}

func lintDataFlow(g *Graph, defs map[NodeType]NodeDefinition, add func(LintIssue)) {
	for i := range g.Nodes {
		n := g.Nodes[i]
		deps := ExtractDependencies(n.Config)
		if len(deps) == 0 {
			continue
		}
		var ancestors map[string]bool
		for _, dep := range deps {
			if dep.Scope != "node" || dep.NodeID == "" {
				continue
			}
			if ancestors == nil {
				ancestors = g.AncestorsOf(n.ID)
			}
			producer := g.FindNode(dep.NodeID)
			if producer == nil || !ancestors[dep.NodeID] {
				add(LintIssue{
					Code: LintDanglingDataRef, Severity: SeverityAdvisory,
					NodeID:  n.ID,
					Message: fmt.Sprintf("nó %q referencia {{node.%s.%s}}, mas %q não é um ancestral (o valor não estará disponível em tempo de execução)", n.ID, dep.NodeID, dep.Key, dep.NodeID),
					Hint:    "Referencie apenas saídas de nós que estejam ANTES deste no fluxo (ancestrais).",
				})
				continue
			}
			if dep.Key == "" {
				continue
			}
			pdef, hasDef := defs[producer.Type]
			if !hasDef || len(pdef.OutputKeys) == 0 {
				continue
			}
			found := false
			for _, ok := range pdef.OutputKeys {
				if ok.Key == dep.Key {
					found = true
					break
				}
			}
			if !found {
				add(LintIssue{
					Code: LintUnknownOutputKey, Severity: SeverityAdvisory,
					NodeID:  n.ID,
					Message: fmt.Sprintf("nó %q referencia a chave %q de {{node.%s}}, que não consta nas saídas declaradas desse nó", n.ID, dep.Key, dep.NodeID),
					Hint:    "Use uma das chaves de saída declaradas do nó produtor (veja get_node_spec).",
				})
			}
		}
	}
}

func lintNodeShape(g *Graph, defs map[NodeType]NodeDefinition, add func(LintIssue)) {
	for i := range g.Nodes {
		n := &g.Nodes[i]
		def, ok := defs[n.Type]
		if !ok || len(def.ConfigSchema) == 0 || len(n.Config) == 0 {
			continue
		}

		declared := make(map[string]ConfigField, len(def.ConfigSchema))
		for _, f := range def.ConfigSchema {
			declared[f.Key] = f
		}
		known := make(map[string]bool, len(declared)+len(def.DefaultConfig))
		for k := range declared {
			known[k] = true
		}
		for k := range def.DefaultConfig {
			known[k] = true
		}

		for key, val := range n.Config {
			if IsCommonNodeConfigKey(key) {
				continue
			}
			if !known[key] {
				add(LintIssue{
					Code: LintUnknownConfigKey, Severity: SeverityAdvisory,
					NodeID:  n.ID,
					Field:   key,
					Message: fmt.Sprintf("nó %q define a chave de config %q, que não consta no schema do tipo %s, pode ser ignorada em tempo de execução", n.ID, key, n.Type),
					Hint:    "Use apenas campos declarados para este tipo de nó (veja get_node_spec). Se a chave for válida, ela ainda não está declarada no schema.",
				})
				continue
			}
			lintFieldShape(n, declared[key], val, add)
		}
	}
}

func lintFieldShape(n *Node, f ConfigField, val interface{}, add func(LintIssue)) {
	if val == nil {
		return
	}
	switch f.Type {
	case "tools":
		arr, ok := val.([]interface{})
		if !ok {
			add(LintIssue{
				Code: LintConfigShapeMismatch, Severity: SeverityAdvisory,
				NodeID: n.ID, Field: f.Key,
				Message: fmt.Sprintf("nó %q campo %q deveria ser uma lista de ferramentas, mas é %T", n.ID, f.Key, val),
				Hint:    "custom_tools é um array: [{name, description, parameters:[{name, type, required, ...}]}].",
			})
			return
		}
		for _, item := range arr {
			tm, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			params, exists := tm["parameters"]
			if !exists || params == nil {
				continue
			}
			if _, isArr := params.([]interface{}); !isArr {
				toolName, _ := tm["name"].(string)
				add(LintIssue{
					Code: LintConfigShapeMismatch, Severity: SeverityAdvisory,
					NodeID: n.ID, Field: f.Key,
					Message: fmt.Sprintf("nó %q ferramenta %q tem 'parameters' como %T (formato JSON-Schema) em vez de array, assim os parâmetros são DESCARTADOS em runtime e a IA chama a ferramenta sem argumentos", n.ID, toolName, params),
					Hint:    "Declare parameters como array: \"parameters\": [{\"name\":\"cep\",\"type\":\"string\",\"required\":true}]. NÃO use o objeto {type:object, properties:{...}}.",
				})
			}
		}
	case "multi-select":
		if _, ok := val.([]interface{}); !ok {
			add(LintIssue{
				Code: LintConfigShapeMismatch, Severity: SeverityAdvisory,
				NodeID: n.ID, Field: f.Key,
				Message: fmt.Sprintf("nó %q campo %q (multi-select) deveria ser um array, mas é %T", n.ID, f.Key, val),
				Hint:    "Campos multi-select são arrays de ids/valores.",
			})
		}
	}
}

func lintFunctionalResourceRefs(g *Graph, add func(LintIssue)) {
	emit := func(nodeID, field, what string) {
		add(LintIssue{
			Code: LintMissingResourceRef, Severity: SeverityAdvisory,
			NodeID:  nodeID,
			Field:   field,
			Message: fmt.Sprintf("nó %q não define %q (%s), o nó não terá efeito em tempo de execução", nodeID, field, what),
			Hint:    fmt.Sprintf("Use find_resource para resolver o id e set/update_node para preencher %q.", field),
		})
	}
	cfgStr := func(c map[string]interface{}, k string) string {
		s, _ := c[k].(string)
		return strings.TrimSpace(s)
	}
	for i := range g.Nodes {
		n := g.Nodes[i]
		switch n.Type {
		case NodeTypeActionAIAgent:
			source := cfgStr(n.Config, "source")
			if source != "prompt" && cfgStr(n.Config, "agent_id") == "" {
				emit(n.ID, "agent_id", "agente referenciado")
			}
		case NodeTypeActionSendTemplate:
			if cfgStr(n.Config, "template_id") == "" {
				emit(n.ID, "template_id", "template a enviar")
			}
		case NodeTypeActionAssignLabel, NodeTypeConditionCheckLabel:
			if cfgStr(n.Config, "label_id") == "" {
				emit(n.ID, "label_id", "etiqueta")
			}
		case NodeTypeActionMoveStage, NodeTypeConditionCheckStage:
			if cfgStr(n.Config, "stage_id") == "" {
				emit(n.ID, "stage_id", "etapa")
			}
		case NodeTypeActionManageOpportunity, NodeTypeConditionCheckOpportunity:
			if cfgStr(n.Config, "pipeline_id") == "" {
				emit(n.ID, "pipeline_id", "funil de oportunidades")
			}
		case NodeTypeActionAssignMember:
			if cfgStr(n.Config, "member_id") == "" {
				emit(n.ID, "member_id", "atendente")
			}
		case NodeTypeActionRunWorkflow:
			if cfgStr(n.Config, "workflow_id") == "" {
				emit(n.ID, "workflow_id", "workflow referenciado")
			}
		}
	}
}

func nodeIDFromErr(err error) string {
	return firstQuoted(err.Error())
}

func fieldFromErr(err error) string {
	parts := quotedParts(err.Error())
	if len(parts) >= 2 {
		return parts[len(parts)-1]
	}
	return ""
}

func firstQuoted(s string) string {
	p := quotedParts(s)
	if len(p) > 0 {
		return p[0]
	}
	return ""
}

func quotedParts(s string) []string {
	var out []string
	for {
		i := strings.IndexByte(s, '"')
		if i < 0 {
			break
		}
		j := strings.IndexByte(s[i+1:], '"')
		if j < 0 {
			break
		}
		out = append(out, s[i+1:i+1+j])
		s = s[i+2+j:]
	}
	return out
}

func lintConversationNodesOnLeadRuns(g *Graph, defs map[NodeType]NodeDefinition, add func(LintIssue)) {
	startsOnLead := false
	for _, n := range g.Nodes {
		if TriggerNodeStartsOnLead(n) {
			startsOnLead = true
			break
		}
	}
	if !startsOnLead {
		return
	}
	for _, n := range g.Nodes {
		if !defs[n.Type].NeedsConversation {
			continue
		}
		add(LintIssue{
			Code: LintNodeNeedsConversation, Severity: SeverityAdvisory,
			NodeID:  n.ID,
			Message: fmt.Sprintf("O nó %q precisa de uma conversa, mas este fluxo começa em um lead", n.ID),
			Hint:    "Execuções que começam em um lead (Aniversário, ou Webhook iniciando em Lead) não têm conversa aberta, então este nó segue sem efeito. Use nós que funcionam sem conversa, como e-mail, CRM e requisição HTTP.",
		})
	}
}
