package googlesheet

import (
	"context"
	"time"

	"vozko/domain/shared"
)

type Target struct {
	SpreadsheetID string
	TabID         int64
	TabName       string
}

func (s *Sheet) Target() Target {
	return Target{SpreadsheetID: s.SpreadsheetID, TabID: s.TabID, TabName: s.TabName}
}

type SpreadsheetSpec struct {
	Title    string
	TabName  string
	Locale   string
	TimeZone string
	Columns  []Column
}

func (s *Sheet) Spec() SpreadsheetSpec {
	return SpreadsheetSpec{Title: s.Title, TabName: s.TabName, Locale: s.Locale, TimeZone: s.TimeZone, Columns: s.Columns}
}

type CreatedSpreadsheet struct {
	SpreadsheetID string
	TabID         int64
}

type AppendResult struct {
	FirstRow int
	Rows     int
}

type TabInfo struct {
	Title   string
	Rows    int
	Columns int
}

const MaxCellsPerSpreadsheet = 10_000_000

func (t TabInfo) NearlyFull(extraRows, columns int) bool {
	return (t.Rows+extraRows)*columns >= MaxCellsPerSpreadsheet*95/100
}

type SheetsAPI interface {
	CreateSpreadsheet(ctx context.Context, token string, spec SpreadsheetSpec) (CreatedSpreadsheet, error)
	TrashSpreadsheet(ctx context.Context, token, spreadsheetID string) error
	ApplyColumns(ctx context.Context, token string, target Target, columns []Column) error
	AppendRows(ctx context.Context, token string, target Target, rows [][]any) (AppendResult, error)
	ReadRange(ctx context.Context, token string, target Target, a1 string) ([][]any, error)
	TabInfo(ctx context.Context, token string, target Target) (TabInfo, error)
}

type ErrorClassifier interface {
	Classify(err error) Failure
}

type ListInput struct {
	WorkspaceID string
	Search      string
	Pagination  shared.Pagination
}

type SheetRepository interface {
	Create(ctx context.Context, s *Sheet) error
	Update(ctx context.Context, s *Sheet, expectedVersion int) error
	SaveStatus(ctx context.Context, s *Sheet) error
	UpdateTabName(ctx context.Context, id, tabName string) error
	Get(ctx context.Context, id string) (*Sheet, error)
	FindInWorkspace(ctx context.Context, workspaceID, id string) (*Sheet, error)
	List(ctx context.Context, in ListInput) (*shared.PaginatedResult[*Sheet], error)
	Delete(ctx context.Context, workspaceID, id string) error
	TouchWritten(ctx context.Context, id string, at time.Time) error
}

type PendingSheet struct {
	SheetID         string
	OldestPendingAt time.Time
}

type WrittenRow struct {
	ID        string
	RowNumber int
}

const HealthCountCap = 100_000

type Health struct {
	Pending         int
	InFlight        int
	Held            int
	Failed          int
	Written         int
	OldestPendingAt *time.Time
	Problems        []*Row
}

type RowRepository interface {
	ListRows(ctx context.Context, workspaceID, sheetID string, pagination shared.Pagination) (*shared.PaginatedResult[*Row], error)
	Insert(ctx context.Context, row *Row) (stored *Row, duplicate bool, err error)
	PendingSheets(ctx context.Context, readyAt time.Time, limit int) ([]PendingSheet, error)
	Claim(ctx context.Context, sheetID, batchID string, limit int, now time.Time) ([]*Row, error)
	MarkWritten(ctx context.Context, rows []WrittenRow, at time.Time) error
	Release(ctx context.Context, ids []string, nextAttemptAt time.Time, code string) error
	Unclaim(ctx context.Context, ids []string) error
	Hold(ctx context.Context, ids []string, code, detail string) error
	Fail(ctx context.Context, id, code, detail string) error
	RequeueProblems(ctx context.Context, sheetID string, now time.Time) (int, error)
	StuckInFlight(ctx context.Context, sheetID string, claimedBefore time.Time) ([]*Row, error)
	PurgeWritten(ctx context.Context, writtenBefore time.Time, limit int) (int, error)
	DeleteForLead(ctx context.Context, workspaceID, leadID string) (int, error)
	DeleteUnwritten(ctx context.Context, sheetID string) error
	Health(ctx context.Context, sheetID string, problems int) (Health, error)
}

type SystemValueResolver interface {
	Resolve(ctx context.Context, workspaceID string, origin Origin) (SystemValues, error)
}

type Limiter interface {
	TakeWrite(ctx context.Context, accountKey string) (granted bool, err error)
	TakeRead(ctx context.Context, accountKey string) (granted bool, err error)
	WritePressure(ctx context.Context) (float64, error)
}
