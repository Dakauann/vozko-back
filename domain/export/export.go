package export

import (
	"io"
)

type EntryType string

const (
	EntryTypeWhatsApp  EntryType = "whatsapp"
	EntryTypeInstagram EntryType = "instagram"
	EntryTypeTelegram  EntryType = "telegram"
)

// ChannelEntry is one conversation as a channel-neutral export row source.
//
// Export was WhatsApp-only and structurally campaign-keyed: it required a
// CampaignID, which channels without campaigns simply do not have, and rejected
// every other entry type outright. This type is what lets a channel contribute
// rows without inventing a campaign — the container is the account, and the
// contact is whatever identity that channel actually has.
type ChannelEntry struct {
	EntryID string

	// Number is the CRM's contact-identity slot. WhatsApp fills it with the
	// phone; channels with no phone fill it with the @handle, which is what an
	// operator recognises and searches by.
	Number string
	Name   string

	Status    string
	CreatedAt string
	UpdatedAt string

	// Variables and Metadata are optional; only WhatsApp campaigns carry them.
	Variables []string
	Metadata  map[string]interface{}
}

// ChannelEntryLister lists one channel's conversations for export.
//
// containerID is the channel's container: a WhatsApp campaign id, or an account
// id. An empty containerID means the whole workspace, which is the only sensible
// reading for a channel with no campaigns.
type ChannelEntryLister interface {
	ListForExport(workspaceID, containerID string) ([]ChannelEntry, error)
}

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
