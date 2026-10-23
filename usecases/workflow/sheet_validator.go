package workflow_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vozko/domain/googlesheet"
	"vozko/domain/workflow"
	"vozko/usecases/workflow/node_executors"
)

type GoogleSheetLookup interface {
	Get(ctx context.Context, workspaceID, id string) (*googlesheet.Sheet, error)
}

func (uc *activateWorkflowUseCase) SetGoogleSheets(sheets GoogleSheetLookup) {
	uc.googleSheets = sheets
}

type googleSheetValidator struct {
	sheets      GoogleSheetLookup
	workspaceID string
}

func (v *googleSheetValidator) Validate(n *workflow.Node) error {
	if n.Type != workflow.NodeTypeActionAddSheetRow {
		return nil
	}
	sheetID, _ := n.Config[node_executors.AddSheetRowSheetKey].(string)
	if strings.TrimSpace(sheetID) == "" {
		return nil
	}
	sheet, err := v.sheets.Get(context.Background(), v.workspaceID, strings.TrimSpace(sheetID))
	if errors.Is(err, googlesheet.ErrSheetNotFound) {
		return fmt.Errorf("%w: node %q sheet_id %q", workflow.ErrNodeInvalidSheetID, n.ID, sheetID)
	}
	if err != nil {
		return err
	}
	if err := sheet.CheckMapping(sheetMapping(n.Config[node_executors.AddSheetRowValuesKey])); err != nil {
		return fmt.Errorf("%w: node %q: %v", workflow.ErrNodeSheetMappingInvalid, n.ID, err)
	}
	return nil
}

func sheetMapping(raw interface{}) map[string]string {
	values, _ := raw.(map[string]interface{})
	mapping := make(map[string]string, len(values))
	for key, value := range values {
		text, ok := value.(string)
		if !ok {
			text = workflow.TemplateText(value)
		}
		mapping[key] = text
	}
	return mapping
}
