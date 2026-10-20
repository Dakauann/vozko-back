package whatsappform_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"vozko/domain/customfield"
	businessphone "vozko/domain/whatsapp/business_phone"
	wf "vozko/domain/whatsappform"
	"vozko/domain/workspace"
)

var (
	ctx     = context.Background()
	baseNow = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
)

const (
	ws       = "ws-1"
	otherWS  = "ws-2"
	member   = "user-1"
	wabaA    = "waba-a"
	wabaB    = "waba-b"
	wabaD360 = "waba-360"
)

type permissions struct {
	granted map[string]bool
}

func grant(t *testing.T, keys ...workspace.CapabilityKey) *permissions {
	t.Helper()
	p := &permissions{granted: map[string]bool{}}
	entries, err := workspace.CapabilitiesRequire(keys)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		p.granted[string(e.Resource)+":"+string(e.Action)] = true
	}
	return p
}

func (p *permissions) HasWorkspacePermission(userID, workspaceID, resource, action string, isAdmin bool) bool {
	return workspaceID == ws && p.granted[resource+":"+action]
}

type formsRepo struct {
	mu       sync.Mutex
	forms    map[string]wf.Form
	versions map[string]wf.Version
	pubs     *pubsRepo
	failName bool
}

func newFormsRepo() *formsRepo {
	return &formsRepo{forms: map[string]wf.Form{}, versions: map[string]wf.Version{}}
}

func (r *formsRepo) Create(_ context.Context, form wf.Form, version wf.Version) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, f := range r.forms {
		if f.WorkspaceID == form.WorkspaceID && f.Name == form.Name && f.ArchivedAt == nil {
			return wf.ErrNameTaken
		}
	}
	r.forms[form.ID], r.versions[version.ID] = form, stored(version)
	return nil
}

func (r *formsRepo) Get(_ context.Context, workspaceID, formID string) (*wf.Form, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.forms[formID]
	if !ok || f.WorkspaceID != workspaceID {
		return nil, wf.ErrFormNotFound
	}
	return &f, nil
}

func (r *formsRepo) List(_ context.Context, q wf.FormQuery) ([]wf.FormSummary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []wf.FormSummary
	for _, f := range r.forms {
		if f.WorkspaceID == q.WorkspaceID && (q.IncludeArchived || f.ArchivedAt == nil) {
			out = append(out, wf.FormSummary{Form: f})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Form.ID < out[j].Form.ID })
	return out, nil
}

func (r *formsRepo) SaveDraft(_ context.Context, workspaceID, formID, baseVersionID string, version wf.Version, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.forms[formID]
	if !ok || f.WorkspaceID != workspaceID {
		return wf.ErrFormNotFound
	}
	if f.DraftVersionID != baseVersionID {
		return wf.ErrStaleDraft
	}
	r.versions[version.ID] = stored(version)
	f.DraftVersionID, f.UpdatedAt = version.ID, at
	r.forms[formID] = f
	return nil
}

func (r *formsRepo) Rename(_ context.Context, workspaceID, formID, name, description string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.forms[formID]
	if !ok || f.WorkspaceID != workspaceID {
		return wf.ErrFormNotFound
	}
	for id, other := range r.forms {
		if id != formID && other.WorkspaceID == workspaceID && other.Name == name && other.ArchivedAt == nil {
			return wf.ErrNameTaken
		}
	}
	f.Name, f.Description, f.UpdatedAt = name, description, at
	r.forms[formID] = f
	return nil
}

func (r *formsRepo) SetPublished(_ context.Context, workspaceID, formID, versionID string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.forms[formID]
	if !ok || f.WorkspaceID != workspaceID {
		return wf.ErrFormNotFound
	}
	f.PublishedVersionID, f.UpdatedAt = versionID, at
	r.forms[formID] = f
	return nil
}

func (r *formsRepo) Archive(_ context.Context, workspaceID, formID string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.forms[formID]
	if !ok || f.WorkspaceID != workspaceID {
		return wf.ErrFormNotFound
	}
	f.ArchivedAt = &at
	r.forms[formID] = f
	return nil
}

