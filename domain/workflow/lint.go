package workflow

import (
	"errors"
	"fmt"
	"strings"
)

// LintGraph is the source-of-truth accumulating validator for the AI Workflow
// Builder. It is pure and deterministic, safe to call on every loop iteration.
//
// Design contract (the termination guarantee):
//
//   - The BLOCKING tier is composed ENTIRELY of calls to the same pure functions
//     the production save/activate path uses (ValidateGraph, ValidateNodeScopes,
//     ValidateRequiredOutputEdges, ValidateNodeConfigs[required-only],
//     ValidateSegmentedSendConflict). Therefore
//     LintReport.IsGreen() is true iff the graph passes every PURE production
//     rule, never stricter, never more lenient. The builder can only finish on a
//     green report, so "the builder said valid" == "production accepts it".
//
//   - The ADVISORY tier (data-flow refs, dynamic-handle labels, functional
//     resource references) is surfaced to the model as fix hints but NEVER gates
//     finish. This keeps the green gate a subset/equal of production's gate, so a
//     production-valid graph is never "unfinishable", while still nudging the
//     model toward fully-wired, runtime-correct graphs.
//
// Repo-backed resource existence (template/agent/etc. must exist in the
// workspace) is NOT checked here, that requires IO and runs at activation. The
// AI builder resolves resource ids against live workspace data up front, so
// referenced ids are real.

type LintSeverity string

const (
	SeverityBlocking LintSeverity = "blocking"
	SeverityAdvisory LintSeverity = "advisory"
)

type LintIssueCode string

const (
	// --- BLOCKING: a green report requires zero of these. ---
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
	LintGraphStructure           LintIssueCode = "GRAPH_STRUCTURE" // structural failure not mapped above
	LintNodeIncompatibleScope    LintIssueCode = "NODE_INCOMPATIBLE_SCOPE"
	LintMissingRequiredOutput    LintIssueCode = "NODE_MISSING_REQUIRED_OUTPUT"
	LintMissingRequiredField     LintIssueCode = "NODE_MISSING_REQUIRED_FIELD"
	LintFieldOutOfRange          LintIssueCode = "NODE_FIELD_OUT_OF_RANGE"
	LintSegmentedSendConflict    LintIssueCode = "NODE_SEGMENTED_SEND_CONFLICT"
	LintInvalidToolParamType     LintIssueCode = "NODE_INVALID_TOOL_PARAM_TYPE"
	LintInvalidInteractiveConfig LintIssueCode = "NODE_INVALID_INTERACTIVE_CONFIG"

	// --- ADVISORY: never gate finish; fix hints only. ---
	LintBadHandleLabel      LintIssueCode = "EDGE_BAD_HANDLE_LABEL"
	LintDanglingDataRef     LintIssueCode = "DATA_REF_DANGLING"
	LintUnknownOutputKey    LintIssueCode = "DATA_REF_UNKNOWN_KEY"
	LintMissingResourceRef  LintIssueCode = "NODE_MISSING_RESOURCE_REF"
	LintUnknownConfigKey    LintIssueCode = "NODE_UNKNOWN_CONFIG_KEY"
	LintConfigShapeMismatch LintIssueCode = "NODE_CONFIG_SHAPE_MISMATCH"
)

// LintIssue is a single accumulated problem. Message is human-readable
// (Brazilian Portuguese, matching the rest of the workflow domain); Hint is a
// concrete fix instruction aimed at the LLM.
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

// Blocking returns only the issues that gate finish.
func (r LintReport) Blocking() []LintIssue {
	out := make([]LintIssue, 0, len(r.Issues))
	for _, i := range r.Issues {
		if i.Severity == SeverityBlocking {
			out = append(out, i)
		}
	}
	return out
}

// Advisory returns only the non-gating hint issues.
func (r LintReport) Advisory() []LintIssue {
	out := make([]LintIssue, 0, len(r.Issues))
	for _, i := range r.Issues {
		if i.Severity == SeverityAdvisory {
			out = append(out, i)
		}
	}
	return out
}

// IsGreen reports whether the graph passes every blocking rule. THIS is the
// termination gate for the AI builder.
func (r LintReport) IsGreen() bool {
	for _, i := range r.Issues {
		if i.Severity == SeverityBlocking {
			return false
		}
	}
	return true
}

