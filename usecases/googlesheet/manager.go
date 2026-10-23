package googlesheet

import (
	"context"
	"errors"
	"fmt"
	"log"

	"vozko/domain/googleaccount"
	"vozko/domain/googlesheet"
	"vozko/domain/shared"
)

const healthProblemRows = 50

type ManagerDeps struct {
	Sheets  googlesheet.SheetRepository
	Rows    googlesheet.RowRepository
	API     googlesheet.SheetsAPI
	Errors  googlesheet.ErrorClassifier
	Access  googleaccount.AccessSource
	Limiter googlesheet.Limiter
	Marks   Marker
	Alerts  Alerts
	Nudger  Nudger
}

type Manager struct {
	now     Clock
	sheets  googlesheet.SheetRepository
	rows    googlesheet.RowRepository
	api     googlesheet.SheetsAPI
	errs    googlesheet.ErrorClassifier
	access  googleaccount.AccessSource
	limiter googlesheet.Limiter
	marks   Marker
	alerts  Alerts
	nudger  Nudger
}

func NewManager(d ManagerDeps) *Manager {
	if d.Sheets == nil || d.Rows == nil || d.API == nil || d.Errors == nil || d.Access == nil || d.Limiter == nil || d.Marks == nil || d.Alerts == nil || d.Nudger == nil {
		panic("googlesheet: the manager requires sheets, rows, the sheets api, an error classifier, google access, a limiter, marks, alerts and a nudger")
	}
	return &Manager{
		sheets: d.Sheets, rows: d.Rows, api: d.API, errs: d.Errors, access: d.Access,
		limiter: d.Limiter, marks: d.Marks, alerts: d.Alerts, nudger: d.Nudger, now: utcNow,
	}
}

type CreateInput struct {
	WorkspaceID string
	UserID      string
	Title       string
	TabName     string
	Description string
	TimeZone    string
	Columns     []googlesheet.ColumnInput
}

func (m *Manager) Create(ctx context.Context, in CreateInput) (*googlesheet.Sheet, error) {
	access, err := m.access.Access(ctx, in.WorkspaceID, googleaccount.ProductSheets)
	if err != nil {
		return nil, err
	}
	sheet, err := googlesheet.NewSheet(googlesheet.NewSheetInput{
		WorkspaceID: in.WorkspaceID,
		AccountID:   access.AccountID,
		Title:       in.Title,
		TabName:     in.TabName,
		Description: in.Description,
		TimeZone:    in.TimeZone,
		CreatedBy:   in.UserID,
		Columns:     in.Columns,
	})
	if err != nil {
		return nil, err
	}
	if err := m.takeWrite(ctx, access); err != nil {
		return nil, err
	}
	created, err := m.api.CreateSpreadsheet(ctx, access.Token, sheet.Spec())
	if err != nil {
		if created.SpreadsheetID != "" {
			m.trash(ctx, access, created.SpreadsheetID)
		}
		return nil, m.googleRefusal(err)
	}
	sheet.SpreadsheetID, sheet.TabID = created.SpreadsheetID, created.TabID
	if err := m.sheets.Create(ctx, sheet); err != nil {
		m.trash(ctx, access, created.SpreadsheetID)
		return nil, err
	}
	return sheet, nil
}

func (m *Manager) trash(ctx context.Context, access googleaccount.Access, spreadsheetID string) {
	if err := m.api.TrashSpreadsheet(ctx, access.Token, spreadsheetID); err != nil {
		log.Printf("[googlesheet] spreadsheet %s was created but not recorded and could not be moved to the trash: %v", spreadsheetID, err)
	}
}

func (m *Manager) Get(ctx context.Context, workspaceID, id string) (*googlesheet.Sheet, error) {
	return m.sheets.FindInWorkspace(ctx, workspaceID, id)
}

func (m *Manager) List(ctx context.Context, in googlesheet.ListInput) (*shared.PaginatedResult[*googlesheet.Sheet], error) {
	return m.sheets.List(ctx, in)
}

