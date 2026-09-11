package comment_analysis

import (
	"testing"
	"time"

	"vozko/domain/shared"
)

// The engine used to accept exactly one channel. It now accepts every channel
// the shared sets declare analysable, and Valid() must mean THAT rather than
// "is a messaging channel": the two differ on voice (analysable, not messaging)
// and on support (messaging, never analysed).
func TestSourceValidFollowsTheAnalysisSets(t *testing.T) {
	for _, s := range []Source{SourceInstagram, SourceWhatsApp, SourceTelegram, SourceUnofficialWhatsApp, SourceVoice} {
		if !s.Valid() {
			t.Errorf("%q should be an analysable source", s)
		}
	}
	for _, s := range []Source{"support", "", "Instagram", "facebook", "instagram "} {
		if s.Valid() {
			t.Errorf("%q should not be an analysable source", s)
		}
	}

	// Voice is the case that proves Valid() is not EntryType.Valid().
	if SourceVoice.EntryType().Valid() {
		t.Fatal("precondition: voice is not a messaging entry type")
	}
	if !SourceVoice.Valid() {
		t.Error("voice must be an analysable source")
	}
}

func TestSourceEntryTypeRoundTrip(t *testing.T) {
	for _, e := range shared.ConversationAnalysableEntryTypes() {
		if got := SourceOf(e).EntryType(); got != e {
			t.Errorf("round trip of %q gave %q", e, got)
		}
	}
}

// Which subject exists is a narrower question than which channel is analysable.
func TestSubjectKindSupportedOn(t *testing.T) {
	if !SubjectKindComment.SupportedOn(SourceInstagram) {
		t.Error("instagram should carry comments")
	}
	for _, s := range []Source{SourceWhatsApp, SourceTelegram, SourceVoice, SourceUnofficialWhatsApp} {
		if SubjectKindComment.SupportedOn(s) {
			t.Errorf("%q has no public comments", s)
		}
	}
	for _, s := range []Source{SourceWhatsApp, SourceInstagram, SourceTelegram, SourceUnofficialWhatsApp, SourceVoice} {
		if !SubjectKindConversation.SupportedOn(s) {
			t.Errorf("%q should carry conversations", s)
		}
	}
	if SubjectKind("").SupportedOn(SourceInstagram) || SubjectKind("post").SupportedOn(SourceInstagram) {
		t.Error("an unknown subject kind is supported nowhere")
	}
}

// Rows written before conversations existed have no subject kind, and must keep
// reading as comments rather than becoming an unknown kind.
func TestEmptySubjectKindReadsAsComment(t *testing.T) {
	a := &CommentAnalysis{Source: SourceInstagram, AccountID: "acc", ContainerID: "post"}
	if a.Kind() != SubjectKindComment {
		t.Errorf("Kind() = %q, want comment", a.Kind())
	}
	if got := a.Container().Kind; got != SubjectKindComment {
		t.Errorf("Container().Kind = %q, want comment", got)
	}
	if err := (ContainerRef{Source: SourceInstagram, AccountID: "a", ContainerID: "c"}).Validate(); err != nil {
		t.Errorf("a kindless ref should still validate as a comment container: %v", err)
	}
}

func conversationClassification() Classification {
	return Classification{
		Sentiment:     shared.SentimentPositive,
		Interest:      InterestInterested,
		Disposition:   DispositionFillingInfo,
		Qualification: QualificationHotLead,
		NextAction:    NextActionContinue,
		Summary:       "  Cliente pediu orçamento e enviou os documentos.  ",
		Language:      " pt ",
		Quality: ConversationQuality{
			GoalProgress: shared.QualityLevelHigh, CustomerEngagement: shared.QualityLevelHigh,
			AgentConduct: shared.QualityLevelHigh, Professionalism: shared.QualityLevelHigh,
		},
	}
}

func TestValidateConversationAcceptsAndTrims(t *testing.T) {
	c := conversationClassification()
	if err := c.ValidateConversation(); err != nil {
		t.Fatalf("ValidateConversation: %v", err)
	}
	if c.Summary != "Cliente pediu orçamento e enviou os documentos." {
		t.Errorf("summary not trimmed: %q", c.Summary)
	}
	if c.Language != "pt" {
		t.Errorf("language not trimmed: %q", c.Language)
	}
}

