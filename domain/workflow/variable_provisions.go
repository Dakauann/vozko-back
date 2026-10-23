package workflow

import (
	"regexp"
	"strings"
)

const (
	labelSeparator       = " › "
	defaultAIResponseVar = "ai_response"
	defaultLoopItemVar   = "item"
	subWorkflowVarPrefix = "sub_"
)

var (
	codeStateSetCall    = regexp.MustCompile(`\bstate\.set\(`)
	codeStateSetLiteral = regexp.MustCompile(`\bstate\.set\(\s*(?:'([^'\\]+)'|"([^"\\]+)")`)
)

var triggerFieldTypes = map[TriggerInputFieldType]VariableType{
	TriggerFieldText:    VariableTypeText,
	TriggerFieldNumber:  VariableTypeNumber,
	TriggerFieldBoolean: VariableTypeBoolean,
	TriggerFieldObject:  VariableTypeObject,
	TriggerFieldList:    VariableTypeList,
}

func label(parts ...string) string {
	return strings.Join(parts, labelSeparator)
}

func examplePtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func everyEdge(string) bool { return true }

func onlyEdge(label string) func(string) bool {
	return func(l string) bool { return l == label }
}

func exceptEdges(labels ...string) func(string) bool {
	return func(l string) bool {
		for _, excluded := range labels {
			if l == excluded {
				return false
			}
		}
		return true
	}
}

type nodeProvisions struct {
	flow *variableFlow
	node int
	edge func(string) bool
}

func (np nodeProvisions) add(p provision) {
	p.node = np.node
	if np.edge != nil && p.edge == nil {
		p.edge = np.edge
	}
	np.flow.add(p)
}

func (np nodeProvisions) on(edge func(string) bool) nodeProvisions {
	np.edge = edge
	return np
}

func (np nodeProvisions) visible(ref string, match provisionMatch, text string, group VariableGroup, kind VariableKind, typ VariableType) {
	np.add(provision{ref: ref, match: match, label: text, group: group, kind: kind, typ: typ})
}

func (np nodeProvisions) hidden(ref string, match provisionMatch) {
	np.add(provision{ref: ref, match: match, hidden: true})
}

func (f *variableFlow) collectProvisions(sources VariableSources) {
	for i := range f.graph.Nodes {
		if !f.reached[i] {
			continue
		}
		n := &f.graph.Nodes[i]
		np := nodeProvisions{flow: f, node: i}
		if n.Type.IsTrigger() {
			spec, found := FindTriggerInputSpec(sources.Specs, n.ID)
			f.triggerProvisions(np, n, spec, found)
			continue
		}
		f.nodeProvisions(np, n, sources.Forms)
	}
}

func (f *variableFlow) triggerProvisions(np nodeProvisions, n *Node, spec TriggerInputSpec, found bool) {
	readable := found && spec.Readable()
	switch n.Type {
	case NodeTypeTriggerAgentCall:
		if !readable {
			np.hidden(DataKeyAgentInput+".", matchFamily)
		}
		for _, field := range spec.Fields {
			if field.Key == agentNameInputKey {
				continue
			}
			np.add(provision{
				ref: DataKeyAgentInput + "." + field.Key, match: fieldMatch(field), label: label("Enviado pelo agente", field.Key),
				group: VariableGroupTrigger, kind: VariableKindTrigger, typ: triggerFieldTypes[field.Type], example: examplePtr(field.Example),
			})
		}
		np.add(provision{ref: agentNameInputKey, match: matchExact, label: label("Agente que chamou", "nome"), group: VariableGroupTrigger, kind: VariableKindTrigger, typ: VariableTypeText, example: examplePtr(DefaultTestAgentName)})
		np.visible(DataKeyAgent+".id", matchExact, label("Agente que chamou", "id"), VariableGroupTrigger, VariableKindTrigger, VariableTypeText)
	case NodeTypeTriggerWebhook:
		np.visible(webhookBodyRefPrefix[:len(webhookBodyRefPrefix)-1], matchOpen, "Corpo do webhook", VariableGroupTrigger, VariableKindTrigger, VariableTypeObject)
		for _, path := range spec.ReadKeys {
			np.visible(webhookBodyRefPrefix+path, matchExact, label(append([]string{"Corpo do webhook"}, strings.Split(path, ".")...)...), VariableGroupTrigger, VariableKindTrigger, VariableTypeUnknown)
		}
		np.visible(DataKeyWebhook+".method", matchExact, label("Webhook", "método"), VariableGroupTrigger, VariableKindTrigger, VariableTypeText)
		if TriggerNodeStartsOnLead(*n) {
			leadProvisions(np)
		}
	case NodeTypeTriggerFormSubmitted:
		ids, ok := FormTriggerFormIDs(n.Config)
		if !readable || !ok || len(ids) == 0 {
			np.hidden(DataKeyFormAnswerPrefix, matchFamily)
		}
		for _, field := range spec.Fields {
			if answer, isAnswer := strings.CutPrefix(field.Key, DataKeyFormAnswerPrefix); isAnswer {
				np.visible(field.Key, matchOpen, label("Resposta do formulário", firstNonEmpty(field.Description, answer)), VariableGroupTrigger, VariableKindTrigger, VariableTypeText)
			}
		}
		formIdentity(np, VariableGroupTrigger, VariableKindTrigger)
	case NodeTypeTriggerStageAdded:
		np.visible(DataKeyStageID, matchExact, label("Etapa em que entrou", "id"), VariableGroupTrigger, VariableKindTrigger, VariableTypeText)
	case NodeTypeTriggerBirthday:
		np.visible(DataKeyBirthday, matchExact, label("Aniversário", "data"), VariableGroupTrigger, VariableKindTrigger, VariableTypeDate)
		leadProvisions(np)
	case NodeTypeTriggerMessageReceived, NodeTypeTriggerFirstMessage:
		messageEventProvisions(np, label("Conversa", "mensagem recebida"), VariableGroupContact, VariableKindSystem, false)
	case NodeTypeTriggerCallReceived:
		np.visible("caller_number", matchExact, label("Ligação", "número de quem ligou"), VariableGroupContact, VariableKindSystem, VariableTypeText)
		np.visible("called_number", matchExact, label("Ligação", "número discado"), VariableGroupContact, VariableKindSystem, VariableTypeText)
		np.hidden("call_id", matchExact)
	default:
		np.hidden("", matchAnyVariable)
	}
}

