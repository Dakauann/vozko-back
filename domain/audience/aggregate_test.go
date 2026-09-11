package audience

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"vozko/domain/shared"
)

// ---- Counters / Stats ----

func TestCounters_StanceMix(t *testing.T) {
	c := Counters{StanceSupporter: 1, StanceNeutral: 2, StanceCritic: 3, StanceHostile: 4}
	if got := c.StanceMix(); got != (StanceMix{1, 2, 3, 4}) {
		t.Fatalf("StanceMix() = %+v", got)
	}
}

// The repository fills the counters; Finalize derives the score so a chart
// and the number above it are the same arithmetic.
func TestStats_Finalize(t *testing.T) {
	s := Stats{Counters: Counters{Analyzed: 30, StanceSupporter: 30}}
	s.Finalize()
	if s.AcceptanceScore != 100 {
		t.Fatalf("AcceptanceScore = %d, want 100", s.AcceptanceScore)
	}
	s = Stats{Counters: Counters{Analyzed: 30, StanceSupporter: 30, SeverityHighCount: 30}}
	s.Finalize()
	if s.AcceptanceScore != 50 {
		t.Fatalf("AcceptanceScore with full high-severity share = %d, want 50", s.AcceptanceScore)
	}
	if !s.Finalized {
		t.Fatal("Finalize must mark the stats")
	}
}

// Rollups carry the same counters and the same derived score; a trend
// point cannot disagree with the live number for the same slice.
func TestRollup_Finalize(t *testing.T) {
	r := Rollup{Counters: Counters{Analyzed: 30, StanceHostile: 30, SeverityHighCount: 30}}
	r.Finalize()
	if r.AcceptanceScore != 0 {
		t.Fatalf("AcceptanceScore = %d, want 0", r.AcceptanceScore)
	}
}

func TestRollupScope_Valid(t *testing.T) {
	for _, s := range []RollupScope{ScopeAccount, ScopeContainer, ScopeTopic} {
		if !s.Valid() {
			t.Errorf("%q should be valid", s)
		}
	}
	if RollupScope("workspace").Valid() || RollupScope("").Valid() {
		t.Error("unknown scope should be invalid")
	}
}

// Buckets are calendar days in UTC. Every stored instant is UTC; a local
// bucket would shift a day's comments by the server's offset.
func TestBucketDate(t *testing.T) {
	in := time.Date(2026, 9, 2, 23, 59, 59, 0, time.FixedZone("BRT", -3*3600)) // 02:59:59 UTC on the 3rd
	got := BucketDate(in)
	want := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("BucketDate = %v, want %v", got, want)
	}
	if got.Location() != time.UTC {
		t.Fatal("bucket must be UTC")
	}
}

// ---- Authors ----

func TestAuthorStats_Derive(t *testing.T) {
	a := AuthorStats{Counters: Counters{StanceHostile: 3, SeverityHighCount: 3}}
	a.Derive()
	if a.DerivedStance != StanceHostile || !a.IsFlagged {
		t.Fatalf("three hostile, three high-severity → %+v", a)
	}
	a = AuthorStats{Counters: Counters{StanceHostile: 1, SeverityHighCount: 1}}
	a.Derive()
	if a.DerivedStance != StanceCritic || a.IsFlagged {
		t.Fatalf("one hostile comment → %+v", a)
	}
}

func TestModerationState_Valid(t *testing.T) {
	for _, s := range []ModerationState{ModerationNone, ModerationWatched, ModerationMuted, ModerationBlocked} {
		if !s.Valid() {
			t.Errorf("%q should be valid", s)
		}
	}
	if ModerationState("banned").Valid() || ModerationState("").Valid() {
		t.Error("unknown state should be invalid")
	}
}

// ---- Filters ----

func TestListInput_Validate(t *testing.T) {
	ok := ListInput{WorkspaceID: "ws-1"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("minimal input should validate: %v", err)
	}
	cases := map[string]ListInput{
		"workspace required":    {},
		"bad source":            {WorkspaceID: "ws-1", Source: "x"},
		"bad status":            {WorkspaceID: "ws-1", Statuses: []Status{"done"}},
		"bad stance":            {WorkspaceID: "ws-1", Stance: "hater"},
		"bad sentiment":         {WorkspaceID: "ws-1", Sentiment: "meh"},
		"bad intent":            {WorkspaceID: "ws-1", Intent: "lead"},
		"severity out of range": {WorkspaceID: "ws-1", SeverityMin: intp(-1)},
		"severity inverted":     {WorkspaceID: "ws-1", SeverityMin: intp(80), SeverityMax: intp(20)},
		"range inverted":        {WorkspaceID: "ws-1", From: timep(now), To: timep(now.Add(-time.Hour))},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if err := in.Validate(); !errors.Is(err, ErrInvalidFilter) {
				t.Errorf("expected ErrInvalidFilter, got %v", err)
			}
		})
	}
}

