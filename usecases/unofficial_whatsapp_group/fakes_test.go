package unofficial_whatsapp_group_usecase

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"vozko/domain/ai"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	uwg "vozko/domain/unofficial_whatsapp_group"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

var testNow = time.Date(2026, 10, 17, 15, 0, 0, 0, time.UTC)

type fakeSeats struct {
	uw.GroupRepository
	mu    sync.Mutex
	seats map[string]*uw.Group
}

func newFakeSeats() *fakeSeats { return &fakeSeats{seats: map[string]*uw.Group{}} }

func (f *fakeSeats) put(g *uw.Group) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if g.CreatedAt.IsZero() {
		g.CreatedAt = testNow.Add(-24*time.Hour + time.Duration(len(f.seats))*time.Minute)
	}
	f.seats[g.InstanceID+"|"+g.JID] = g
}

func (f *fakeSeats) Upsert(_ context.Context, g *uw.Group) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := g.InstanceID + "|" + g.JID
	if existing, ok := f.seats[key]; ok && len(g.Participants) == 0 {
		g.Participants = existing.Participants
		g.CreatedAt = existing.CreatedAt
	}
	if g.CreatedAt.IsZero() {
		g.CreatedAt = testNow
	}
	copied := *g
	f.seats[key] = &copied
	return nil
}

func (f *fakeSeats) FindByJID(_ context.Context, instanceID, jid string) (*uw.Group, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if g, ok := f.seats[instanceID+"|"+jid]; ok {
		return g, nil
	}
	return nil, uw.ErrGroupNotFound
}

func (f *fakeSeats) ListByJIDs(_ context.Context, workspaceID string, jids []string) (map[string][]*uw.Group, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string][]*uw.Group{}
	for _, jid := range jids {
		for _, g := range f.seats {
			if g.WorkspaceID == workspaceID && g.JID == jid {
				out[jid] = append(out[jid], g)
			}
		}
	}
	return out, nil
}

func (f *fakeSeats) instancesOf(jid string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, g := range f.seats {
		if g.JID == jid {
			out = append(out, g.InstanceID)
		}
	}
	return out
}

func (f *fakeSeats) remove(instanceID, jid string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.seats, instanceID+"|"+jid)
}

type fakeGroups struct {
	mu    sync.Mutex
	byID  map[string]*uwg.Group
	seats *fakeSeats
	saves int
}

func newFakeGroups(seats *fakeSeats) *fakeGroups {
	return &fakeGroups{byID: map[string]*uwg.Group{}, seats: seats}
}

func (f *fakeGroups) inScope(g *uwg.Group, scope uwg.Scope) bool {
	if !scope.Restrict {
		return true
	}
	for _, id := range f.seats.instancesOf(g.JID) {
		if scope.Allows(id) {
			return true
		}
	}
	return false
}

func (f *fakeGroups) UpsertSeen(_ context.Context, g *uwg.Group) error {
	g.Normalize()
	if err := g.Validate(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, stored := range f.byID {
		if stored.WorkspaceID == g.WorkspaceID && stored.JID == g.JID {
			if g.Subject != "" {
				stored.Subject = g.Subject
			}
			stored.Description, stored.Announce = g.Description, g.Announce
			if g.MemberCount > 0 && !stored.Followed() {
				stored.MemberCount = g.MemberCount
			}
			*g = *stored
			return nil
		}
	}
	g.ID = "grp-" + g.JID
	g.CreatedAt = testNow
	copied := *g
	f.byID[g.ID] = &copied
	return nil
}

func (f *fakeGroups) Save(_ context.Context, g *uwg.Group) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves++
	copied := *g
	if stored, ok := f.byID[g.ID]; ok {
		copied.FollowedAt, copied.MembersSyncedAt, copied.MemberCount = stored.FollowedAt, stored.MembersSyncedAt, stored.MemberCount
		copied.PrimaryInstanceID = stored.PrimaryInstanceID
	}
	f.byID[g.ID] = &copied
	return nil
}

func (f *fakeGroups) SetFollowed(_ context.Context, _, id string, at *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if stored, ok := f.byID[id]; ok {
		stored.FollowedAt, stored.MembersSyncedAt = at, nil
	}
	return nil
}

func (f *fakeGroups) SetPrimary(_ context.Context, _, id, instanceID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if stored, ok := f.byID[id]; ok {
		stored.PrimaryInstanceID = instanceID
	}
	return nil
}

