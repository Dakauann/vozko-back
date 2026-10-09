package leadimport

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/unofficial_whatsapp"
	"vozko/domain/workspace"
)

const (
	MaxFileBytes   = 20 << 20
	MaxRows        = lead.MaxImportRows
	SampleRows     = 5
	BatchRows      = 500
	Retention      = 7 * 24 * time.Hour
	StaleAfter     = 2 * time.Minute
	MaxAttempts    = 3
	MaxHeaderCells = 200
)

type Status string

const (
	StatusUploaded  Status = "uploaded"
	StatusAnalyzing Status = "analyzing"
	StatusAnalyzed  Status = "analyzed"
	StatusImporting Status = "importing"
	StatusDone      Status = "done"
	StatusFailed    Status = "failed"
)

func (s Status) Active() bool {
	return s == StatusAnalyzing || s == StatusImporting
}

func ActiveStatuses() []Status {
	return []Status{StatusAnalyzing, StatusImporting}
}

type Stage string

const (
	StageRows  Stage = "rows"
	StageLinks Stage = "links"
	StageSeed  Stage = "seed"
)

type FailureCode string

const (
	FailureStalled         FailureCode = "stalled"
	FailureFileUnavailable FailureCode = "file_unavailable"
	FailureForbidden       FailureCode = "forbidden"
	FailureInternal        FailureCode = "internal"
	FailureInterrupted     FailureCode = "interrupted"
)

var (
	ErrNotFound          = errors.New("lead import: not found")
	ErrWorkspaceRequired = errors.New("lead import: workspace is required")
	ErrRequesterRequired = errors.New("lead import: the person importing is required")
	ErrFileEmpty         = errors.New("lead import: the file has a header and no rows, or no header at all")
	ErrFileTooLarge      = fmt.Errorf("lead import: the file is larger than %d MB", MaxFileBytes>>20)
	ErrTooManyRows       = fmt.Errorf("lead import: the file has more than %d rows", MaxRows)
	ErrUnsupportedFile   = errors.New("lead import: the file is not a CSV or TSV text sheet")
	ErrFileUnavailable   = errors.New("lead import: the file can no longer be read")
	ErrRunning           = errors.New("lead import: another import of this workspace is running")
	ErrNotReady          = errors.New("lead import: the import is not in a state that allows this step")
	ErrClaimLost         = errors.New("lead import: another worker took over this import")
	ErrUnavailable       = errors.New("lead import: imports are not available on this server")
	ErrForbidden         = errors.New("lead import: a mapped column needs a permission you do not have")
	ErrOnlyMine          = errors.New("lead import: only your own imports are listed")
)

type PermissionError struct {
	Resource workspace.Resource
	Action   workspace.Action
}

func (e *PermissionError) Permission() string {
	resource := e.Resource
	if resource == "" {
		resource = workspace.ResourceLeads
	}
	return string(resource) + ":" + string(e.Action)
}

func (e *PermissionError) Error() string {
	return fmt.Sprintf("%v: %s", ErrForbidden, e.Permission())
}

func (e *PermissionError) Unwrap() error {
	return ErrForbidden
}

type File struct {
	MediaID   string
	Name      string
	SizeBytes int64
	Owned     bool
}

type Preview struct {
	Headers []string   `json:"headers"`
	Sample  [][]string `json:"sample"`
	Columns []Column   `json:"columns"`
}

type SeedOutcome struct {
	Queued         int    `json:"queued"`
	ScriptedQueued int    `json:"scriptedQueued,omitempty"`
	Error          string `json:"error,omitempty"`
	ScriptError    string `json:"scriptError,omitempty"`
	Unconfirmed    int    `json:"unconfirmed,omitempty"`
	Batches        int    `json:"batches,omitempty"`
	Sending        int    `json:"sending,omitempty"`
}

const (
	SeedForbidden       = "seed_forbidden"
	SeedUnavailable     = "seed_unavailable"
	SeedFailed          = "seed_failed"
	SeedScriptForbidden = "script_forbidden"
)

type Grants struct {
	Seed   bool `json:"seed"`
	Script bool `json:"script"`
}

