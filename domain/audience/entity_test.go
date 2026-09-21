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

func TestContainerRef_KeyRoundTrip(t *testing.T) {
	r := ref()
	if got := r.Key(); got != "instagram:acc-1:media-1" {
		t.Fatalf("Key() = %q", got)
	}
	back, err := ParseContainerKey(r.Key())
	if err != nil {
		t.Fatalf("ParseContainerKey: %v", err)
	}
	if want := r.withDefaults(); back != want {
		t.Fatalf("round trip = %+v, want %+v", back, want)
	}
}

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

	comment := ContainerRef{Kind: SubjectKindComment, Source: SourceWhatsApp, AccountID: "acc-1", ContainerID: "camp-1"}
	if comment.Key() == r.Key() {
		t.Error("comment and conversation containers share a key")
	}
}

func TestParseContainerKey_RejectsKindChannelMismatch(t *testing.T) {
	for _, k := range []string{
		"telegram:acc:post",
		"conversation:support:acc:entry",
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
	if StatusPending.IsTerminal() || StatusInFlight.IsTerminal() || StatusFailed.IsTerminal() {
		t.Error("pending, in_flight and failed are not terminal")
	}
}

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

func TestNewPending_ExcerptIsRuneCapped(t *testing.T) {
	long := strings.Repeat("ção", 100)
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
	if a.Severity != 80 {
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

func TestAnalysis_ApplyRequiresInFlight(t *testing.T) {
	a, _ := NewPending(newInput("oi"))
	err := a.Apply(good(), ActionPolicy{}, Provenance{}, now)
	if !errors.Is(err, ErrStatusTransition) {
		t.Fatalf("expected ErrStatusTransition, got %v", err)
	}
}

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

func TestAnalysis_UnclaimGivesTheAttemptBack(t *testing.T) {
	a, _ := NewPending(newInput("oi"))
	_ = a.Claim(now)
	a.Unclaim(now)
	if a.Status != StatusPending || a.Attempts != 0 {
		t.Fatalf("after unclaim: %+v", a)
	}
	a.Unclaim(now)
	if a.Status != StatusPending || a.Attempts != 0 {
		t.Fatalf("unclaim on pending must be a no-op: %+v", a)
	}
}

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

func TestAnalysis_ResetStale(t *testing.T) {
	a, _ := NewPending(newInput("oi"))
	_ = a.Claim(now)
	a.Release(ReasonDispatchInterrupted, now.Add(11*time.Minute))
	if a.Status != StatusPending || a.Attempts != 1 {
		t.Fatalf("after reset: %+v", a)
	}
}