func (f *fakeGroups) FindByID(_ context.Context, workspaceID, id string, scope uwg.Scope) (*uwg.Group, error) {
	f.mu.Lock()
	g, ok := f.byID[id]
	f.mu.Unlock()
	if !ok || g.WorkspaceID != workspaceID || !f.inScope(g, scope) {
		return nil, uwg.ErrGroupNotFound
	}
	copied := *g
	return &copied, nil
}

func (f *fakeGroups) FindByJID(_ context.Context, workspaceID, jid string) (*uwg.Group, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, g := range f.byID {
		if g.WorkspaceID == workspaceID && g.JID == jid {
			copied := *g
			return &copied, nil
		}
	}
	return nil, uwg.ErrGroupNotFound
}

func (f *fakeGroups) FindByIDs(ctx context.Context, workspaceID string, ids []string, scope uwg.Scope) ([]*uwg.Group, error) {
	var out []*uwg.Group
	for _, id := range ids {
		if g, err := f.FindByID(ctx, workspaceID, id, scope); err == nil {
			out = append(out, g)
		}
	}
	return out, nil
}

func (f *fakeGroups) Exists(_ context.Context, workspaceID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, g := range f.byID {
		if g.WorkspaceID == workspaceID {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeGroups) List(_ context.Context, filter uwg.GroupFilter) (*shared.PaginatedResult[*uwg.Group], error) {
	f.mu.Lock()
	all := make([]*uwg.Group, 0, len(f.byID))
	for _, g := range f.byID {
		all = append(all, g)
	}
	f.mu.Unlock()
	var items []*uwg.Group
	for _, g := range all {
		if g.WorkspaceID != filter.WorkspaceID || !f.inScope(g, filter.Scope) {
			continue
		}
		if filter.Followed != nil && g.Followed() != *filter.Followed {
			continue
		}
		if filter.SeriesID != "" && g.SeriesID != filter.SeriesID {
			continue
		}
		copied := *g
		items = append(items, &copied)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Subject < items[j].Subject })
	return shared.NewPaginatedResult(items, filter.Pagination, int64(len(items))), nil
}

func (f *fakeGroups) Categories(context.Context, string, uwg.Scope) ([]string, error) {
	return nil, nil
}

func (f *fakeGroups) NextSeriesSequence(_ context.Context, workspaceID, seriesID string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	next := 1
	for _, g := range f.byID {
		if g.WorkspaceID == workspaceID && g.SeriesID == seriesID && g.SeriesSequence >= next {
			next = g.SeriesSequence + 1
		}
	}
	return next, nil
}

func (f *fakeGroups) get(id string) *uwg.Group {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byID[id]
}

type fakeMembers struct {
	mu      sync.Mutex
	byGroup map[string]map[string]uwg.Member
	events  []uwg.MemberEvent
	groups  *fakeGroups
	applies int
}

func newFakeMembers(groups *fakeGroups) *fakeMembers {
	return &fakeMembers{byGroup: map[string]map[string]uwg.Member{}, groups: groups}
}

func (f *fakeMembers) All(_ context.Context, groupID string) ([]uwg.Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []uwg.Member
	for _, m := range f.byGroup[groupID] {
		out = append(out, m)
	}
	return out, nil
}

func (f *fakeMembers) Apply(ctx context.Context, g *uwg.Group, diff uwg.MemberDiff, at time.Time) error {
	f.mu.Lock()
	f.applies++
	if f.byGroup[g.ID] == nil {
		f.byGroup[g.ID] = map[string]uwg.Member{}
	}
	for _, m := range diff.Upserts {
		f.byGroup[g.ID][m.PersonKey] = m
	}
	f.events = append(f.events, diff.Events...)
	active := 0
	for _, m := range f.byGroup[g.ID] {
		if m.Status == uwg.MemberActive && !m.OurNumber {
			active++
		}
	}
	f.mu.Unlock()
	stored := f.groups.get(g.ID)
	if stored != nil {
		stored.MemberCount = active
		stamp := at
		stored.MembersSyncedAt = &stamp
	}
	return nil
}

func (f *fakeMembers) List(_ context.Context, _ string, filter uwg.MemberFilter) (*shared.PaginatedResult[*uwg.MemberRow], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var items []*uwg.MemberRow
	for _, m := range f.byGroup[filter.GroupID] {
		items = append(items, &uwg.MemberRow{Member: m})
	}
	return shared.NewPaginatedResult(items, filter.Pagination, int64(len(items))), nil
}

func (f *fakeMembers) Counts(context.Context, string, time.Time) (uwg.MemberCounts, error) {
	return uwg.MemberCounts{}, nil
}

func (f *fakeMembers) ByPersonKeys(_ context.Context, groupID string, keys []string) ([]uwg.Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []uwg.Member
	for _, k := range keys {
		if m, ok := f.byGroup[groupID][k]; ok {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeMembers) SetLead(_ context.Context, groupID, personKey, leadID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.byGroup[groupID][personKey]; ok && m.LeadID == "" {
		m.LeadID = leadID
		f.byGroup[groupID][personKey] = m
	}
	return nil
}

func (f *fakeMembers) Names(_ context.Context, groupID string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := map[string]string{}
	for key, m := range f.byGroup[groupID] {
		if m.DisplayName != "" {
			names[key] = m.DisplayName
		}
	}
	return names, nil
}

func (f *fakeMembers) GroupsOf(_ context.Context, _ string, keys []string, _ uwg.Scope) (map[string][]uwg.GroupRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string][]uwg.GroupRef{}
	for groupID, members := range f.byGroup {
		for _, k := range keys {
			if m, ok := members[k]; ok && m.Status == uwg.MemberActive {
				out[k] = append(out[k], uwg.GroupRef{GroupID: groupID})
			}
		}
	}
	return out, nil
}

func (f *fakeMembers) eventsOf(kind uwg.MemberEventKind) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

var errLeaseLost = errors.New("lease lost")

type fakeAnalyses struct {
	mu       sync.Mutex
	byID     map[string]*uwg.Analysis
	leases   map[string]time.Time
	owners   map[string]string
	extended int
}

func newFakeAnalyses() *fakeAnalyses {
	return &fakeAnalyses{byID: map[string]*uwg.Analysis{}, leases: map[string]time.Time{}, owners: map[string]string{}}
}

func (f *fakeAnalyses) Create(_ context.Context, a *uwg.Analysis) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	a.ID = "an-" + strings.ReplaceAll(strings.ToLower(a.Name), " ", "-")
	copied := *a
	f.byID[a.ID] = &copied
	return nil
}

func (f *fakeAnalyses) Save(_ context.Context, a *uwg.Analysis) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored := f.byID[a.ID]
	pending, until, last := stored.PendingMessages, stored.AnalyzedUntil, stored.LastRunAt
	copied := *a
	copied.PendingMessages, copied.AnalyzedUntil, copied.LastRunAt = pending, until, last
	f.byID[a.ID] = &copied
	return nil
}