type DetailsInput struct {
	WorkspaceID string
	ID          string
	Version     int
	Title       *string
	Description *string
}

func (m *Manager) UpdateDetails(ctx context.Context, in DetailsInput) (*googlesheet.Sheet, error) {
	sheet, err := m.sheets.FindInWorkspace(ctx, in.WorkspaceID, in.ID)
	if err != nil {
		return nil, err
	}
	if in.Version != sheet.Version {
		return nil, googlesheet.ErrStaleSheet
	}
	if err := sheet.Rename(in.Title, in.Description); err != nil {
		return nil, err
	}
	if err := m.sheets.Update(ctx, sheet, in.Version); err != nil {
		return nil, err
	}
	return sheet, nil
}

type ColumnsInput struct {
	WorkspaceID string
	ID          string
	Version     int
	Edit        googlesheet.ColumnsEdit
}

func (m *Manager) EditColumns(ctx context.Context, in ColumnsInput) (*googlesheet.Sheet, error) {
	sheet, err := m.sheets.FindInWorkspace(ctx, in.WorkspaceID, in.ID)
	if err != nil {
		return nil, err
	}
	if in.Version != sheet.Version {
		return nil, googlesheet.ErrStaleSheet
	}
	if err := sheet.EditColumns(in.Edit); err != nil {
		return nil, err
	}
	access, err := m.sheetAccess(ctx, sheet)
	if err != nil {
		return nil, err
	}
	if err := m.takeWrite(ctx, access); err != nil {
		return nil, err
	}
	if err := m.api.ApplyColumns(ctx, access.Token, sheet.Target(), sheet.Columns); err != nil {
		return nil, m.googleRefusal(err)
	}
	if err := m.sheets.Update(ctx, sheet, in.Version); err != nil {
		return nil, err
	}
	m.forgetHeaderCheck(sheet.ID)
	return sheet, nil
}

func (m *Manager) Delete(ctx context.Context, workspaceID, id string) error {
	if err := m.sheets.Delete(ctx, workspaceID, id); err != nil {
		return err
	}
	return m.rows.DeleteUnwritten(ctx, id)
}

type HealthView struct {
	Sheet  *googlesheet.Sheet
	Health googlesheet.Health
}

