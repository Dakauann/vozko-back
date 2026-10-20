package workflow

import "strings"

const (
	FormNodeFormIDKey     = "form_id"
	FormTriggerFormIDsKey = "form_ids"

	FormHandleCompleted  = "completed"
	FormHandleNoReply    = InteractiveHandleNoReply
	FormHandleSendFailed = InteractiveHandleSendFailed

	DataKeyFormID         = "form_id"
	DataKeyFormResponseID = "form_response_id"
)

type FormUse string

const (
	FormUseTrigger FormUse = "trigger"
	FormUseNode    FormUse = "node"
)

func (t TriggerType) StartsRunPerEvent() bool {
	return t == TriggerFormSubmitted
}

func (e TriggerEvent) OccurrenceKey() string {
	if e.TriggerType != TriggerFormSubmitted {
		return ""
	}
	id, _ := e.Data[DataKeyFormResponseID].(string)
	return strings.TrimSpace(id)
}

func FormTriggerAccepts(config map[string]interface{}, formID string) bool {
	raw, present := config[FormTriggerFormIDsKey]
	if !present || raw == nil {
		return true
	}
	ids, readable := formIDList(raw)
	if !readable {
		return false
	}
	if len(ids) == 0 {
		return true
	}
	formID = strings.TrimSpace(formID)
	for _, id := range ids {
		if formID != "" && id == formID {
			return true
		}
	}
	return false
}

func formIDList(raw interface{}) ([]string, bool) {
	var values []string
	switch list := raw.(type) {
	case []string:
		values = list
	case []interface{}:
		for _, item := range list {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			values = append(values, text)
		}
	default:
		return nil, false
	}
	ids := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			ids = append(ids, trimmed)
		}
	}
	return ids, true
}

func (w *Workflow) FormUseOf(formID string) (FormUse, bool) {
	formID = strings.TrimSpace(formID)
	if formID == "" {
		return "", false
	}
	if trigger := w.Graph.TriggerNodeByType(TriggerFormSubmitted); trigger != nil && FormTriggerAccepts(trigger.Config, formID) {
		return FormUseTrigger, true
	}
	for _, n := range w.Graph.Nodes {
		if !n.Type.IsFormPrompt() {
			continue
		}
		if id, _ := n.Config[FormNodeFormIDKey].(string); strings.TrimSpace(id) == formID {
			return FormUseNode, true
		}
	}
	return "", false
}
