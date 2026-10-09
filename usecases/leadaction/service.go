package leadaction_usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/advertising"
	"vozko/domain/cache"
	"vozko/domain/calls/calllist"
	"vozko/domain/campaign"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/leadaction"
	"vozko/domain/metrics"
	"vozko/domain/report"
	"vozko/domain/selection"
	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
	adsuc "vozko/usecases/advertising"
	calllist_usecase "vozko/usecases/calls/calllist"
	lead_usecase "vozko/usecases/lead"
	report_usecase "vozko/usecases/report"
)

var errIncomplete = errors.New("lead actions: a required dependency is missing")

var idNamespace = uuid.MustParse("6f1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d")

type Actor = lead_usecase.Actor

type Definitions interface {
	ListByObject(workspaceID string, objectType customfield.ObjectType) ([]*customfield.Definition, error)
}

type Selections interface {
	Count(ctx context.Context, scope selection.Scope, s selection.Selection) (int, error)
	Selected(ctx context.Context, scope selection.Scope, s selection.Selection) (int, error)
	Resolve(ctx context.Context, scope selection.Scope, s selection.Selection, after string, limit int) ([]selection.Ref, error)
	Freeze(ctx context.Context, scope selection.Scope, s selection.Selection, snapshotID string, pending *lead.Assignment) (lead.Frozen, error)
	Snapshot(ctx context.Context, workspaceID, snapshotID, after string, limit int) ([]string, error)
	SnapshotSize(ctx context.Context, workspaceID, snapshotID string) (int, error)
	DropSnapshot(ctx context.Context, workspaceID, snapshotID string) error
	SweepSnapshots(ctx context.Context, before time.Time, limit int) (int64, error)
}

type Owners interface {
	CheckOwner(a Actor, owner string) error
}

type MetaPhones interface {
	Phone(workspaceID, businessPhoneID string) (lead.WhatsAppBlock, error)
}

type Reports interface {
	Create(input report_usecase.CreateInput) (*report.Job, error)
}

type Audiences interface {
	CreateCustomerList(ctx context.Context, a adsuc.Requester, draft advertising.CustomerListDraft) (*adsuc.CustomerListResult, error)
}

type CallLists interface {
	View(ctx context.Context, a Actor, id string) (calllist_usecase.ListView, error)
	Prepare(ctx context.Context, a Actor, d calllist.Draft) (calllist_usecase.Prepared, error)
	CreateFromSnapshot(ctx context.Context, a Actor, p calllist_usecase.Prepared, selected int) (calllist_usecase.ListView, error)
}

type Deps struct {
	Runs        leadaction.Store
	Writer      leadaction.BulkWriter
	Notifier    leadaction.BulkNotifier
	Selections  Selections
	Permissions workspace.PermissionChecker
	Definitions Definitions
	Owners      Owners
	MetaPhones  MetaPhones
	MetaLimiter cache.RateLimiter
	State       cache.SharedState
	Gate        cache.Gate
	Reports     Reports
	Audiences   Audiences
	CallLists   CallLists
	Sends       Sends
	Metrics     metrics.LeadActionMetricsRecorder
	Now         func() time.Time
	NewID       func() string
	Background  func(func())
	Sleep       func(ctx context.Context, d time.Duration) error
	After       func(d time.Duration, run func())
}

type Service struct {
	deps Deps
}

