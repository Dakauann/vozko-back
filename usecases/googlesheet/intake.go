package googlesheet

import (
	"context"
	"log"
	"strings"

	"vozko/domain/googlesheet"
)

type Intake struct {
	sheets   googlesheet.SheetRepository
	rows     googlesheet.RowRepository
	resolver googlesheet.SystemValueResolver
	nudger   Nudger
	now      Clock
}

func NewIntake(sheets googlesheet.SheetRepository, rows googlesheet.RowRepository, resolver googlesheet.SystemValueResolver, nudger Nudger) *Intake {
	if sheets == nil || rows == nil || resolver == nil || nudger == nil {
		panic("googlesheet: intake requires sheets, rows, a system value resolver and a nudger")
	}
	return &Intake{sheets: sheets, rows: rows, resolver: resolver, nudger: nudger, now: utcNow}
}

type EnqueueInput struct {
	WorkspaceID    string
	SheetID        string
	Values         map[string]any
	IdempotencyKey string
	Origin         googlesheet.Origin
}

type EnqueueResult struct {
	RowID     string
	Duplicate bool
	Sheet     *googlesheet.Sheet
	Cells     googlesheet.Cells
}

func (in *Intake) Enqueue(ctx context.Context, input EnqueueInput) (*EnqueueResult, error) {
	sheet, cells, err := in.accept(ctx, input.WorkspaceID, input.SheetID, input.Values, input.Origin)
	if err != nil {
		return nil, err
	}
	row, err := googlesheet.NewRow(sheet, cells, input.IdempotencyKey, input.Origin, in.now())
	if err != nil {
		return nil, err
	}
	stored, duplicate, err := in.rows.Insert(ctx, row)
	if err != nil {
		return nil, err
	}
	if !duplicate {
		in.nudger.Nudge(sheet.ID)
	}
	return &EnqueueResult{RowID: stored.ID, Duplicate: duplicate, Sheet: sheet, Cells: stored.Cells}, nil
}

func (in *Intake) Preview(ctx context.Context, workspaceID, sheetID string, values map[string]any, origin googlesheet.Origin) (*googlesheet.Sheet, googlesheet.Cells, error) {
	return in.accept(ctx, workspaceID, sheetID, values, origin)
}

func (in *Intake) accept(ctx context.Context, workspaceID, sheetID string, values map[string]any, origin googlesheet.Origin) (*googlesheet.Sheet, googlesheet.Cells, error) {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(sheetID) == "" {
		return nil, nil, googlesheet.ErrSheetNotFound
	}
	sheet, err := in.sheets.FindInWorkspace(ctx, workspaceID, sheetID)
	if err != nil {
		return nil, nil, err
	}
	if err := sheet.Writable(); err != nil {
		return nil, nil, err
	}
	system := googlesheet.SystemValues{}
	if needsContext(sheet) {
		system, err = in.resolver.Resolve(ctx, workspaceID, origin)
		if err != nil {
			log.Printf("[googlesheet] system values of %s unreadable: %v", sheet.ID, err)
			return nil, nil, err
		}
	}
	cells, err := sheet.Accept(values, system, in.now())
	if err != nil {
		return nil, nil, err
	}
	return sheet, cells, nil
}

func needsContext(sheet *googlesheet.Sheet) bool {
	for _, col := range sheet.Columns {
		if !col.Archived && col.Source.IsSystem() && col.Source != googlesheet.SourceWrittenAt {
			return true
		}
	}
	return false
}
