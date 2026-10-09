package lead_usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"vozko/domain/customfield"
	"vozko/domain/leadimport"
	"vozko/domain/metrics"
	"vozko/domain/unofficial_whatsapp"
	"vozko/domain/workspace"
)

const (
	defaultImportFileName = "leads.csv"
	maxImportFileName     = 255
	importSweepLimit      = 20
	importExpireLimit     = 100
)

var errImportIncomplete = errors.New("lead import: a required dependency is missing")

type ImportDeps struct {
	Jobs        leadimport.Store
	Writer      leadimport.Writer
	Lookup      leadimport.Lookup
	Files       leadimport.Files
	Members     leadimport.Members
	Permissions Permissions
	Visibility  MemberVisibility
	Definitions DefinitionSource
	Seeder      leadimport.Seeder
	Metrics     metrics.LeadImportMetricsRecorder
	Now         func() time.Time
	NewID       func() string
	Background  func(func())
}

type Import struct {
	deps    ImportDeps
	viewers viewers
}

func NewImport(deps ImportDeps) (*Import, error) {
	missing := map[string]bool{
		"jobs":        deps.Jobs == nil,
		"writer":      deps.Writer == nil,
		"lookup":      deps.Lookup == nil,
		"files":       deps.Files == nil,
		"members":     deps.Members == nil,
		"permissions": deps.Permissions == nil,
		"visibility":  deps.Visibility == nil,
		"definitions": deps.Definitions == nil,
		"metrics":     deps.Metrics == nil,
	}
	for name, absent := range missing {
		if absent {
			return nil, fmt.Errorf("%w: %s", errImportIncomplete, name)
		}
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.NewID == nil {
		deps.NewID = uuid.NewString
	}
	if deps.Background == nil {
		deps.Background = func(run func()) { go run() }
	}
	return &Import{deps: deps, viewers: viewers{permissions: deps.Permissions, definitions: deps.Definitions}}, nil
}

func (i *Import) now() time.Time {
	return i.deps.Now().UTC()
}

func (i *Import) permit(a Actor, actions ...workspace.Action) error {
	for _, action := range actions {
		if !i.viewers.allowed(a, action) {
			return &leadimport.PermissionError{Action: action}
		}
	}
	return nil
}

func (i *Import) definitions(workspaceID string) ([]*customfield.Definition, error) {
	defs, err := i.deps.Definitions.ListByObject(workspaceID, customfield.ObjectLead)
	if err != nil {
		return nil, fmt.Errorf("lead fields of workspace %s: %w", workspaceID, err)
	}
	return defs, nil
}

type UploadInput struct {
	FileName string
	Data     []byte
	MediaID  string
}

func (i *Import) Upload(ctx context.Context, a Actor, in UploadInput) (*leadimport.Job, error) {
	if err := i.permit(a, workspace.ActionCreate); err != nil {
		return nil, err
	}
	data, owned := in.Data, strings.TrimSpace(in.MediaID) == ""
	if !owned {
		if !i.readsMedia(a) {
			return nil, &leadimport.PermissionError{Resource: workspace.ResourceMedia, Action: workspace.ActionRead}
		}
		read, err := i.deps.Files.ReadLibrary(ctx, a.WorkspaceID, strings.TrimSpace(in.MediaID))
		if err != nil {
			return nil, err
		}
		data = read
	}
	scan, err := leadimport.ScanFile(data)
	if err != nil {
		return nil, err
	}
	defs, err := i.definitions(a.WorkspaceID)
	if err != nil {
		return nil, err
	}
	preview := leadimport.Preview{Headers: scan.Headers, Sample: scan.Sample, Columns: leadimport.Suggest(scan.Headers, defs)}
	file := leadimport.File{MediaID: strings.TrimSpace(in.MediaID), Name: importFileName(in.FileName), SizeBytes: int64(len(data)), Owned: owned}
	job, err := leadimport.NewJob(a.WorkspaceID, a.UserID, file, preview, scan.Rows, i.now())
	if err != nil {
		return nil, err
	}
	job.ID = i.deps.NewID()
	if err := i.makeRoom(ctx, a); err != nil {
		return nil, err
	}
	if owned {
		mediaID, err := i.deps.Files.Store(ctx, a.WorkspaceID, job.File.Name, data)
		if err != nil {
			return nil, err
		}
		job.File.MediaID = mediaID
	}
	if err := i.deps.Jobs.Create(ctx, job); err != nil {
		if owned {
			i.erase(ctx, job)
		}
		return nil, err
	}
	return job, nil
}

func (i *Import) readsMedia(a Actor) bool {
	return strings.TrimSpace(a.UserID) != "" &&
		i.deps.Permissions.HasWorkspacePermission(a.UserID, a.WorkspaceID, string(workspace.ResourceMedia), string(workspace.ActionRead), a.IsAdmin)
}

func (i *Import) makeRoom(ctx context.Context, a Actor) error {
	unused, err := i.deps.Jobs.Unused(ctx, a.WorkspaceID, a.UserID)
	if err != nil {
		return err
	}
	for _, job := range leadimport.UploadsToErase(unused) {
		if err := i.discard(ctx, &job); err != nil {
			return err
		}
	}
	return nil
}

func (i *Import) discard(ctx context.Context, job *leadimport.Job) error {
	if job.File.Owned && job.File.MediaID != "" {
		if err := i.deps.Files.Erase(ctx, job.WorkspaceID, job.File.MediaID); err != nil {
			return fmt.Errorf("the file of import %s could not be erased: %w", job.ID, err)
		}
	}
	return i.deps.Jobs.Delete(ctx, job.ID)
}

func importFileName(raw string) string {
	name := strings.TrimSpace(filepath.Base(strings.ReplaceAll(raw, "\\", "/")))
	if name == "" || name == "." || name == "/" {
		return defaultImportFileName
	}
	for utf8.RuneCountInString(name) > maxImportFileName {
		runes := []rune(name)
		name = string(runes[len(runes)-maxImportFileName:])
	}
	return name
}

type ImportField struct {
	leadimport.Field
	Allowed bool
}

type ImportCatalog struct {
	Fields     []ImportField
	FillEmpty  bool
	SeedInbox  bool
	SeedScript bool
}

func (i *Import) Catalog(ctx context.Context, a Actor) (ImportCatalog, error) {
	if err := i.permit(a, workspace.ActionCreate); err != nil {
		return ImportCatalog{}, err
	}
	defs, err := i.definitions(a.WorkspaceID)
	if err != nil {
		return ImportCatalog{}, err
	}
	fields := leadimport.Catalog(defs)
	catalog := ImportCatalog{Fields: make([]ImportField, len(fields)), FillEmpty: i.viewers.allowed(a, workspace.ActionUpdate)}
	for idx, f := range fields {
		catalog.Fields[idx] = ImportField{Field: f, Allowed: f.Requires == "" || i.viewers.allowed(a, f.Requires)}
	}
	seed := i.grants(a, leadimport.Settings{SeedInbox: true, Script: &unofficial_whatsapp.SeedScript{}})
	catalog.SeedInbox, catalog.SeedScript = seed.Seed && i.deps.Seeder != nil, seed.Script && i.deps.Seeder != nil
	return catalog, nil
}

func (i *Import) Get(ctx context.Context, a Actor, id string) (*leadimport.Job, error) {
	return i.owned(ctx, a, id)
}

func (i *Import) owned(ctx context.Context, a Actor, id string) (*leadimport.Job, error) {
	if err := i.permit(a, workspace.ActionCreate); err != nil {
		return nil, err
	}
	job, err := i.deps.Jobs.Get(ctx, a.WorkspaceID, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if !job.VisibleTo(a.UserID) || job.Expired(i.now()) {
		return nil, leadimport.ErrNotFound
	}
	return job, nil
}

func (i *Import) DryRun(ctx context.Context, a Actor, id string, s leadimport.Settings) (*leadimport.Job, error) {
	job, err := i.owned(ctx, a, id)
	if err != nil {
		return nil, err
	}
	defs, err := i.definitions(a.WorkspaceID)
	if err != nil {
		return nil, err
	}
	s.Normalize()
	if err := s.Validate(job.Preview.Headers, defs); err != nil {
		return nil, err
	}
	if err := i.permit(a, s.Requirements(defs)...); err != nil {
		return nil, err
	}
	from := job.Status
	job.RequestedByAdmin = a.IsAdmin
	if err := job.Configure(s, i.now()); err != nil {
		return nil, err
	}
	if err := i.deps.Jobs.Save(ctx, job, leadimport.Guard{From: []leadimport.Status{from}}); err != nil {
		return nil, err
	}
	i.launch(leadimport.Ref{ID: job.ID, WorkspaceID: job.WorkspaceID})
	return job, nil
}

func (i *Import) Start(ctx context.Context, a Actor, id string) (*leadimport.Job, error) {
	job, err := i.owned(ctx, a, id)
	if err != nil {
		return nil, err
	}
	if job.Settings == nil {
		return nil, leadimport.ErrNotReady
	}
	defs, err := i.definitions(a.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if err := i.permit(a, job.Settings.Requirements(defs)...); err != nil {
		return nil, err
	}
	job.RequestedByAdmin = a.IsAdmin
	if err := job.Start(i.grants(a, *job.Settings), i.now()); err != nil {
		return nil, err
	}
	if err := i.deps.Jobs.Save(ctx, job, leadimport.Guard{From: []leadimport.Status{leadimport.StatusAnalyzed}}); err != nil {
		return nil, err
	}
	i.launch(leadimport.Ref{ID: job.ID, WorkspaceID: job.WorkspaceID})
	return job, nil
}

func (i *Import) grants(a Actor, s leadimport.Settings) leadimport.Grants {
	seed := s.SeedInbox && strings.TrimSpace(a.UserID) != "" &&
		i.deps.Permissions.HasWorkspacePermission(a.UserID, a.WorkspaceID,
			string(workspace.ResourceUnofficialWhatsAppInstances), string(workspace.ActionSend), a.IsAdmin)
	return leadimport.Grants{Seed: seed, Script: seed && s.Script != nil && a.IsAdmin}
}

func (i *Import) Rejections(ctx context.Context, a Actor, id string, after int64, limit int) ([]leadimport.IssueRow, error) {
	job, err := i.owned(ctx, a, id)
	if err != nil {
		return nil, err
	}
	return i.deps.Jobs.Issues(ctx, job.ID, after, limit)
}

func (i *Import) log(job *leadimport.Job) *slog.Logger {
	return slog.With("import_id", job.ID, "workspace_id", job.WorkspaceID)
}

func (i *Import) launch(ref leadimport.Ref) {
	i.deps.Background(func() {
		if err := i.Process(context.Background(), ref.ID); err != nil {
			slog.Warn("lead import: the run stopped", "import_id", ref.ID, "workspace_id", ref.WorkspaceID, "error", err)
		}
	})
}

func (i *Import) Process(ctx context.Context, id string) error {
	token := i.deps.NewID()
	job, err := i.deps.Jobs.Claim(ctx, id, token, i.now())
	if errors.Is(err, leadimport.ErrClaimLost) || errors.Is(err, leadimport.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	switch job.Status {
	case leadimport.StatusAnalyzing:
		err = i.analyze(ctx, job)
	case leadimport.StatusImporting:
		err = i.importAll(ctx, job)
	default:
		return nil
	}
	if err != nil {
		i.stop(ctx, job, err)
	}
	return err
}

func (i *Import) stop(ctx context.Context, job *leadimport.Job, cause error) {
	if errors.Is(cause, leadimport.ErrClaimLost) {
		return
	}
	code, permanent := leadimport.FailureOf(cause)
	if permanent {
		i.fail(ctx, job, code, cause)
		return
	}
	i.log(job).Warn("lead import: handed back to the sweeper after a passing error", "status", job.Status, "stage", job.Stage,
		"processed", job.Processed, "attempts", job.Attempts, "error", cause)
	from, claim := job.Status, job.Claim
	if err := job.Release(i.now()); err != nil {
		i.log(job).Error("lead import: could not be released", "error", err)
		return
	}
	if err := i.deps.Jobs.Save(context.WithoutCancel(ctx), job, leadimport.Guard{From: []leadimport.Status{from}, Claim: claim}); err != nil {
		i.log(job).Error("lead import: could not be released", "error", err)
	}
}

func (i *Import) fail(ctx context.Context, job *leadimport.Job, code leadimport.FailureCode, cause error) {
	from, claim := job.Status, job.Claim
	if err := job.Fail(code, i.now()); err != nil {
		i.log(job).Error("lead import: could not be marked failed", "failure_code", code, "error", err)
		return
	}
	if err := i.deps.Jobs.Save(context.WithoutCancel(ctx), job, leadimport.Guard{From: []leadimport.Status{from}, Claim: claim}); err != nil {
		i.log(job).Error("lead import: could not be marked failed", "failure_code", code, "error", err)
		return
	}
	if from == leadimport.StatusImporting {
		i.deps.Metrics.AddLeadImportRuns(string(code), 1)
	}
	i.log(job).Warn("lead import: failed", "failure_code", code, "status", from, "processed", job.Processed, "error", cause)
}

func (i *Import) finished(job *leadimport.Job) {
	i.deps.Metrics.AddLeadImportRuns(metrics.LeadImportDone, 1)
	var elapsed time.Duration
	if job.StartedAt != nil && job.FinishedAt != nil {
		elapsed = job.FinishedAt.Sub(*job.StartedAt)
		i.deps.Metrics.ObserveLeadImportDuration(elapsed)
	}
	attrs := []any{"rows", job.TotalRows, "elapsed_ms", elapsed.Milliseconds()}
	if r := job.Result; r != nil {
		attrs = append(attrs, "created", r.Created, "enriched", r.Enriched, "unchanged", r.Unchanged, "skipped", r.Skipped,
			"rejected", r.Rejected, "addresses_added", r.AddressesAdded, "links_created", r.LinksCreated)
	}
	if s := job.Seed; s != nil {
		attrs = append(attrs, "seed_queued", s.Queued, "seed_unconfirmed", s.Unconfirmed, "seed_error", s.Error)
	}
	i.log(job).Info("lead import: done", attrs...)
}

func (i *Import) countRows(before, after leadimport.Counts) {
	for _, delta := range []struct {
		outcome string
		n       int
	}{
		{metrics.LeadImportRowsCreated, after.Created - before.Created},
		{metrics.LeadImportRowsEnriched, after.Enriched - before.Enriched},
		{metrics.LeadImportRowsUnchanged, after.Unchanged - before.Unchanged},
		{metrics.LeadImportRowsSkipped, after.Skipped - before.Skipped},
		{metrics.LeadImportRowsRejected, after.Rejected - before.Rejected},
	} {
		if delta.n > 0 {
			i.deps.Metrics.AddLeadImportRows(delta.outcome, delta.n)
		}
	}
}

func (i *Import) Sweep(ctx context.Context) error {
	now := i.now()
	stalled, err := i.deps.Jobs.FailStalled(ctx, now)
	if err != nil {
		return err
	}
	i.stalled(stalled)
	refs, err := i.deps.Jobs.Claimable(ctx, now, importSweepLimit)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		i.launch(ref)
	}
	expired, err := i.deps.Jobs.Expired(ctx, now, importExpireLimit)
	if err != nil {
		return err
	}
	for idx := range expired {
		if err := i.discard(ctx, &expired[idx]); err != nil {
			i.log(&expired[idx]).Error("lead import: an expired import stays", "error", err)
		}
	}
	return nil
}

func (i *Import) stalled(stalled []leadimport.Stalled) {
	imports := 0
	for _, s := range stalled {
		if s.Status == leadimport.StatusImporting {
			imports++
		}
		slog.Warn("lead import: stopped after every attempt", "import_id", s.ID, "workspace_id", s.WorkspaceID, "status", s.Status,
			"attempts", leadimport.MaxAttempts)
	}
	if imports > 0 {
		i.deps.Metrics.AddLeadImportRuns(string(leadimport.FailureStalled), imports)
	}
}

func (i *Import) erase(ctx context.Context, job *leadimport.Job) {
	if job.File.MediaID == "" {
		return
	}
	if err := i.deps.Files.Erase(ctx, job.WorkspaceID, job.File.MediaID); err != nil {
		i.log(job).Error("lead import: the file could not be erased", "error", err)
	}
}

func (i *Import) List(ctx context.Context, a Actor) ([]leadimport.Job, error) {
	if err := i.permit(a, workspace.ActionCreate); err != nil {
		return nil, err
	}
	now := i.now()
	jobs, err := i.deps.Jobs.Mine(ctx, a.WorkspaceID, a.UserID, now, leadimport.MaxListed)
	if err != nil {
		return nil, err
	}
	return leadimport.Listed(jobs, a.UserID, now), nil
}

type ImportDetail struct {
	Job       *leadimport.Job
	Placement *leadimport.Placement
}

func (i *Import) Detail(ctx context.Context, a Actor, id string) (ImportDetail, error) {
	job, err := i.owned(ctx, a, id)
	if err != nil {
		return ImportDetail{}, err
	}
	out := ImportDetail{Job: job}
	if job.Status != leadimport.StatusDone || job.Result == nil || !job.Result.AddressesMeasured() {
		return out, nil
	}
	placement, err := i.deps.Jobs.Placement(ctx, job.WorkspaceID, job.ID)
	if err != nil {
		return ImportDetail{}, err
	}
	out.Placement = &placement
	return out, nil
}