func (f *fakeAnalyses) Delete(_ context.Context, workspaceID, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.byID[id]; !ok || a.WorkspaceID != workspaceID {
		return uwg.ErrAnalysisNotFound
	}
	delete(f.byID, id)
	return nil
}

func (f *fakeAnalyses) FindByID(_ context.Context, workspaceID, id string) (*uwg.Analysis, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.byID[id]
	if !ok || a.WorkspaceID != workspaceID {
		return nil, uwg.ErrAnalysisNotFound
	}
	copied := *a
	return &copied, nil
}

func (f *fakeAnalyses) List(_ context.Context, filter uwg.AnalysisFilter) (*shared.PaginatedResult[*uwg.AnalysisRow], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var items []*uwg.AnalysisRow
	for _, a := range f.byID {
		if a.WorkspaceID == filter.WorkspaceID && (filter.GroupID == "" || filter.GroupID == a.GroupID) {
			items = append(items, &uwg.AnalysisRow{Analysis: *a})
		}
	}
	return shared.NewPaginatedResult(items, filter.Pagination, int64(len(items))), nil
}

func (f *fakeAnalyses) AddPending(_ context.Context, groupID string, n int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.byID {
		if a.GroupID == groupID && a.Status == uwg.AnalysisActive && a.Trigger == uwg.TriggerMessageCount {
			a.PendingMessages += n
		}
	}
	return nil
}

func (f *fakeAnalyses) ClaimNext(_ context.Context, now time.Time, owner string, leaseUntil time.Time) (*uwg.Analysis, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.byID {
		if f.leases[a.ID].After(now) || a.Status != uwg.AnalysisActive {
			continue
		}
		notBefore := a.NextRunAt == nil || !a.NextRunAt.After(now)
		byCount := a.Trigger == uwg.TriggerMessageCount && a.PendingMessages >= a.MessagesPerCycle && notBefore
		bySchedule := a.Trigger == uwg.TriggerScheduled && a.NextRunAt != nil && !a.NextRunAt.After(now)
		if byCount || bySchedule {
			f.leases[a.ID] = leaseUntil
			f.owners[a.ID] = owner
			copied := *a
			return &copied, nil
		}
	}
	return nil, nil
}

