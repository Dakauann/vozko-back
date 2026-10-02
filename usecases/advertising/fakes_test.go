package advertising

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	ads "vozko/domain/advertising"
	"vozko/domain/media"
)

var testNow = time.Date(2026, 10, 1, 15, 0, 0, 0, time.UTC)

type fakeGrants struct{ byID map[string]*ads.Grant }

func (f *fakeGrants) Upsert(_ context.Context, g *ads.Grant) error {
	if g.ID == "" {
		g.ID = fmt.Sprintf("grant-%d", len(f.byID)+1)
	}
	f.byID[g.ID] = g
	return nil
}
func (f *fakeGrants) FindByID(_ context.Context, id string) (*ads.Grant, error) {
	g, ok := f.byID[id]
	if !ok {
		return nil, ads.ErrGrantNotFound
	}
	return g, nil
}
func (f *fakeGrants) MarkChecked(context.Context, string, []string, map[string][]string, time.Time) error {
	return nil
}
func (f *fakeGrants) Revoke(context.Context, string, time.Time) error { return nil }

type fakeAccounts struct {
	byID        map[string]*ads.AdAccount
	connections map[string]ads.Connection
}

func (f *fakeAccounts) Upsert(_ context.Context, a *ads.AdAccount) error {
	for _, existing := range f.byID {
		if existing.MetaAccountID == a.MetaAccountID {
			if existing.WorkspaceID != a.WorkspaceID {
				return ads.ErrAccountLinkedElsewhere
			}
			a.ID = existing.ID
		}
	}
	if a.ID == "" {
		a.ID = "acc-" + a.MetaAccountID
	}
	clone := *a
	f.byID[a.ID] = &clone
	return nil
}
func (f *fakeAccounts) FindByID(_ context.Context, ws, id string) (*ads.AdAccount, error) {
	a, ok := f.byID[id]
	if !ok || a.WorkspaceID != ws {
		return nil, ads.ErrAccountNotFound
	}
	clone := *a
	return &clone, nil
}
func (f *fakeAccounts) ListByWorkspace(context.Context, string) ([]*ads.AdAccount, error) {
	return nil, nil
}
func (f *fakeAccounts) ListConnected(context.Context, int, int) ([]*ads.AdAccount, error) {
	return nil, nil
}
func (f *fakeAccounts) SetConnection(_ context.Context, id string, c ads.Connection) error {
	f.connections[id] = c
	if a, ok := f.byID[id]; ok {
		a.Connection = c
	}
	return nil
}
func (f *fakeAccounts) MarkSynced(context.Context, string, time.Time) error { return nil }
func (f *fakeAccounts) FindByMetaAccountID(_ context.Context, metaID string) (*ads.AdAccount, error) {
	for _, a := range f.byID {
		if a.MetaAccountID == metaID && a.Connection != ads.ConnectionDisconnected {
			clone := *a
			return &clone, nil
		}
	}
	return nil, ads.ErrAccountNotFound
}

type fakeObjects struct{ byID map[string]*ads.Object }

func (f *fakeObjects) ReplaceLevel(_ context.Context, _ string, _ ads.Level, objects []*ads.Object, _ time.Time) error {
	for _, o := range objects {
		f.byID[o.MetaID] = o
	}
	return nil
}
func (f *fakeObjects) Upsert(_ context.Context, o *ads.Object) error {
	f.byID[o.MetaID] = o
	return nil
}
func (f *fakeObjects) Find(_ context.Context, ws, id string) (*ads.Object, error) {
	o, ok := f.byID[id]
	if !ok || o.WorkspaceID != ws {
		return nil, ads.ErrObjectNotFound
	}
	return o, nil
}
func (f *fakeObjects) List(_ context.Context, q ads.ObjectQuery) ([]*ads.Object, error) {
	var out []*ads.Object
	for _, o := range f.byID {
		if o.WorkspaceID != q.WorkspaceID || o.AdAccountID != q.AdAccountID || o.Level != q.Level {
			continue
		}
		if len(q.CampaignIDs) > 0 && !slices.Contains(q.CampaignIDs, o.CampaignMetaID) {
			continue
		}
		if len(q.AdSetIDs) > 0 && !slices.Contains(q.AdSetIDs, o.AdSetMetaID) {
			continue
		}
		if len(q.MetaIDs) > 0 && !slices.Contains(q.MetaIDs, o.MetaID) {
			continue
		}
		out = append(out, o)
	}
	slices.SortFunc(out, func(a, b *ads.Object) int {
		if a.MetaID < b.MetaID {
			return -1
		}
		return 1
	})
	return out, nil
}

