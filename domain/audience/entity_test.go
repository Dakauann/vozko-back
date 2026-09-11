package audience

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/shared"
)

var now = time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

func ref() ContainerRef {
	return ContainerRef{Source: SourceInstagram, AccountID: "acc-1", ContainerID: "media-1"}
}

func newInput(text string) NewInput {
	return NewInput{
		WorkspaceID:      "ws-1",
		Container:        ref(),
		SubjectID:        "c-1",
		AuthorExternalID: "igsid-1",
		AuthorHandle:     "maria",
		Text:             text,
		Now:              now,
	}
}

func good() Classification {
	return Classification{
		Sentiment:      shared.SentimentNegative,
		Stance:         StanceCritic,
		Intent:         IntentComplaint,
		TopicKey:       TopicKeyOther,
		Language:       "pt",
		Toxicity:       shared.QualityLevelLow,
		PersonalAttack: shared.QualityLevelNone,
		LegalRisk:      shared.QualityLevelNone,
	}
}

// ---- Source / ContainerRef ----

func TestSource_Valid(t *testing.T) {
	if !SourceInstagram.Valid() {
		t.Error("instagram should be a valid source")
	}
	for _, s := range []Source{"", "Instagram", "facebook", "instagram "} {
		if s.Valid() {
			t.Errorf("%q should be invalid", s)
		}
	}
}

// The key is the Redis debounce field, the per-container lock name and the
// backstop's grouping key, so its format is pinned and it must round-trip.
func TestContainerRef_KeyRoundTrip(t *testing.T) {
	r := ref()
	if got := r.Key(); got != "instagram:acc-1:media-1" {
		t.Fatalf("Key() = %q", got)
	}
	back, err := ParseContainerKey(r.Key())
	if err != nil {
		t.Fatalf("ParseContainerKey: %v", err)
	}
	// Parsing resolves the implicit kind, so the round trip is compared against
	// the normalised ref. The key format itself is unchanged, which is what the
	// assertion above pins: the debounce entries already in Redis still parse.
	if want := r.withDefaults(); back != want {
		t.Fatalf("round trip = %+v, want %+v", back, want)
	}
}

// A conversation container is keyed with its kind in front, so a post id and a
// campaign id can never land on the same lock.
func TestContainerRef_ConversationKeyRoundTrip(t *testing.T) {
	r := ContainerRef{
		Kind: SubjectKindConversation, Source: SourceWhatsApp,
		AccountID: "acc-1", ContainerID: "camp-1",
	}
	if got := r.Key(); got != "conversation:whatsapp:acc-1:camp-1" {
		t.Fatalf("Key() = %q", got)
	}
	back, err := ParseContainerKey(r.Key())
	if err != nil {
		t.Fatalf("ParseContainerKey: %v", err)
	}
	if back != r {
		t.Fatalf("round trip = %+v, want %+v", back, r)
	}

	// The two kinds on the same ids must not collide.
	comment := ContainerRef{Kind: SubjectKindComment, Source: SourceWhatsApp, AccountID: "acc-1", ContainerID: "camp-1"}
	if comment.Key() == r.Key() {
		t.Error("comment and conversation containers share a key")
	}
}

// A kind only parses where that subject exists on that channel: Telegram has no
// posts, so a Telegram comment key is not a thing however analysable Telegram
// is as a conversation.
func TestParseContainerKey_RejectsKindChannelMismatch(t *testing.T) {
	for _, k := range []string{
		"telegram:acc:post",              // comment on a channel with no posts
		"conversation:support:acc:entry", // support is never analysed
	} {
		if _, err := ParseContainerKey(k); err == nil {
			t.Errorf("ParseContainerKey(%q) should fail", k)
		}
	}
	for _, k := range []string{
		"instagram:acc:post",
		"conversation:telegram:acc:chat",
		"conversation:instagram:acc:camp",
	} {
		if _, err := ParseContainerKey(k); err != nil {
			t.Errorf("ParseContainerKey(%q) should succeed: %v", k, err)
		}
	}
}

