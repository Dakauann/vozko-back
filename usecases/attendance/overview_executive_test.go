package attendance_usecase

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/attendance"
	at "vozko/domain/attendance_target"
	"vozko/domain/conversation"
	wh "vozko/domain/working_hours"
	dept "vozko/domain/workspace/workspace_department"
	wsc "vozko/domain/workspace_config"
)

type stubOverviewRepo struct {
	attendance.Repository
	summary        *attendance.SummarySection
	team           *attendance.TeamSection
	stages         attendance.OverviewStages
	backlog        attendance.BacklogXray
	rework         attendance.OverviewRework
	trend          attendance.TrendResult
	revenue        []attendance.RevenueTally
	revenueByMonth []attendance.RevenueMonthRow
	monthOwners    []string
	revenueScopes  []attendance.RevenueScope
	trendErr       error
	revErr         error
	onRead         func(section string)

	mu    sync.Mutex
	reads map[string]int
}

func (r *stubOverviewRepo) read(section string) {
	r.mu.Lock()
	if r.reads == nil {
		r.reads = map[string]int{}
	}
	r.reads[section]++
	r.mu.Unlock()
	if r.onRead != nil {
		r.onRead(section)
	}
}

func (r *stubOverviewRepo) calls(section string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reads[section]
}

func (r *stubOverviewRepo) ReadSummary(context.Context, string, attendance.OverviewFilter) (*attendance.SummarySection, error) {
	r.read("summary")
	if r.summary == nil {
		return &attendance.SummarySection{Hourly: make([]attendance.HourlyPoint, 24), Definitions: attendance.DefaultDefinitions()}, nil
	}
	copied := *r.summary
	return &copied, nil
}

func (r *stubOverviewRepo) ReadTeam(context.Context, string, attendance.OverviewFilter) (*attendance.TeamSection, error) {
	r.read("team")
	if r.team == nil {
		return &attendance.TeamSection{}, nil
	}
	copied := *r.team
	return &copied, nil
}

func (r *stubOverviewRepo) ReadStages(context.Context, string, attendance.OverviewFilter) (attendance.OverviewStages, error) {
	r.read("stages")
	return r.stages, nil
}

func (r *stubOverviewRepo) ReadBacklog(context.Context, string, attendance.OverviewFilter, time.Time) (attendance.BacklogXray, error) {
	r.read("backlog")
	return r.backlog, nil
}

func (r *stubOverviewRepo) ReadRework(context.Context, string, attendance.OverviewFilter) (attendance.OverviewRework, error) {
	r.read("rework")
	return r.rework, nil
}

func (r *stubOverviewRepo) GetTrend(context.Context, string, attendance.OverviewFilter, int, *time.Location) (attendance.TrendResult, error) {
	r.read("trend")
	return r.trend, r.trendErr
}

func (r *stubOverviewRepo) GetRevenue(_ context.Context, _ string, _, _ time.Time, scope attendance.RevenueScope) ([]attendance.RevenueTally, int64, error) {
	r.read("revenue")
	r.mu.Lock()
	r.revenueScopes = append(r.revenueScopes, scope)
	r.mu.Unlock()
	return r.revenue, 0, r.revErr
}

func (r *stubOverviewRepo) GetRevenueByMonth(
	_ context.Context,
	_ string,
	_, _ time.Time,
	_ *time.Location,
	ownerID string,
	scope attendance.RevenueScope,
) ([]attendance.RevenueMonthRow, error) {
	r.read("revenue_by_month")
	r.mu.Lock()
	r.monthOwners = append(r.monthOwners, ownerID)
	r.revenueScopes = append(r.revenueScopes, scope)
	r.mu.Unlock()
	return r.revenueByMonth, r.revErr
}

type stubConfigReader struct {
	config *wsc.WorkspaceConfig
	err    error
}

func (s stubConfigReader) GetByWorkspaceID(context.Context, string) (*wsc.WorkspaceConfig, error) {
	return s.config, s.err
}

type stubDepartmentSchedules struct {
	rows []dept.DepartmentSchedule
	err  error
}

func (s stubDepartmentSchedules) ListWorkingHours([]string) ([]dept.DepartmentSchedule, error) {
	return s.rows, s.err
}

type stubTargetRepo struct {
	targets      []at.Target
	err          error
	listedPeriod time.Time
}

func (s *stubTargetRepo) GetByID(string, string) (*at.Target, error) { return nil, at.ErrNotFound }