func (r *formsRepo) PublishedWorkspaces(_ context.Context) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, f := range r.forms {
		if f.PublishedVersionID != "" && f.ArchivedAt == nil && !seen[f.WorkspaceID] {
			seen[f.WorkspaceID] = true
			out = append(out, f.WorkspaceID)
		}
	}
	return out, nil
}

func (r *formsRepo) GetVersion(_ context.Context, workspaceID, versionID string) (*wf.Version, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.versions[versionID]
	if !ok || v.WorkspaceID != workspaceID {
		return nil, wf.ErrVersionNotFound
	}
	v = stored(v)
	return &v, nil
}

type versionsRepo struct{ forms *formsRepo }

func (r versionsRepo) Get(c context.Context, workspaceID, versionID string) (*wf.Version, error) {
	return r.forms.GetVersion(c, workspaceID, versionID)
}

func (r versionsRepo) List(_ context.Context, workspaceID, formID string) ([]wf.Version, error) {
	r.forms.mu.Lock()
	defer r.forms.mu.Unlock()
	var out []wf.Version
	for _, v := range r.forms.versions {
		if v.WorkspaceID == workspaceID && v.FormID == formID {
			out = append(out, stored(v))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number > out[j].Number })
	return out, nil
}

type pubsRepo struct {
	mu   sync.Mutex
	rows map[string]wf.Publication
}

func newPubsRepo() *pubsRepo { return &pubsRepo{rows: map[string]wf.Publication{}} }

func (r *pubsRepo) ListForForm(_ context.Context, workspaceID, formID string) ([]wf.Publication, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []wf.Publication
	for _, p := range r.rows {
		if p.WorkspaceID == workspaceID && p.FormID == formID {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MetaWABAID < out[j].MetaWABAID })
	return out, nil
}

func (r *pubsRepo) Find(_ context.Context, workspaceID, formID, metaWABAID string) (*wf.Publication, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.rows {
		if p.WorkspaceID == workspaceID && p.FormID == formID && p.MetaWABAID == metaWABAID {
			return &p, nil
		}
	}
	return nil, wf.ErrPublicationNotFound
}

func (r *pubsRepo) FindByMetaFlow(_ context.Context, metaWABAID, metaFlowID string) (*wf.Publication, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.rows {
		if p.MetaWABAID == metaWABAID && p.MetaFlowID == metaFlowID {
			return &p, nil
		}
	}
	return nil, wf.ErrPublicationNotFound
}

func (r *pubsRepo) Insert(_ context.Context, p wf.Publication) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.rows {
		if existing.FormID == p.FormID && existing.MetaWABAID == p.MetaWABAID {
			return errors.New("duplicate publication")
		}
	}
	r.rows[p.ID] = p
	return nil
}

func (r *pubsRepo) Save(_ context.Context, p wf.Publication) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.rows[p.ID]; !ok {
		return wf.ErrPublicationNotFound
	}
	r.rows[p.ID] = p
	return nil
}

func (r *pubsRepo) ListStuck(_ context.Context, before time.Time, limit int) ([]wf.Publication, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []wf.Publication
	for _, p := range r.rows {
		if p.Status == wf.PublicationCreating && p.UpdatedAt.Before(before) {
			out = append(out, p)
		}
	}
	return out, nil
}

func (r *pubsRepo) only(t *testing.T, metaWABAID string) wf.Publication {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.rows {
		if p.MetaWABAID == metaWABAID {
			return p
		}
	}
	t.Fatalf("no publication on %s", metaWABAID)
	return wf.Publication{}
}

type gatewayCall struct {
	op, waba, flow, name string
	publish              bool
}

type gateway struct {
	mu          sync.Mutex
	calls       []gatewayCall
	nextID      int
	flows       map[string]wf.RemoteFlow
	createErr   error
	createValid []wf.MetaValidationError
	uploadValid []wf.MetaValidationError
	publishErr  error
	deprecErr   error
}

func newGateway() *gateway { return &gateway{flows: map[string]wf.RemoteFlow{}} }

