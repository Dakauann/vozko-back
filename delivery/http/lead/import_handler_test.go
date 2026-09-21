package lead

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/auth"
	"vozko/domain/conversation"
	leaddomain "vozko/domain/lead"
	"vozko/domain/unofficial_whatsapp"
	"vozko/infra/http/middleware"
)

type stubLeadRepo struct {
	leaddomain.Repository
	outcome *leaddomain.ImportOutcome
	err     error
	calls   int
}

func (s *stubLeadRepo) ImportMany(string, []leaddomain.BulkLeadInput, leaddomain.ExistingPolicy) (*leaddomain.ImportOutcome, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.outcome, nil
}

type stubSeeder struct {
	published []unofficial_whatsapp.SeedRequest
	queued    unofficial_whatsapp.SeedQueued
	err       error
}

func (s *stubSeeder) Publish(in unofficial_whatsapp.SeedRequest) (unofficial_whatsapp.SeedQueued, error) {
	s.published = append(s.published, in)
	if s.err != nil {
		return unofficial_whatsapp.SeedQueued{}, s.err
	}
	return s.queued, nil
}

type stubAuthorizer struct {
	conversation.ConversationAuthorizer
	allow bool
}

func (s stubAuthorizer) HasWorkspacePermission(string, string, string, string, bool) bool {
	return s.allow
}

type importCase struct {
	handler *LeadHandler
	seeder  *stubSeeder
	repo    *stubLeadRepo
}

func newImportCase(t *testing.T, channelPermission bool) importCase {
	t.Helper()
	repo := &stubLeadRepo{outcome: &leaddomain.ImportOutcome{Created: 2}}
	handler := NewLeadHandler(repo, nil, nil, nil, nil, nil, nil)
	seeder := &stubSeeder{}
	handler.SetInboxSeeder(seeder)
	handler.SetAuthorizer(stubAuthorizer{allow: channelPermission})
	return importCase{handler: handler, seeder: seeder, repo: repo}
}

func (c importCase) run(t *testing.T, role string, body map[string]any) (*httptest.ResponseRecorder, ImportLeadsResponse) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/leads/import", bytes.NewReader(payload))
	ctx := context.WithValue(req.Context(), middleware.WorkspaceIDContextKey, "ws-1")
	ctx = context.WithValue(ctx, middleware.ClaimsContextKey,
		&auth.Claims{UserID: "user-1", Role: role})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	c.handler.ImportLeads(rec, req)

	var out ImportLeadsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func rows() []map[string]any {
	return []map[string]any{
		{"line": 1, "number": "5511999999999", "name": "Marina"},
		{"line": 2, "number": "5511988888888", "name": "Joao"},
	}
}

func aScriptSpec() map[string]any {
	return map[string]any{
		"bodies":      []string{"Oi {{1}}, tudo bem?"},
		"maxMessages": 4,
	}
}

func TestImportRefusesScriptedSeedingWithoutInboxSeeding(t *testing.T) {
	c := newImportCase(t, true)
	rec, _ := c.run(t, "admin", map[string]any{
		"rows":              rows(),
		"seedInbox":         false,
		"seedConversations": aScriptSpec(),
	})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if c.repo.calls != 0 {
		t.Fatal("leads were imported before the request was refused")
	}
	if len(c.seeder.published) != 0 {
		t.Fatal("something was queued for a refused request")
	}
}

func TestImportRefusesAMalformedScript(t *testing.T) {
	cases := map[string]map[string]any{
		"variants using different variables": {
			"bodies":      []string{"Oi {{1}}", "Oi {{2}}"},
			"maxMessages": 4,
		},
		"no bodies at all": {
			"bodies":      []string{"   "},
			"maxMessages": 4,
		},
	}

	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			c := newImportCase(t, true)
			rec, _ := c.run(t, "admin", map[string]any{
				"rows":              rows(),
				"seedInbox":         true,
				"seedConversations": spec,
			})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if c.repo.calls != 0 {
				t.Fatal("leads were imported before the script was refused")
			}
		})
	}
}