type fakeInsights struct{ rows []ads.DailyInsight }

func (f *fakeInsights) ReplaceDays(_ context.Context, _ string, _ ads.DateRange, rows []ads.DailyInsight) error {
	f.rows = rows
	return nil
}
func (f *fakeInsights) Rows(_ context.Context, _ string, r ads.DateRange) ([]ads.DailyInsight, error) {
	var out []ads.DailyInsight
	for _, row := range f.rows {
		if r.Contains(row.Day) {
			out = append(out, row)
		}
	}
	return out, nil
}
func (f *fakeInsights) AdDay(_ context.Context, adID string, day time.Time) (*ads.DailyInsight, error) {
	for _, row := range f.rows {
		if row.AdMetaID == adID && row.Day.Equal(day) {
			return &row, nil
		}
	}
	return nil, ads.ErrObjectNotFound
}

type fakeAttribution struct {
	rows          []ads.Attribution
	conversations int64
}

func (f *fakeAttribution) ByAd(_ context.Context, _ string, ids []string, _, _ time.Time) ([]ads.Attribution, error) {
	var out []ads.Attribution
	for _, a := range f.rows {
		if slices.Contains(ids, a.AdMetaID) {
			out = append(out, a)
		}
	}
	return out, nil
}
func (f *fakeAttribution) Conversations(context.Context, string, string, time.Time, time.Time) (int64, error) {
	return f.conversations, nil
}

type fakeJobs struct {
	mu   sync.Mutex
	byID map[string]*ads.PublishJob
	log  []ads.PublishJob
}

func (f *fakeJobs) Create(_ context.Context, j *ads.PublishJob) error {
	j.ID = fmt.Sprintf("job-%d", len(f.byID)+1)
	j.CreatedAt = testNow
	f.byID[j.ID] = j
	return nil
}
func (f *fakeJobs) Find(_ context.Context, ws, id string) (*ads.PublishJob, error) {
	j, ok := f.byID[id]
	if !ok || j.WorkspaceID != ws {
		return nil, ads.ErrJobNotFound
	}
	return j, nil
}
func (f *fakeJobs) Save(_ context.Context, j *ads.PublishJob) error {
	f.log = append(f.log, *j)
	f.byID[j.ID] = j
	return nil
}
func (f *fakeJobs) Claim(_ context.Context, id string, from, to ads.JobStatus) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.byID[id]
	if !ok || j.Status != from {
		return false, nil
	}
	j.Status = to
	return true, nil
}
func (f *fakeJobs) ListByWorkspace(context.Context, string, int) ([]*ads.PublishJob, error) {
	return nil, nil
}
func (f *fakeJobs) ListStale(context.Context, time.Time, int) ([]*ads.PublishJob, error) {
	var out []*ads.PublishJob
	for _, j := range f.byID {
		if j.Status == ads.JobQueued || j.Status == ads.JobRunning {
			out = append(out, j)
		}
	}
	return out, nil
}

type fakeNumbers struct{ numbers []ads.WorkspaceNumber }

func (f *fakeNumbers) List(context.Context, string) ([]ads.WorkspaceNumber, error) {
	return f.numbers, nil
}

type fakeMedia struct{ err error }

func (f *fakeMedia) Describe(_ context.Context, _ string, ref ads.MediaRef) (*media.Media, error) {
	if f.err != nil {
		return nil, f.err
	}
	kind := media.MediaTypeProductImage
	if ref.Kind == ads.MediaVideo {
		kind = media.MediaTypeProductVideo
	}
	return &media.Media{ID: ref.MediaID, URL: "https://cdn/" + ref.MediaID, Type: kind}, nil
}

func (f *fakeMedia) Load(ctx context.Context, ws string, ref ads.MediaRef) (*CreativeFile, error) {
	if _, err := f.Describe(ctx, ws, ref); err != nil {
		return nil, err
	}
	return &CreativeFile{MediaID: ref.MediaID, Name: ref.MediaID, Bytes: []byte("data")}, nil
}