func TestListInput_Normalize(t *testing.T) {
	in := ListInput{WorkspaceID: " ws-1 ", AccountID: " a ", ContainerID: " c ", TopicKey: "Saúde Pública", AuthorExternalID: " u "}
	in.Normalize()
	if in.WorkspaceID != "ws-1" || in.AccountID != "a" || in.ContainerID != "c" || in.AuthorExternalID != "u" {
		t.Errorf("ids not trimmed: %+v", in)
	}
	if in.TopicKey != "saude-publica" {
		t.Errorf("topic filter must be normalised like a topic key, got %q", in.TopicKey)
	}
	if in.Options.Pagination.PageSize != shared.DefaultPageSize {
		t.Errorf("pagination not normalised: %+v", in.Options.Pagination)
	}
}

func TestTrendInput_Validate(t *testing.T) {
	ok := TrendInput{WorkspaceID: "ws-1", Scope: ScopeAccount, ScopeID: "a", From: now.Add(-7 * 24 * time.Hour), To: now}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid trend input rejected: %v", err)
	}
	cases := map[string]func(*TrendInput){
		"workspace required": func(in *TrendInput) { in.WorkspaceID = "" },
		"scope required":     func(in *TrendInput) { in.Scope = "" },
		"scope id required":  func(in *TrendInput) { in.ScopeID = "" },
		"range inverted":     func(in *TrendInput) { in.From, in.To = in.To, in.From },
		"range too long":     func(in *TrendInput) { in.From = in.To.Add(-(MaxTrendRange + time.Hour)) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := ok
			mutate(&in)
			if err := in.Validate(); !errors.Is(err, ErrInvalidFilter) {
				t.Errorf("expected ErrInvalidFilter, got %v", err)
			}
		})
	}
}

func TestAuthorsInput_Validate(t *testing.T) {
	ok := AuthorsInput{WorkspaceID: "ws-1"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("minimal input should validate: %v", err)
	}
	bad := AuthorsInput{WorkspaceID: "ws-1", ModerationState: "banned"}
	if err := bad.Validate(); !errors.Is(err, ErrInvalidFilter) {
		t.Fatalf("expected ErrInvalidFilter, got %v", err)
	}
}

// ---- Settings ----

func TestSettings_NormalizeAndValidate(t *testing.T) {
	s := Settings{WorkspaceID: "ws-1", Source: SourceInstagram, AccountID: "acc-1"}
	s.Normalize()
	if !s.Topics.Has(TopicKeyOther) {
		t.Error("an empty topic set must be seeded with other")
	}
	if s.ActionPolicy.SeverityThreshold != DefaultActionThreshold {
		t.Errorf("threshold not defaulted: %+v", s.ActionPolicy)
	}
	if s.DailyCap != DefaultDailyCap {
		t.Errorf("daily cap not defaulted: %d", s.DailyCap)
	}
	if s.Vertical != VerticalServices {
		t.Errorf("vertical should default to services, got %q", s.Vertical)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("normalised settings should validate: %v", err)
	}
	// Disabled by default: ship off, nothing runs, nothing is billed (§16).
	if s.Enabled {
		t.Error("settings must default to disabled")
	}
}