func (m *Manager) Health(ctx context.Context, workspaceID, id string) (*HealthView, error) {
	sheet, err := m.sheets.FindInWorkspace(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	health, err := m.rows.Health(ctx, sheet.ID, healthProblemRows)
	if err != nil {
		return nil, err
	}
	return &HealthView{Sheet: sheet, Health: health}, nil
}

func (m *Manager) RetryProblems(ctx context.Context, workspaceID, id string) (int, error) {
	sheet, err := m.sheets.FindInWorkspace(ctx, workspaceID, id)
	if err != nil {
		return 0, err
	}
	if err := sheet.Writable(); err != nil {
		return 0, err
	}
	requeued, err := m.rows.RequeueProblems(ctx, sheet.ID, m.now())
	if err != nil {
		return 0, err
	}
	if requeued > 0 {
		m.nudger.Nudge(sheet.ID)
	}
	return requeued, nil
}

type ReactivateInput struct {
	WorkspaceID  string
	ID           string
	RepairHeader bool
}

func (m *Manager) Reactivate(ctx context.Context, in ReactivateInput) (*googlesheet.Sheet, error) {
	sheet, err := m.sheets.FindInWorkspace(ctx, in.WorkspaceID, in.ID)
	if err != nil {
		return nil, err
	}
	access, err := m.sheetAccess(ctx, sheet)
	if err != nil {
		return nil, err
	}
	if err := m.takeRead(ctx, access); err != nil {
		return nil, err
	}
	info, err := m.api.TabInfo(ctx, access.Token, sheet.Target())
	if err != nil {
		return nil, m.googleRefusal(err)
	}
	renamed := info.Title != sheet.TabName
	sheet.TabName = info.Title
	if info.NearlyFull(0, len(sheet.Columns)+1) {
		return nil, fmt.Errorf("%w: %s", googlesheet.ErrSheetNotActive, googlesheet.ReasonSheetFull)
	}
	if in.RepairHeader {
		if err := m.takeWrite(ctx, access); err != nil {
			return nil, err
		}
		if err := m.api.ApplyColumns(ctx, access.Token, sheet.Target(), sheet.Columns); err != nil {
			return nil, m.googleRefusal(err)
		}
	} else if err := m.confirmHeader(ctx, access, sheet); err != nil {
		return nil, err
	}
	sheet.Activate()
	if renamed {
		if err := m.sheets.UpdateTabName(ctx, sheet.ID, sheet.TabName); err != nil {
			return nil, err
		}
	}
	if err := m.sheets.SaveStatus(ctx, sheet); err != nil {
		return nil, err
	}
	m.forgetHeaderCheck(sheet.ID)
	if err := m.alerts.Resolved(ctx, sheet); err != nil {
		log.Printf("[googlesheet] attention alert of %s not resolved: %v", sheet.ID, err)
	}
	m.nudger.Nudge(sheet.ID)
	return sheet, nil
}

func (m *Manager) confirmHeader(ctx context.Context, access googleaccount.Access, sheet *googlesheet.Sheet) error {
	if err := m.takeRead(ctx, access); err != nil {
		return err
	}
	header, err := m.api.ReadRange(ctx, access.Token, sheet.Target(), "1:1")
	if err != nil {
		return m.googleRefusal(err)
	}
	if difference := googlesheet.HeaderDifference(sheet.HeaderRow(), header); difference != "" {
		return fmt.Errorf("%w: %s: %s", googlesheet.ErrSheetNotActive, googlesheet.ReasonHeaderMismatch, difference)
	}
	return nil
}

func (m *Manager) sheetAccess(ctx context.Context, sheet *googlesheet.Sheet) (googleaccount.Access, error) {
	access, err := m.access.Access(ctx, sheet.WorkspaceID, googleaccount.ProductSheets)
	if err != nil {
		return googleaccount.Access{}, err
	}
	if access.AccountID != sheet.AccountID {
		return googleaccount.Access{}, googlesheet.ErrAccountChanged
	}
	return access, nil
}

func (m *Manager) takeWrite(ctx context.Context, access googleaccount.Access) error {
	granted, err := m.limiter.TakeWrite(ctx, access.Email)
	if err != nil {
		return fmt.Errorf("%w: %v", googlesheet.ErrGoogleBusy, err)
	}
	if !granted {
		return googlesheet.ErrGoogleBusy
	}
	return nil
}

func (m *Manager) takeRead(ctx context.Context, access googleaccount.Access) error {
	granted, err := m.limiter.TakeRead(ctx, access.Email)
	if err != nil {
		return fmt.Errorf("%w: %v", googlesheet.ErrGoogleBusy, err)
	}
	if !granted {
		return googlesheet.ErrGoogleBusy
	}
	return nil
}

func (m *Manager) forgetHeaderCheck(sheetID string) {
	if err := m.marks.Del(headerMarkKey(sheetID)); err != nil {
		log.Printf("[googlesheet] header check mark of %s not cleared: %v", sheetID, err)
	}
}

type GoogleRefusal struct {
	Failure googlesheet.Failure
	Err     error
}

func (e *GoogleRefusal) Error() string { return "googlesheet: google refused: " + e.Err.Error() }

func (e *GoogleRefusal) Unwrap() error { return e.Err }

func (m *Manager) googleRefusal(err error) error {
	var refusal *GoogleRefusal
	if errors.As(err, &refusal) {
		return err
	}
	return &GoogleRefusal{Failure: m.errs.Classify(err), Err: err}
}
