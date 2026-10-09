package leadsend_usecase

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"vozko/domain/address"
	"vozko/domain/balance"
	"vozko/domain/cache"
	"vozko/domain/campaign"
	"vozko/domain/conversation"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wd "vozko/domain/workspace/workspace_department"
)

const workspaceID = "ws-1"

type fakePermissions struct{ denied map[string]bool }

func (p fakePermissions) HasWorkspacePermission(_, _, resource, action string, _ bool) bool {
	return !p.denied[resource+":"+action]
}

type fakeSnapshots struct {
	ids     []string
	dropped []string
	pages   int
}

func (f *fakeSnapshots) Snapshot(_ context.Context, _, _, after string, limit int) ([]string, error) {
	f.pages++
	sorted := append([]string(nil), f.ids...)
	sort.Strings(sorted)
	var out []string
	for _, id := range sorted {
		if id > after {
			out = append(out, id)
		}
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (f *fakeSnapshots) DropSnapshot(_ context.Context, _, snapshotID string) error {
	f.dropped = append(f.dropped, snapshotID)
	return nil
}

type fakeLeads struct {
	byID  map[string]*lead.Lead
	calls int
}

func (f *fakeLeads) FindByIDs(ws string, ids []string) ([]*lead.Lead, error) {
	f.calls++
	var out []*lead.Lead
	for _, id := range ids {
		if l, ok := f.byID[id]; ok && l.WorkspaceID == ws {
			out = append(out, l)
		}
	}
	return out, nil
}

type fakeContacts struct{ calls int }

func (f *fakeContacts) AttachContactDetails(_ context.Context, _ string, leads []*lead.Lead) error {
	f.calls++
	for _, l := range leads {
		if l.Name == "Maria" {
			l.Addresses = []lead.Address{{Primary: true, Postal: address.Postal{District: "Jardim Silveira", City: "Barueri"}}}
		}
	}
	return nil
}

type fakeNames struct{}

func (fakeNames) Names(ids ...string) map[string]string {
	out := map[string]string{}
	for _, id := range ids {
		out[id] = "Ana"
	}
	return out
}

type fakeDefinitions struct{}

func (fakeDefinitions) ListByObject(string, customfield.ObjectType) ([]*customfield.Definition, error) {
	return []*customfield.Definition{{Key: "classificacao", ObjectType: customfield.ObjectLead, Sensitive: true}}, nil
}

type createdPart struct {
	official   *wc.Campaign
	unofficial *uwc.Campaign
	deleted    bool
	started    bool
}

type world struct {
	mu           sync.Mutex
	parts        map[string]*createdPart
	order        []string
	running      map[string]bool
	lateRunning  map[string]bool
	skipped      map[string]int
	cooldownDays map[string]int
	createErr    error
	startedBy    map[string]bool
	deleted      []string
}

func newWorld() *world {
	return &world{parts: map[string]*createdPart{}, running: map[string]bool{}, lateRunning: map[string]bool{}, skipped: map[string]int{}, cooldownDays: map[string]int{}, startedBy: map[string]bool{}}
}

func (w *world) SkipInRunning(_ context.Context, _ campaign.Channel, _ string, id string) (int64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	p := w.parts[id]
	marked := int64(0)
	if p.official != nil {
		for i, in := range p.official.PhoneInputs {
			if in.Skip == "" && w.lateRunning[in.LeadID] {
				p.official.PhoneInputs[i].Skip = campaign.SkipAlreadyInRunningCampaign
				marked++
			}
		}
	} else {
		for i, in := range p.unofficial.Targets {
			if in.Skip == "" && w.lateRunning[in.LeadID] {
				p.unofficial.Targets[i].Skip = campaign.SkipAlreadyInRunningCampaign
				marked++
			}
		}
	}
	return marked, nil
}

func (w *world) DeleteStopped(_ context.Context, _ campaign.Channel, _ string, id string) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	p, ok := w.parts[id]
	if !ok || p.deleted || p.started || w.startedBy[id] {
		return false, nil
	}
	p.deleted = true
	w.deleted = append(w.deleted, id)
	return true, nil
}

type openGate struct{ busy bool }

func (g openGate) Acquire(context.Context) (func(), error) {
	if g.busy {
		return nil, cache.ErrGateBusy
	}
	return func() {}, nil
}

type forgetful struct{ forgotten []string }

func (f *forgetful) Forget(id string) { f.forgotten = append(f.forgotten, id) }

func (w *world) byKey(key string) (string, bool) {
	for id, p := range w.parts {
		if p.deleted {
			continue
		}
		if (p.official != nil && p.official.IdempotencyKey == key) || (p.unofficial != nil && p.unofficial.IdempotencyKey == key) {
			return id, true
		}
	}
	return "", false
}

func (w *world) InRunningCampaigns(_ context.Context, _ string, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, id := range ids {
		if w.running[id] {
			out[id] = true
		}
	}
	return out, nil
}

func skipsOf(p *createdPart) []campaign.SkipReason {
	var skips []campaign.SkipReason
	for _, in := range inputsOf(p) {
		skips = append(skips, in.skip)
	}
	return skips
}

type fakeInput struct {
	skip    campaign.SkipReason
	missing []campaign.MissingVariable
}

func inputsOf(p *createdPart) []fakeInput {
	var out []fakeInput
	if p.official != nil {
		for _, in := range p.official.PhoneInputs {
			out = append(out, fakeInput{skip: in.Skip, missing: in.Missing})
		}
	} else {
		for _, in := range p.unofficial.Targets {
			out = append(out, fakeInput{skip: in.Skip, missing: in.Missing})
		}
	}
	return out
}

func (w *world) Tally(_ context.Context, _ campaign.Channel, _ string, ids []string, _ string, _ time.Time) ([]campaign.PartTally, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []campaign.PartTally
	for _, id := range ids {
		p, ok := w.parts[id]
		if !ok || p.deleted {
			continue
		}
		t := campaign.PartTally{CampaignID: id, Missing: map[campaign.MissingVariable]int{}, Tally: campaign.Tally{Skipped: map[campaign.SkipReason]int{}, Counted: map[campaign.CountedReason]int{campaign.CountedNoConsentRecorded: 1}}}
		for _, in := range inputsOf(p) {
			t.Entries++
			if in.skip == "" {
				t.Eligible++
				continue
			}
			t.Skipped[in.skip]++
			if in.skip == campaign.SkipMissingVariable {
				for _, missing := range in.missing {
					t.Missing[missing]++
				}
			}
		}
		t.CooldownDays = w.cooldownDays[id]
		over := w.skipped[id]
		t.Eligible -= over
		if over > 0 {
			t.Skipped[campaign.SkipOverCap] += over
		}
		if p.started {
			t.Eligible = 0
		}
		out = append(out, t)
	}
	return out, nil
}

func (w *world) SkipBeyond(_ context.Context, _ campaign.Channel, _ string, id string, keep int) (int64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	pending := 0
	for _, skip := range skipsOf(w.parts[id]) {
		if skip == "" {
			pending++
		}
	}
	w.skipped[id] = pending - keep
	return int64(pending - keep), nil
}

func (w *world) KeyedParts(_ context.Context, _ campaign.Channel, _ string, base string) ([]campaign.KeyedPart, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []campaign.KeyedPart
	for id, p := range w.parts {
		key := ""
		if p.official != nil {
			key = p.official.IdempotencyKey
		} else {
			key = p.unofficial.IdempotencyKey
		}
		if !p.deleted && strings.HasPrefix(key, base+":") {
			out = append(out, campaign.KeyedPart{ID: id, Key: key})
		}
	}
	return out, nil
}

type officialCreate struct {
	w        *world
	contexts []context.Context
}

func (c *officialCreate) Execute(ctx context.Context, in *wc.Campaign) (*wc.Campaign, error) {
	c.w.mu.Lock()
	defer c.w.mu.Unlock()
	c.contexts = append(c.contexts, ctx)
	if c.w.createErr != nil {
		return nil, c.w.createErr
	}
	if id, ok := c.w.byKey(in.IdempotencyKey); ok {
		return c.w.parts[id].official, nil
	}
	in.ID = "c-" + in.IdempotencyKey
	c.w.parts[in.ID] = &createdPart{official: in}
	c.w.order = append(c.w.order, in.ID)
	return in, nil
}

type officialAccess struct{ w *world }

func (a officialAccess) Owned(ws string, departments *wd.DepartmentFilter, id string) (*wc.Campaign, error) {
	a.w.mu.Lock()
	defer a.w.mu.Unlock()
	p, ok := a.w.parts[id]
	if !ok || p.deleted || p.official == nil || p.official.WorkspaceID != ws || !departments.Allows(p.official.DepartmentID) {
		return nil, wc.ErrCampaignNotFound
	}
	copy := *p.official
	if p.started {
		copy.Status = campaign.StatusRunning
	}
	return &copy, nil
}

type officialStart struct {
	w          *world
	started    []string
	lostTheRun bool
}

func (s *officialStart) StartReviewed(_ string, _ *wd.DepartmentFilter, id string) (*wc.Campaign, error) {
	s.w.mu.Lock()
	defer s.w.mu.Unlock()
	if s.lostTheRun {
		return nil, campaign.ErrAlreadyRunning
	}
	s.started = append(s.started, id)
	s.w.parts[id].started = true
	return s.w.parts[id].official, nil
}

type unofficialCreate struct{ w *world }

func (c *unofficialCreate) Execute(_ context.Context, in *uwc.Campaign, _ uw.DepartmentScope) (*uwc.Campaign, error) {
	c.w.mu.Lock()
	defer c.w.mu.Unlock()
	if id, ok := c.w.byKey(in.IdempotencyKey); ok {
		return c.w.parts[id].unofficial, nil
	}
	in.ID = "u-" + in.IdempotencyKey
	c.w.parts[in.ID] = &createdPart{unofficial: in}
	return in, nil
}

type unofficialAccess struct{ w *world }

func (a unofficialAccess) Owned(_ context.Context, ws string, _ uw.DepartmentScope, id string) (*uwc.Campaign, error) {
	a.w.mu.Lock()
	defer a.w.mu.Unlock()
	p, ok := a.w.parts[id]
	if !ok || p.deleted || p.unofficial == nil || p.unofficial.WorkspaceID != ws {
		return nil, uwc.ErrCampaignNotFound
	}
	copy := *p.unofficial
	if p.started {
		copy.Status = campaign.StatusRunning
	}
	return &copy, nil
}

type unofficialAct struct {
	w       *world
	started []string
}

func (a *unofficialAct) StartReviewed(_ context.Context, _ string, _ uw.DepartmentScope, id string) (*uwc.Campaign, error) {
	a.w.mu.Lock()
	defer a.w.mu.Unlock()
	a.started = append(a.started, id)
	a.w.parts[id].started = true
	return a.w.parts[id].unofficial, nil
}

type fakeInstances struct{ err error }

func (f fakeInstances) Usable(context.Context, string, uw.DepartmentScope, string) (*uw.Instance, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &uw.Instance{ID: "inst-1", WorkspaceID: workspaceID, Status: uw.StatusConnected, DailySendCap: 250}, nil
}

type fakeScopes struct{}

func (fakeScopes) GetDepartmentScope(string, string, bool) (conversation.DepartmentAccessScope, bool) {
	return conversation.DepartmentAccessScope{}, true
}

type fakeDepartments struct{ list []wd.Department }

func (f fakeDepartments) ListDepartments(string) ([]wd.Department, error) { return f.list, nil }

type fakeResolver struct{}

func (fakeResolver) Resolve(ctx context.Context, _ string) (string, error) {
	scope, ok := wd.GetCreationScope(ctx)
	if !ok {
		return "", nil
	}
	return scope.RequestedDepartmentID, nil
}

type fakeTemplates struct{ byID map[string]*template.Template }

func (f fakeTemplates) Get(_, id string) (*template.Template, error) {
	if t, ok := f.byID[id]; ok {
		return t, nil
	}
	return nil, template.ErrTemplateAccessDenied
}

type fakeCosts struct{ unit int64 }

func (f fakeCosts) GetTemplateCostMicros(string, string) (int64, error) { return f.unit, nil }

type fakeBalances struct{ micros int64 }

func (f fakeBalances) GetBalance(string) (int64, error) { return f.micros, nil }

type fakeCaps struct{ usage *balance.SendCapUsage }

func (f fakeCaps) MonthlySendCapUsage(string, time.Time) (*balance.SendCapUsage, error) {
	return f.usage, nil
}

type harness struct {
	svc         *Service
	world       *world
	snapshots   *fakeSnapshots
	leads       *fakeLeads
	contacts    *fakeContacts
	create      *officialCreate
	start       *officialStart
	act         *unofficialAct
	cached      *forgetful
	metrics     *fakeMetrics
	deps        Deps
	departments []wd.Department
}

type fakeMetrics struct {
	runs      map[string]int
	durations map[string]int
	skips     map[string]int
}

func newFakeMetrics() *fakeMetrics {
	return &fakeMetrics{runs: map[string]int{}, durations: map[string]int{}, skips: map[string]int{}}
}

func (m *fakeMetrics) AddLeadActionRuns(action, status, failure string, n int) {
	if n > 0 {
		m.runs[action+"|"+status+"|"+failure] += n
	}
}

func (m *fakeMetrics) ObserveLeadActionDuration(action, status string, _ time.Duration) {
	m.durations[action+"|"+status]++
}

func (m *fakeMetrics) AddLeadActionSkips(action, reason string, n int) {
	if n > 0 {
		m.skips[action+"|"+reason] += n
	}
}

func approvedTemplate(body string) *template.Template {
	return &template.Template{ID: "tpl-1", Name: "matriculas", Status: template.TemplateStatusApproved, Category: template.TemplateCategoryMarketing,
		WABAId: "waba-1", Components: []template.TemplateComponent{{Type: "BODY", Text: body}}}
}

func newHarness(change func(*Deps)) *harness {
	w := newWorld()
	h := &harness{world: w, snapshots: &fakeSnapshots{}, contacts: &fakeContacts{}, create: &officialCreate{w: w},
		start: &officialStart{w: w}, act: &unofficialAct{w: w}, cached: &forgetful{}, metrics: newFakeMetrics(),
		leads: &fakeLeads{byID: map[string]*lead.Lead{}}}
	header := approvedTemplate("Olá {{1}}")
	header.ID = "tpl-header"
	header.Components = append(header.Components, template.TemplateComponent{Type: "HEADER", Format: "TEXT", Text: "Oi {{1}}"})
	h.deps = Deps{
		Permissions: fakePermissions{}, Snapshots: h.snapshots, Leads: h.leads, Contacts: h.contacts, Names: fakeNames{},
		Definitions: fakeDefinitions{}, Store: w, Gate: openGate{}, Departments: fakeDepartments{}, CreationDepartments: fakeResolver{},
		Templates: fakeTemplates{byID: map[string]*template.Template{"tpl-1": approvedTemplate("Olá {{1}}, do bairro {{2}}"), "tpl-literal": approvedTemplate("Matrículas abertas {{1}}"), "tpl-header": header}},
		Costs:     fakeCosts{unit: 62500}, Balances: fakeBalances{micros: 100_000_000}, Caps: fakeCaps{},
		Official:   Official{Create: h.create, Access: officialAccess{w: w}, Start: h.start, Cache: h.cached},
		Unofficial: &Unofficial{Create: &unofficialCreate{w: w}, Access: unofficialAccess{w: w}, Start: h.act, Instances: fakeInstances{}, Scopes: fakeScopes{}},
		Metrics:    h.metrics,
		Now:        func() time.Time { return time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC) },
	}
	if change != nil {
		change(&h.deps)
	}
	svc, err := NewService(h.deps)
	if err != nil {
		panic(err)
	}
	h.svc = svc
	return h
}

func (h *harness) seed(n int) []string {
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		id := leadID(i)
		ids = append(ids, id)
		h.leads.byID[id] = &lead.Lead{ID: id, WorkspaceID: workspaceID, Number: "55119999" + pad(i), Name: "Maria"}
	}
	h.snapshots.ids = ids
	return ids
}

func leadID(i int) string {
	return "00000000-0000-4000-8000-" + pad12(i)
}

func pad(i int) string {
	s := itoa(i)
	for len(s) < 5 {
		s = "0" + s
	}
	return s
}

func pad12(i int) string {
	s := itoa(i)
	for len(s) < 12 {
		s = "0" + s
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for ; n > 0; n /= 10 {
		s = string(rune('0'+n%10)) + s
	}
	return s
}

func requester() Actor {
	return Actor{UserID: "user-1", WorkspaceID: workspaceID}
}

func scoped() context.Context {
	return wd.WithCreationScope(context.Background(), wd.CreationScope{UserID: "user-1"})
}

func allDepartments() *wd.DepartmentFilter {
	return &wd.DepartmentFilter{IsOwnerOrAdmin: true}
}