type fakeFees struct {
	chargeErr  error
	charged    []string
	quantities []int
	refunded   []string
}

func (f *fakeFees) Quote(string) (Fee, error) {
	return Fee{PriceMicros: 1_000_000, Currency: "USD"}, nil
}
func (f *fakeFees) Charge(_, ref string, quantity int) (Fee, error) {
	if f.chargeErr != nil {
		return Fee{}, f.chargeErr
	}
	f.charged = append(f.charged, ref)
	f.quantities = append(f.quantities, quantity)
	return Fee{PriceMicros: int64(quantity) * 1_000_000, Currency: "USD"}, nil
}
func (f *fakeFees) Refund(_, ref string, _ int64) error {
	f.refunded = append(f.refunded, ref)
	return nil
}

type fakeGateway struct {
	calls       []string
	failOn      string
	failWith    error
	pages       []ads.RemotePage
	accounts    []ads.RemoteAdAccount
	objects     map[ads.Level][]*ads.Object
	insights    []ads.DailyInsight
	statuses    map[string]ads.ConfiguredStatus
	edits       map[string]ads.EditSpec
	deleted     []string
	campaigns   []ads.CampaignSpec
	adSets      []ads.AdSetSpec
	creatives   []ads.CreativeSpec
	videoStates []ads.VideoState
	detail      *ads.ObjectDetail
	forms       []ads.LeadForm
	leads       []ads.FormLead
	pixels      []ads.Pixel
	audiences   []ads.Audience
	termsOK     bool
	batches     []ads.HashedCustomers
	sessions    []ads.CustomerSession
	lookalike   ads.LookalikeDraft
	sent        []ads.ConversionEvent
	rules       []ads.AutomatedRule
	tests       []ads.SplitTest
	subscribed  []string
	minimums    ads.MinimumBudgets
	minimumBid  int64
	billing     ads.RemoteBilling
	billingWith []bool
}

func newFakeGateway() *fakeGateway {
	return &fakeGateway{objects: map[ads.Level][]*ads.Object{}, statuses: map[string]ads.ConfiguredStatus{}, edits: map[string]ads.EditSpec{}, termsOK: true}
}

func (g *fakeGateway) step(name string) error {
	g.calls = append(g.calls, name)
	if g.failOn == name {
		return g.failWith
	}
	return nil
}