func TestImportDropsTheScriptForANonAdminAndStillSeedsPlain(t *testing.T) {
	c := newImportCase(t, true)
	c.seeder.queued = unofficial_whatsapp.SeedQueued{Targets: 2}

	rec, out := c.run(t, "user", map[string]any{
		"rows":              rows(),
		"seedInbox":         true,
		"seedConversations": aScriptSpec(),
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: the leads imported fine", rec.Code)
	}
	if out.Created != 2 {
		t.Errorf("created = %d, want 2", out.Created)
	}
	if out.ScriptedSeedError == "" {
		t.Error("the operator was not told their script was dropped")
	}
	if out.ScriptedSeedQueued != 0 {
		t.Errorf("ScriptedSeedQueued = %d, want 0", out.ScriptedSeedQueued)
	}
	if out.InboxSeedQueued != 2 {
		t.Errorf("InboxSeedQueued = %d, want 2", out.InboxSeedQueued)
	}
	if len(c.seeder.published) != 1 {
		t.Fatalf("published %d seed requests, want 1", len(c.seeder.published))
	}
	if c.seeder.published[0].Script != nil {
		t.Fatal("a non-admin's script reached the queue")
	}
}

func TestImportQueuesTheScriptForASystemAdmin(t *testing.T) {
	c := newImportCase(t, true)
	c.seeder.queued = unofficial_whatsapp.SeedQueued{Targets: 2, Scripted: 2}

	spec := aScriptSpec()
	spec["context"] = "curso tecnico de enfermagem"

	rec, out := c.run(t, "admin", map[string]any{
		"rows":              rows(),
		"seedInbox":         true,
		"seedConversations": spec,
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if out.ScriptedSeedError != "" {
		t.Errorf("ScriptedSeedError = %q, want none", out.ScriptedSeedError)
	}
	if out.InboxSeedQueued != 2 || out.ScriptedSeedQueued != 2 {
		t.Errorf("queued = %d/%d, want 2 conversations and 2 scripted",
			out.InboxSeedQueued, out.ScriptedSeedQueued)
	}

	if len(c.seeder.published) != 1 {
		t.Fatalf("published %d seed requests, want 1", len(c.seeder.published))
	}
	published := c.seeder.published[0]
	if published.Script == nil {
		t.Fatal("the script did not reach the queue")
	}
	if len(published.Script.Bodies) != 1 || published.Script.MaxMessages != 4 {
		t.Errorf("script = %+v, want the operator's", published.Script)
	}
	if published.Script.Context != "curso tecnico de enfermagem" {
		t.Errorf("script context = %q", published.Script.Context)
	}
	if published.WorkspaceID != "ws-1" {
		t.Errorf("workspace = %q, want the request's", published.WorkspaceID)
	}
	if len(published.Targets) != 2 {
		t.Errorf("targets = %d, want 2", len(published.Targets))
	}
}

func TestImportWithoutTheChannelPermissionSeedsNothing(t *testing.T) {
	c := newImportCase(t, false)

	rec, out := c.run(t, "admin", map[string]any{
		"rows":              rows(),
		"seedInbox":         true,
		"seedConversations": aScriptSpec(),
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if out.InboxSeedError == "" {
		t.Error("the operator was not told seeding was refused")
	}
	if len(c.seeder.published) != 0 {
		t.Fatal("something was queued without the channel permission")
	}
}

func TestImportWithoutAScriptIsUnchanged(t *testing.T) {
	c := newImportCase(t, true)
	c.seeder.queued = unofficial_whatsapp.SeedQueued{Targets: 2}

	rec, out := c.run(t, "admin", map[string]any{
		"rows":      rows(),
		"seedInbox": true,
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if out.InboxSeedQueued != 2 {
		t.Errorf("InboxSeedQueued = %d, want 2", out.InboxSeedQueued)
	}
	if out.ScriptedSeedQueued != 0 || out.ScriptedSeedError != "" {
		t.Errorf("a script-free import reported %d scripted and %q",
			out.ScriptedSeedQueued, out.ScriptedSeedError)
	}
	if c.seeder.published[0].Script != nil {
		t.Fatal("a script was invented for an import that asked for none")
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"scriptedSeedQueued", "scriptedSeedError"} {
		if _, present := raw[key]; present {
			t.Errorf("%q is on the wire for an import that asked for no script", key)
		}
	}
}

func TestImportReportsAQueueFailureWithoutFailingTheImport(t *testing.T) {
	c := newImportCase(t, true)
	c.seeder.err = errors.New("broker down")

	rec, out := c.run(t, "admin", map[string]any{
		"rows":              rows(),
		"seedInbox":         true,
		"seedConversations": aScriptSpec(),
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: the leads imported", rec.Code)
	}
	if out.Created != 2 {
		t.Errorf("created = %d, want 2", out.Created)
	}
	if out.InboxSeedError == "" {
		t.Error("the queue failure was not reported")
	}
}
