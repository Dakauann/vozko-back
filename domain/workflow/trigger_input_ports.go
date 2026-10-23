package workflow

import (
	"errors"
	"time"

	"vozko/domain/conversation"
)

var (
	ErrTriggerInputNotFound    = errors.New("workflow: the test trigger is not a trigger node of this draft")
	ErrTriggerInputUnavailable = errors.New("workflow: what this trigger carries could not be read, so the test cannot start from it")
	ErrNoRecentTriggerInput    = errors.New("workflow: no visible run of this trigger kept its trigger data")
)

type TriggerInputInvalidError struct {
	Issues []TriggerInputIssue
}

func (e *TriggerInputInvalidError) Error() string {
	return "workflow: the test trigger input does not pass the trigger's rules"
}

func TriggerInputIssuesOf(err error) ([]TriggerInputIssue, bool) {
	var invalid *TriggerInputInvalidError
	if errors.As(err, &invalid) {
		return invalid.Issues, true
	}
	return nil, false
}

type TriggerFormCatalog interface {
	TriggerFormChoices(workspaceID string) ([]TriggerInputOption, error)
	TriggerForm(workspaceID, formID string) (TriggerForm, error)
}

type TriggerStageCatalog interface {
	TriggerStages(workspaceID string) ([]TriggerInputOption, error)
}

type RecordedTriggerData struct {
	RunID     string
	StartedAt time.Time
	Data      map[string]interface{}
}

type TriggerInputHistory interface {
	LatestTriggerData(workspaceID, workflowID string, trigger TriggerType, visibility conversation.VisibilityScope) (*RecordedTriggerData, error)
}

const TriggerInputHistoryWindow = 200
