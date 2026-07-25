package export

import (
	"io"
)

type EntryType string

const (
	EntryTypeWhatsApp EntryType = "whatsapp"
)

type ExportFilter struct {
	CampaignID  string
	WorkspaceID string
	EntryType   EntryType

	Status  string
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

	Status    string
	CreatedAt string
	UpdatedAt string

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
	Export(filter ExportFilter, w io.Writer) (int, error)
}