func (g *gateway) record(c gatewayCall) {
	g.calls = append(g.calls, c)
}

func (g *gateway) ops() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []string
	for _, c := range g.calls {
		out = append(out, c.op+":"+c.waba)
	}
	return out
}

func (g *gateway) Create(_ context.Context, a wf.FlowAccount, in wf.CreateFlowInput) (wf.RemoteFlow, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.record(gatewayCall{op: "create", waba: a.MetaWABAID, name: in.Name, publish: in.Publish})
	if g.createErr != nil {
		return wf.RemoteFlow{}, g.createErr
	}
	g.nextID++
	flow := wf.RemoteFlow{ID: fmt.Sprintf("flow-%d", g.nextID), Name: in.Name, Status: "PUBLISHED", ValidationErrors: g.createValid}
	if !in.Publish || len(g.createValid) > 0 {
		flow.Status = "DRAFT"
	}
	g.flows[a.MetaWABAID+"/"+in.Name] = flow
	created := flow
	created.Status = ""
	return created, nil
}

func (g *gateway) UploadJSON(_ context.Context, a wf.FlowAccount, flowID string, raw []byte) ([]wf.MetaValidationError, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.record(gatewayCall{op: "upload", waba: a.MetaWABAID, flow: flowID})
	return g.uploadValid, nil
}

func (g *gateway) Publish(_ context.Context, a wf.FlowAccount, flowID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.record(gatewayCall{op: "publish", waba: a.MetaWABAID, flow: flowID})
	return g.publishErr
}

func (g *gateway) Deprecate(_ context.Context, a wf.FlowAccount, flowID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.record(gatewayCall{op: "deprecate", waba: a.MetaWABAID, flow: flowID})
	return g.deprecErr
}

func (g *gateway) Get(_ context.Context, a wf.FlowAccount, flowID string) (wf.RemoteFlow, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.record(gatewayCall{op: "get", waba: a.MetaWABAID, flow: flowID})
	for _, f := range g.flows {
		if f.ID == flowID {
			return f, nil
		}
	}
	return wf.RemoteFlow{}, errors.New("flow not found")
}

func (g *gateway) FindByName(_ context.Context, a wf.FlowAccount, name string) (*wf.RemoteFlow, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.record(gatewayCall{op: "find", waba: a.MetaWABAID, name: name})
	if flow, ok := g.flows[a.MetaWABAID+"/"+name]; ok {
		return &flow, nil
	}
	return nil, nil
}

func (g *gateway) Preview(_ context.Context, a wf.FlowAccount, flowID string, refresh bool) (wf.RemoteFlow, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.record(gatewayCall{op: "preview", waba: a.MetaWABAID, flow: flowID})
	expires := baseNow.Add(30 * 24 * time.Hour)
	return wf.RemoteFlow{ID: flowID, PreviewURL: "https://business.facebook.com/wa/manage/flows/" + flowID + "/preview", PreviewExpiresAt: &expires}, nil
}

func (g *gateway) List(_ context.Context, a wf.FlowAccount) ([]wf.RemoteFlow, error) {
	return nil, nil
}

func (g *gateway) DownloadJSON(_ context.Context, a wf.FlowAccount, flowID string) ([]byte, error) {
	return nil, nil
}

type phonesRepo struct {
	phones []*businessphone.WhatsAppBusinessPhoneNumber
}

func phone(id, owner, waba string, provider businessphone.Provider) *businessphone.WhatsAppBusinessPhoneNumber {
	return &businessphone.WhatsAppBusinessPhoneNumber{
		ID: id, OwnerWorkspaceID: owner, WABAId: waba, Provider: provider, Status: businessphone.StatusConnected,
		AccessToken: "token-" + id, MetaPhoneNumberID: "meta-" + id, DisplayPhoneNumber: "55110000" + id,
	}
}

func (r *phonesRepo) FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	for _, p := range r.phones {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, businessphone.ErrPhoneNumberNotFound
}

