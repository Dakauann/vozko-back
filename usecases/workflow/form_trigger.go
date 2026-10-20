package workflow_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vozko/domain/workflow"
	whatsappform_usecase "vozko/usecases/whatsappform"
	"vozko/usecases/workflow/node_executors"
)

var (
	_ whatsappform_usecase.TriggerFirer = (*FormTriggerFirer)(nil)
	_ whatsappform_usecase.RunResumer   = (*FormRunResumer)(nil)
	_ node_executors.FormSender         = (*whatsappform_usecase.Service)(nil)
)

var ErrFormTriggerIncomplete = errors.New("workflow: a form answer needs its workspace, conversation, form and response to start automations")

type FormTriggerFirer struct {
	evaluator workflow.TriggerEvaluator
}

func NewFormTriggerFirer(evaluator workflow.TriggerEvaluator) *FormTriggerFirer {
	return &FormTriggerFirer{evaluator: evaluator}
}

func (f *FormTriggerFirer) FormSubmitted(_ context.Context, in whatsappform_usecase.FormTrigger) error {
	required := map[string]string{"workspace": in.WorkspaceID, "entry": in.EntryID, "entry type": in.EntryType, "form": in.FormID, "response": in.ResponseID}
	for name, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: no %s", ErrFormTriggerIncomplete, name)
		}
	}
	data := make(map[string]interface{}, len(in.Data)+2)
	for k, v := range in.Data {
		data[k] = v
	}
	data[workflow.DataKeyFormID] = in.FormID
	data[workflow.DataKeyFormResponseID] = strings.TrimSpace(in.ResponseID)
	f.evaluator.Evaluate(workflow.TriggerEvent{
		WorkspaceID: in.WorkspaceID,
		EntryID:     in.EntryID,
		EntryType:   in.EntryType,
		TriggerType: workflow.TriggerFormSubmitted,
		Data:        data,
	})
	return nil
}
