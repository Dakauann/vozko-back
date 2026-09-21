package export

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"
)

type EntryType string

const (
	EntryTypeWhatsApp           EntryType = "whatsapp"
	EntryTypeInstagram          EntryType = "instagram"
	EntryTypeTelegram           EntryType = "telegram"
	EntryTypeUnofficialWhatsApp EntryType = "unofficial_whatsapp"
)

func (t EntryType) HasSendStatus() bool {
	return t == EntryTypeWhatsApp || t == EntryTypeUnofficialWhatsApp
}

var ErrTooManyRows = errors.New("export: too many rows for a single export")

type Scope struct {
	WorkspaceID string

	ContainerID string

	ContainerType string

	DepartmentIDs []string

	Statuses []string

	CreatedFrom *time.Time
	CreatedTo   *time.Time
}

func (s Scope) SpansContainers() bool {
	return strings.TrimSpace(s.ContainerID) == ""
}

type ChannelEntry struct {
	EntryID string

	Number string
	Name   string
	Age    *int

	ContainerName string

	Status    string
	CreatedAt string
	UpdatedAt string

	FailureCode   int
	FailureReason string

	Variables []string
	Metadata  map[string]interface{}
}

type ChannelEntryLister interface {
	ListForExport(ctx context.Context, scope Scope, emit func(ChannelEntry) error) error
}

type ExportFilter struct {
	Scope     Scope
	EntryType EntryType

	StageID string
	Number  string

	Interest             string
	Disposition          string
	Sentiment            string
	Qualification        string
	NextAction           string
	AttendanceQualityMin *int
	AttendanceQualityMax *int
	HasAnalysis          *bool

	HasToolCalls    *bool
	ToolName        string
	MessageType     string
	MinMessageCount *int
	MaxMessageCount *int
}

type ExportRow struct {
	Number string
	Name   string
	Age    *int

	CampaignName string

	Status    string
	CreatedAt string
	UpdatedAt string

	FailureCode   int
	FailureReason string

	StageName string

	Variables []string

	Metadata map[string]interface{}

	AnalysisInterest          string
	AnalysisDisposition       string
	AnalysisSentiment         string
	AnalysisQualification     string
	AnalysisNextAction        string
	AnalysisAttendanceQuality *int
	AnalysisSummary           string
	AnalysisProductInterest   string
}

type ExportEntriesUseCase interface {
	Export(ctx context.Context, filter ExportFilter, w io.Writer) (int, error)
}