func (g *fakeGateway) ListAdAccounts(context.Context, string) ([]ads.RemoteAdAccount, error) {
	return g.accounts, g.step("list_accounts")
}
func (g *fakeGateway) GetAdAccount(_ context.Context, _, id string) (*ads.RemoteAdAccount, error) {
	for _, a := range g.accounts {
		if a.MetaAccountID == id {
			return &a, g.step("get_account")
		}
	}
	return nil, fmt.Errorf("no account %s", id)
}
func (g *fakeGateway) GetBilling(_ context.Context, _, _ string, withPaymentMethod bool) (*ads.RemoteBilling, error) {
	g.billingWith = append(g.billingWith, withPaymentMethod)
	billing := g.billing
	if !withPaymentMethod {
		billing.PaymentMethod = ""
	}
	return &billing, g.step("billing")
}
func (g *fakeGateway) SubscribeAccount(_ context.Context, _, id string) error {
	g.subscribed = append(g.subscribed, id)
	return g.step("subscribe_account")
}
func (g *fakeGateway) ListPages(context.Context, string) ([]ads.RemotePage, error) {
	return g.pages, g.step("list_pages")
}
func (g *fakeGateway) ListObjects(_ context.Context, _, _ string, level ads.Level) ([]*ads.Object, error) {
	return g.objects[level], g.step("list_" + string(level))
}
func (g *fakeGateway) DailyInsights(context.Context, string, string, ads.DateRange) ([]ads.DailyInsight, error) {
	return g.insights, g.step("insights")
}
func (g *fakeGateway) SetStatus(_ context.Context, _, id string, s ads.ConfiguredStatus) error {
	if err := g.step("status:" + id); err != nil {
		return err
	}
	g.statuses[id] = s
	return nil
}
func (g *fakeGateway) UploadImage(context.Context, string, string, []byte, string) (string, error) {
	return "hash-1", g.step("image")
}
func (g *fakeGateway) UploadVideo(context.Context, string, string, []byte, string) (string, error) {
	return "video-1", g.step("video")
}
func (g *fakeGateway) VideoStatus(context.Context, string, string) (*ads.RemoteVideo, error) {
	state := ads.VideoReady
	if len(g.videoStates) > 0 {
		state, g.videoStates = g.videoStates[0], g.videoStates[1:]
	}
	return &ads.RemoteVideo{ID: "video-1", State: state}, g.step("video_status")
}
func (g *fakeGateway) CreateCampaign(_ context.Context, _, _ string, s ads.CampaignSpec) (string, error) {
	g.campaigns = append(g.campaigns, s)
	return "c-1", g.step("campaign")
}
func (g *fakeGateway) CreateAdSet(_ context.Context, _, _ string, s ads.AdSetSpec) (string, error) {
	g.adSets = append(g.adSets, s)
	return "s-1", g.step("adset")
}
func (g *fakeGateway) CreateCreative(_ context.Context, _, _ string, s ads.CreativeSpec) (string, error) {
	g.creatives = append(g.creatives, s)
	return fmt.Sprintf("cr-%d", len(g.creatives)), g.step("creative")
}
func (g *fakeGateway) CreateAd(context.Context, string, string, ads.AdSpec) (string, error) {
	n := 0
	for _, c := range g.calls {
		if c == "ad" {
			n++
		}
	}
	return fmt.Sprintf("a-%d", n+1), g.step("ad")
}
func (g *fakeGateway) DeleteObject(_ context.Context, _, id string) error {
	g.deleted = append(g.deleted, id)
	return g.step("delete:" + id)
}
func (g *fakeGateway) GetObjectDetail(context.Context, string, string, ads.Level) (*ads.ObjectDetail, error) {
	if g.detail == nil {
		return &ads.ObjectDetail{}, g.step("detail")
	}
	detail := *g.detail
	return &detail, g.step("detail")
}
func (g *fakeGateway) UpdateObject(_ context.Context, _, id string, _ ads.Level, spec ads.EditSpec) error {
	g.edits[id] = spec
	return g.step("update:" + id)
}
func (g *fakeGateway) CopyObject(_ context.Context, _, id string, _ ads.Level, _ ads.CopyRequest) (string, error) {
	return id + "-copy", g.step("copy:" + id)
}
func (g *fakeGateway) SetSpendCap(context.Context, string, string, int64) error {
	return g.step("spend_cap")
}
func (g *fakeGateway) RemoveSpendCap(context.Context, string, string) error {
	return g.step("remove_spend_cap")
}
func (g *fakeGateway) SearchLocations(context.Context, string, string) ([]ads.RemoteLocation, error) {
	return nil, g.step("locations")
}
func (g *fakeGateway) SearchTargeting(context.Context, string, string, ads.TargetingSearchKind, string) ([]ads.TargetingOption, error) {
	return nil, g.step("targeting")
}
func (g *fakeGateway) EstimateReach(context.Context, string, string, ads.Targeting, ads.Placements, ads.OptimizationGoal) (*ads.ReachEstimate, error) {
	return &ads.ReachEstimate{Lower: 1, Upper: 2, Ready: true}, g.step("reach")
}
func (g *fakeGateway) ListCatalogs(context.Context, string, string) ([]ads.RemoteCatalog, error) {
	return []ads.RemoteCatalog{{ID: "cat-1", ProductSets: []ads.RemoteProductSet{{ID: "set-1"}}}}, g.step("catalogs")
}
func (g *fakeGateway) ListApps(context.Context, string, string) ([]ads.RemoteApp, error) {
	return []ads.RemoteApp{{ID: "app-1", StoreURLs: []string{"https://play.google.com/store/apps/details?id=x"}}}, g.step("apps")
}
func (g *fakeGateway) ListPagePosts(context.Context, string, string) ([]ads.RemotePost, error) {
	return []ads.RemotePost{{ID: "page-1_99"}}, g.step("posts")
}
func (g *fakeGateway) ListInstagramMedia(context.Context, string, string) ([]ads.RemotePost, error) {
	return []ads.RemotePost{{ID: "ig-media-1"}}, g.step("ig_media")
}
func (g *fakeGateway) ListInstantExperiences(context.Context, string, string) ([]ads.RemoteInstantExperience, error) {
	return []ads.RemoteInstantExperience{{ID: "canvas-1"}}, g.step("canvases")
}
func (g *fakeGateway) ListForms(context.Context, string, string) ([]ads.LeadForm, error) {
	return g.forms, g.step("forms")
}
func (g *fakeGateway) CreateForm(context.Context, string, ads.LeadFormDraft) (string, error) {
	return "form-new", g.step("create_form")
}
func (g *fakeGateway) ArchiveForm(context.Context, string, string, string) error {
	return g.step("archive_form")
}
func (g *fakeGateway) ListLeads(context.Context, string, string, string, time.Time) ([]ads.FormLead, error) {
	return g.leads, g.step("leads")
}
func (g *fakeGateway) GetLead(_ context.Context, _, _, id string) (*ads.FormLead, error) {
	for _, l := range g.leads {
		if l.MetaID == id {
			return &l, g.step("lead")
		}
	}
	return nil, fmt.Errorf("no lead %s", id)
}
func (g *fakeGateway) SubscribeLeadgen(context.Context, string, string) error {
	return g.step("subscribe_leadgen")
}
func (g *fakeGateway) ListPixels(context.Context, string, string) ([]ads.Pixel, error) {
	return g.pixels, g.step("pixels")
}
func (g *fakeGateway) CreatePixel(context.Context, string, string, string) (string, error) {
	return "px-new", g.step("create_pixel")
}
func (g *fakeGateway) DatasetForWABA(context.Context, string, string) (string, error) {
	return "ds-1", g.step("dataset")
}
func (g *fakeGateway) SendEvents(_ context.Context, _ string, events []ads.ConversionEvent) error {
	if err := g.step("send_events"); err != nil {
		return err
	}
	g.sent = append(g.sent, events...)
	return nil
}
func (g *fakeGateway) CustomAudienceTermsAccepted(context.Context, string, string) (bool, error) {
	return g.termsOK, g.step("terms")
}
func (g *fakeGateway) ListAudiences(context.Context, string, string) ([]ads.Audience, error) {
	return g.audiences, g.step("audiences")
}
func (g *fakeGateway) CreateCustomerList(context.Context, string, string, string, string) (string, error) {
	return "aud-new", g.step("create_audience")
}
func (g *fakeGateway) AddCustomers(_ context.Context, _, _ string, batch ads.HashedCustomers, session ads.CustomerSession) error {
	if err := g.step("add_customers"); err != nil {
		return err
	}
	g.batches = append(g.batches, batch)
	g.sessions = append(g.sessions, session)
	return nil
}
func (g *fakeGateway) CreateLookalike(_ context.Context, _, _ string, draft ads.LookalikeDraft) (string, error) {
	g.lookalike = draft
	return "lal-new", g.step("create_lookalike")
}
func (g *fakeGateway) DeleteAudience(_ context.Context, _, id string) error {
	return g.step("delete_audience:" + id)
}
func (g *fakeGateway) ListRules(context.Context, string, string) ([]ads.AutomatedRule, error) {
	return g.rules, g.step("rules")
}
func (g *fakeGateway) CreateRule(context.Context, string, string, string, ads.AutomatedRule) (string, error) {
	return "rule-new", g.step("create_rule")
}
func (g *fakeGateway) SetRuleStatus(_ context.Context, _, id string, s ads.RuleStatus) error {
	return g.step("rule_status:" + id + ":" + string(s))
}
func (g *fakeGateway) DeleteRule(_ context.Context, _, id string) error {
	return g.step("delete_rule:" + id)
}
func (g *fakeGateway) RuleHistory(context.Context, string, string) ([]ads.RuleRun, error) {
	return nil, g.step("rule_history")
}
func (g *fakeGateway) LiveInsights(context.Context, string, string, ads.LiveQuery) ([]ads.LiveRow, error) {
	return []ads.LiveRow{{ObjectID: "c-1", Values: ads.LiveMetrics{Metrics: ads.Metrics{Currency: "BRL"}, Reach: 100}}}, g.step("live")
}
func (g *fakeGateway) CreateSplitTest(context.Context, string, string, ads.SplitTest) (string, error) {
	return "study-1", g.step("create_test")
}
func (g *fakeGateway) ListSplitTests(context.Context, string, string) ([]ads.SplitTest, error) {
	return g.tests, g.step("tests")
}

