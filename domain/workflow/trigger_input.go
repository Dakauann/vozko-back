package workflow

import (
	"sort"
	"strings"
	"time"
)

type TriggerInputShape string

const (
	TriggerInputFields  TriggerInputShape = "fields"
	TriggerInputJSON    TriggerInputShape = "json"
	TriggerInputMessage TriggerInputShape = "message"
	TriggerInputNone    TriggerInputShape = "none"
)

type TriggerInputFieldType string

const (
	TriggerFieldText    TriggerInputFieldType = "text"
	TriggerFieldNumber  TriggerInputFieldType = "number"
	TriggerFieldBoolean TriggerInputFieldType = "boolean"
	TriggerFieldObject  TriggerInputFieldType = "object"
	TriggerFieldList    TriggerInputFieldType = "list"
)

type TriggerInputIssueCode string

const (
	IssueRequired         TriggerInputIssueCode = "required"
	IssueWrongType        TriggerInputIssueCode = "wrong_type"
	IssueUnknownField     TriggerInputIssueCode = "unknown_field"
	IssueEventNotAccepted TriggerInputIssueCode = "event_not_accepted"
	IssueUnknownOption    TriggerInputIssueCode = "unknown_option"
	IssueTooLarge         TriggerInputIssueCode = "too_large"
	IssueReadNotDeclared  TriggerInputIssueCode = "read_not_declared"
)

const (
	DefaultTestAgentName = "Agente de teste"
	TestWebhookMethod    = "POST"
	agentNameInputKey    = DataKeyAgent + ".name"
	webhookBodyRefPrefix = DataKeyWebhook + ".body."
	webhookEventKey      = "event"
)

type TriggerInputIssue struct {
	Code TriggerInputIssueCode `json:"code"`
	Key  string                `json:"key"`
}

type TriggerInputOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type TriggerInputField struct {
	Key         string                `json:"key"`
	Type        TriggerInputFieldType `json:"type"`
	Required    bool                  `json:"required"`
	Description string                `json:"description"`
	Example     string                `json:"example"`
	Options     []TriggerInputOption  `json:"options,omitempty"`
	ReadBy      []string              `json:"readBy"`

	path   string
	closed bool
	date   bool
}

type TriggerInputSpec struct {
	NodeID         string                 `json:"nodeId"`
	TriggerType    TriggerType            `json:"triggerType"`
	Label          string                 `json:"label"`
	Shape          TriggerInputShape      `json:"shape"`
	Fields         []TriggerInputField    `json:"fields"`
	Sample         map[string]interface{} `json:"sample"`
	ReadKeys       []string               `json:"readKeys"`
	Issues         []TriggerInputIssue    `json:"issues"`
	Window         *ServiceWindow         `json:"window" enums:"open,closed"`
	WindowEditable bool                   `json:"windowEditable"`
	AcceptedEvents []string               `json:"acceptedEvents"`

	config       map[string]interface{}
	unreadable   bool
	startsOnLead bool
	formID       string
}

type TriggerFormQuestion struct {
	Key      string
	Prompt   string
	Required bool
	Choices  []string
}

type TriggerForm struct {
	ID        string
	Name      string
	Questions []TriggerFormQuestion
}

type TriggerInputSources struct {
	Forms       map[string]TriggerForm
	FormChoices []TriggerInputOption
	Stages      []TriggerInputOption
	Unreadable  map[string]bool
}

type TriggerInputNeeds struct {
	FormIDs    []string
	AnyForm    bool
	Stages     bool
	FormNodes  []string
	StageNodes []string
}

type TriggerTestSubject struct {
	RunID         string
	EntryID       string
	ContactNumber string
	LeadID        string
	LeadName      string
	Today         time.Time
}

func (s TriggerInputSpec) Readable() bool {
	return !s.unreadable
}

func FindTriggerInputSpec(specs []TriggerInputSpec, nodeID string) (TriggerInputSpec, bool) {
	for _, spec := range specs {
		if spec.NodeID == nodeID {
			return spec, true
		}
	}
	return TriggerInputSpec{}, false
}