// DynamicHandleResolver returns the valid output handle ids for a node whose
// handles depend on its config (text_match cases, ai_agent custom tools). ok is
// false for node types without dynamic handles. It is injected by the usecase
// layer so this domain package stays free of executor dependencies.
type DynamicHandleResolver func(n Node) (handles []HandleDefinition, ok bool)

// LintGraph runs the full blocking + advisory rule set, accumulating issues.
// exempt is the outgoing-edge exemption set (execute-mode AI-agent leaves),
// computed identically to the activation path. resolveHandles may be nil.
// GraphRule is a PURE, graph-only blocking rule (no DB, no catalog, no wfType).
// Every such rule lives in ONE registry, PureGraphRules, so the builder lint
// and activation enforce EXACTLY the same set. Add a rule here once and BOTH
// paths pick it up; neither can drift (the gap that let a model-less prompt
// agent pass the lint while failing activation/runtime).
type GraphRule struct {
	Code     LintIssueCode
	Hint     string
	Validate func(*Graph) error
}

// PureGraphRules is the single source of truth for graph-only blocking rules.
// Structural / scope / required-output / required-field rules need extra inputs
// (catalog, wfType, the dynamic-handle resolver) so they stay parametrised in
// LintGraph; everything that is a plain func(*Graph) error belongs HERE, and is
// run by LintGraph (builder) and RunPureGraphRules (activation) alike.
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

// RunPureGraphRules runs every rule in PureGraphRules in order and returns the
// first failure (the rule's own typed error, preserved for callers that map it).
// Activation calls this to enforce the same pure blocking rules as the lint
// without re-listing them, one registry, two consumers.
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

	// ---------- BLOCKING TIER (== pure production rules) ----------

	// 1. Structural, delegate to the production validator for exact parity.
	if err := ValidateGraph(g, wfType, exempt); err != nil {
		code, hint := structuralIssueInfo(err)
		// Node-scoped structural errors (missing incoming/outgoing edge, orphan)
		// quote the offending id, so lift it into NodeID, that is what lets the
		// editor anchor the alert to a node and offer "Ver no fluxo". Whole-graph
		// errors (empty, no trigger, cycle) leave it blank, which is correct.
		add(LintIssue{Code: code, Severity: SeverityBlocking, NodeID: nodeIDFromErr(err), Message: err.Error(), Hint: hint})
	}

	// 2. Scope.
	if err := ValidateNodeScopes(g, wfType, catalog); err != nil {
		add(LintIssue{
			Code: LintNodeIncompatibleScope, Severity: SeverityBlocking,
			NodeID:  nodeIDFromErr(err),
			Message: err.Error(),
			Hint:    fmt.Sprintf("Este tipo de nó não é permitido em workflows do tipo %q. Remova-o ou troque por um nó compatível.", wfType),
		})
	}

	// 3. Required STATIC output edges.
	if err := ValidateRequiredOutputEdges(g, catalog); err != nil {
		add(LintIssue{
			Code: LintMissingRequiredOutput, Severity: SeverityBlocking,
			NodeID:  nodeIDFromErr(err),
			Message: err.Error(),
			Hint:    "Conecte uma aresta a partir de cada saída obrigatória deste nó (o rótulo da aresta deve ser o id da saída).",
		})
	}

	// 3b. Required DYNAMIC output edges (e.g. an AI-agent's response/"default"
	// path). Same rule the activation gate runs, the backend, not the frontend,
	// decides what must be connected.
	if err := ValidateRequiredDynamicOutputs(g, resolveHandles); err != nil {
		add(LintIssue{
			Code: LintMissingRequiredOutput, Severity: SeverityBlocking,
			NodeID:  nodeIDFromErr(err),
			Message: err.Error(),
			Hint:    "Conecte uma aresta a partir de cada saída obrigatória deste nó (o rótulo da aresta deve ser o id da saída).",
		})
	}

	// 4. Required config fields (pure required-only; no repo validators).
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

	// 5. Pure graph-only blocking rules, the SINGLE registry shared with
	// activation (segmented-send conflict, VoIP stream pairing, AI-agent
	// prompt-mode model/instructions, …). Add new such rules to PureGraphRules
	// and BOTH the builder lint and activation enforce them automatically.
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

	// ---------- ADVISORY TIER (never gates finish) ----------
	defs := NodeCatalogMap(catalog)
	lintDynamicHandles(g, resolveHandles, add)
	lintDataFlow(g, defs, add)
	lintFunctionalResourceRefs(g, add)
	lintNodeShape(g, defs, add)

	return report
}