func (s *stubTargetRepo) ListForPeriod(_ string, period time.Time) ([]at.Target, error) {
	s.listedPeriod = period
	return s.targets, s.err
}

func (s *stubTargetRepo) ListRange(string, time.Time, time.Time) ([]at.Target, error) {
	return s.targets, s.err
}

func (s *stubTargetRepo) Upsert(target at.Target) (*at.Target, error) { return &target, s.err }

func (s *stubTargetRepo) Delete(string, string) error { return s.err }

func businessConfig() *wsc.WorkspaceConfig {
	return &wsc.WorkspaceConfig{
		WorkspaceID: "ws1",
		WorkingHours: &wh.Spec{
			Timezone: "America/Sao_Paulo",
			Days: map[string][]wh.Window{
				"mon": {{Start: "09:00", End: "18:00"}},
				"tue": {{Start: "09:00", End: "18:00"}},
				"wed": {{Start: "09:00", End: "18:00"}},
				"thu": {{Start: "09:00", End: "18:00"}},
				"fri": {{Start: "09:00", End: "18:00"}},
			},
		},
	}
}

func newTestUseCase(
	repo attendance.Repository,
	config *wsc.WorkspaceConfig,
	configErr error,
	targets *stubTargetRepo,
) *getOverviewUseCase {
	uc := &getOverviewUseCase{repo: repo, now: func() time.Time {
		return time.Date(2026, 9, 28, 15, 36, 0, 0, time.UTC)
	}}
	resolver := NewScheduleResolver(stubConfigReader{config: config, err: configErr}, stubDepartmentSchedules{})
	var service *TargetsService
	if targets != nil {
		service = NewTargetsService(targets, resolver)
	}
	uc.SetExecutiveDeps(resolver, service)
	return uc
}

func TestExecuteBuildsPeriodAndProjections(t *testing.T) {
	repo := &stubOverviewRepo{summary: &attendance.SummarySection{
		KPIs:        attendance.OverviewKPIs{Finished: 1538, Engaged: 2000},
		Definitions: attendance.DefaultDefinitions(),
	}}
	targets := &stubTargetRepo{targets: []at.Target{
		{Scope: at.ScopeWorkspace, MetricKey: attendance.MetricFinished, Value: 1786},
	}}
	uc := newTestUseCase(repo, businessConfig(), nil, targets)

	out, err := uc.Execute("ws1", attendance.OverviewFilter{})
	if err != nil {
		t.Fatalf("Execute() err = %v, want nil", err)
	}
	if !out.Period.Available {
		t.Fatalf("Execute() Period.Available = false, want true; reason %q", out.Period.Reason)
	}
	if out.Period.OpenDaysTotal != 22 {
		t.Fatalf("Execute() OpenDaysTotal = %d, want 22", out.Period.OpenDaysTotal)
	}

	var finished *attendance.MetricProjection
	for i := range out.Projections {
		if out.Projections[i].MetricKey == attendance.MetricFinished {
			finished = &out.Projections[i]
		}
	}
	if finished == nil {
		t.Fatalf("Execute() produced no projection for %q", attendance.MetricFinished)
	}
	if finished.Target == nil || *finished.Target != 1786 {
		t.Fatalf("Execute() finished target = %v, want 1786", finished.Target)
	}
	if finished.Projected == nil {
		t.Fatalf("Execute() finished projection = nil, want a run rate")
	}
	if out.Standing.TargetsSet != 1 {
		t.Fatalf("Execute() Standing.TargetsSet = %d, want 1", out.Standing.TargetsSet)
	}
}

func TestExecutePropagatesATargetReadFailure(t *testing.T) {
	repo := &stubOverviewRepo{}
	readErr := errors.New("targets table is unreachable")
	uc := newTestUseCase(repo, businessConfig(), nil, &stubTargetRepo{err: readErr})

	_, err := uc.Execute("ws1", attendance.OverviewFilter{})
	if !errors.Is(err, readErr) {
		t.Fatalf("Execute() err = %v, want the target read error rather than a silent no-target overview", err)
	}
}

func TestExecutePropagatesAConfigReadFailure(t *testing.T) {
	repo := &stubOverviewRepo{}
	readErr := errors.New("config table is unreachable")
	uc := newTestUseCase(repo, nil, readErr, &stubTargetRepo{})

	_, err := uc.Execute("ws1", attendance.OverviewFilter{})
	if !errors.Is(err, readErr) {
		t.Fatalf("Execute() err = %v, want the config read error", err)
	}
}