func (f *fakeAnalyses) Extend(_ context.Context, id, owner string, leaseUntil time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.extended++
	if f.owners[id] != owner {
		return errLeaseLost
	}
	f.leases[id] = leaseUntil
	return nil
}

func (f *fakeAnalyses) Finish(_ context.Context, a *uwg.Analysis, _ string, consumed int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored := f.byID[a.ID]
	stored.PendingMessages -= consumed
	if stored.PendingMessages < 0 {
		stored.PendingMessages = 0
	}
	stored.AnalyzedUntil, stored.AnalyzedUntilID, stored.LastRunAt, stored.NextRunAt = a.AnalyzedUntil, a.AnalyzedUntilID, a.LastRunAt, a.NextRunAt
	stored.Failures, stored.PausedReason, stored.Status = a.Failures, a.PausedReason, a.Status
	delete(f.leases, a.ID)
	return nil
}

func (f *fakeAnalyses) Counts(context.Context, uwg.StatsScope) (int64, int64, error) {
	return int64(len(f.byID)), int64(len(f.byID)), nil
}

func (f *fakeAnalyses) GroupsWithActive(context.Context, string, []string) (map[string]bool, error) {
	return map[string]bool{}, nil
}

func (f *fakeAnalyses) get(id string) *uwg.Analysis {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byID[id]
}

type fakeRuns struct {
	mu          sync.Mutex
	runs        []uwg.Run
	failCreates int
}

func (f *fakeRuns) Create(_ context.Context, r *uwg.Run) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failCreates > 0 {
		f.failCreates--
		return errors.New("database unavailable")
	}
	r.ID = "run-" + string(rune('a'+len(f.runs)))
	f.runs = append(f.runs, *r)
	return nil
}

func (f *fakeRuns) ListByAnalysis(_ context.Context, _, analysisID string, p shared.Pagination) (*shared.PaginatedResult[*uwg.Run], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var items []*uwg.Run
	for i := range f.runs {
		if f.runs[i].AnalysisID == analysisID {
			r := f.runs[i]
			items = append(items, &r)
		}
	}
	return shared.NewPaginatedResult(items, p, int64(len(items))), nil
}

func (f *fakeRuns) FindByID(_ context.Context, _, analysisID, runID string) (*uwg.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.runs {
		if r.ID == runID && r.AnalysisID == analysisID {
			copied := r
			return &copied, nil
		}
	}
	return nil, uwg.ErrAnalysisNotFound
}

func (f *fakeRuns) Since(_ context.Context, _, analysisID string, since time.Time) ([]uwg.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []uwg.Run
	for _, r := range f.runs {
		if r.AnalysisID == analysisID && !r.CompletedAt.Before(since) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeRuns) RecentForGroups(_ context.Context, _ string, groupIDs []string, _ time.Time, _ int) (map[string][]uwg.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string][]uwg.Run{}
	for _, r := range f.runs {
		for _, id := range groupIDs {
			if r.GroupID == id {
				out[id] = append(out[id], r)
			}
		}
	}
	return out, nil
}

func (f *fakeRuns) NextCycle(_ context.Context, analysisID string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	next := 1
	for _, r := range f.runs {
		if r.AnalysisID == analysisID && r.Cycle >= next {
			next = r.Cycle + 1
		}
	}
	return next, nil
}

func (f *fakeRuns) Totals(context.Context, uwg.StatsScope) (uwg.RunTotals, error) {
	return uwg.RunTotals{Runs: int64(len(f.runs))}, nil
}
func (f *fakeRuns) SentimentTally(context.Context, uwg.StatsScope, time.Time) (uwg.SentimentTally, error) {
	return uwg.SentimentTally{Positive: 3, Neutral: 2, Negative: 5}, nil
}
func (f *fakeRuns) SentimentByGroup(context.Context, uwg.StatsScope, time.Time, int) ([]uwg.GroupSentiment, error) {
	return nil, nil
}
func (f *fakeRuns) Themes(_ context.Context, filter uwg.ThemeFilter) (*shared.PaginatedResult[*uwg.ThemeAggregate], error) {
	return shared.NewPaginatedResult([]*uwg.ThemeAggregate{}, filter.Pagination, 0), nil
}
func (f *fakeRuns) RecentInsights(context.Context, uwg.StatsScope, int) ([]uwg.Insight, error) {
	return nil, nil
}

func (f *fakeRuns) all() []uwg.Run {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uwg.Run(nil), f.runs...)
}

type fakeJoins struct {
	mu    sync.Mutex
	tasks []uwg.JoinTask
}