func TestParseContainerKey_Rejects(t *testing.T) {
	for _, k := range []string{"", "instagram", "instagram:acc", "facebook:a:b", "instagram::b", "instagram:a:", ":a:b"} {
		if _, err := ParseContainerKey(k); err == nil {
			t.Errorf("ParseContainerKey(%q) should fail", k)
		}
	}
}

func TestContainerRef_Validate(t *testing.T) {
	if err := ref().Validate(); err != nil {
		t.Fatalf("valid ref rejected: %v", err)
	}
	cases := map[string]ContainerRef{
		"bad source":   {Source: "x", AccountID: "a", ContainerID: "b"},
		"no account":   {Source: SourceInstagram, ContainerID: "b"},
		"no container": {Source: SourceInstagram, AccountID: "a"},
	}
	for name, r := range cases {
		if err := r.Validate(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

// ---- Status machine ----

func TestStatus_Valid(t *testing.T) {
	for _, s := range []Status{StatusPending, StatusInFlight, StatusAnalyzed, StatusFailed, StatusSkipped} {
		if !s.Valid() {
			t.Errorf("%q should be valid", s)
		}
	}
	if Status("done").Valid() || Status("").Valid() {
		t.Error("unknown status should be invalid")
	}
}

// The transitions are the concurrency contract of §8: a row leaves pending
// exactly once per attempt, comes back only through a reconcile/reset or a
// manual retry, and never leaves analyzed.
func TestStatus_CanTransitionTo(t *testing.T) {
	allowed := map[Status][]Status{
		StatusPending:  {StatusInFlight, StatusFailed, StatusSkipped},
		StatusInFlight: {StatusAnalyzed, StatusPending, StatusFailed},
		StatusFailed:   {StatusPending},
		StatusAnalyzed: {},
		StatusSkipped:  {},
	}
	all := []Status{StatusPending, StatusInFlight, StatusAnalyzed, StatusFailed, StatusSkipped}
	for from, tos := range allowed {
		ok := map[Status]bool{}
		for _, to := range tos {
			ok[to] = true
		}
		for _, to := range all {
			if got := from.CanTransitionTo(to); got != ok[to] {
				t.Errorf("%s -> %s = %v, want %v", from, to, got, ok[to])
			}
		}
		if from.CanTransitionTo("bogus") {
			t.Errorf("%s -> bogus must be rejected", from)
		}
	}
}

func TestStatus_IsTerminal(t *testing.T) {
	if !StatusAnalyzed.IsTerminal() || !StatusSkipped.IsTerminal() {
		t.Error("analyzed and skipped are terminal")
	}
	// failed is NOT terminal: the retry endpoint re-queues it.
	if StatusPending.IsTerminal() || StatusInFlight.IsTerminal() || StatusFailed.IsTerminal() {
		t.Error("pending, in_flight and failed are not terminal")
	}
}

// ---- Enums ----

func TestStance_Valid(t *testing.T) {
	for _, s := range []Stance{StanceSupporter, StanceNeutral, StanceCritic, StanceHostile} {
		if !s.Valid() {
			t.Errorf("%q should be valid", s)
		}
	}
	if Stance("hater").Valid() || Stance("").Valid() {
		t.Error("unknown stance should be invalid")
	}
}

func TestIntent_Valid(t *testing.T) {
	all := []Intent{IntentPraise, IntentQuestion, IntentComplaint, IntentSupportRequest, IntentSpam, IntentSalesLead, IntentOther}
	for _, i := range all {
		if !i.Valid() {
			t.Errorf("%q should be valid", i)
		}
	}
	if Intent("lead").Valid() || Intent("").Valid() {
		t.Error("unknown intent should be invalid")
	}
}

// ---- NewPending ----

func TestNewPending(t *testing.T) {
	a, err := NewPending(newInput("Que lindo esse asfalto novo!"))
	if err != nil {
		t.Fatalf("NewPending: %v", err)
	}
	if a.Status != StatusPending {
		t.Errorf("status = %q, want pending", a.Status)
	}
	if a.Source != SourceInstagram || a.AccountID != "acc-1" || a.ContainerID != "media-1" {
		t.Errorf("container not copied: %+v", a)
	}
	if a.Excerpt != "Que lindo esse asfalto novo!" {
		t.Errorf("excerpt = %q", a.Excerpt)
	}
	if a.Attempts != 0 || a.Severity != 0 || a.RequiresAction {
		t.Errorf("fresh row must carry no classification: %+v", a)
	}
	if !a.CreatedAt.Equal(now) || !a.UpdatedAt.Equal(now) {
		t.Errorf("timestamps must come from the input clock")
	}
	if !a.OccurredAt.Equal(now) {
		t.Errorf("an unknown comment time falls back to now, got %v", a.OccurredAt)
	}
}

// Rollups bucket by when the comment was POSTED. A backfilled comment from
// March must land on March's day, not on the day it was ingested.
func TestNewPending_KeepsOccurredAt(t *testing.T) {
	in := newInput("oi")
	posted := now.Add(-90 * 24 * time.Hour)
	in.OccurredAt = posted
	a, err := NewPending(in)
	if err != nil {
		t.Fatal(err)
	}
	if !a.OccurredAt.Equal(posted) {
		t.Fatalf("OccurredAt = %v, want %v", a.OccurredAt, posted)
	}
	if !a.CreatedAt.Equal(now) {
		t.Fatal("CreatedAt is the ingest time and must not follow OccurredAt")
	}
}

// Comment bodies are never copied (§5.1). The excerpt is what the dashboard
// row shows, capped in RUNES so a cut never lands mid-character in pt-BR.
func TestNewPending_ExcerptIsRuneCapped(t *testing.T) {
	long := strings.Repeat("ção", 100) // 300 runes, 500 bytes
	a, err := NewPending(newInput(long))
	if err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(a.Excerpt)); n != ExcerptMaxRunes {
		t.Fatalf("excerpt has %d runes, want %d", n, ExcerptMaxRunes)
	}
	if !strings.HasSuffix(a.Excerpt, "ção") && !strings.HasSuffix(a.Excerpt, "çã") && !strings.HasSuffix(a.Excerpt, "ç") {
		t.Fatalf("excerpt must end on a whole rune: %q", a.Excerpt[len(a.Excerpt)-6:])
	}
	if !a.Truncated {
		t.Fatal("a cut excerpt must not pretend to be whole")
	}
}

// A blank comment (emoji stripped by the channel, or empty text) has nothing
// to classify. It is recorded so the totals stay honest, but never sent to
// the model.
func TestNewPending_BlankTextIsSkipped(t *testing.T) {
	a, err := NewPending(newInput("   \n "))
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != StatusSkipped {
		t.Fatalf("status = %q, want skipped", a.Status)
	}
	if a.FailureReason != "" {
		t.Fatal("skipped is a decision, not a failure")
	}
}

func TestNewPending_Validation(t *testing.T) {
	cases := map[string]func(*NewInput){
		"workspace required":  func(in *NewInput) { in.WorkspaceID = "" },
		"comment id required": func(in *NewInput) { in.SubjectID = "" },
		"bad container":       func(in *NewInput) { in.Container.Source = "x" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := newInput("oi")
			mutate(&in)
			if _, err := NewPending(in); err == nil {
				t.Error("expected error")
			}
		})
	}
}

// ---- Classification / Apply ----

func TestClassification_Validate(t *testing.T) {
	topics := DefaultTopicsFor(VerticalGov)
	c := good()
	c.TopicKey = "saude"
	if err := c.Validate(topics); err != nil {
		t.Fatalf("good classification rejected: %v", err)
	}

	cases := map[string]func(*Classification){
		"bad sentiment": func(c *Classification) { c.Sentiment = "meh" },
		"bad stance":    func(c *Classification) { c.Stance = "hater" },
		"bad intent":    func(c *Classification) { c.Intent = "lead" },
		"topic not in set": func(c *Classification) {
			c.TopicKey = "not-a-topic"
		},
		"bad toxicity":        func(c *Classification) { c.Toxicity = "extreme" },
		"bad personal attack": func(c *Classification) { c.PersonalAttack = "yes" },
		"bad legal risk":      func(c *Classification) { c.LegalRisk = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := good()
			mutate(&c)
			if err := c.Validate(topics); !errors.Is(err, ErrInvalidClassification) {
				t.Errorf("expected ErrInvalidClassification, got %v", err)
			}
		})
	}
}

