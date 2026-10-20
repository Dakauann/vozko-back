package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"vozko/domain/shared"
)

const (
	AgentCallInputsKey    = "inputs"
	ReplyToAgentFieldsKey = "fields"

	DataKeyAgentInput = "input"
	DataKeyAgent      = "agent"
)

const (
	LintAgentCallInputsInvalid     LintIssueCode = "NODE_AGENT_CALL_INPUTS_INVALID"
	LintReplyToAgentWithoutTrigger LintIssueCode = "NODE_REPLY_TO_AGENT_WITHOUT_AGENT_TRIGGER"
)

var (
	ErrNoAgentCallTrigger          = errors.New("workflow: the workflow has no agent-call trigger")
	ErrAgentCallInputsInvalid      = errors.New("workflow: the agent-call trigger declares invalid inputs")
	ErrAgentCallInputType          = errors.New("workflow: an agent-call input has the wrong type")
	ErrReplyToAgentWithoutTrigger  = errors.New("workflow: the reply-to-agent node needs an agent-call trigger in the same workflow")
	errAgentCallInputsNotAList     = errors.New("os campos devem ser uma lista")
	errAgentCallInputNotAnObject   = errors.New("cada campo deve ser um objeto")
	errAgentCallInputNameMissing   = errors.New("todo campo precisa de nome")
	errAgentCallInputNameDuplicate = errors.New("nome de campo repetido")
)

type AgentCallInputType string

const (
	AgentCallInputString  AgentCallInputType = "string"
	AgentCallInputNumber  AgentCallInputType = "number"
	AgentCallInputBoolean AgentCallInputType = "boolean"
	AgentCallInputObject  AgentCallInputType = "object"
	AgentCallInputArray   AgentCallInputType = "array"
)

func (t AgentCallInputType) Valid() bool {
	switch t {
	case AgentCallInputString, AgentCallInputNumber, AgentCallInputBoolean, AgentCallInputObject, AgentCallInputArray:
		return true
	}
	return false
}

func (t AgentCallInputType) Accepts(value interface{}) bool {
	switch t {
	case AgentCallInputString:
		_, ok := value.(string)
		return ok
	case AgentCallInputNumber:
		switch value.(type) {
		case float64, float32, int, int64, int32, json.Number:
			return true
		}
		return false
	case AgentCallInputBoolean:
		_, ok := value.(bool)
		return ok
	case AgentCallInputObject:
		_, ok := value.(map[string]interface{})
		return ok
	case AgentCallInputArray:
		_, ok := value.([]interface{})
		return ok
	}
	return false
}

type AgentCallInput struct {
	Name        string
	Type        AgentCallInputType
	Description string
	Required    bool
	Example     string
}

func AgentCallInputsOf(config map[string]interface{}) ([]AgentCallInput, error) {
	raw, present := config[AgentCallInputsKey]
	if !present || raw == nil {
		return nil, nil
	}
	items, isList := raw.([]interface{})
	if !isList {
		return nil, fmt.Errorf("%w: %v", ErrAgentCallInputsInvalid, errAgentCallInputsNotAList)
	}
	inputs := make([]AgentCallInput, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		fields, isObject := item.(map[string]interface{})
		if !isObject {
			return nil, fmt.Errorf("%w: %v", ErrAgentCallInputsInvalid, errAgentCallInputNotAnObject)
		}
		in := agentCallInputFrom(fields)
		switch {
		case in.Name == "":
			return nil, fmt.Errorf("%w: %v", ErrAgentCallInputsInvalid, errAgentCallInputNameMissing)
		case !shared.ValidIdentifierName(in.Name):
			return nil, fmt.Errorf("%w: o nome %s só aceita letras minúsculas, dígitos e _", ErrAgentCallInputsInvalid, in.Name)
		case !in.Type.Valid():
			return nil, fmt.Errorf("%w: o campo %s tem o tipo %s, use string, number, boolean, object ou array", ErrAgentCallInputsInvalid, in.Name, in.Type)
		case seen[in.Name]:
			return nil, fmt.Errorf("%w: %v: %s", ErrAgentCallInputsInvalid, errAgentCallInputNameDuplicate, in.Name)
		}
		seen[in.Name] = true
		inputs = append(inputs, in)
	}
	return inputs, nil
}

