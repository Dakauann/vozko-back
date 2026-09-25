package workflow

import (
	"errors"
	"fmt"
	"strings"
)

type LintSeverity string

const (
	SeverityBlocking LintSeverity = "blocking"
	SeverityAdvisory LintSeverity = "advisory"
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

	LintBadHandleLabel      LintIssueCode = "EDGE_BAD_HANDLE_LABEL"
	LintDanglingDataRef     LintIssueCode = "DATA_REF_DANGLING"
	LintUnknownOutputKey    LintIssueCode = "DATA_REF_UNKNOWN_KEY"
	LintMissingResourceRef  LintIssueCode = "NODE_MISSING_RESOURCE_REF"
	LintUnknownConfigKey    LintIssueCode = "NODE_UNKNOWN_CONFIG_KEY"
	LintConfigShapeMismatch LintIssueCode = "NODE_CONFIG_SHAPE_MISMATCH"
)

type LintIssue struct {
	Code     LintIssueCode `json:"code"`
	Severity LintSeverity  `json:"severity"`
	NodeID   string        `json:"nodeId,omitempty"`
	Field    string        `json:"field,omitempty"`
	EdgeRef  string        `json:"edgeRef,omitempty"`
	Message  string        `json:"message"`
	Hint     string        `json:"hint,omitempty"`
}

type LintReport struct {
	Issues []LintIssue `json:"issues"`
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

	if err := ValidateGraph(g, wfType, exempt); err != nil {
		code, hint := structuralIssueInfo(err)
		add(LintIssue{Code: code, Severity: SeverityBlocking, NodeID: nodeIDFromErr(err), Message: err.Error(), Hint: hint})
	}

	if err := ValidateNodeScopes(g, wfType, catalog); err != nil {
		add(LintIssue{
			Code: LintNodeIncompatibleScope, Severity: SeverityBlocking,
			NodeID:  nodeIDFromErr(err),
			Message: err.Error(),
			Hint:    fmt.Sprintf("Este tipo de nó não é permitido em workflows do tipo %q. Remova-o ou troque por um nó compatível.", wfType),
		})
	}

	if err := ValidateRequiredOutputEdges(g, catalog); err != nil {
		add(LintIssue{
			Code: LintMissingRequiredOutput, Severity: SeverityBlocking,
			NodeID:  nodeIDFromErr(err),
			Message: err.Error(),
			Hint:    "Conecte uma aresta a partir de cada saída obrigatória deste nó (o rótulo da aresta deve ser o id da saída).",
		})
	}

	if err := ValidateRequiredDynamicOutputs(g, resolveHandles); err != nil {
		add(LintIssue{
			Code: LintMissingRequiredOutput, Severity: SeverityBlocking,
			NodeID:  nodeIDFromErr(err),
			Message: err.Error(),
			Hint:    "Conecte uma aresta a partir de cada saída obrigatória deste nó (o rótulo da aresta deve ser o id da saída).",
		})
	}

	if err := ValidateNodeConfigs(g, catalog); err != nil {
		code := LintMissingRequiredField
		hint := "Preencha o campo de configuração obrigatório (use get_node_spec para ver o schema do nó)."
		if errors.Is(err, ErrNodeFieldOutOfRange) {
			code = LintFieldOutOfRange
			hint = "Ajuste o valor para dentro do intervalo permitido do campo (veja Min/Max em get_node_spec)."
		}
		add(LintIssue{
			Code: code, Severity: SeverityBlocking,
			NodeID:  nodeIDFromErr(err),
			Field:   fieldFromErr(err),
			Message: err.Error(),
			Hint:    hint,
		})
	}

	for _, rule := range PureGraphRules {
		if err := rule.Validate(g); err != nil {
			add(LintIssue{
				Code: rule.Code, Severity: SeverityBlocking,
				NodeID:  nodeIDFromErr(err),
				Field:   fieldFromErr(err),
				Message: err.Error(),
				Hint:    rule.Hint,
			})
		}
	}

	defs := NodeCatalogMap(catalog)
	lintDynamicHandles(g, resolveHandles, add)
	lintDataFlow(g, defs, add)
	lintFunctionalResourceRefs(g, add)
	lintNodeShape(g, defs, add)

	return report
}

