package audience_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	ca "vozko/domain/audience"
)

// The fallback rule, post override → account settings → disabled defaults,
// lives in the resolver and nowhere else. These pin every tier.

func TestResolver_UnconfiguredAccountIsOff(t *testing.T) {
	r := NewSettingsResolver(newFakeSettings())
	s, err := r.Resolve(context.Background(), ref())
	if err != nil {
		t.Fatal(err)
	}
	if s.Enabled || !s.Topics.Has(ca.TopicKeyOther) {
		t.Fatalf("unconfigured account must resolve to the disabled defaults: %+v", s)
	}
	if _, err := r.Resolve(context.Background(), ca.ContainerRef{}); !errors.Is(err, ca.ErrContainerInvalid) {
		t.Fatalf("an invalid ref must be refused: %v", err)
	}
}

func TestResolver_PostOverrideLayersOverAccount(t *testing.T) {
	store := newFakeSettings(enabledSettings())
	r := NewSettingsResolver(store)

	// No override: the account's settings verbatim.
	s, err := r.Resolve(context.Background(), ref())
	if err != nil || !s.Enabled || !s.Topics.Has("saude") {
		t.Fatalf("account tier: %+v %v", s, err)
	}

	off := false
	instructions := "post sobre o asfalto"
	threshold := 90
	topics := ca.TopicSet{{Key: "asfalto", Label: "Asfalto"}}
	_ = store.SaveOverride(context.Background(), &ca.ContainerOverride{
		WorkspaceID: "ws-1", Source: ca.SourceInstagram, AccountID: "acc-1", ContainerID: "media-1",
		Enabled: &off, Instructions: &instructions, SeverityThreshold: &threshold, Topics: &topics,
	})
	s, err = r.Resolve(context.Background(), ref())
	if err != nil {
		t.Fatal(err)
	}
	if s.Enabled || s.ActionPolicy.SeverityThreshold != 90 || !strings.Contains(s.Instructions, "asfalto") || s.Topics.Has("saude") || !s.Topics.Has("asfalto") {
		t.Fatalf("post tier not applied: %+v", s)
	}
	// Another post of the same account still gets the account's settings.
	other := ref()
	other.ContainerID = "media-2"
	s, _ = r.Resolve(context.Background(), other)
	if !s.Enabled || !s.Topics.Has("saude") {
		t.Fatalf("override leaked to another post: %+v", s)
	}
}