func (f *fakeJoins) CreateMany(_ context.Context, tasks []uwg.JoinTask) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range tasks {
		tasks[i].ID = "join-" + string(rune('a'+len(f.tasks)))
		f.tasks = append(f.tasks, tasks[i])
	}
	return nil
}

func (f *fakeJoins) ClaimNext(_ context.Context, now time.Time, owner string, leaseUntil time.Time) (*uwg.JoinTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.tasks {
		t := &f.tasks[i]
		expired := t.Status == uwg.JoinRunning && t.LeaseUntil != nil && t.LeaseUntil.Before(now)
		if (t.Status == uwg.JoinPending && !t.DueAt.After(now)) || expired {
			t.Lease(owner, leaseUntil)
			copied := *t
			return &copied, nil
		}
	}
	return nil, nil
}

func (f *fakeJoins) Save(_ context.Context, t *uwg.JoinTask) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.tasks {
		if f.tasks[i].ID == t.ID {
			if f.tasks[i].LeaseOwner != t.LeaseOwner {
				return errLeaseLost
			}
			saved := *t
			saved.LeaseOwner, saved.LeaseUntil = "", nil
			f.tasks[i] = saved
		}
	}
	return nil
}

func (f *fakeJoins) FailRemaining(_ context.Context, planID, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.tasks {
		if f.tasks[i].PlanID == planID && f.tasks[i].Status == uwg.JoinPending {
			f.tasks[i].Status, f.tasks[i].Error = uwg.JoinFailed, reason
		}
	}
	return nil
}

func (f *fakeJoins) CancelPlan(_ context.Context, _, planID string, scope uwg.Scope) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for i := range f.tasks {
		if f.tasks[i].PlanID == planID && f.tasks[i].Status == uwg.JoinPending && scope.Allows(f.tasks[i].InstanceID) {
			f.tasks[i].Status = uwg.JoinCancelled
			n++
		}
	}
	return n, nil
}

func (f *fakeJoins) Recent(context.Context, string, uwg.Scope, time.Time) ([]uwg.JoinTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uwg.JoinTask(nil), f.tasks...), nil
}

func (f *fakeJoins) snapshot() []uwg.JoinTask {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uwg.JoinTask(nil), f.tasks...)
}

type fakeMessages struct {
	conversations map[string]string
	lines         map[string][]uwg.TranscriptLine
}

func (f *fakeMessages) GroupConversation(_ context.Context, instanceID, groupJID string) (string, error) {
	if id, ok := f.conversations[instanceID+"|"+groupJID]; ok {
		return id, nil
	}
	return "", uwg.ErrNoGroupConversation
}

func (f *fakeMessages) ordered(conversationID string) []uwg.TranscriptLine {
	lines := append([]uwg.TranscriptLine(nil), f.lines[conversationID]...)
	sort.SliceStable(lines, func(i, j int) bool {
		if !lines[i].At.Equal(lines[j].At) {
			return lines[i].At.Before(lines[j].At)
		}
		return lines[i].ID < lines[j].ID
	})
	return lines
}

func (f *fakeMessages) TextAfter(_ context.Context, conversationID string, after uwg.Cursor, limit int) ([]uwg.TranscriptLine, error) {
	var out []uwg.TranscriptLine
	for _, l := range f.ordered(conversationID) {
		beyond := l.At.After(after.At) || (l.At.Equal(after.At) && l.ID > after.ID)
		if beyond && len(out) < limit {
			out = append(out, l)
		}
	}
	return out, nil
}

func (f *fakeMessages) Between(_ context.Context, conversationID string, from, to time.Time, offset, limit int) ([]uwg.TranscriptLine, error) {
	var out []uwg.TranscriptLine
	for _, l := range f.ordered(conversationID) {
		if !l.At.Before(from) && !l.At.After(to) {
			out = append(out, l)
		}
	}
	return out, nil
}

type fakeInstances struct {
	uw.InstanceRepository
	instances []*uw.Instance
}

func (f *fakeInstances) ListByWorkspace(_ context.Context, in uw.ListInstancesInput) (*shared.PaginatedResult[*uw.Instance], error) {
	var items []*uw.Instance
	for _, i := range f.instances {
		if i.WorkspaceID == in.WorkspaceID {
			items = append(items, i)
		}
	}
	return shared.NewPaginatedResult(items, in.Options.Pagination, int64(len(items))), nil
}

func (f *fakeInstances) FindByID(_ context.Context, id string) (*uw.Instance, error) {
	for _, i := range f.instances {
		if i.ID == id {
			return i, nil
		}
	}
	return nil, uw.ErrInstanceNotFound
}

