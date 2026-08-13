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
	EntryTypeWhatsApp  EntryType = "whatsapp"
	EntryTypeInstagram EntryType = "instagram"
	EntryTypeTelegram  EntryType = "telegram"
	// EntryTypeUnofficialWhatsApp is WhatsApp over a linked-device session. Its
	// export matters more than the other added channels': the contact identity
	// is a real phone number, so the rows are directly usable by whatever the
	// tenant is migrating to.
	EntryTypeUnofficialWhatsApp EntryType = "unofficial_whatsapp"
)

// ErrTooManyRows is returned when a scope selects more rows than an export is
// allowed to produce. It is a refusal, not a truncation: a silently cut file
// looks complete and gets acted on as if it were.
var ErrTooManyRows = errors.New("export: too many rows for a single export")

// Scope selects which conversations an export covers.
//
// Every field is a question any channel could answer, and a channel that cannot
// answer one ignores it. This is what lets one port serve both "this campaign"
// and "everything the disparos summary is counting", without the usecase
// growing a branch per channel or per scope.
type Scope struct {
	WorkspaceID string

	// ContainerID is the channel's container: a WhatsApp campaign id, or an
	// account id. Empty means every container in the workspace, which is the
	// only sensible reading for a channel with no campaigns and the whole point
	// of the workspace-wide export.
	ContainerID string

	// ContainerType narrows which containers are in scope. WhatsApp reads it as
	// the campaign type ("standard"/"organic"); channels without a container
	// type ignore it.
	ContainerType string

	// DepartmentIDs, when set, restricts to containers in those departments. It
	// carries the caller's department scope, so an export can never widen what
	// the operator is allowed to see.
	DepartmentIDs []string

	// Statuses, when set, restricts to those entry statuses. Empty means every
	// status. Filtering here rather than in memory is what keeps a
	// workspace-wide export from dragging every row out of the database to
	// throw most of them away.
	Statuses []string

	// CreatedFrom/CreatedTo bound the CONTAINER's creation date, inclusive; a
	// nil bound is unbounded. Same meaning as the disparos summary filter, so
	// the file and the tiles answer the same question.
	CreatedFrom *time.Time
	CreatedTo   *time.Time
}

// SpansContainers reports whether the scope covers more than one container. It
// decides whether rows need a column naming the campaign they came from.
func (s Scope) SpansContainers() bool {
	return strings.TrimSpace(s.ContainerID) == ""
}

// ChannelEntry is one conversation as a channel-neutral export row source.
//
// Export was WhatsApp-only and structurally campaign-keyed: it required a
// CampaignID, which channels without campaigns simply do not have, and rejected
// every other entry type outright. This type is what lets a channel contribute
// rows without inventing a campaign, the container is the account, and the
// contact is whatever identity that channel actually has.
type ChannelEntry struct {
	EntryID string

	// Number is the CRM's contact-identity slot. WhatsApp fills it with the
	// phone; channels with no phone fill it with the @handle, which is what an
	// operator recognises and searches by.
	Number string
	Name   string
	Age    *int

	// ContainerName names the row's container (WhatsApp: the campaign). It is
	// only written to the file when the scope spans containers, where without
	// it a row cannot be traced back to what produced it.
	ContainerName string

	Status    string
	CreatedAt string
	UpdatedAt string

	// Variables and Metadata are optional; only WhatsApp campaigns carry them.
	Variables []string
	Metadata  map[string]interface{}
}

// ChannelEntryLister streams one channel's conversations for export.
//
// It pushes rows through emit rather than returning a slice so a channel can
// hold a bounded window of rows in memory regardless of how many the scope
// selects; an implementation is free to page the database and is expected to
// for anything workspace-wide. Returning an error from emit aborts the walk and
// surfaces that error unchanged, which is how the row cap stops a runaway
// export at the source instead of after materialising it.
//
// ctx is honoured: an operator closing the download tab must stop the query.
type ChannelEntryLister interface {
	ListForExport(ctx context.Context, scope Scope, emit func(ChannelEntry) error) error
}

// ExportFilter is a Scope plus the post-hoc predicates that need data the
// channel query does not carry (stage, AI analysis).
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
	// Export streams a CSV to w and returns the number of data rows written.
	//
	// It writes nothing at all when no row survives the filter, so a caller can
	// still answer with an error status after calling it — the CSV header is
	// only emitted once there is a first row to follow it.
	Export(ctx context.Context, filter ExportFilter, w io.Writer) (int, error)
}
