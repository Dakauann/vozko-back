package audience_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	ca "vozko/domain/audience"
)

type fakeDrafter struct {
	seen ca.ReplyDraftRequest
	text string
	err  error
}

func (f *fakeDrafter) Draft(_ context.Context, req ca.ReplyDraftRequest) (*ca.ReplyDraftResult, error) {
	f.seen = req
	if f.err != nil {
		return nil, f.err
	}
	return &ca.ReplyDraftResult{Text: f.text, Model: "model-x"}, nil
}

type fakeReplier struct {
	posted []string
	err    error
}

func (f *fakeReplier) ReplyToComment(_ context.Context, _, _, _, text string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.posted = append(f.posted, text)
	return "reply-1", nil
}

type fakeSettingsStore struct {
	settings map[string]*ca.Settings
	err      error
}

func (f *fakeSettingsStore) Find(_ context.Context, source ca.Source, accountID string) (*ca.Settings, error) {
	if f.err != nil {
		return nil, f.err
	}
	if s, ok := f.settings[string(source)+":"+accountID]; ok {
		return s, nil
	}
	return nil, ca.ErrNotFound
}
func (f *fakeSettingsStore) Save(context.Context, *ca.Settings) error { return nil }
func (f *fakeSettingsStore) ListEnabled(context.Context) ([]*ca.Settings, error) {
	return nil, nil
}
func (f *fakeSettingsStore) ListByWorkspace(context.Context, string) ([]*ca.Settings, error) {
	return nil, nil
}
func (f *fakeSettingsStore) FindOverride(context.Context, ca.ContainerRef) (*ca.ContainerOverride, error) {
	return nil, ca.ErrNotFound
}
func (f *fakeSettingsStore) SaveOverride(context.Context, *ca.ContainerOverride) error { return nil }
func (f *fakeSettingsStore) DeleteOverride(context.Context, ca.ContainerRef) error     { return nil }
func (f *fakeSettingsStore) ListOverrides(context.Context, ca.Source, string) ([]*ca.ContainerOverride, error) {
	return nil, nil
}

