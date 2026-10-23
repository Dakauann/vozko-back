package workflow

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"vozko/domain/redact"
	"vozko/domain/tracepayload"
)

func ValidateTriggerInput(spec TriggerInputSpec, input map[string]interface{}) []TriggerInputIssue {
	if spec.Shape != TriggerInputFields && spec.Shape != TriggerInputJSON {
		return nil
	}
	if encoded, err := json.Marshal(input); err != nil || len(encoded) > tracepayload.MaxBytes {
		return []TriggerInputIssue{{Code: IssueTooLarge}}
	}
	if spec.Shape == TriggerInputJSON {
		return webhookBodyIssues(spec, input)
	}
	return fieldIssues(spec.Fields, input)
}

func fieldIssues(fields []TriggerInputField, input map[string]interface{}) []TriggerInputIssue {
	var issues []TriggerInputIssue
	known := make(map[string]bool, len(fields))
	for _, field := range fields {
		known[field.Key] = true
		if issue, bad := fieldIssue(field, input); bad {
			issues = append(issues, issue)
		}
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].Key < issues[j].Key })
	for _, key := range sortedKeys(input) {
		if !known[key] {
			issues = append(issues, TriggerInputIssue{Code: IssueUnknownField, Key: key})
		}
	}
	return issues
}

func fieldIssue(field TriggerInputField, input map[string]interface{}) (TriggerInputIssue, bool) {
	rule := []AgentCallInput{{Name: field.Key, Type: field.Type.acceptedAs(), Required: field.Required}}
	cleaned, missing, err := ValidateAgentCallInputs(rule, input)
	switch {
	case errors.Is(err, ErrAgentCallInputType):
		return TriggerInputIssue{Code: IssueWrongType, Key: field.Key}, true
	case len(missing) > 0:
		return TriggerInputIssue{Code: IssueRequired, Key: field.Key}, true
	}
	value, given := cleaned[field.Key]
	if !given {
		return TriggerInputIssue{}, false
	}
	text, _ := value.(string)
	if field.date {
		if _, err := time.Parse(birthdayDateLayout, strings.TrimSpace(text)); err != nil {
			return TriggerInputIssue{Code: IssueWrongType, Key: field.Key}, true
		}
	}
	if field.closed && !hasOption(field.Options, text) {
		return TriggerInputIssue{Code: IssueUnknownOption, Key: field.Key}, true
	}
	return TriggerInputIssue{}, false
}

func hasOption(options []TriggerInputOption, value string) bool {
	for _, option := range options {
		if option.Value == value {
			return true
		}
	}
	return false
}

func webhookBodyIssues(spec TriggerInputSpec, body map[string]interface{}) []TriggerInputIssue {
	events, readable := webhookAcceptedEvents(spec.config)
	if readable && len(events) == 0 {
		return nil
	}
	event, _ := body[webhookEventKey].(string)
	if !WebhookAcceptsEvent(spec.config, strings.TrimSpace(event)) {
		return []TriggerInputIssue{{Code: IssueEventNotAccepted, Key: webhookEventKey}}
	}
	return nil
}

func TriggerRunDataFor(spec TriggerInputSpec, input map[string]interface{}, subject TriggerTestSubject) (map[string]interface{}, []TriggerInputIssue) {
	if issues := ValidateTriggerInput(spec, input); len(issues) > 0 {
		return nil, issues
	}
	switch spec.TriggerType {
	case TriggerAgentCall:
		return agentCallTestData(spec, input), nil
	case TriggerWebhook:
		body := cloneConfigMap(input)
		if body == nil {
			body = map[string]interface{}{}
		}
		return spec.onSubject(WebhookRunData(TestWebhookMethod, body, nil), subject), nil
	case TriggerFormSubmitted:
		return formTestData(spec, input, subject), nil
	case TriggerStageAdded:
		stageID, _ := input[DataKeyStageID].(string)
		return StageAddedRunData(stageID), nil
	case TriggerBirthday:
		day := subject.Today
		if text, _ := input[DataKeyBirthday].(string); strings.TrimSpace(text) != "" {
			day, _ = time.Parse(birthdayDateLayout, strings.TrimSpace(text))
		}
		return spec.onSubject(BirthdayRunData(day), subject), nil
	}
	if spec.Shape == TriggerInputMessage {
		return nil, nil
	}
	return map[string]interface{}{}, nil
}