func FormTriggerFormIDs(config map[string]interface{}) ([]string, bool) {
	raw, present := config[FormTriggerFormIDsKey]
	if !present || raw == nil {
		return nil, true
	}
	return formIDList(raw)
}

func TriggerInputNeedsOf(g *Graph) TriggerInputNeeds {
	var needs TriggerInputNeeds
	seen := map[string]bool{}
	for _, n := range g.TriggerNodes() {
		switch n.Type {
		case NodeTypeTriggerFormSubmitted:
			needs.FormNodes = append(needs.FormNodes, n.ID)
			ids, ok := FormTriggerFormIDs(n.Config)
			if !ok {
				continue
			}
			if len(ids) == 0 {
				needs.AnyForm = true
			}
			for _, id := range ids {
				if !seen[id] {
					seen[id] = true
					needs.FormIDs = append(needs.FormIDs, id)
				}
			}
		case NodeTypeTriggerStageAdded:
			needs.StageNodes = append(needs.StageNodes, n.ID)
			needs.Stages = true
		}
	}
	return needs
}

type graphRead struct {
	ref    string
	nodeID string
}

func graphReadsOf(g *Graph) []graphRead {
	var reads []graphRead
	for _, n := range g.Nodes {
		for _, dep := range ExtractDependencies(n.Config) {
			if dep.Scope == "sys" || dep.Scope == SecretScope {
				continue
			}
			ref := normalizeIndexing(dep.FullRef)
			if inner, isVar := strings.CutPrefix(ref, "var."); isVar {
				ref = inner
			}
			reads = append(reads, graphRead{ref: ref, nodeID: n.ID})
		}
	}
	return reads
}

func readersOf(reads []graphRead, path string) []string {
	readers := []string{}
	seen := map[string]bool{}
	for _, r := range reads {
		if (r.ref == path || strings.HasPrefix(r.ref, path+".")) && !seen[r.nodeID] {
			seen[r.nodeID] = true
			readers = append(readers, r.nodeID)
		}
	}
	return readers
}

func readPathsUnder(reads []graphRead, prefix string) []string {
	seen := map[string]bool{}
	var paths []string
	for _, r := range reads {
		rest, ok := strings.CutPrefix(r.ref, prefix)
		if !ok || rest == "" || seen[rest] {
			continue
		}
		seen[rest] = true
		paths = append(paths, rest)
	}
	sort.Strings(paths)
	return paths
}

func TriggerInputSpecsOf(g *Graph, sources TriggerInputSources) []TriggerInputSpec {
	reads := graphReadsOf(g)
	specs := []TriggerInputSpec{}
	for _, n := range g.TriggerNodes() {
		spec := triggerInputSpecOf(*n, g, reads, sources)
		for i := range spec.Fields {
			spec.Fields[i].ReadBy = readersOf(reads, spec.Fields[i].path)
		}
		if spec.ReadKeys == nil {
			spec.ReadKeys = readFieldKeys(spec.Fields)
		}
		if sources.Unreadable[n.ID] {
			spec.unreadable = true
		}
		specs = append(specs, spec)
	}
	return specs
}

func readFieldKeys(fields []TriggerInputField) []string {
	keys := []string{}
	for _, f := range fields {
		if len(f.ReadBy) > 0 {
			keys = append(keys, f.Key)
		}
	}
	sort.Strings(keys)
	return keys
}