// Topic keys arrive from the model exactly as the schema enum spelled them,
// but a rubric edit or a lenient provider can hand back the label instead.
// Folding on validate is what keeps "Saúde Pública" from forking the topic.
func TestClassification_ValidateFoldsTopic(t *testing.T) {
	topics := TopicSet{{Key: "saude-publica", Label: "Saúde Pública"}, OtherTopic()}
	c := good()
	c.TopicKey = "Saúde Pública"
	if err := c.Validate(topics); err != nil {
		t.Fatalf("label spelling should resolve to its key: %v", err)
	}
	if c.TopicKey != "saude-publica" {
		t.Fatalf("TopicKey = %q, want the canonical key", c.TopicKey)
	}
}

// Severity is COMPUTED from the three ordinal dimensions with the rubric
// weights (toxicity 45, personal attack 35, legal risk 20), never model-set.
func TestClassification_Severity(t *testing.T) {
	cases := []struct {
		name          string
		tox, att, leg shared.QualityLevel
		want          int
	}{
		{"all none", shared.QualityLevelNone, shared.QualityLevelNone, shared.QualityLevelNone, 0},
		{"all high", shared.QualityLevelHigh, shared.QualityLevelHigh, shared.QualityLevelHigh, 100},
		{"toxicity high only", shared.QualityLevelHigh, shared.QualityLevelNone, shared.QualityLevelNone, 45},
		{"attack high only", shared.QualityLevelNone, shared.QualityLevelHigh, shared.QualityLevelNone, 35},
		{"legal high only", shared.QualityLevelNone, shared.QualityLevelNone, shared.QualityLevelHigh, 20},
		// 0.45*0.66 + 0.35*0.33 + 0.20*1.0 = 0.6125 -> 61
		{"mixed", shared.QualityLevelMedium, shared.QualityLevelLow, shared.QualityLevelHigh, 61},
		{"garbage rates as none", "extreme", shared.QualityLevelHigh, shared.QualityLevelNone, 35},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Classification{Toxicity: tc.tox, PersonalAttack: tc.att, LegalRisk: tc.leg}
			if got := c.Severity(); got != tc.want {
				t.Errorf("Severity() = %d, want %d", got, tc.want)
			}
		})
	}
}

