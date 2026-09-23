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
	WorkspaceID string `json:"-"`

	ContainerID string `json:"containerId,omitempty"`

	ContainerType string `json:"containerType,omitempty"`

	DepartmentIDs []string `json:"departmentIds,omitempty"`

	Statuses []string `json:"statuses,omitempty"`

	CreatedFrom *time.Time `json:"createdFrom,omitempty"`
	CreatedTo   *time.Time `json:"createdTo,omitempty"`
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
	Scope     Scope     `json:"scope"`
	EntryType EntryType `json:"entryType"`

	StageID string `json:"stageId,omitempty"`
	Number  string `json:"number,omitempty"`

	Interest             string `json:"interest,omitempty"`
	Disposition          string `json:"disposition,omitempty"`
	Sentiment            string `json:"sentiment,omitempty"`
	Qualification        string `json:"qualification,omitempty"`
	NextAction           string `json:"nextAction,omitempty"`
	AttendanceQualityMin *int   `json:"attendanceQualityMin,omitempty"`
	AttendanceQualityMax *int   `json:"attendanceQualityMax,omitempty"`
	HasAnalysis          *bool  `json:"hasAnalysis,omitempty"`

	HasToolCalls    *bool  `json:"hasToolCalls,omitempty"`
	ToolName        string `json:"toolName,omitempty"`
	MessageType     string `json:"messageType,omitempty"`
	MinMessageCount *int   `json:"minMessageCount,omitempty"`
	MaxMessageCount *int   `json:"maxMessageCount,omitempty"`
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