func triggerInputSpecOf(n Node, g *Graph, reads []graphRead, sources TriggerInputSources) TriggerInputSpec {
	spec := TriggerInputSpec{
		NodeID:         n.ID,
		TriggerType:    TriggerType(n.Type),
		Label:          n.DisplayName(),
		Shape:          TriggerInputNone,
		Fields:         []TriggerInputField{},
		Issues:         []TriggerInputIssue{},
		Window:         DefaultServiceWindow(TriggerType(n.Type)),
		WindowEditable: ServiceWindowEditable(TriggerType(n.Type)),
		AcceptedEvents: []string{},
		config:         n.Config,
		startsOnLead:   TriggerNodeStartsOnLead(n),
	}
	switch n.Type {
	case NodeTypeTriggerAgentCall:
		agentCallSpec(&spec, reads)
	case NodeTypeTriggerWebhook:
		webhookSpec(&spec, reads)
	case NodeTypeTriggerFormSubmitted:
		formSpec(&spec, reads, sources)
	case NodeTypeTriggerStageAdded:
		spec.Shape = TriggerInputFields
		spec.Fields = append(spec.Fields, TriggerInputField{
			Key: DataKeyStageID, Type: TriggerFieldText, Required: true, Description: "Etapa em que a conversa entrou",
			Options: stageOptionsFor(n.Config, sources.Stages), path: DataKeyStageID, closed: true,
		})
	case NodeTypeTriggerBirthday:
		spec.Shape = TriggerInputFields
		spec.Fields = append(spec.Fields, TriggerInputField{
			Key: DataKeyBirthday, Type: TriggerFieldText, Description: "Dia do aniversário (AAAA-MM-DD); vazio usa hoje", path: DataKeyBirthday, date: true,
		})
	case NodeTypeTriggerMessageReceived, NodeTypeTriggerFirstMessage:
		spec.Shape = TriggerInputMessage
	}
	return spec
}

var agentFieldTypes = map[AgentCallInputType]TriggerInputFieldType{
	AgentCallInputString:  TriggerFieldText,
	AgentCallInputNumber:  TriggerFieldNumber,
	AgentCallInputBoolean: TriggerFieldBoolean,
	AgentCallInputObject:  TriggerFieldObject,
	AgentCallInputArray:   TriggerFieldList,
}

func (t TriggerInputFieldType) acceptedAs() AgentCallInputType {
	for agentType, fieldType := range agentFieldTypes {
		if fieldType == t {
			return agentType
		}
	}
	return AgentCallInputString
}

func agentCallSpec(spec *TriggerInputSpec, reads []graphRead) {
	spec.Shape = TriggerInputFields
	inputs, err := AgentCallInputsOf(spec.config)
	if err != nil {
		spec.unreadable = true
		return
	}
	declared := map[string]bool{}
	for _, in := range inputs {
		declared[in.Name] = true
		spec.Fields = append(spec.Fields, TriggerInputField{
			Key: in.Name, Type: agentFieldTypes[in.Type], Required: in.Required, Description: in.Description, Example: in.Example,
			path: DataKeyAgentInput + "." + in.Name,
		})
	}
	spec.Fields = append(spec.Fields, TriggerInputField{
		Key: agentNameInputKey, Type: TriggerFieldText, Description: "Nome do agente que chama o fluxo", Example: DefaultTestAgentName, path: agentNameInputKey,
	})
	readKeys := map[string]interface{}{}
	for _, path := range readPathsUnder(reads, DataKeyAgentInput+".") {
		key, _, _ := strings.Cut(path, ".")
		readKeys[key] = true
	}
	if len(readersOf(reads, agentNameInputKey)) > 0 {
		readKeys[agentNameInputKey] = true
	}
	spec.ReadKeys = sortedKeys(readKeys)
	for _, key := range spec.ReadKeys {
		if key != agentNameInputKey && !declared[key] {
			spec.Issues = append(spec.Issues, TriggerInputIssue{Code: IssueReadNotDeclared, Key: key})
		}
	}
}

func webhookSpec(spec *TriggerInputSpec, reads []graphRead) {
	spec.Shape = TriggerInputJSON
	paths := readPathsUnder(reads, webhookBodyRefPrefix)
	sample := map[string]interface{}{}
	for _, path := range paths {
		placeSampleLeaf(sample, strings.Split(path, "."))
	}
	if events, ok := webhookAcceptedEvents(spec.config); ok && len(events) > 0 {
		spec.AcceptedEvents = events
		if _, taken := sample[webhookEventKey].(map[string]interface{}); !taken {
			sample[webhookEventKey] = events[0]
		}
	}
	spec.Sample = sample
	spec.ReadKeys = append([]string{}, paths...)
}