type world struct {
	gateway  *fakeGateway
	grants   *fakeGrants
	accounts *fakeAccounts
	objects  *fakeObjects
	insights *fakeInsights
	attrib   *fakeAttribution
	jobs     *fakeJobs
	fees     *fakeFees
	numbers  *fakeNumbers
	media    *fakeMedia
	sync     *SyncUseCase
}

func newWorld() *world {
	w := &world{
		gateway:  newFakeGateway(),
		grants:   &fakeGrants{byID: map[string]*ads.Grant{}},
		accounts: &fakeAccounts{byID: map[string]*ads.AdAccount{}, connections: map[string]ads.Connection{}},
		objects:  &fakeObjects{byID: map[string]*ads.Object{}},
		insights: &fakeInsights{},
		attrib:   &fakeAttribution{},
		jobs:     &fakeJobs{byID: map[string]*ads.PublishJob{}},
		fees:     &fakeFees{},
		numbers:  &fakeNumbers{numbers: []ads.WorkspaceNumber{{Kind: ads.NumberOfficial, Label: "Loja", Number: "5511988887777"}}},
		media:    &fakeMedia{},
	}
	w.grants.byID["grant-1"] = &ads.Grant{ID: "grant-1", WorkspaceID: "ws-1", Status: ads.GrantActive, AccessToken: "tok", Scopes: ads.RequiredScopes()}
	w.accounts.byID["acc-1"] = &ads.AdAccount{
		ID: "acc-1", WorkspaceID: "ws-1", GrantID: "grant-1", MetaAccountID: "111", Name: "Loja", BusinessID: "biz-1",
		Currency: "BRL", Timezone: "America/Sao_Paulo", MetaStatus: ads.MetaAccountActive, HasFunding: true,
		Tasks: adminTasks(), Connection: ads.ConnectionConnected,
	}
	w.gateway.accounts = []ads.RemoteAdAccount{{MetaAccountID: "111", Name: "Loja", BusinessID: "biz-1", Currency: "BRL", Timezone: "America/Sao_Paulo", Status: ads.MetaAccountActive, HasFunding: true, Tasks: adminTasks()}}
	w.gateway.pages = []ads.RemotePage{{PageID: "page-1", Name: "Loja", WhatsAppNumber: "+55 11 98888-7777", InstagramUserID: "ig-1", CanAdvertise: true, LeadTermsAccepted: true}}
	w.sync = NewSyncUseCase(w.accounts, w.grants, w.gateway, w.objects, w.insights)
	w.sync.access.now = func() time.Time { return testNow }
	return w
}