func replyFixture(t *testing.T, mode ca.ReplyMode) (*fakeRepo, *fakeSettingsStore) {
	t.Helper()
	repo := newFakeRepo()
	a, err := ca.NewPending(ca.NewInput{
		WorkspaceID: "ws-1", Container: ref(), SubjectID: "c-1",
		AuthorExternalID: "ig-99", AuthorHandle: "fulano", Text: "onde eu compro?", Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	a.ID = "row-1"
	if _, err := repo.Insert(context.Background(), a); err != nil {
		t.Fatal(err)
	}

	s := ca.NewSettings("ws-1", ca.SourceInstagram, ref().AccountID, ca.VerticalServices)
	s.ReplyPolicy = ca.ReplyPolicy{Mode: mode}
	s.Normalize()
	return repo, &fakeSettingsStore{settings: map[string]*ca.Settings{
		string(ca.SourceInstagram) + ":" + ref().AccountID: &s,
	}}
}

// An account left at its defaults drafts nothing. The check is here rather
// than in the handler, so a second caller cannot skip it.
func TestSuggestRefusesWhenTheAccountRepliesOff(t *testing.T) {
	repo, settings := replyFixture(t, ca.ReplyModeOff)
	drafter := &fakeDrafter{text: "claro, chama no direct!"}

	suggest, _ := NewReplyUseCases(ReplyDeps{Repo: repo, Settings: settings, Drafter: drafter})
	_, err := suggest.Execute(context.Background(), ca.SuggestReplyInput{WorkspaceID: "ws-1", CommentID: "row-1"})
	if !errors.Is(err, ca.ErrInvalidFilter) {
		t.Fatalf("err = %v, want ErrInvalidFilter", err)
	}
	if drafter.seen.WorkspaceID != "" {
		t.Fatal("an account with replying off must not reach the model, which is also what it costs")
	}
}

// The draft carries the account's own context, not a second prompt stack.
func TestSuggestPassesTheAccountsContext(t *testing.T) {
	repo, settings := replyFixture(t, ca.ReplyModeSuggest)
	for _, s := range settings.settings {
		s.Instructions = "somos uma clínica odontológica"
		s.Model = "model-y"
	}
	drafter := &fakeDrafter{text: "  claro, chama no direct!  "}
	adapter := &fakeAdapter{caption: "promoção de setembro", texts: map[string]string{"c-1": "onde eu compro?"}}

	suggest, _ := NewReplyUseCases(ReplyDeps{
		Repo: repo, Settings: settings, Drafter: drafter,
		Adapters: map[ca.Source]ca.SourceAdapter{ca.SourceInstagram: adapter},
	})
	out, err := suggest.Execute(context.Background(), ca.SuggestReplyInput{WorkspaceID: "ws-1", CommentID: "row-1"})
	if err != nil {
		t.Fatal(err)
	}
	if drafter.seen.Instructions != "somos uma clínica odontológica" || drafter.seen.Model != "model-y" {
		t.Fatalf("request = %+v", drafter.seen)
	}
	if drafter.seen.Caption != "promoção de setembro" {
		t.Fatalf("the post's caption must reach the draft: %q", drafter.seen.Caption)
	}
	// Read back from the channel, which owns the words; the engine stores an
	// excerpt only.
	if drafter.seen.Comment != "onde eu compro?" {
		t.Fatalf("comment = %q", drafter.seen.Comment)
	}
	if out.Text != "claro, chama no direct!" {
		t.Fatalf("draft = %q, must be trimmed by the value object", out.Text)
	}
}

// Drafting is not posting. Nothing in the suggest path may reach a channel.
func TestSuggestNeverPosts(t *testing.T) {
	repo, settings := replyFixture(t, ca.ReplyModeAuto)
	replier := &fakeReplier{}

	suggest, _ := NewReplyUseCases(ReplyDeps{
		Repo: repo, Settings: settings, Drafter: &fakeDrafter{text: "oi"},
		Repliers: map[ca.Source]ca.CommentReplier{ca.SourceInstagram: replier},
	})
	if _, err := suggest.Execute(context.Background(), ca.SuggestReplyInput{WorkspaceID: "ws-1", CommentID: "row-1"}); err != nil {
		t.Fatal(err)
	}
	if len(replier.posted) != 0 {
		t.Fatal("a draft must never reach the channel")
	}
}

func TestPostPublishesTheOperatorsText(t *testing.T) {
	repo, settings := replyFixture(t, ca.ReplyModeSuggest)
	replier := &fakeReplier{}

	_, post := NewReplyUseCases(ReplyDeps{
		Repo: repo, Settings: settings,
		Repliers: map[ca.Source]ca.CommentReplier{ca.SourceInstagram: replier},
	})
	out, err := post.Execute(context.Background(), ca.PostReplyInput{
		WorkspaceID: "ws-1", CommentID: "row-1", Text: "  chama no direct!  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(replier.posted) != 1 || replier.posted[0] != "chama no direct!" {
		t.Fatalf("posted = %v", replier.posted)
	}
	if out.Text != "chama no direct!" {
		t.Fatalf("out = %+v", out)
	}
}

// The same bound a draft gets: an operator cannot publish something longer
// than the domain would ever have drafted.
func TestPostBoundsTheText(t *testing.T) {
	repo, settings := replyFixture(t, ca.ReplyModeSuggest)
	replier := &fakeReplier{}

	_, post := NewReplyUseCases(ReplyDeps{
		Repo: repo, Settings: settings,
		Repliers: map[ca.Source]ca.CommentReplier{ca.SourceInstagram: replier},
	})
	if _, err := post.Execute(context.Background(), ca.PostReplyInput{
		WorkspaceID: "ws-1", CommentID: "row-1", Text: strings.Repeat("x", ca.MaxReplyLength+300),
	}); err != nil {
		t.Fatal(err)
	}
	if len(replier.posted[0]) > ca.MaxReplyLength {
		t.Fatalf("posted %d characters", len(replier.posted[0]))
	}
}

func TestPostRefusesAnEmptyText(t *testing.T) {
	repo, settings := replyFixture(t, ca.ReplyModeSuggest)
	replier := &fakeReplier{}

	_, post := NewReplyUseCases(ReplyDeps{
		Repo: repo, Settings: settings,
		Repliers: map[ca.Source]ca.CommentReplier{ca.SourceInstagram: replier},
	})
	if _, err := post.Execute(context.Background(), ca.PostReplyInput{
		WorkspaceID: "ws-1", CommentID: "row-1", Text: "   ",
	}); !errors.Is(err, ca.ErrInvalidFilter) {
		t.Fatalf("err = %v", err)
	}
	if len(replier.posted) != 0 {
		t.Fatal("nothing may be posted")
	}
}

// A channel with no replier configured refuses, rather than reporting success
// for a reply that was never published.
func TestPostRefusesWithoutAReplier(t *testing.T) {
	repo, settings := replyFixture(t, ca.ReplyModeSuggest)
	_, post := NewReplyUseCases(ReplyDeps{Repo: repo, Settings: settings})
	if _, err := post.Execute(context.Background(), ca.PostReplyInput{
		WorkspaceID: "ws-1", CommentID: "row-1", Text: "oi",
	}); !errors.Is(err, ca.ErrInvalidFilter) {
		t.Fatalf("err = %v", err)
	}
}

// A comment of another workspace is not answerable from here.
func TestReplyIsWorkspaceScoped(t *testing.T) {
	repo, settings := replyFixture(t, ca.ReplyModeSuggest)
	replier := &fakeReplier{}

	suggest, post := NewReplyUseCases(ReplyDeps{
		Repo: repo, Settings: settings, Drafter: &fakeDrafter{text: "oi"},
		Repliers: map[ca.Source]ca.CommentReplier{ca.SourceInstagram: replier},
	})
	if _, err := suggest.Execute(context.Background(), ca.SuggestReplyInput{WorkspaceID: "ws-2", CommentID: "row-1"}); !errors.Is(err, ca.ErrNotFound) {
		t.Fatalf("suggest err = %v, want ErrNotFound", err)
	}
	if _, err := post.Execute(context.Background(), ca.PostReplyInput{WorkspaceID: "ws-2", CommentID: "row-1", Text: "oi"}); !errors.Is(err, ca.ErrNotFound) {
		t.Fatalf("post err = %v, want ErrNotFound", err)
	}
	if len(replier.posted) != 0 {
		t.Fatal("nothing may be posted for another workspace's comment")
	}
}

// An account nobody configured has the disabled defaults, so a missing
// settings row means "does not reply" rather than "replies with defaults".
func TestReplyTreatsAnUnconfiguredAccountAsOff(t *testing.T) {
	repo, _ := replyFixture(t, ca.ReplyModeSuggest)
	empty := &fakeSettingsStore{settings: map[string]*ca.Settings{}}

	suggest, _ := NewReplyUseCases(ReplyDeps{Repo: repo, Settings: empty, Drafter: &fakeDrafter{text: "oi"}})
	if _, err := suggest.Execute(context.Background(), ca.SuggestReplyInput{WorkspaceID: "ws-1", CommentID: "row-1"}); !errors.Is(err, ca.ErrInvalidFilter) {
		t.Fatalf("err = %v, want a refusal", err)
	}
}