func fieldMatch(field TriggerInputField) provisionMatch {
	if field.Type == TriggerFieldObject || field.Type == TriggerFieldList {
		return matchOpen
	}
	return matchExact
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func leadProvisions(np nodeProvisions) {
	np.visible(DataKeyLeadID, matchExact, label("Lead", "id"), VariableGroupContact, VariableKindSystem, VariableTypeText)
	np.visible(DataKeyLeadName, matchExact, label("Lead", "nome"), VariableGroupContact, VariableKindSystem, VariableTypeText)
}

func formIdentity(np nodeProvisions, group VariableGroup, kind VariableKind) {
	np.visible(DataKeyFormID, matchExact, label("Formulário", "id"), group, kind, VariableTypeText)
	np.visible(DataKeyFormResponseID, matchExact, label("Formulário", "id da resposta"), group, kind, VariableTypeText)
}

func messageEventProvisions(np nodeProvisions, messageLabel string, group VariableGroup, kind VariableKind, showChoice bool) {
	np.visible(DataKeyMessage, matchExact, messageLabel, group, kind, VariableTypeText)
	np.visible(DataKeyContactNumber, matchExact, label("Contato", "número"), VariableGroupContact, VariableKindSystem, VariableTypeText)
	np.visible(VarChannel, matchExact, label("Conversa", "canal"), VariableGroupContact, VariableKindSystem, VariableTypeText)
	if showChoice {
		np.visible(DataKeySelectedOptionID, matchExact, label("Opção escolhida", "id"), group, kind, VariableTypeText)
		np.visible(DataKeySelectedOptionTitle, matchExact, label("Opção escolhida", "título"), group, kind, VariableTypeText)
	} else {
		np.hidden(DataKeySelectedOptionID, matchExact)
		np.hidden(DataKeySelectedOptionTitle, matchExact)
	}
	for _, key := range []string{DataKeyMessageType, DataKeyMediaType, DataKeySelectedOptionKind, DataKeyTriggerMessageID, DataKeyWorkspaceID, "campaign_id", "campaign_workflow_id", "account_workflow_id"} {
		np.hidden(key, matchExact)
	}
	np.hidden(DataKeyCampaignVars, matchOpen)
}

func (f *variableFlow) nodeProvisions(np nodeProvisions, n *Node, forms map[string]TriggerForm) {
	def, known := f.defs[n.Type]
	if !known {
		np.hidden("", matchAnyVariable)
		np.hidden("last.", matchFamily)
		np.hidden("node."+n.ID+".", matchFamily)
	}
	name := f.nodeName(np.node)
	for _, out := range outputKeysOf(n, def) {
		if out.Key == "" {
			np.hidden("node."+n.ID+".", matchFamily)
			np.hidden("last.", matchFamily)
			continue
		}
		np.hidden("last."+out.Key, matchOpen)
		if out.RepeatsConfig {
			np.hidden("node."+n.ID+"."+out.Key, matchOpen)
			continue
		}
		np.visible("node."+n.ID+"."+out.Key, matchOpen, label(name, firstNonEmpty(out.Label, out.Key)), VariableGroupNodes, VariableKindNode, offeredOutputType(out))
	}
	stateVar := func(ref, text string, typ VariableType) {
		if ref = canonicalRef(ref); ref != "" && !strings.Contains(ref, "{{") {
			np.visible(ref, matchOpen, text, VariableGroupNodes, VariableKindNode, typ)
		}
	}
	switch n.Type {
	case NodeTypeActionSetVariable:
		variable := configText(n, "variable")
		stateVar(variable, label("Variável", variable), VariableTypeUnknown)
	case NodeTypeActionHTTPRequest:
		capture := configText(n, "capture_variable")
		stateVar(capture, label("Resposta HTTP", capture), VariableTypeObject)
	case NodeTypeActionCode:
		codeProvisions(np, n, stateVar)
	case NodeTypeActionAIAgent:
		response := firstNonEmpty(configText(n, "response_variable"), defaultAIResponseVar)
		stateVar(response, label("Resposta da IA", response), VariableTypeObject)
		np.hidden("ai.", matchFamily)
	case NodeTypeActionGetCurrentTime:
		if variable := configText(n, "variable"); variable != "" {
			stateVar(variable, label("Data e hora", variable), VariableTypeObject)
			for _, suffix := range []string{"_datetime", "_hour", "_weekday"} {
				stateVar(variable+suffix, label("Data e hora", variable+suffix), VariableTypeText)
			}
		}
	case NodeTypeActionLoop:
		item := firstNonEmpty(configText(n, "item_variable"), defaultLoopItemVar)
		stateVar(item, label("Item da repetição", item), VariableTypeUnknown)
		stateVar(item+"_index", label("Item da repetição", item+"_index"), VariableTypeNumber)
	case NodeTypeActionRunWorkflow:
		np.hidden(subWorkflowVarPrefix, matchFamily)
	case NodeTypeWaitForReply:
		messageEventProvisions(np.on(onlyEdge("replied")), label(name, "resposta"), VariableGroupNodes, VariableKindNode, false)
	case NodeTypeActionSendInteractive:
		messageEventProvisions(np.on(exceptEdges(InteractiveHandleNoReply, InteractiveHandleSendFailed)), label(name, "resposta"), VariableGroupNodes, VariableKindNode, true)
	case NodeTypeActionSendForm:
		formAnswerProvisions(np.on(onlyEdge(FormHandleCompleted)), n, name, forms)
	}
}

func formAnswerProvisions(np nodeProvisions, n *Node, name string, forms map[string]TriggerForm) {
	formIdentity(np, VariableGroupNodes, VariableKindNode)
	form, found := forms[strings.TrimSpace(configText(n, FormNodeFormIDKey))]
	if !found {
		np.hidden(DataKeyFormAnswerPrefix, matchFamily)
		return
	}
	for _, q := range form.Questions {
		np.visible(FormAnswerDataKey(q.Key), matchOpen, label("Resposta do formulário", firstNonEmpty(q.Prompt, q.Key)), VariableGroupNodes, VariableKindNode, VariableTypeText)
	}
}

func codeProvisions(np nodeProvisions, n *Node, stateVar func(ref, text string, typ VariableType)) {
	if output := configText(n, "output_variable"); output != "" {
		stateVar(output, label("Retorno do código", output), VariableTypeUnknown)
	}
	code := configText(n, "code")
	literals := codeStateSetLiteral.FindAllStringSubmatch(code, -1)
	for _, m := range literals {
		key := m[1] + m[2]
		stateVar(key, label("Variável", key), VariableTypeUnknown)
	}
	if len(codeStateSetCall.FindAllStringIndex(code, -1)) > len(literals) {
		np.hidden("", matchAnyVariable)
	}
}

func outputKeysOf(n *Node, def NodeDefinition) []OutputKeyDefinition {
	var keys []OutputKeyDefinition
	for _, ok := range def.OutputKeys {
		switch {
		case n.Type == NodeTypeActionReplyToAgent && strings.Contains(ok.Key, "<"):
			for _, field := range ReplyToAgentFieldsOf(n.Config) {
				keys = append(keys, OutputKeyDefinition{Key: field.Key, Label: field.Key, Type: VariableTypeText})
			}
		case IsDynamicOutputKey(ok.Key):
			keys = append(keys, OutputKeyDefinition{})
		default:
			keys = append(keys, ok)
		}
	}
	return keys
}

func IsDynamicOutputKey(key string) bool {
	return strings.ContainsAny(key, "*<")
}

func offeredOutputType(out OutputKeyDefinition) VariableType {
	if out.Type == "" {
		return VariableTypeUnknown
	}
	return out.Type
}