func TestExecuteWithoutAScheduleLeavesTheProjectionUnavailable(t *testing.T) {
	repo := &stubOverviewRepo{summary: &attendance.SummarySection{
		KPIs:        attendance.OverviewKPIs{Finished: 1538},
		Definitions: attendance.DefaultDefinitions(),
	}}
	uc := newTestUseCase(repo, &wsc.WorkspaceConfig{WorkspaceID: "ws1"}, nil, &stubTargetRepo{})

	out, err := uc.Execute("ws1", attendance.OverviewFilter{})
	if err != nil {
		t.Fatalf("Execute() err = %v, want nil", err)
	}
	if out.Period.Available {
		t.Fatalf("Execute() Period.Available = true with no schedule, want false")
	}
	if out.Period.Reason != attendance.ReasonNoSchedule {
		t.Fatalf("Execute() Period.Reason = %q, want %q", out.Period.Reason, attendance.ReasonNoSchedule)
	}
	for _, projection := range out.Projections {
		if projection.Projected != nil {
			t.Fatalf("Execute() projected %q without a schedule", projection.MetricKey)
		}
	}
}

func TestExecuteScopesRevenueToTheConversationFilters(t *testing.T) {
	repo := &stubOverviewRepo{}
	uc := newTestUseCase(repo, businessConfig(), nil, &stubTargetRepo{})
	filter := attendance.OverviewFilter{DepartmentID: "dept1", Channel: "whatsapp", CampaignID: "c1", CampaignType: "whatsapp"}

	out, err := uc.Execute("ws1", filter)
	if err != nil {
		t.Fatalf("Execute() err = %v, want nil", err)
	}
	if !out.Revenue.Available {
		t.Fatalf("Execute() Revenue.Available = false under a department filter, want true")
	}
	if len(repo.revenueScopes) == 0 {
		t.Fatalf("Execute() never read revenue")
	}
	for _, scope := range repo.revenueScopes {
		if scope != filter.RevenueScope() {
			t.Fatalf("revenue read with scope %+v, want %+v", scope, filter.RevenueScope())
		}
	}
}

func TestExecutePropagatesARevenueReadFailure(t *testing.T) {
	readErr := errors.New("opportunities table is unreachable")
	repo := &stubOverviewRepo{revErr: readErr}
	uc := newTestUseCase(repo, businessConfig(), nil, &stubTargetRepo{})

	_, err := uc.Execute("ws1", attendance.OverviewFilter{})
	if !errors.Is(err, readErr) {
		t.Fatalf("Execute() err = %v, want the revenue read error", err)
	}
}

func TestExecuteCarriesTheQualityPolicyIntoTheFilter(t *testing.T) {
	enabled := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	config := businessConfig()
	capture := &conversation.OutcomeCapture{
		Enabled:          true,
		EnabledAt:        &enabled,
		DurableThreshold: 30,
		Outcomes:         []conversation.Outcome{{Code: "sale", Label: "Venda", IsDurable: true}},
	}
	capture.Normalize()
	config.OutcomeCapture = capture

	uc := newTestUseCase(&stubOverviewRepo{}, config, nil, &stubTargetRepo{})
	policy := uc.qualityPolicy(config, "")

	if !policy.Enabled || !policy.Measurable() {
		t.Fatalf("qualityPolicy() = %+v, want an enabled measurable policy", policy)
	}
	if len(policy.DurableCodes) != 1 || policy.DurableCodes[0] != "sale" {
		t.Fatalf("qualityPolicy() DurableCodes = %v, want [sale]", policy.DurableCodes)
	}
}

func TestQualityPolicyOffOutsideTheScopedDepartments(t *testing.T) {
	enabled := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	config := businessConfig()
	config.OutcomeCapture = &conversation.OutcomeCapture{
		Enabled:       true,
		EnabledAt:     &enabled,
		DepartmentIDs: []string{"dept1"},
		Outcomes:      []conversation.Outcome{{Code: "sale", Label: "Venda", IsDurable: true}},
	}

	uc := newTestUseCase(&stubOverviewRepo{}, config, nil, &stubTargetRepo{})
	if uc.qualityPolicy(config, "dept2").Enabled {
		t.Fatalf("qualityPolicy() enabled for a department outside the policy scope")
	}
	if !uc.qualityPolicy(config, "dept1").Enabled {
		t.Fatalf("qualityPolicy() disabled for a department inside the policy scope")
	}
}