// structuralIssueInfo maps a ValidateGraph error to a precise code + LLM hint.
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
			// Only enumerable producers (those that declare OutputKeys) can have
			// their key existence checked; http/code/set_variable/ai-tool-args
			// produce dynamic keys and are intentionally not key-checked.
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

// lintNodeShape is the FIRST, advisory-only pass of a node-config "shape
// contract" check: it flags (a) config keys a node's type doesn't declare and
// (b) structured fields whose stored value has the wrong shape, the prime case
// being an ai_agent custom_tools parameter list arriving as a JSON-Schema object
// ({type,properties,required}) instead of the array [{name,type,...}] the
// executor and UI read. That object shape is otherwise SILENTLY dropped (the type
// assertion in parseCustomToolsConfig fails), so the tool runs with no parameters
// and the AI calls it with empty arguments. ADVISORY by design so it runs on
// every existing workflow without gating finish, letting us MEASURE what fires
// while the AI still sees it in the builder.
//
// TODO(next step): turn this into an ENFORCED shape contract. In order:
//  1. Complete each node's contract first, or strict checks will false-positive:
//     declare every key the executor actually reads (e.g. ai_agent "tool_mode",
//     a hidden field the dynamic-handle resolver reads but which is absent from
//     both ConfigSchema and DefaultConfig), and give nested field types
//     (tools/cases/buttons) a structured sub-schema so their shape is checkable
//     precisely rather than by the conservative ad-hoc checks below.
//  2. Add a boundary normalizer (on save / update_node) that coerces known,
//     losslessly-convertible aliases, custom_tools parameters JSON-Schema object
//     -> array, so the AI's industry-standard shape is ACCEPTED, not dropped.
//  3. Once this lint is quiet on legitimate configs, promote the high-confidence
//     checks (unknown key, tools-shape) to SeverityBlocking and move them into
//     PureGraphRules so activation enforces them too (see RunPureGraphRules).
//
// Until all three land: advisory only, the AI sees it, nothing breaks.
func lintNodeShape(g *Graph, defs map[NodeType]NodeDefinition, add func(LintIssue)) {
	for i := range g.Nodes {
		n := &g.Nodes[i]
		def, ok := defs[n.Type]
		if !ok || len(def.ConfigSchema) == 0 || len(n.Config) == 0 {
			continue // no declared contract to check against
		}

		// declared = the node's contract: ConfigSchema fields. known = declared
		// PLUS DefaultConfig keys, a key the node sets by default is legitimate
		// even if it isn't an editable field. Keeping `known` tight is what keeps
		// this advisory trustworthy (a noisy advisory is one the AI learns to
		// ignore, the exact trap behind the tool-arg bug).
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

// lintFieldShape advises when a declared field's stored value has the wrong
// container shape. Deliberately conservative, only the structured types whose
// shape is unambiguous (tools, multi-select), so the advisory stays low-noise.
// Extend as nested sub-schemas land (see lintNodeShape TODO step 1).
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

// functionalResourceRef describes a config field that is functionally required
// (the node is a runtime no-op without it) for a given node type/mode, even
// though production activation skips it when empty. Surfaced as advisory so the
// builder nudges toward fully-functional graphs without ever being stricter than
// production in a way that could block finish.
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
		case NodeTypeActionTransferDepartment:
			if cfgStr(n.Config, "department_id") == "" {
				emit(n.ID, "department_id", "departamento de destino")
			}
		case NodeTypeActionAssignLabel, NodeTypeConditionCheckLabel:
			if cfgStr(n.Config, "label_id") == "" {
				emit(n.ID, "label_id", "etiqueta")
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

// nodeIDFromErr/fieldFromErr extract the quoted node id / field from the
// formatted production error messages (best-effort, for UI scoping only).
func nodeIDFromErr(err error) string {
	return firstQuoted(err.Error())
}

func fieldFromErr(err error) string {
	// The required-field error has the shape: ... node "<id>" (<type>) field "<key>",
	// only the id and the field are quoted, so the field is the last quoted part
	// when there are at least two.
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