func agentCallInputFrom(fields map[string]interface{}) AgentCallInput {
	text := func(key string) string {
		value, _ := fields[key].(string)
		return strings.TrimSpace(value)
	}
	required, _ := fields["required"].(bool)
	typ := AgentCallInputType(text("type"))
	if typ == "" {
		typ = AgentCallInputString
	}
	return AgentCallInput{
		Name:        text("name"),
		Type:        typ,
		Description: text("description"),
		Required:    required,
		Example:     text("example"),
	}
}

func (g *Graph) AgentCallInputs() ([]AgentCallInput, error) {
	trigger := g.TriggerNodeByType(TriggerAgentCall)
	if trigger == nil {
		return nil, ErrNoAgentCallTrigger
	}
	return AgentCallInputsOf(trigger.Config)
}

func ValidateAgentCallInputs(fields []AgentCallInput, values map[string]interface{}) (map[string]interface{}, []string, error) {
	cleaned := make(map[string]interface{}, len(fields))
	var missing []string
	for _, field := range fields {
		value, present := values[field.Name]
		if !present || value == nil || isBlankText(value) {
			if field.Required {
				missing = append(missing, field.Name)
			}
			continue
		}
		if !field.Type.Accepts(value) {
			return nil, nil, &AgentCallInputTypeError{Field: field.Name, Expected: field.Type}
		}
		cleaned[field.Name] = value
	}
	return cleaned, missing, nil
}

type AgentCallInputTypeError struct {
	Field    string
	Expected AgentCallInputType
}

func (e *AgentCallInputTypeError) Error() string {
	return fmt.Sprintf("%s deve ser %s", e.Field, e.Expected)
}

func (e *AgentCallInputTypeError) Unwrap() error { return ErrAgentCallInputType }

func isBlankText(value interface{}) bool {
	text, isText := value.(string)
	return isText && strings.TrimSpace(text) == ""
}

func AgentCallRunData(inputs map[string]interface{}, agentID, agentName string) map[string]interface{} {
	if inputs == nil {
		inputs = map[string]interface{}{}
	}
	return map[string]interface{}{
		DataKeyAgentInput: inputs,
		DataKeyAgent:      map[string]interface{}{"id": agentID, "name": agentName},
	}
}

type ReplyToAgentField struct {
	Key   string
	Value string
}

func ReplyToAgentFieldsOf(config map[string]interface{}) []ReplyToAgentField {
	switch raw := config[ReplyToAgentFieldsKey].(type) {
	case []interface{}:
		fields := make([]ReplyToAgentField, 0, len(raw))
		for _, item := range raw {
			pair, _ := item.(map[string]interface{})
			key, _ := pair["key"].(string)
			if key = strings.TrimSpace(key); key == "" {
				continue
			}
			fields = append(fields, ReplyToAgentField{Key: key, Value: fmt.Sprint(valueOrEmpty(pair["value"]))})
		}
		return fields
	case map[string]interface{}:
		keys := make([]string, 0, len(raw))
		for key := range raw {
			if strings.TrimSpace(key) != "" {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		fields := make([]ReplyToAgentField, 0, len(keys))
		for _, key := range keys {
			fields = append(fields, ReplyToAgentField{Key: strings.TrimSpace(key), Value: fmt.Sprint(valueOrEmpty(raw[key]))})
		}
		return fields
	}
	return nil
}

func valueOrEmpty(value interface{}) interface{} {
	if value == nil {
		return ""
	}
	return value
}

func ReplyToAgentValues(fields []ReplyToAgentField, state *RunState) map[string]interface{} {
	values := make(map[string]interface{}, len(fields))
	for _, field := range fields {
		values[field.Key] = Interpolate(field.Value, state, nil)
	}
	return values
}

func ValidateAgentCallTriggers(g *Graph) error {
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Type != NodeTypeTriggerAgentCall {
			continue
		}
		if _, err := AgentCallInputsOf(n.Config); err != nil {
			return fmt.Errorf("node %q field %q: %w", n.ID, AgentCallInputsKey, err)
		}
	}
	return nil
}

func ValidateReplyToAgentNodes(g *Graph) error {
	if g.TriggerNodeByType(TriggerAgentCall) != nil {
		return nil
	}
	for i := range g.Nodes {
		if g.Nodes[i].Type == NodeTypeActionReplyToAgent {
			return fmt.Errorf("%w: node %q", ErrReplyToAgentWithoutTrigger, g.Nodes[i].ID)
		}
	}
	return nil
}
