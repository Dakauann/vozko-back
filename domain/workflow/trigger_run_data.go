package workflow

import (
	"strings"
	"time"
)

const (
	DataKeyWebhook          = "webhook"
	DataKeyStageID          = "stage_id"
	DataKeyCampaignVars     = "campvars"
	DataKeyFormAnswerPrefix = "form."
)

func WebhookRunData(method string, body map[string]interface{}, rawBody []byte) map[string]interface{} {
	vars := map[string]interface{}{"method": method}
	if body != nil {
		vars["body"] = body
	} else {
		vars["body"] = string(rawBody)
	}
	return map[string]interface{}{DataKeyWebhook: vars}
}

func FormAnswerDataKey(fieldKey string) string {
	return DataKeyFormAnswerPrefix + fieldKey
}

func FormSubmittedRunData(formID, responseID string, answers map[string]interface{}) map[string]interface{} {
	keyed := make(map[string]interface{}, len(answers))
	for key, value := range answers {
		keyed[FormAnswerDataKey(key)] = value
	}
	return FormSubmissionData(keyed, formID, responseID)
}

func FormSubmissionData(data map[string]interface{}, formID, responseID string) map[string]interface{} {
	out := make(map[string]interface{}, len(data)+2)
	for k, v := range data {
		out[k] = v
	}
	out[DataKeyFormID] = formID
	out[DataKeyFormResponseID] = strings.TrimSpace(responseID)
	return out
}

func StageAddedRunData(stageID string) map[string]interface{} {
	return map[string]interface{}{DataKeyStageID: stageID}
}

func BirthdayRunData(localDay time.Time) map[string]interface{} {
	return map[string]interface{}{DataKeyBirthday: localDay.Format(birthdayDateLayout)}
}

func (s LeadSubject) RunData(data map[string]interface{}) map[string]interface{} {
	if data == nil {
		data = map[string]interface{}{}
	}
	data[DataKeyLeadID] = s.LeadID
	data[DataKeyLeadName] = s.LeadName
	return data
}