func TestValidateConversationRejectsEveryBadLabel(t *testing.T) {
	for name, mutate := range map[string]func(*Classification){
		"sentiment":     func(c *Classification) { c.Sentiment = "angry" },
		"interest":      func(c *Classification) { c.Interest = "maybe" },
		"disposition":   func(c *Classification) { c.Disposition = "sold" },
		"qualification": func(c *Classification) { c.Qualification = "hot" },
		"next action":   func(c *Classification) { c.NextAction = "call" },
		"empty quality": func(c *Classification) { c.Quality = ConversationQuality{} },
		// The dangerous one: a model that rates three of four dimensions would
		// otherwise score as if the fourth were genuinely "none".
		"partial quality": func(c *Classification) { c.Quality.AgentConduct = "" },
	} {
		c := conversationClassification()
		mutate(&c)
		if err := c.ValidateConversation(); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

// Free text is bounded here, not trusted from the model: the summary is the
// only unredacted customer content the engine stores.
func TestValidateConversationBoundsFreeText(t *testing.T) {
	c := conversationClassification()
	c.Summary = string(make([]rune, MaxSummaryRunes+500))
	c.ProductInterest = string(make([]rune, MaxProductInterestRunes+50))
	if err := c.ValidateConversation(); err != nil {
		t.Fatalf("ValidateConversation: %v", err)
	}
	if got := len([]rune(c.Summary)); got != MaxSummaryRunes {
		t.Errorf("summary is %d runes, want %d", got, MaxSummaryRunes)
	}
	if got := len([]rune(c.ProductInterest)); got != MaxProductInterestRunes {
		t.Errorf("product interest is %d runes, want %d", got, MaxProductInterestRunes)
	}
}

// ValidateFor dispatches on the kind, and the two taxonomies must not be
// interchangeable: conversation labels are not a valid comment classification
// and vice versa.
func TestValidateForRefusesTheWrongTaxonomy(t *testing.T) {
	topics := DefaultTopicsFor(VerticalServices)

	conv := conversationClassification()
	if err := conv.ValidateFor(SubjectKindConversation, topics); err != nil {
		t.Fatalf("conversation labels on a conversation: %v", err)
	}
	conv2 := conversationClassification()
	if err := conv2.ValidateFor(SubjectKindComment, topics); err == nil {
		t.Error("conversation labels must not validate as a comment")
	}

	if err := (&Classification{}).ValidateFor("post", topics); err == nil {
		t.Error("an unknown subject kind must be refused")
	}
}

// A classified conversation must not look like a harmless comment. Writing a
// computed severity of 0 would sort every conversation to the bottom of a
// severity ranking as though it had been assessed and found benign.
func TestApplyConversationLeavesCommentDimensionsUnset(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	a, err := NewPending(NewInput{
		WorkspaceID: "ws",
		Container: ContainerRef{
			Kind: SubjectKindConversation, Source: SourceWhatsApp,
			AccountID: "acc", ContainerID: "camp",
		},
		SourceCommentID: "entry-1",
		Text:            "cliente: quero um orçamento",
		Now:             now,
	})
	if err != nil {
		t.Fatalf("NewPending: %v", err)
	}
	if a.SubjectKind != SubjectKindConversation {
		t.Fatalf("subject kind = %q", a.SubjectKind)
	}
	if err := a.Claim(now); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	c := conversationClassification()
	if err := c.ValidateConversation(); err != nil {
		t.Fatalf("ValidateConversation: %v", err)
	}
	if err := a.Apply(c, ActionPolicy{}, Provenance{BatchID: "b1", Model: "m"}, now); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if a.Status != StatusAnalyzed {
		t.Errorf("status = %q", a.Status)
	}
	if a.AttendanceQuality != 100 {
		t.Errorf("attendance quality = %d, want 100", a.AttendanceQuality)
	}
	if a.Interest != InterestInterested || a.Disposition != DispositionFillingInfo {
		t.Errorf("conversation labels not written: %+v", a)
	}
	if a.Summary == "" {
		t.Error("summary not written")
	}

	// The comment dimensions stay zero, severity included.
	if a.Stance != "" || a.Intent != "" || a.TopicKey != "" {
		t.Errorf("comment labels leaked onto a conversation: stance=%q intent=%q topic=%q", a.Stance, a.Intent, a.TopicKey)
	}
	if a.Severity != 0 || a.Toxicity != "" || a.PersonalAttack != "" || a.LegalRisk != "" {
		t.Errorf("severity dimensions leaked onto a conversation: %+v", a)
	}
	if a.IsSpam {
		t.Error("a conversation must not be flagged spam by the comment path")
	}
}

// RequiresAction on a conversation is the escalate signal, not a severity
// threshold, because there is no severity to threshold.
func TestConversationRequiresActionOnEscalate(t *testing.T) {
	now := time.Now().UTC()
	build := func(next NextAction) *CommentAnalysis {
		a, err := NewPending(NewInput{
			WorkspaceID: "ws",
			Container: ContainerRef{
				Kind: SubjectKindConversation, Source: SourceTelegram,
				AccountID: "acc", ContainerID: "camp",
			},
			SourceCommentID: "e1", Text: "oi", Now: now,
		})
		if err != nil {
			t.Fatalf("NewPending: %v", err)
		}
		if err := a.Claim(now); err != nil {
			t.Fatalf("Claim: %v", err)
		}
		c := conversationClassification()
		c.NextAction = next
		if err := a.Apply(c, ActionPolicy{}, Provenance{}, now); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		return a
	}

	if !build(NextActionEscalate).RequiresAction {
		t.Error("escalate should require action")
	}
	if build(NextActionContinue).RequiresAction {
		t.Error("continue should not require action")
	}
}

// A conversation on a channel that has no conversations, or a comment on a
// channel that has no posts, must be refused at ingest rather than queued and
// failed later.
func TestNewPendingRefusesImpossibleSubjects(t *testing.T) {
	now := time.Now().UTC()
	for name, ref := range map[string]ContainerRef{
		"comment on telegram":     {Kind: SubjectKindComment, Source: SourceTelegram, AccountID: "a", ContainerID: "c"},
		"comment on voice":        {Kind: SubjectKindComment, Source: SourceVoice, AccountID: "a", ContainerID: "c"},
		"conversation on support": {Kind: SubjectKindConversation, Source: "support", AccountID: "a", ContainerID: "c"},
		"unknown channel":         {Kind: SubjectKindConversation, Source: "facebook", AccountID: "a", ContainerID: "c"},
	} {
		if _, err := NewPending(NewInput{
			WorkspaceID: "ws", Container: ref, SourceCommentID: "x", Text: "hi", Now: now,
		}); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

// Refs are compared by meaning, not by struct identity. This is a real trap:
// a literal ref carries no Kind, Container() returns an explicit one, and Go's
// == calls those different. The symptom is silent, a lookup that simply finds
// nothing, which is exactly how it first showed up.
func TestContainerRefEqualIgnoresTheImplicitKind(t *testing.T) {
	implicit := ContainerRef{Source: SourceInstagram, AccountID: "a", ContainerID: "c"}
	explicit := ContainerRef{Kind: SubjectKindComment, Source: SourceInstagram, AccountID: "a", ContainerID: "c"}

	if implicit == explicit {
		t.Fatal("precondition: Go's == already treats these as equal, this test is moot")
	}
	if !implicit.Equal(explicit) || !explicit.Equal(implicit) {
		t.Error("a kindless ref must equal the same ref with an explicit comment kind")
	}

	conversation := ContainerRef{Kind: SubjectKindConversation, Source: SourceInstagram, AccountID: "a", ContainerID: "c"}
	if implicit.Equal(conversation) {
		t.Error("a comment container must not equal a conversation container on the same ids")
	}

	// And the row's own ref round-trips through Equal.
	row := &CommentAnalysis{Source: SourceInstagram, AccountID: "a", ContainerID: "c"}
	if !row.Container().Equal(implicit) {
		t.Error("a row's container must equal the literal ref it was built from")
	}
}

// The new filters are validated like every other one: a bad value from a query
// string is refused rather than silently matching nothing.
func TestListInputValidatesTheConversationFilters(t *testing.T) {
	base := func() ListInput {
		return ListInput{WorkspaceID: "ws"}
	}

	ok := base()
	ok.SubjectKinds = []SubjectKind{SubjectKindComment, SubjectKindConversation}
	ok.Interest = InterestInterested
	ok.Disposition = DispositionSale
	ok.Qualification = QualificationHotLead
	ok.NextAction = NextActionEscalate
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid conversation filters rejected: %v", err)
	}

	// Empty means every kind; it must not be mistaken for a bad value.
	if err := base().Validate(); err != nil {
		t.Fatalf("an empty filter set should be valid: %v", err)
	}

	for name, mutate := range map[string]func(*ListInput){
		"subject kind":  func(in *ListInput) { in.SubjectKinds = []SubjectKind{"post"} },
		"interest":      func(in *ListInput) { in.Interest = "maybe" },
		"disposition":   func(in *ListInput) { in.Disposition = "sold" },
		"qualification": func(in *ListInput) { in.Qualification = "hot" },
		"next action":   func(in *ListInput) { in.NextAction = "call" },
	} {
		in := base()
		mutate(&in)
		if err := in.Validate(); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}