// A post switched off by its override enqueues nothing and, if rows were
// already queued, skips them; a post switched ON under a disabled account
// runs alone.
func TestEngine_PostOverrideEnablesAndDisables(t *testing.T) {
	h := newHarness(t, smallBudget())
	h.seed(3)
	off := false
	_ = h.settings.SaveOverride(context.Background(), &ca.ContainerOverride{
		WorkspaceID: "ws-1", Source: ca.SourceInstagram, AccountID: "acc-1", ContainerID: "media-1", Enabled: &off,
	})
	res, err := h.engine.ProcessContainer(context.Background(), ref(), "ws-1", newCycle())
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 3 || len(h.classifier.Calls) != 0 {
		t.Fatalf("a post switched off must skip, not classify: %+v calls=%d", res, len(h.classifier.Calls))
	}

	// Flip: account off, this post on.
	account := enabledSettings()
	account.Enabled = false
	_ = h.settings.Save(context.Background(), account)
	on := true
	instructions := "post sobre a obra da Rua A"
	_ = h.settings.SaveOverride(context.Background(), &ca.ContainerOverride{
		WorkspaceID: "ws-1", Source: ca.SourceInstagram, AccountID: "acc-1", ContainerID: "media-1", Enabled: &on, Instructions: &instructions,
	})
	ingest := NewIngestUseCase(h.repo, NewSettingsResolver(h.settings), h.scheduler, nil, fixedClock{now})
	h.adapter.texts["c-9"] = "ficou ótimo"
	if err := ingest.Enqueue(context.Background(), ca.IngestInput{WorkspaceID: "ws-1", Container: ref(), SubjectID: "c-9", AuthorExternalID: "u-9", Text: "ficou ótimo"}); err != nil {
		t.Fatal(err)
	}
	if h.repo.countStatus(ca.StatusPending) != 1 {
		t.Fatalf("a post switched on under a disabled account must still enqueue, pending=%d", h.repo.countStatus(ca.StatusPending))
	}
	res, err = h.engine.ProcessContainer(context.Background(), ref(), "ws-1", newCycle())
	if err != nil {
		t.Fatal(err)
	}
	if res.Analyzed != 1 || len(h.classifier.Calls) != 1 {
		t.Fatalf("result = %+v calls=%d", res, len(h.classifier.Calls))
	}
	if got := h.classifier.Calls[0].Instructions; !strings.Contains(got, "Rua A") {
		t.Fatalf("the post's instructions must reach the classifier, got %q", got)
	}
	// And a post of the same account WITHOUT an override enqueues nothing.
	other := ref()
	other.ContainerID = "media-2"
	if err := ingest.Enqueue(context.Background(), ca.IngestInput{WorkspaceID: "ws-1", Container: other, SubjectID: "c-10", AuthorExternalID: "u", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if h.repo.bySourceID("c-10") != nil {
		t.Fatal("the account is off; a post without an override must not enqueue")
	}
}

func TestBuildSystemPrompt_CarriesInstructions(t *testing.T) {
	p := BuildSystemPrompt(ca.DefaultTopicsFor(ca.VerticalGov), ca.ContainerContext{Caption: "Asfalto novo"}, "Conta da prefeitura. Este post é sobre a obra da Rua A.")
	if !strings.Contains(p, "CONTEXTO DO OPERADOR") || !strings.Contains(p, "Rua A") {
		t.Fatal("instructions missing from the prompt")
	}
	if !strings.Contains(p, "Asfalto novo") {
		t.Fatal("caption missing from the prompt")
	}
	// No instructions, no block: the prompt is not padded.
	if strings.Contains(BuildSystemPrompt(nil, ca.ContainerContext{}, "   "), "CONTEXTO DO OPERADOR") {
		t.Fatal("blank instructions must not add a block")
	}
}

// ---- per-post use cases ----

func containerHarness() (ca.GetContainerSettingsUseCase, ca.PutContainerSettingsUseCase, ca.DeleteContainerSettingsUseCase, ca.ListAccountSettingsUseCase, *fakeSettings) {
	store := newFakeSettings(enabledSettings())
	verifiers := map[ca.Source]AccountVerifier{ca.SourceInstagram: fakeVerifier{owned: map[string]string{"acc-1": "ws-1"}}}
	get, put, del, list := NewContainerSettingsUseCases(store, NewSettingsResolver(store), verifiers, fixedClock{now})
	return get, put, del, list, store
}

func TestContainerSettings_GetPutDelete(t *testing.T) {
	get, put, del, list, store := containerHarness()
	ctx := context.Background()

	view, err := get.Execute(ctx, "ws-1", ref())
	if err != nil {
		t.Fatal(err)
	}
	if view.Override != nil || !view.Effective.Enabled {
		t.Fatalf("a post without an override inherits: %+v", view)
	}

	threshold := 75
	instructions := "  post sobre o asfalto  "
	view, err = put.Execute(ctx, ca.ContainerOverride{
		WorkspaceID: "ws-1", Source: ca.SourceInstagram, AccountID: "acc-1", ContainerID: "media-1",
		SeverityThreshold: &threshold, Instructions: &instructions,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Override == nil || *view.Override.Instructions != "post sobre o asfalto" || view.Effective.ActionPolicy.SeverityThreshold != 75 {
		t.Fatalf("after put: %+v", view)
	}
	if !view.Effective.Enabled || !view.Effective.Topics.Has("saude") {
		t.Fatal("fields not overridden must still be the account's")
	}

	// An override that changes nothing is deleted, not stored.
	view, err = put.Execute(ctx, ca.ContainerOverride{WorkspaceID: "ws-1", Source: ca.SourceInstagram, AccountID: "acc-1", ContainerID: "media-1"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Override != nil || len(store.overrides) != 0 {
		t.Fatalf("an empty override must remove the row: %+v", view)
	}

	// Put again, then delete explicitly.
	_, _ = put.Execute(ctx, ca.ContainerOverride{WorkspaceID: "ws-1", Source: ca.SourceInstagram, AccountID: "acc-1", ContainerID: "media-1", SeverityThreshold: &threshold})
	view, err = del.Execute(ctx, "ws-1", ref())
	if err != nil || view.Override != nil || view.Effective.ActionPolicy.SeverityThreshold != ca.DefaultActionThreshold {
		t.Fatalf("after delete: %+v %v", view, err)
	}

	// Scoping: another workspace cannot read, write or delete.
	if _, err := get.Execute(ctx, "ws-2", ref()); !errors.Is(err, ca.ErrNotFound) {
		t.Fatalf("cross-workspace get: %v", err)
	}
	if _, err := put.Execute(ctx, ca.ContainerOverride{WorkspaceID: "ws-2", Source: ca.SourceInstagram, AccountID: "acc-1", ContainerID: "media-1", SeverityThreshold: &threshold}); !errors.Is(err, ca.ErrNotFound) {
		t.Fatalf("cross-workspace put: %v", err)
	}
	if _, err := del.Execute(ctx, "ws-2", ref()); !errors.Is(err, ca.ErrNotFound) {
		t.Fatalf("cross-workspace delete: %v", err)
	}

	accounts, err := list.Execute(ctx, "ws-1")
	if err != nil || len(accounts) != 1 || accounts[0].AccountID != "acc-1" {
		t.Fatalf("list: %v %+v", err, accounts)
	}
	if got, _ := list.Execute(ctx, "ws-2"); len(got) != 0 {
		t.Fatal("another workspace must see no accounts")
	}
}

// An unconfigured CONVERSATION container is enabled; an unconfigured COMMENT
// container is not.
//
// This is the difference between the two subjects' switches. A comment account
// is configured in the audience settings, and nothing may run before an
// operator does that. A conversation's switch lives on its channel and was
// already enforced at ingest, so requiring a second audience_settings row meant
// every conversation was enqueued and then skipped as analysis_disabled: the
// channel's own toggle appeared to do nothing, with no error anywhere.
func TestResolverDefaultsConversationsEnabledAndCommentsDisabled(t *testing.T) {
	resolver := NewSettingsResolver(newFakeSettings())

	conversation := ca.ContainerRef{
		Kind: ca.SubjectKindConversation, Source: ca.SourceWhatsApp,
		AccountID: "ws-1", ContainerID: "camp-1",
	}
	got, err := resolver.Resolve(context.Background(), conversation)
	if err != nil {
		t.Fatalf("resolving an unconfigured conversation container: %v", err)
	}
	if !got.Enabled {
		t.Error("an unconfigured conversation container must be enabled: its switch is the channel's")
	}

	comment := ca.ContainerRef{
		Kind: ca.SubjectKindComment, Source: ca.SourceInstagram,
		AccountID: "acc-1", ContainerID: "media-1",
	}
	got, err = resolver.Resolve(context.Background(), comment)
	if err != nil {
		t.Fatalf("resolving an unconfigured comment container: %v", err)
	}
	if got.Enabled {
		t.Error("an unconfigured comment container must stay disabled until configured")
	}

	// The kindless ref reads as a comment, so it keeps the safe default too.
	implicit := ca.ContainerRef{Source: ca.SourceInstagram, AccountID: "acc-1", ContainerID: "media-1"}
	got, err = resolver.Resolve(context.Background(), implicit)
	if err != nil {
		t.Fatalf("resolving a kindless ref: %v", err)
	}
	if got.Enabled {
		t.Error("a kindless ref is a comment and must stay disabled")
	}
}