// RequiresAction fires on severity OR on an intent that deserves a reply. A
// pure-severity trigger would miss "onde compro?", a sales lead sitting
// unanswered under a post.
func TestRequiresAction(t *testing.T) {
	policy := ActionPolicy{SeverityThreshold: 60}
	cases := []struct {
		name     string
		severity int
		intent   Intent
		want     bool
	}{
		{"low severity praise", 0, IntentPraise, false},
		{"low severity question", 0, IntentQuestion, true},
		{"low severity complaint", 10, IntentComplaint, true},
		{"low severity support request", 10, IntentSupportRequest, true},
		{"low severity sales lead", 0, IntentSalesLead, false},
		{"low severity spam", 0, IntentSpam, false},
		{"at threshold praise", 60, IntentPraise, true},
		{"just under threshold other", 59, IntentOther, false},
		{"high severity spam", 90, IntentSpam, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := policy.RequiresAction(tc.severity, tc.intent); got != tc.want {
				t.Errorf("RequiresAction(%d, %s) = %v, want %v", tc.severity, tc.intent, got, tc.want)
			}
		})
	}
}

func TestActionPolicy_Normalize(t *testing.T) {
	var p ActionPolicy
	p.Normalize()
	if p.SeverityThreshold != DefaultActionThreshold {
		t.Fatalf("zero threshold should default to %d, got %d", DefaultActionThreshold, p.SeverityThreshold)
	}
	p = ActionPolicy{SeverityThreshold: 500}
	p.Normalize()
	if p.SeverityThreshold != 100 {
		t.Fatalf("threshold must be clamped to 100, got %d", p.SeverityThreshold)
	}
}