type Job struct {
	ID               string
	WorkspaceID      string
	RequestedBy      string
	RequestedByAdmin bool
	Status           Status
	Stage            Stage
	File             File
	TotalRows        int
	Preview          Preview
	Settings         *Settings
	Grants           Grants
	Fingerprint      string
	DryRun           *Counts
	Result           *Counts
	Seed             *SeedOutcome
	Processed        int
	FailureCode      FailureCode
	Attempts         int
	Claim            string
	HeartbeatAt      *time.Time
	StartedAt        *time.Time
	FinishedAt       *time.Time
	ExpiresAt        time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func NewJob(workspaceID, requestedBy string, file File, preview Preview, rows int, now time.Time) (*Job, error) {
	workspaceID, requestedBy = strings.TrimSpace(workspaceID), strings.TrimSpace(requestedBy)
	switch {
	case workspaceID == "":
		return nil, ErrWorkspaceRequired
	case requestedBy == "":
		return nil, ErrRequesterRequired
	case file.SizeBytes > MaxFileBytes:
		return nil, ErrFileTooLarge
	case rows > MaxRows:
		return nil, ErrTooManyRows
	case rows < 1:
		return nil, ErrFileEmpty
	}
	now = now.UTC()
	return &Job{
		WorkspaceID: workspaceID, RequestedBy: requestedBy, Status: StatusUploaded, File: file, TotalRows: rows,
		Preview: preview, ExpiresAt: now.Add(Retention), CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (j *Job) VisibleTo(userID string) bool {
	return j != nil && strings.TrimSpace(userID) != "" && j.RequestedBy == strings.TrimSpace(userID)
}

func (j *Job) Configure(s Settings, now time.Time) error {
	switch {
	case j.Status == StatusUploaded, j.Status == StatusAnalyzed:
	case j.Status == StatusFailed && j.StartedAt == nil:
	default:
		return ErrNotReady
	}
	settings := s
	j.Settings, j.Fingerprint = &settings, s.Fingerprint()
	j.Status, j.DryRun, j.FailureCode = StatusAnalyzing, nil, ""
	j.Processed, j.Attempts, j.Claim, j.HeartbeatAt = 0, 0, "", nil
	j.UpdatedAt = now.UTC()
	return nil
}

func (j *Job) Start(grants Grants, now time.Time) error {
	if j.Status != StatusAnalyzed || j.DryRun == nil || j.Settings == nil {
		return ErrNotReady
	}
	at := now.UTC()
	j.Status, j.Stage, j.Grants = StatusImporting, StageRows, grants
	j.Processed, j.Attempts, j.Claim, j.HeartbeatAt = 0, 0, "", nil
	result := NewCounts()
	j.Result, j.Seed, j.StartedAt, j.UpdatedAt = &result, nil, &at, at
	return nil
}

func (j Job) Claimable(now time.Time) bool {
	return j.Status.Active() && shared.Lease{Claim: j.Claim, HeartbeatAt: j.HeartbeatAt, Attempts: j.Attempts}.Claimable(now, StaleAfter, MaxAttempts)
}

func (j Job) Expired(now time.Time) bool {
	return !j.ExpiresAt.IsZero() && now.After(j.ExpiresAt)
}

type Counts struct {
	Rows             int            `json:"rows"`
	Created          int            `json:"created"`
	Enriched         int            `json:"enriched"`
	Unchanged        int            `json:"unchanged"`
	Skipped          int            `json:"skipped"`
	Rejected         int            `json:"rejected"`
	Conflicting      int            `json:"conflicting"`
	Blocked          int            `json:"blocked"`
	AddressesAdded   int            `json:"addressesAdded"`
	AddressesLocated int            `json:"addressesLocated"`
	AddressesFilled  int            `json:"addressesFilled"`
	LinksPlanned     int            `json:"linksPlanned"`
	LinksCreated     int            `json:"linksCreated"`
	NoAddress        *int           `json:"noAddress,omitempty"`
	Issues           map[string]int `json:"issues,omitempty"`
}

func NewCounts() Counts {
	return Counts{NoAddress: new(int)}
}

func (c Counts) AddressesMeasured() bool {
	return c.NoAddress != nil
}

func (c *Counts) Record(d lead.ImportDecision, matched *lead.Lead) {
	c.Rows++
	switch d.Verdict {
	case lead.ImportCreated:
		c.Created++
		if d.Lead != nil {
			for _, a := range d.Lead.Addresses {
				c.address(a)
			}
		}
	case lead.ImportEnriched:
		c.Enriched++
		if d.NewAddress != nil {
			c.address(*d.NewAddress)
		}
		if d.FilledAddress != nil {
			c.AddressesFilled++
		}
	case lead.ImportUnchanged:
		c.Unchanged++
	case lead.ImportSkipped:
		c.Skipped++
	case lead.ImportRejected:
		c.Rejected++
	}
	if len(d.Conflicts) > 0 {
		c.Conflicting++
	}
	if matched != nil && matched.Blocked {
		c.Blocked++
	}
	for _, issue := range d.Issues {
		c.count(issue)
	}
}

func (c *Counts) RecordRow(row lead.ImportRecord, d lead.ImportDecision, matched *lead.Lead) {
	c.Record(d, matched)
	if c.NoAddress != nil && !row.AddressGiven && d.Verdict != lead.ImportRejected {
		*c.NoAddress++
	}
}

func (c *Counts) address(a lead.Address) {
	c.AddressesAdded++
	if a.GeoStatus == lead.GeoLocated {
		c.AddressesLocated++
	}
}

func (c *Counts) Issue(issue lead.ImportIssue) {
	if issue.Rejected {
		c.Rows++
		c.Rejected++
	}
	c.count(issue)
}

func (c *Counts) count(issue lead.ImportIssue) {
	if c.Issues == nil {
		c.Issues = map[string]int{}
	}
	c.Issues[string(issue.Reason)]++
}

func (c Counts) Clone() Counts {
	out := c
	if c.NoAddress != nil {
		noAddress := *c.NoAddress
		out.NoAddress = &noAddress
	}
	if c.Issues != nil {
		out.Issues = make(map[string]int, len(c.Issues))
		for k, v := range c.Issues {
			out.Issues[k] = v
		}
	}
	return out
}

func (c Counts) IssueTotal() int {
	total := 0
	for _, n := range c.Issues {
		total += n
	}
	return total
}

type Limits struct {
	MaxBytes               int
	MaxMegabytes           int
	MaxRows                int
	MaxSeededConversations int
	RetentionDays          int
	MaxUnusedUploads       int
}

func CurrentLimits() Limits {
	return Limits{
		MaxBytes: MaxFileBytes, MaxMegabytes: MaxFileBytes >> 20, MaxRows: MaxRows,
		MaxSeededConversations: unofficial_whatsapp.MaxScriptedTargets,
		RetentionDays:          int(Retention / (24 * time.Hour)), MaxUnusedUploads: MaxUnusedUploads,
	}
}