type fakeServers struct{ uw.ServerRepository }

func (fakeServers) FindByID(_ context.Context, id string) (*uw.Server, error) {
	return &uw.Server{ID: id, BaseURL: "https://host.test"}, nil
}

type fakeGroupAPI struct {
	uw.GroupAPI
	mu         sync.Mutex
	listed     map[string][]*uw.Group
	listErr    map[string]error
	created    []string
	made       int
	seats      *fakeSeats
	promoted   [][]string
	joinErr    error
	joined     []string
	info       *uw.Group
	announced  []bool
	describing []string
	duringJoin func()
	promoteErr error
}

func (f *fakeGroupAPI) ListGroups(_ context.Context, ref uw.InstanceRef, _ bool) ([]*uw.Group, error) {
	if err := f.listErr[ref.Token]; err != nil {
		return nil, err
	}
	return f.listed[ref.Token], nil
}

func (f *fakeGroupAPI) CreateGroup(_ context.Context, ref uw.InstanceRef, name string, participants []string) (*uw.Group, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, name+"|"+strings.Join(participants, ","))
	f.made++
	jid := fmt.Sprintf("1203637770000000%02d@g.us", f.made)
	f.seats.put(&uw.Group{WorkspaceID: ws, InstanceID: ref.Token, JID: jid, Subject: name, WeAreAdmin: true, WeCanSend: true})
	return &uw.Group{JID: jid, Subject: name, WeAreAdmin: true}, nil
}

func (f *fakeGroupAPI) JoinGroup(_ context.Context, ref uw.InstanceRef, link string) (*uw.Group, error) {
	if hook := f.duringJoin; hook != nil {
		f.duringJoin = nil
		hook()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.joined = append(f.joined, ref.Token)
	if f.joinErr != nil {
		return nil, f.joinErr
	}
	f.seats.put(&uw.Group{WorkspaceID: ws, InstanceID: ref.Token, JID: "120363888000000001@g.us", Subject: "Feira de Trocas", WeCanSend: true})
	return &uw.Group{JID: "120363888000000001@g.us", Subject: "Feira de Trocas"}, nil
}

func (f *fakeGroupAPI) InviteInfo(context.Context, uw.InstanceRef, string) (*uw.Group, error) {
	return f.info, nil
}

func (f *fakeGroupAPI) UpdateGroupDescription(_ context.Context, _ uw.InstanceRef, _, description string) error {
	f.describing = append(f.describing, description)
	return nil
}

func (f *fakeGroupAPI) UpdateAnnounce(_ context.Context, _ uw.InstanceRef, _ string, adminsOnly bool) error {
	f.announced = append(f.announced, adminsOnly)
	return nil
}

func (f *fakeGroupAPI) UpdateParticipants(_ context.Context, _ uw.InstanceRef, in uw.UpdateParticipantsInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.promoted = append(f.promoted, in.Participants)
	return f.promoteErr
}

type fakeSeatSync struct {
	seats            *fakeSeats
	rosters          map[string][]uw.GroupParticipant
	refreshs         []string
	renamed          []string
	left             []string
	scopes           []uw.DepartmentScope
	duringGet        func()
	participantCalls []string
}

func (f *fakeSeatSync) Get(_ context.Context, workspaceID, instanceID, groupJID string, refresh bool, scope uw.DepartmentScope) (*uw.Group, error) {
	if hook := f.duringGet; hook != nil {
		f.duringGet = nil
		hook()
	}
	f.scopes = append(f.scopes, scope)
	seat, err := f.seats.FindByJID(context.Background(), instanceID, groupJID)
	if err != nil {
		seat = &uw.Group{WorkspaceID: workspaceID, InstanceID: instanceID, JID: groupJID, Subject: "Novo"}
		f.seats.put(seat)
	}
	if refresh {
		f.refreshs = append(f.refreshs, instanceID+"|"+groupJID)
		seat.Participants = f.rosters[groupJID]
	}
	return seat, nil
}

func (f *fakeSeatSync) InviteLink(context.Context, string, string, string, uw.DepartmentScope) (string, error) {
	return "https://chat.whatsapp.com/abcdefghij", nil
}

func (f *fakeSeatSync) UpdateName(_ context.Context, in uwuc.UpdateNameInput) (*uw.Group, error) {
	f.renamed = append(f.renamed, in.InstanceID+"|"+in.Name)
	return f.seats.FindByJID(context.Background(), in.InstanceID, in.GroupJID)
}

func (f *fakeSeatSync) UpdateDescription(_ context.Context, in uwuc.UpdateDescriptionInput) (*uw.Group, error) {
	return f.seats.FindByJID(context.Background(), in.InstanceID, in.GroupJID)
}

func (f *fakeSeatSync) UpdateSettings(_ context.Context, in uwuc.UpdateSettingsInput) (*uw.Group, error) {
	return f.seats.FindByJID(context.Background(), in.InstanceID, in.GroupJID)
}

func (f *fakeSeatSync) UpdateImage(_ context.Context, _, instanceID, groupJID, _ string, _ uw.DepartmentScope) (*uw.Group, error) {
	return f.seats.FindByJID(context.Background(), instanceID, groupJID)
}

func (f *fakeSeatSync) UpdateParticipants(_ context.Context, in uwuc.UpdateParticipantsInput) (*uw.Group, error) {
	f.participantCalls = append(f.participantCalls, in.InstanceID+"|"+string(in.Action)+"|"+strings.Join(in.Participants, ","))
	return f.seats.FindByJID(context.Background(), in.InstanceID, in.GroupJID)
}

func (f *fakeSeatSync) Leave(_ context.Context, _, instanceID, groupJID string, _ uw.DepartmentScope) error {
	f.left = append(f.left, instanceID)
	f.seats.remove(instanceID, groupJID)
	return nil
}

type fakeLeads struct{ linked []string }

func (f *fakeLeads) EnsureLeadForPhone(_ context.Context, _, phone, _ string) (string, error) {
	f.linked = append(f.linked, phone)
	return "lead-" + phone, nil
}

type fakeAI struct {
	mu      sync.Mutex
	inputs  []ai.GenerateInput
	content string
	err     error
}

func (f *fakeAI) Generate(_ context.Context, in ai.GenerateInput) (*ai.GenerateOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputs = append(f.inputs, in)
	if f.err != nil {
		return nil, f.err
	}
	return &ai.GenerateOutput{Message: ai.Message{Content: f.content}, Usage: ai.Usage{PromptTokens: 1200, CompletionTokens: 300}}, nil
}

func (f *fakeAI) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.inputs)
}

