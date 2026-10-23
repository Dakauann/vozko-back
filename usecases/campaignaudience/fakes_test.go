package campaignaudience_usecase

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"vozko/domain/campaignaudience"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/selection"
	"vozko/domain/sheet"
	"vozko/usecases/campaignaudience/audiencetest"
	"vozko/usecases/emailhygiene/hygienetest"
)

var testNow = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

var testActor = Actor{WorkspaceID: "ws-1", UserID: "user-1"}

type fakeSelections struct {
	mu       sync.Mutex
	ids      []string
	resolved int
	err      error
	pause    chan struct{}
}

func (f *fakeSelections) Resolve(_ context.Context, scope selection.Scope, s selection.Selection, after string, limit int) ([]selection.Ref, error) {
	if f.err != nil {
		return nil, f.err
	}
	if scope.WorkspaceID != testActor.WorkspaceID {
		return nil, lead.ErrLeadForbidden
	}
	if f.pause != nil {
		<-f.pause
	}
	f.mu.Lock()
	f.resolved++
	f.mu.Unlock()
	var out []selection.Ref
	for _, id := range f.chosen(s) {
		if id > after && len(out) < limit {
			out = append(out, selection.Ref{ID: id, Type: "lead"})
		}
	}
	return out, nil
}

func (f *fakeSelections) chosen(s selection.Selection) []string {
	ids := f.ids
	if s.Mode == selection.ModeIDs {
		ids = s.IDs
	}
	out := []string{}
	for _, id := range ids {
		if !slices.Contains(s.ExcludeIDs, id) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func (f *fakeSelections) Selected(_ context.Context, _ selection.Scope, s selection.Selection) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	return len(f.chosen(s)), nil
}

type csvSheets struct{}

var errNotASheet = errors.New("not a sheet")

func (csvSheets) Read(data []byte) ([]sheet.Row, error) {
	if strings.HasPrefix(string(data), "PK") {
		return nil, errNotASheet
	}
	return sheet.Parse(data), nil
}

type fakePhones map[string]string

func (f fakePhones) IdentityIDs(_ context.Context, workspaceID string, numbers []string) (map[string]string, error) {
	out := map[string]string{}
	if workspaceID != testActor.WorkspaceID {
		return out, nil
	}
	for number, id := range f {
		lead.IndexByNumber(out, number, id)
	}
	return out, nil
}

type fakeEmails map[string]string

func (f fakeEmails) IDsByEmails(_ context.Context, _ string, addresses []string) (map[string]string, error) {
	out := map[string]string{}
	for _, a := range addresses {
		if id, ok := f[a]; ok {
			out[a] = id
		}
	}
	return out, nil
}

type fakeEmailLeads struct {
	created []lead.BulkEmailInput
}

func (f *fakeEmailLeads) FindOrCreateManyByEmail(_ context.Context, _ string, inputs []lead.BulkEmailInput) (map[string]*lead.Lead, error) {
	f.created = append(f.created, inputs...)
	out := map[string]*lead.Lead{}
	for _, in := range inputs {
		out[in.Email] = &lead.Lead{ID: "lead-" + in.Email, Email: in.Email, Name: in.Name}
	}
	return out, nil
}

type fakeDefinitions []*customfield.Definition

func (f fakeDefinitions) ListByObject(string, customfield.ObjectType) ([]*customfield.Definition, error) {
	return f, nil
}

type memSuggestions struct {
	stored map[string][]campaignaudience.Suggestion
}

func (m *memSuggestions) Get(_ context.Context, workspaceID, templateID string) ([]campaignaudience.Suggestion, error) {
	return m.stored[workspaceID+"|"+templateID], nil
}

func (m *memSuggestions) Replace(_ context.Context, workspaceID, templateID, _ string, s []campaignaudience.Suggestion) error {
	if m.stored == nil {
		m.stored = map[string][]campaignaudience.Suggestion{}
	}
	m.stored[workspaceID+"|"+templateID] = s
	return nil
}

type openGate struct {
	mu       sync.Mutex
	acquired int
}

func (g *openGate) Acquire(context.Context) (func(), error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.acquired++
	return func() {}, nil
}

type fakeJudge struct {
	leads   map[string]campaignaudience.Candidate
	refused map[string]string
	fills   map[string][]string
}

func (j fakeJudge) Leads(_ context.Context, ids []string) ([]campaignaudience.Candidate, error) {
	var out []campaignaudience.Candidate
	for _, id := range ids {
		c, ok := j.leads[id]
		if !ok {
			continue
		}
		c.LeadID = id
		out = append(out, c)
	}
	return out, nil
}

func (j fakeJudge) Rows(_ context.Context, rows []RowCandidate) ([]campaignaudience.Candidate, error) {
	out := make([]campaignaudience.Candidate, 0, len(rows))
	for _, r := range rows {
		values := append([]string(nil), r.Values...)
		for i, v := range values {
			if v == "" && j.fills[r.Row.Contact] != nil {
				values[i] = j.fills[r.Row.Contact][i]
			}
		}
		out = append(out, campaignaudience.Candidate{Line: r.Row.Line, LeadID: r.LeadID, Contact: r.Row.Contact, Name: r.Row.Name, HasContact: true, Refusal: j.refused[r.LeadID], Values: values, Metadata: r.Metadata})
	}
	return out, nil
}

type fakeChannel struct {
	name     campaignaudience.Channel
	slots    []campaignaudience.Slot
	judge    fakeJudge
	opened   []campaignaudience.Source
	bounds   [][]campaignaudience.Bound
	refuse   error
	prepErr  error
	metadata campaignaudience.MetadataRule
	awaits   campaignaudience.Blocker
	params   []Params
}

func (c *fakeChannel) Name() campaignaudience.Channel { return c.name }

func (c *fakeChannel) Prepare(_ context.Context, _ Actor, params Params) (Prepared, error) {
	c.params = append(c.params, params)
	if c.prepErr != nil {
		return Prepared{}, c.prepErr
	}
	return Prepared{Slots: c.slots, Awaits: c.awaits, Metadata: c.metadata, Open: func(_ context.Context, bound []campaignaudience.Bound, source campaignaudience.Source) (Judge, error) {
		if c.refuse != nil {
			return nil, c.refuse
		}
		c.opened = append(c.opened, source)
		c.bounds = append(c.bounds, bound)
		return c.judge, nil
	}}, nil
}

type world struct {
	service    *Service
	selections *fakeSelections
	places     *fakePlaces
	lists      *audiencetest.Lists
	channel    *fakeChannel
	email      *fakeChannel
	emailLeads *fakeEmailLeads
	gate       *openGate
	state      *audiencetest.State
	seen       *hygienetest.SeenEmails
	ids        int
}

func newWorld(t interface{ Fatal(...any) }) *world {
	w := &world{
		selections: &fakeSelections{},
		places:     &fakePlaces{},
		lists:      audiencetest.NewLists(),
		channel:    &fakeChannel{name: campaignaudience.ChannelWhatsApp},
		email:      &fakeChannel{name: campaignaudience.ChannelEmail},
		emailLeads: &fakeEmailLeads{},
		gate:       &openGate{},
		state:      audiencetest.NewState(),
		seen:       &hygienetest.SeenEmails{},
	}
	service, err := New(Deps{
		Selections:  w.selections,
		Places:      w.places,
		Lists:       w.lists,
		Sheets:      csvSheets{},
		Phones:      fakePhones{"5511900010142": "lead-maria"},
		Emails:      fakeEmails{"ana@exemplo.com.br": "lead-ana"},
		EmailLeads:  w.emailLeads,
		EmailsSeen:  w.seen,
		Definitions: fakeDefinitions{{Key: "serie", Label: "Série", ObjectType: customfield.ObjectLead, Type: customfield.TypeText}, {Key: "cpf", ObjectType: customfield.ObjectLead, Sensitive: true}},
		Channels:    []Channel{w.channel, w.email},
		Gate:        w.gate,
		Flight:      audiencetest.Flight(w.state),
		Now:         func() time.Time { return testNow },
		NewID: func() string {
			w.ids++
			return fmt.Sprintf("id-%03d", w.ids)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	w.service = service
	return w
}

var _ Lists = (*audiencetest.Lists)(nil)