// Default settings for a fresh account come seeded from the vertical.
func TestNewSettings_SeedsTopics(t *testing.T) {
	s := NewSettings("ws-1", ref().Source, ref().AccountID, VerticalGov)
	if !s.Topics.Has("saude") {
		t.Fatalf("gov defaults not seeded: %+v", s.Topics)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSettings_Validate(t *testing.T) {
	cases := map[string]func(*Settings){
		"workspace required": func(s *Settings) { s.WorkspaceID = "" },
		"bad source":         func(s *Settings) { s.Source = "x" },
		"account required":   func(s *Settings) { s.AccountID = "" },
		"bad topics":         func(s *Settings) { s.Topics = TopicSet{{Key: "Saúde"}} },
		"negative cap":       func(s *Settings) { s.DailyCap = -1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := NewSettings("ws-1", SourceInstagram, "acc-1", VerticalRetail)
			mutate(&s)
			if err := s.Validate(); err == nil {
				t.Error("expected error")
			}
		})
	}
}

func intp(v int) *int              { return &v }
func timep(v time.Time) *time.Time { return &v }

// ---- Container overrides ----

// A post override carries only what it changes; everything else is the
// account's. Enabled, model, topics, threshold and instructions can each be
// overridden independently, and an empty override changes nothing.
func TestSettings_WithOverride(t *testing.T) {
	base := NewSettings("ws-1", SourceInstagram, "acc-1", VerticalGov)
	base.Enabled = true
	base.Model = "m-account"
	base.Instructions = "conta de prefeitura"

	if got := base.WithOverride(nil); !reflect.DeepEqual(got, base) {
		t.Fatal("a nil override must return the account settings unchanged")
	}
	if got := base.WithOverride(&ContainerOverride{}); got.Enabled != true || got.Model != "m-account" || got.Instructions != "conta de prefeitura" || len(got.Topics) != len(base.Topics) {
		t.Fatalf("an empty override changed something: %+v", got)
	}

	off := false
	model := "m-post"
	threshold := 80
	instructions := "post sobre o asfalto da rua A"
	topics := TopicSet{{Key: "asfalto", Label: "Asfalto"}}
	got := base.WithOverride(&ContainerOverride{
		ContainerID: "m-1", Enabled: &off, Model: &model, Topics: &topics, SeverityThreshold: &threshold, Instructions: &instructions,
	})
	if got.Enabled {
		t.Error("enabled not overridden")
	}
	if got.Model != "m-post" || got.ActionPolicy.SeverityThreshold != 80 {
		t.Errorf("model/threshold not overridden: %+v", got)
	}
	// Post instructions do not replace the account's; both reach the model.
	if got.Instructions != "conta de prefeitura\n\npost sobre o asfalto da rua A" {
		t.Errorf("instructions = %q", got.Instructions)
	}
	// The override's topics are normalised and still carry other.
	if !got.Topics.Has("asfalto") || !got.Topics.Has(TopicKeyOther) || len(got.Topics) != 2 {
		t.Errorf("topics = %+v", got.Topics)
	}
	// The base was not mutated.
	if !base.Enabled || base.Topics.Has("asfalto") && len(base.Topics) == 2 {
		t.Error("WithOverride must not mutate the receiver")
	}
	// Whitespace-only instructions inherit rather than blank the account's.
	blank := "   "
	if got := base.WithOverride(&ContainerOverride{Instructions: &blank}); got.Instructions != "conta de prefeitura" {
		t.Errorf("blank instructions override = %q", got.Instructions)
	}
}

func TestContainerOverride_NormalizeAndValidate(t *testing.T) {
	o := ContainerOverride{WorkspaceID: " ws-1 ", Source: SourceInstagram, AccountID: " acc-1 ", ContainerID: " m-1 "}
	o.Normalize()
	if o.WorkspaceID != "ws-1" || o.AccountID != "acc-1" || o.ContainerID != "m-1" {
		t.Fatalf("not trimmed: %+v", o)
	}
	if err := o.Validate(); err != nil {
		t.Fatalf("an empty override is valid (it inherits everything): %v", err)
	}
	if o.IsEmpty() != true {
		t.Fatal("an override with no fields is empty")
	}
	bad := ContainerOverride{WorkspaceID: "ws-1", Source: SourceInstagram, AccountID: "acc-1"}
	if err := bad.Validate(); err == nil {
		t.Fatal("a container id is required")
	}
	th := 500
	o.SeverityThreshold = &th
	o.Normalize()
	if *o.SeverityThreshold != 100 {
		t.Fatalf("threshold must be clamped, got %d", *o.SeverityThreshold)
	}
	topics := TopicSet{{Label: "Saúde Pública"}}
	o.Topics = &topics
	o.Normalize()
	if !o.Topics.Has("saude-publica") || !o.Topics.Has(TopicKeyOther) {
		t.Fatalf("override topics not normalised: %+v", *o.Topics)
	}
	if o.IsEmpty() {
		t.Fatal("an override with fields is not empty")
	}
	long := TopicSet{{Key: "x", Label: string(make([]rune, MaxTopicLabelRunes+1))}}
	o.Topics = &long
	if err := o.Validate(); err == nil {
		t.Fatal("override topics are validated like the account's")
	}
}