type fakeBalance struct {
	micros     int64
	paidChecks int
	limited    bool
}

func (f *fakeBalance) HasSufficientBalance(string, int64) (bool, error) { return f.micros > 0, nil }
func (f *fakeBalance) GetBalance(string) (int64, error) {
	if f.limited {
		if f.paidChecks == 0 {
			return 0, nil
		}
		f.paidChecks--
	}
	return f.micros, nil
}
func (f *fakeBalance) Invalidate(string)          {}
func (f *fakeBalance) InvalidateDebounced(string) {}

type fakeLocations struct{}

func (fakeLocations) Location(context.Context, string, string) (*time.Location, error) {
	return time.LoadLocation("America/Sao_Paulo")
}

type fakeCampaigns struct{}

func (fakeCampaigns) ForGroup(context.Context, string, string, uwg.Scope, int) ([]uwg.GroupCampaign, error) {
	return nil, nil
}
func (fakeCampaigns) RunningForGroups(context.Context, string, []string, uwg.Scope) (map[string]int, error) {
	return map[string]int{}, nil
}

type fakeStats struct{}

func (fakeStats) GroupTotals(context.Context, uwg.StatsScope) (uwg.GroupTotals, error) {
	return uwg.GroupTotals{Groups: 3, Followed: 2, Open: 1, Closed: 1}, nil
}
func (fakeStats) PeopleTotals(context.Context, uwg.StatsScope) (uwg.PeopleTotals, error) {
	return uwg.PeopleTotals{Memberships: 9, Unique: 7}, nil
}
func (fakeStats) DailyMessages(context.Context, uwg.StatsScope, uwg.Window, *time.Location) (map[string]uwg.DailyCount, error) {
	return map[string]uwg.DailyCount{"2026-10-16": {Messages: 40, ActiveGroups: 2}}, nil
}
func (fakeStats) TopGroupsByMessages(context.Context, uwg.StatsScope, uwg.Window, int) ([]uwg.GroupCount, error) {
	return nil, nil
}
func (fakeStats) ResponseTimes(context.Context, uwg.StatsScope, uwg.Window, *time.Location) (map[string]uwg.ResponseTimePoint, error) {
	return map[string]uwg.ResponseTimePoint{"2026-10-16": {AvgMinutes: 3, Samples: 8}}, nil
}
func (fakeStats) DailyGrowth(context.Context, uwg.StatsScope, string, uwg.Window, *time.Location) (map[string]uwg.DailyCount, error) {
	return map[string]uwg.DailyCount{"2026-10-10": {Joins: 4, Leaves: 1}}, nil
}
func (fakeStats) TopGrowing(context.Context, uwg.StatsScope, uwg.Window, int) ([]uwg.GroupGrowth, error) {
	return nil, nil
}
func (fakeStats) GroupActivity(context.Context, string, []string, time.Time) (map[string]uwg.GroupActivity, error) {
	return map[string]uwg.GroupActivity{}, nil
}
func (fakeStats) CrossGroupPeople(_ context.Context, f uwg.PeopleFilter) (*shared.PaginatedResult[*uwg.Person], error) {
	return shared.NewPaginatedResult([]*uwg.Person{}, f.Pagination, 0), nil
}
func (fakeStats) Membership(context.Context, uwg.MembershipFilter) ([]uwg.MembershipStat, error) {
	return nil, nil
}