func placeSampleLeaf(node map[string]interface{}, parts []string) {
	head := parts[0]
	if len(parts) == 1 {
		if _, isObject := node[head].(map[string]interface{}); !isObject {
			node[head] = ""
		}
		return
	}
	child, isObject := node[head].(map[string]interface{})
	if !isObject {
		child = map[string]interface{}{}
		node[head] = child
	}
	placeSampleLeaf(child, parts[1:])
}

func formSpec(spec *TriggerInputSpec, reads []graphRead, sources TriggerInputSources) {
	spec.Shape = TriggerInputFields
	ids, ok := FormTriggerFormIDs(spec.config)
	if !ok {
		spec.unreadable = true
		return
	}
	forms := make([]TriggerForm, 0, len(ids))
	for _, id := range ids {
		form, found := sources.Forms[id]
		if !found {
			spec.unreadable = true
			return
		}
		forms = append(forms, form)
	}
	switch len(forms) {
	case 0:
		spec.Fields = append(spec.Fields, formChoiceField(sources.FormChoices))
	case 1:
		spec.formID = forms[0].ID
	default:
		choices := make([]TriggerInputOption, 0, len(forms))
		for _, form := range forms {
			choices = append(choices, TriggerInputOption{Value: form.ID, Label: form.Name})
		}
		spec.Fields = append(spec.Fields, formChoiceField(choices))
	}
	known := map[string]bool{}
	for _, form := range forms {
		for _, q := range form.Questions {
			key := FormAnswerDataKey(q.Key)
			if known[key] {
				continue
			}
			known[key] = true
			spec.Fields = append(spec.Fields, formQuestionField(key, q, len(forms) == 1))
		}
	}
	for _, path := range readPathsUnder(reads, DataKeyFormAnswerPrefix) {
		key := FormAnswerDataKey(path)
		if known[key] {
			continue
		}
		known[key] = true
		if len(forms) > 0 {
			spec.Issues = append(spec.Issues, TriggerInputIssue{Code: IssueReadNotDeclared, Key: key})
			continue
		}
		spec.Fields = append(spec.Fields, TriggerInputField{Key: key, Type: TriggerFieldText, Description: "Resposta lida pelo fluxo", path: key})
	}
}

func formChoiceField(choices []TriggerInputOption) TriggerInputField {
	return TriggerInputField{
		Key: DataKeyFormID, Type: TriggerFieldText, Required: true, Description: "Formulário respondido",
		Options: append([]TriggerInputOption{}, choices...), path: DataKeyFormID, closed: true,
	}
}

func formQuestionField(key string, q TriggerFormQuestion, onlyForm bool) TriggerInputField {
	field := TriggerInputField{Key: key, Type: TriggerFieldText, Required: q.Required && onlyForm, Description: q.Prompt, path: key}
	if len(q.Choices) > 0 {
		field.closed = true
		for _, choice := range q.Choices {
			field.Options = append(field.Options, TriggerInputOption{Value: choice, Label: choice})
		}
	}
	return field
}

func stageOptionsFor(config map[string]interface{}, stages []TriggerInputOption) []TriggerInputOption {
	only, _ := config[DataKeyStageID].(string)
	only = strings.TrimSpace(only)
	options := []TriggerInputOption{}
	for _, stage := range stages {
		if only == "" || stage.Value == only {
			options = append(options, stage)
		}
	}
	return options
}

func PathStartTrigger(g *Graph, targetID string) *Node {
	if target := g.FindNode(targetID); target != nil && target.Type.IsTrigger() {
		return target
	}
	ancestors := g.AncestorsOf(targetID)
	if primary := g.TriggerNode(); primary != nil && ancestors[primary.ID] {
		return primary
	}
	for _, n := range g.TriggerNodes() {
		if ancestors[n.ID] {
			return n
		}
	}
	return nil
}

func (s TriggerInputSpec) TakesInput() bool {
	return s.Shape == TriggerInputFields || s.Shape == TriggerInputJSON
}