func (r *phonesRepo) OwnedPhones(_ context.Context, workspaceID string) ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	var out []*businessphone.WhatsAppBusinessPhoneNumber
	for _, p := range r.phones {
		if p.BelongsToWorkspace(workspaceID) {
			out = append(out, p)
		}
	}
	return out, nil
}

type definitions struct {
	defs []*customfield.Definition
}

func (d definitions) ListByObject(workspaceID string, objectType customfield.ObjectType) ([]*customfield.Definition, error) {
	return d.defs, nil
}

type assets struct {
	files map[string][]byte
}

func (a assets) Load(_ context.Context, workspaceID string, mediaIDs []string) (wf.Assets, error) {
	out := wf.Assets{}
	for _, id := range mediaIDs {
		if data, ok := a.files[id]; ok {
			out[id] = data
		}
	}
	return out, nil
}

type world struct {
	t           *testing.T
	svc         *Service
	forms       *formsRepo
	pubs        *pubsRepo
	gateway     *gateway
	phones      *phonesRepo
	perms       *permissions
	now         time.Time
	ids         int
	viewer      Actor
	defs        definitions
	assets      assets
	sends       *sendsRepo
	answers     *responsesRepo
	sender      *flowSender
	targets     *targets
	effects     *effects
	automations *automationCatalog
	notices     *notices
}

func newWorld(t *testing.T, caps ...workspace.CapabilityKey) *world {
	t.Helper()
	w := &world{
		t: t, forms: newFormsRepo(), pubs: newPubsRepo(), gateway: newGateway(), now: baseNow,
		phones: &phonesRepo{phones: []*businessphone.WhatsAppBusinessPhoneNumber{
			phone("p1", ws, wabaA, businessphone.ProviderMeta),
			phone("p2", ws, wabaA, businessphone.ProviderMeta),
			phone("p3", ws, wabaB, businessphone.ProviderMeta),
			phone("p4", otherWS, "waba-other", businessphone.ProviderMeta),
		}},
		perms:       grant(t, caps...),
		viewer:      Actor{UserID: member, WorkspaceID: ws},
		defs:        definitions{defs: []*customfield.Definition{{Key: "plano", Label: "Plano", Type: customfield.TypeSelect, Options: []string{"Básico", "Pro"}, ObjectType: customfield.ObjectLead}, {Key: "cpf", Label: "CPF", Type: customfield.TypeText, Sensitive: true, LegalBasis: "contrato", ObjectType: customfield.ObjectLead}}},
		assets:      assets{files: map[string][]byte{}},
		sends:       newSendsRepo(),
		answers:     newResponsesRepo(),
		sender:      &flowSender{},
		targets:     newTargets(),
		effects:     newEffects(),
		automations: &automationCatalog{},
		notices:     &notices{},
	}
	w.forms.pubs = w.pubs
	w.build()
	return w
}

func (w *world) build() {
	w.t.Helper()
	svc, err := New(Deps{
		Forms: w.forms, Versions: versionsRepo{w.forms}, Publications: w.pubs, Sends: w.sends, Responses: w.answers,
		Gateway: w.gateway, Phones: w.phones, OwnedPhones: w.phones, Permissions: w.perms, Definitions: w.defs, Assets: w.assets,
		Sender: w.sender, Conversations: w.targets, Access: w.targets, Scopes: w.targets, History: w.effects,
		Profiles: w.effects, Consents: w.effects, Media: w.effects, Runs: w.effects, Triggers: w.effects, LeadNames: w.effects, Automations: w.automations, Notices: w.notices,
		Now:    func() time.Time { return w.now },
		NewID:  func() string { w.ids++; return fmt.Sprintf("id-%03d", w.ids) },
		Random: w,
	})
	if err != nil {
		w.t.Fatal(err)
	}
	w.svc = svc
}

func (w *world) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(w.ids + i)
	}
	w.ids++
	return len(p), nil
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func newTextDefinition(key string) *customfield.Definition {
	return &customfield.Definition{Key: key, Label: key, Type: customfield.TypeText, ObjectType: customfield.ObjectLead}
}

func stored(v wf.Version) wf.Version {
	v.Document = wf.CloneDocument(v.Document)
	return v
}