type world struct {
	svc       *Service
	seats     *fakeSeats
	groups    *fakeGroups
	members   *fakeMembers
	analyses  *fakeAnalyses
	runs      *fakeRuns
	joins     *fakeJoins
	messages  *fakeMessages
	api       *fakeGroupAPI
	seatSync  *fakeSeatSync
	leads     *fakeLeads
	ai        *fakeAI
	balance   *fakeBalance
	instances *fakeInstances
}

const ws = "ws-1"

func connected(id, phone, department string) *uw.Instance {
	dept := department
	return &uw.Instance{ID: id, WorkspaceID: ws, ServerID: "srv", InstanceToken: id, PhoneNumber: phone, DepartmentID: &dept, Status: uw.StatusConnected}
}

func newWorld() *world {
	seats := newFakeSeats()
	groups := newFakeGroups(seats)
	w := &world{
		seats: seats, groups: groups, members: newFakeMembers(groups), analyses: newFakeAnalyses(),
		runs: &fakeRuns{}, joins: &fakeJoins{},
		messages: &fakeMessages{conversations: map[string]string{}, lines: map[string][]uwg.TranscriptLine{}},
		api:      &fakeGroupAPI{listed: map[string][]*uw.Group{}, listErr: map[string]error{}},
		leads:    &fakeLeads{}, ai: &fakeAI{}, balance: &fakeBalance{micros: 50_000_000},
		instances: &fakeInstances{instances: []*uw.Instance{
			connected("n1", "5511970001201", "sales"),
			connected("n2", "5511970001202", "support"),
		}},
	}
	w.api.seats = seats
	w.seatSync = &fakeSeatSync{seats: seats, rosters: map[string][]uw.GroupParticipant{}}
	w.svc = New(Deps{
		Groups: groups, Members: w.members, Stats: fakeStats{}, Analyses: w.analyses, Runs: w.runs,
		Joins: w.joins, Messages: w.messages, Campaigns: fakeCampaigns{},
		Instances: w.instances, Servers: fakeServers{}, Seats: seats, GroupAPI: w.api,
		SeatSync: w.seatSync, Leads: w.leads, Locations: fakeLocations{},
		AI: w.ai, Model: "openai/gpt-4o-mini", Balance: w.balance,
		Owner: "test", Clock: func() time.Time { return testNow },
	})
	return w
}

func everyone() Actor { return Actor{WorkspaceID: ws, UserID: "u1"} }

func salesOnly() Actor {
	return Actor{WorkspaceID: ws, UserID: "u2", Departments: uw.DepartmentScope{Restrict: true, DepartmentIDs: []string{"sales"}}}
}

func (w *world) groupWithSeats(t interface{ Fatal(...any) }, jid, subject string, instanceIDs ...string) *uwg.Group {
	for _, id := range instanceIDs {
		w.seats.put(&uw.Group{WorkspaceID: ws, InstanceID: id, JID: jid, Subject: subject, WeCanSend: true})
	}
	g := &uwg.Group{WorkspaceID: ws, JID: jid, Subject: subject}
	if err := w.groups.UpsertSeen(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	if len(instanceIDs) > 0 {
		_ = w.groups.SetPrimary(context.Background(), ws, g.ID, instanceIDs[0])
	}
	return w.groups.get(g.ID)
}

func participants(phones ...string) []uw.GroupParticipant {
	out := make([]uw.GroupParticipant, 0, len(phones))
	for _, p := range phones {
		out = append(out, uw.GroupParticipant{JID: p + "@s.whatsapp.net", PhoneNumber: p, DisplayName: "P" + p[len(p)-2:], Role: uw.GroupRoleMember})
	}
	return out
}