func TestAnalysis_Apply(t *testing.T) {
	a, _ := NewPending(newInput("esse prefeito é um ladrão"))
	if err := a.Claim(now); err != nil {
		t.Fatal(err)
	}
	c := good()
	c.Toxicity = shared.QualityLevelHigh
	c.PersonalAttack = shared.QualityLevelHigh
	c.Stance = StanceHostile
	c.Intent = IntentOther
	later := now.Add(time.Minute)

	if err := a.Apply(c, ActionPolicy{SeverityThreshold: 60}, Provenance{BatchID: "b-1", Model: "m"}, later); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if a.Status != StatusAnalyzed {
		t.Errorf("status = %q", a.Status)
	}
	if a.Severity != 80 { // 0.45 + 0.35
		t.Errorf("severity = %d, want 80", a.Severity)
	}
	if !a.RequiresAction {
		t.Error("severity 80 must require action")
	}
	if a.Stance != StanceHostile || a.Sentiment != shared.SentimentNegative || a.TopicKey != TopicKeyOther {
		t.Errorf("labels not applied: %+v", a)
	}
	if a.BatchID != "b-1" || a.Model != "m" || a.AnalyzedAt == nil || !a.AnalyzedAt.Equal(later) {
		t.Errorf("provenance not recorded: %+v", a)
	}
	if !a.UpdatedAt.Equal(later) {
		t.Error("UpdatedAt must advance")
	}
}

// Apply is only legal from in_flight: a result for a row that was never
// claimed is a result for a row another replica owns.
func TestAnalysis_ApplyRequiresInFlight(t *testing.T) {
	a, _ := NewPending(newInput("oi"))
	err := a.Apply(good(), ActionPolicy{}, Provenance{}, now)
	if !errors.Is(err, ErrStatusTransition) {
		t.Fatalf("expected ErrStatusTransition, got %v", err)
	}
}

// ---- Claim / Release / Fail / Retry ----

func TestAnalysis_ClaimIncrementsAttempts(t *testing.T) {
	a, _ := NewPending(newInput("oi"))
	if err := a.Claim(now); err != nil {
		t.Fatal(err)
	}
	if a.Status != StatusInFlight || a.Attempts != 1 {
		t.Fatalf("after claim: %+v", a)
	}
	if err := a.Claim(now); !errors.Is(err, ErrStatusTransition) {
		t.Fatalf("double claim must fail, got %v", err)
	}
}

// Release is the reconcile path: a missing ref goes back to pending and is
// retried, until MaxAttempts turns it into a visible failure: never a
// silent drop and never an infinite loop.
func TestAnalysis_ReleaseUntilMaxAttempts(t *testing.T) {
	a, _ := NewPending(newInput("oi"))
	for i := 1; i < MaxAttempts; i++ {
		if err := a.Claim(now); err != nil {
			t.Fatal(err)
		}
		a.Release("missing ref", now)
		if a.Status != StatusPending {
			t.Fatalf("attempt %d: status = %q, want pending", i, a.Status)
		}
	}
	if err := a.Claim(now); err != nil {
		t.Fatal(err)
	}
	a.Release("missing ref", now)
	if a.Status != StatusFailed {
		t.Fatalf("after %d attempts status = %q, want failed", MaxAttempts, a.Status)
	}
	if a.FailureReason != "missing ref" {
		t.Fatalf("failure reason = %q", a.FailureReason)
	}
}