func NewService(deps Deps) (*Service, error) {
	missing := map[string]bool{
		"runs":         deps.Runs == nil,
		"writer":       deps.Writer == nil,
		"notifier":     deps.Notifier == nil,
		"selections":   deps.Selections == nil,
		"permissions":  deps.Permissions == nil,
		"definitions":  deps.Definitions == nil,
		"owners":       deps.Owners == nil,
		"meta phones":  deps.MetaPhones == nil,
		"meta limiter": deps.MetaLimiter == nil,
		"state":        deps.State == nil,
		"gate":         deps.Gate == nil,
		"reports":      deps.Reports == nil,
		"audiences":    deps.Audiences == nil,
		"call lists":   deps.CallLists == nil,
		"sends":        deps.Sends == nil,
		"metrics":      deps.Metrics == nil,
	}
	for name, absent := range missing {
		if absent {
			return nil, fmt.Errorf("%w: %s", errIncomplete, name)
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
	if deps.Sleep == nil {
		deps.Sleep = sleep
	}
	if deps.After == nil {
		deps.After = func(d time.Duration, run func()) { time.AfterFunc(d, run) }
	}
	return &Service{deps: deps}, nil
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type Request struct {
	Actor            Actor
	DepartmentID     string
	DepartmentFilter *wd.DepartmentFilter
	Action           leadaction.Action
	Params           leadaction.Params
	Selection        selection.Selection
	IdempotencyKey   string
	Locale           string
}

type Outcome struct {
	Run      *leadaction.Run
	Report   *report.Job
	Audience *leadaction.AudienceJob
	CallList *calllist_usecase.ListView
	Send     *campaign.SendReview
}

func (s *Service) now() time.Time {
	return s.deps.Now().UTC()
}

func scopeOf(a Actor, departmentID string) selection.Scope {
	return selection.Scope{WorkspaceID: a.WorkspaceID, ActorID: a.UserID, IsAdmin: a.IsAdmin, DepartmentID: departmentID}
}

func (s *Service) holds(a Actor, keys []workspace.CapabilityKey) (bool, error) {
	permissions, err := leadaction.PermissionsOf(keys)
	if err != nil {
		return false, err
	}
	return workspace.HoldsAll(s.deps.Permissions, a.WorkspaceID, a.UserID, a.IsAdmin, permissions), nil
}

func (s *Service) readsSensitive(a Actor) bool {
	ok, err := s.holds(a, []workspace.CapabilityKey{leadaction.CapabilityReadSensitive})
	return err == nil && ok
}

func (s *Service) sensitiveField(workspaceID string, action leadaction.Action, p leadaction.Params) (bool, []*customfield.Definition, error) {
	if action != leadaction.ActionClassify {
		return false, nil, nil
	}
	defs, err := s.deps.Definitions.ListByObject(workspaceID, customfield.ObjectLead)
	if err != nil {
		return false, nil, fmt.Errorf("lead fields of workspace %s: %w", workspaceID, err)
	}
	key := strings.TrimSpace(p.Key)
	for _, def := range defs {
		if def != nil && def.Key == key {
			return def.Sensitive, defs, nil
		}
	}
	return false, defs, nil
}

func (s *Service) authorize(a Actor, action leadaction.Action, p leadaction.Params) ([]*customfield.Definition, error) {
	if strings.TrimSpace(a.WorkspaceID) == "" {
		return nil, leadaction.ErrWorkspaceRequired
	}
	if strings.TrimSpace(a.UserID) == "" {
		return nil, leadaction.ErrActorRequired
	}
	if !action.Known() {
		return nil, fmt.Errorf("%w: %q", leadaction.ErrUnknownAction, action)
	}
	if err := p.Validate(action); err != nil {
		return nil, err
	}
	sensitive, defs, err := s.sensitiveField(a.WorkspaceID, action, p)
	if err != nil {
		return nil, err
	}
	allowed, err := s.holds(a, action.Requirements(p, sensitive))
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, leadaction.ErrForbidden
	}
	return defs, nil
}

func (s *Service) metaPhone(workspaceID string, p leadaction.Params) (lead.WhatsAppBlock, error) {
	if _, err := uuid.Parse(p.BusinessPhoneID); err != nil {
		return nil, fmt.Errorf("%w: %q", leadaction.ErrPhoneUnavailable, p.BusinessPhoneID)
	}
	phone, err := s.deps.MetaPhones.Phone(workspaceID, p.BusinessPhoneID)
	if errors.Is(err, lead_usecase.ErrBlockingPhoneUnavailable) {
		return nil, fmt.Errorf("%w: %w", leadaction.ErrPhoneUnavailable, err)
	}
	if err != nil {
		return nil, err
	}
	if phone == nil {
		return nil, leadaction.ErrPhoneUnavailable
	}
	return phone, nil
}

func (s *Service) checkPhone(a Actor, action leadaction.Action, p leadaction.Params) error {
	if action != leadaction.ActionBlock || strings.TrimSpace(p.BusinessPhoneID) == "" {
		return nil
	}
	_, err := s.metaPhone(a.WorkspaceID, p)
	return err
}

func (s *Service) editFor(a Actor, action leadaction.Action, p leadaction.Params, defs []*customfield.Definition) (leadaction.Edit, error) {
	edit, err := leadaction.EditFor(action, p, defs, customfield.Viewer{ReadsSensitive: s.readsSensitive(a)})
	if err != nil {
		return leadaction.Edit{}, err
	}
	if edit.Kind == leadaction.EditOwner {
		if err := s.deps.Owners.CheckOwner(a, edit.Owner()); err != nil {
			return leadaction.Edit{}, err
		}
	}
	return edit, nil
}

func (s *Service) gated(ctx context.Context, fn func(context.Context) error) error {
	return cache.Gated(ctx, s.deps.Gate, fn)
}

func derivedID(parts ...string) string {
	return uuid.NewSHA1(idNamespace, []byte(strings.Join(parts, "|"))).String()
}

func (s *Service) Run(ctx context.Context, a Actor, id string) (*leadaction.Run, error) {
	if strings.TrimSpace(a.WorkspaceID) == "" {
		return nil, leadaction.ErrWorkspaceRequired
	}
	run, err := s.deps.Runs.Get(ctx, a.WorkspaceID, id)
	if err != nil {
		return nil, err
	}
	holds, err := s.holds(a, run.Action.Requirements(run.Params, false))
	if err != nil {
		return nil, err
	}
	if !run.VisibleTo(a.UserID, holds) {
		return nil, leadaction.ErrRunNotFound
	}
	return s.redacted(a, run)
}

func (s *Service) redacted(a Actor, run *leadaction.Run) (*leadaction.Run, error) {
	sensitive, _, err := s.sensitiveField(run.WorkspaceID, run.Action, run.Params)
	if err != nil {
		return nil, err
	}
	run.Params = run.Params.Redacted(sensitive && !s.readsSensitive(a))
	return run, nil
}

func (s *Service) dropSnapshot(ctx context.Context, workspaceID, snapshotID string) {
	if err := s.deps.Selections.DropSnapshot(context.WithoutCancel(ctx), workspaceID, snapshotID); err != nil {
		slog.Warn("lead action: the frozen set stays until the sweep", "run_id", snapshotID, "workspace_id", workspaceID, "error", err)
	}
}