func structuralIssueInfo(err error) (LintIssueCode, string) {
	switch {
	case errors.Is(err, ErrGraphEmpty):
		return LintGraphEmpty, "O grafo está vazio. Adicione ao menos um nó de gatilho (trigger) e um nó 'end'."
	case errors.Is(err, ErrGraphTooManyNodes):
		return LintGraphTooManyNodes, "O grafo excede o número máximo de nós. Simplifique o fluxo."
	case errors.Is(err, ErrGraphTooManyEdges):
		return LintGraphTooManyEdges, "O grafo excede o número máximo de arestas. Simplifique as conexões."
	case errors.Is(err, ErrInvalidWorkflowType):
		return LintInvalidWorkflowType, "Defina um tipo de workflow válido ('messages' ou 'voip') via set_meta."
	case errors.Is(err, ErrGraphDuplicateNodeID):
		return LintDuplicateNodeID, "Há ids de nó duplicados. Cada nó precisa de um id único."
	case errors.Is(err, ErrInvalidNodeType):
		return LintInvalidNodeType, "Há um nó com tipo inválido. Use apenas tipos do catálogo."
	case errors.Is(err, ErrGraphInvalidEdgeRef):
		return LintInvalidEdgeRef, "Uma aresta referencia um nó inexistente. Remova-a ou corrija os ids de source/target."
	case errors.Is(err, ErrGraphNoTrigger):
		return LintNoTrigger, "Adicione um nó de gatilho (trigger), todo workflow precisa de pelo menos um."
	case errors.Is(err, ErrGraphNoEndNode):
		return LintNoEnd, "Adicione um nó 'end', todo workflow precisa de pelo menos um caminho que termine em 'end'."
	case errors.Is(err, ErrGraphTriggerIncompatibleWithType):
		return LintTriggerIncompatible, "O gatilho não é compatível com o tipo do workflow. Troque o gatilho ou o tipo (set_meta)."
	case errors.Is(err, ErrGraphDuplicateTriggerType):
		return LintDuplicateTriggerType, "Há gatilhos duplicados do mesmo tipo. Mantenha apenas um por tipo."
	case errors.Is(err, ErrGraphNodeNoIncoming):
		return LintNodeNoIncoming, "Um nó (não-gatilho) não tem aresta de entrada. Conecte-o a partir de um nó anterior."
	case errors.Is(err, ErrGraphNodeNoOutgoing):
		return LintNodeNoOutgoing, "Um nó (não-end) não tem aresta de saída. Conecte-o a um próximo nó ou a 'end'."
	case errors.Is(err, ErrGraphOrphanNode):
		return LintOrphanNode, "Há nós inalcançáveis a partir do gatilho. Conecte-os ao fluxo ou remova-os."
	case errors.Is(err, ErrGraphCycleDetected):
		return LintCycleDetected, "Há um ciclo inválido. Ciclos só são permitidos quando contêm um nó wait_* ou action_loop e conseguem alcançar um nó 'end'."
	default:
		return LintGraphStructure, "Corrija a estrutura do grafo conforme a mensagem."
	}
}

func lintDynamicHandles(g *Graph, resolveHandles DynamicHandleResolver, add func(LintIssue)) {
	if resolveHandles == nil {
		return
	}
	reserved := map[string]bool{"": true, "default": true, "erro": true}
	for i := range g.Nodes {
		n := g.Nodes[i]
		handles, ok := resolveHandles(n)
		if !ok {
			continue
		}
		valid := make(map[string]bool, len(handles))
		for _, h := range handles {
			valid[h.ID] = true
		}
		for _, e := range g.OutgoingEdges(n.ID) {
			if reserved[e.Label] || valid[e.Label] {
				continue
			}
			add(LintIssue{
				Code: LintBadHandleLabel, Severity: SeverityAdvisory,
				NodeID:  n.ID,
				EdgeRef: e.Source + "->" + e.Target,
				Message: fmt.Sprintf("aresta do nó %q usa o rótulo %q, que não corresponde a nenhuma saída atual do nó", n.ID, e.Label),
				Hint:    "Defina as saídas (cases / custom_tools) ANTES de conectar, e use o id exato da saída como rótulo da aresta.",
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
				emit(n.ID, "pipeline_id", "funil de negócios")
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