func TestAnalysis_Fail(t *testing.T) {
	a, _ := NewPending(newInput("oi"))
	_ = a.Claim(now)
	if err := a.Fail("provider refused", now); err != nil {
		t.Fatal(err)
	}
	if a.Status != StatusFailed || a.FailureReason != "provider refused" {
		t.Fatalf("after fail: %+v", a)
	}
}

// The retry endpoint resets attempts so the row gets a full set of tries
// again; the operator asked for it explicitly.
func TestAnalysis_Retry(t *testing.T) {
	a, _ := NewPending(newInput("oi"))
	_ = a.Claim(now)
	_ = a.Fail("boom", now)
	if err := a.Retry(now); err != nil {
		t.Fatal(err)
	}
	if a.Status != StatusPending || a.Attempts != 0 || a.FailureReason != "" {
		t.Fatalf("after retry: %+v", a)
	}
	if err := a.Retry(now); !errors.Is(err, ErrStatusTransition) {
		t.Fatal("retrying a pending row is not a transition")
	}
}

// A provider outage is not the comment's fault. Unclaim hands the row back
// WITHOUT the attempt, so three ticks of outage do not turn every pending
// comment into a failure.
func TestAnalysis_UnclaimGivesTheAttemptBack(t *testing.T) {
	a, _ := NewPending(newInput("oi"))
	_ = a.Claim(now)
	a.Unclaim(now)
	if a.Status != StatusPending || a.Attempts != 0 {
		t.Fatalf("after unclaim: %+v", a)
	}
	// Only an in_flight row can be unclaimed; anything else is a no-op.
	a.Unclaim(now)
	if a.Status != StatusPending || a.Attempts != 0 {
		t.Fatalf("unclaim on pending must be a no-op: %+v", a)
	}
}

// A comment whose text is gone by flush time (deleted on the channel after
// ingest) has nothing to classify. It is skipped, visibly, not failed.
func TestAnalysis_MarkSkipped(t *testing.T) {
	a, _ := NewPending(newInput("oi"))
	if err := a.MarkSkipped(ReasonTextUnavailable, now); err != nil {
		t.Fatal(err)
	}
	if a.Status != StatusSkipped || a.FailureReason != ReasonTextUnavailable {
		t.Fatalf("after skip: %+v", a)
	}
	if err := a.MarkSkipped(ReasonTextUnavailable, now); !errors.Is(err, ErrStatusTransition) {
		t.Fatal("skipped is terminal")
	}
}

// A deleted comment's analysis is tombstoned, not removed: it leaves the feed
// and the live stats but stays in the rollups that already counted it.
func TestAnalysis_SoftDelete(t *testing.T) {
	a, _ := NewPending(newInput("oi"))
	a.SoftDelete(now)
	if a.DeletedAt == nil || !a.DeletedAt.Equal(now) {
		t.Fatalf("DeletedAt not set: %+v", a)
	}
	later := now.Add(time.Hour)
	a.SoftDelete(later)
	if !a.DeletedAt.Equal(now) {
		t.Fatal("a second delete must not move the tombstone")
	}
}

// A crash between claim and apply leaves a row in_flight forever unless the
// backstop resets it. Attempts were already counted at claim, so a crash loop
// still terminates.
func TestAnalysis_ResetStale(t *testing.T) {
	a, _ := NewPending(newInput("oi"))
	_ = a.Claim(now)
	a.Release(ReasonDispatchInterrupted, now.Add(11*time.Minute))
	if a.Status != StatusPending || a.Attempts != 1 {
		t.Fatalf("after reset: %+v", a)
	}
}