func noSleep(context.Context, time.Duration) error { return nil }

func (w *world) publisher() *PublishUseCase {
	uc := NewPublishUseCase(w.sync, w.gateway, w.jobs, w.numbers, w.media, w.fees)
	uc.media.sleep = noSleep
	uc.preflight.media.sleep = noSleep
	return uc
}

func (w *world) manager() *ManageUseCase {
	uc := NewManageUseCase(w.sync, w.gateway, w.media)
	uc.media.sleep = noSleep
	return uc
}

func imageAd() ads.CreativeDraft {
	return ads.CreativeDraft{Format: ads.FormatImage, PrimaryText: "Fale com a gente", Media: ads.MediaRef{Kind: ads.MediaImage, MediaID: "media-1"}}
}

func publishableDraft() ads.AdDraft {
	return ads.AdDraft{
		AdAccountID: "acc-1",
		Identity:    ads.Identity{PageID: "page-1"},
		Campaign:    ads.CampaignDraft{Name: "Leads outubro", Objective: ads.ObjectiveEngagement},
		AdSet: ads.AdSetDraft{
			Destination: ads.DestinationWhatsApp, Goal: ads.GoalConversations, WhatsAppNumber: "5511988887777",
			Budget:    &ads.Budget{Kind: ads.BudgetDaily, Amount: 2000},
			Targeting: ads.Targeting{Locations: []ads.GeoLocation{{Kind: ads.LocationCountry, Key: "BR"}}},
		},
		Ads: []ads.AdItem{{Creative: imageAd()}},
	}
}

func adminTasks() []string { return []string{"MANAGE", "ADVERTISE", "ANALYZE"} }

func (g *fakeGateway) MinimumBudgets(_ context.Context, _, _ string, bidAmount int64) (ads.MinimumBudgets, error) {
	g.minimumBid = bidAmount
	return g.minimums, g.step("minimum_budgets")
}