func (s TriggerInputSpec) onSubject(data map[string]interface{}, subject TriggerTestSubject) map[string]interface{} {
	if !s.startsOnLead {
		return data
	}
	return LeadSubject{LeadID: subject.LeadID, LeadName: subject.LeadName}.RunData(data)
}

func agentCallTestData(spec TriggerInputSpec, input map[string]interface{}) map[string]interface{} {
	inputs := map[string]interface{}{}
	for _, field := range spec.Fields {
		if field.Key == agentNameInputKey {
			continue
		}
		if value, given := input[field.Key]; given && value != nil && !isBlankText(value) {
			inputs[field.Key] = value
		}
	}
	name, _ := input[agentNameInputKey].(string)
	if name = strings.TrimSpace(name); name == "" {
		name = DefaultTestAgentName
	}
	return AgentCallRunData(inputs, "", name)
}

func formTestData(spec TriggerInputSpec, input map[string]interface{}, subject TriggerTestSubject) map[string]interface{} {
	formID := spec.formID
	if formID == "" {
		formID, _ = input[DataKeyFormID].(string)
	}
	answers := map[string]interface{}{}
	for key, value := range input {
		if question, isAnswer := strings.CutPrefix(key, DataKeyFormAnswerPrefix); isAnswer && value != nil && !isBlankText(value) {
			answers[question] = value
		}
	}
	return FormSubmittedRunData(formID, subject.RunID, answers)
}

func TriggerInputFromRunData(spec TriggerInputSpec, data map[string]interface{}) map[string]interface{} {
	switch spec.Shape {
	case TriggerInputJSON:
		webhook, _ := data[DataKeyWebhook].(map[string]interface{})
		if body, isObject := webhook["body"].(map[string]interface{}); isObject {
			return cloneConfigMap(body)
		}
		return map[string]interface{}{}
	case TriggerInputMessage:
		if message, isText := data["message"].(string); isText {
			return map[string]interface{}{"message": message}
		}
		return map[string]interface{}{}
	case TriggerInputFields:
		return recordedFieldValues(spec, data)
	}
	return map[string]interface{}{}
}

func recordedFieldValues(spec TriggerInputSpec, data map[string]interface{}) map[string]interface{} {
	input := map[string]interface{}{}
	for _, field := range spec.Fields {
		if value, found := recordedValue(field, data); found && value != nil {
			input[field.Key] = value
		}
	}
	return input
}

func recordedValue(field TriggerInputField, data map[string]interface{}) (interface{}, bool) {
	if strings.HasPrefix(field.Key, DataKeyFormAnswerPrefix) {
		value, found := data[field.Key]
		return value, found
	}
	return deepResolve(data, field.path)
}

func MaskedTriggerInputKeys(spec TriggerInputSpec, input map[string]interface{}) []string {
	keys := []string{}
	if spec.Shape == TriggerInputJSON {
		collectMaskedPaths(input, "", &keys)
		sort.Strings(keys)
		return keys
	}
	for _, key := range sortedKeys(input) {
		if holdsMask(input[key]) {
			keys = append(keys, key)
		}
	}
	return keys
}

func holdsMask(value interface{}) bool {
	switch typed := value.(type) {
	case string:
		return redact.IsMasked(typed)
	case map[string]interface{}:
		for _, item := range typed {
			if holdsMask(item) {
				return true
			}
		}
	case []interface{}:
		for _, item := range typed {
			if holdsMask(item) {
				return true
			}
		}
	}
	return false
}

func collectMaskedPaths(value interface{}, path string, keys *[]string) {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, item := range typed {
			collectMaskedPaths(item, joinPath(path, key), keys)
		}
	case []interface{}:
		if holdsMask(typed) {
			*keys = append(*keys, path)
		}
	case string:
		if redact.IsMasked(typed) {
			*keys = append(*keys, path)
		}
	}
}

func joinPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}
