package comment_analysis

import (
	"strings"
	"testing"
)

func linkedObservation() AlertObservation {
	obs := alertObservation()
	obs.AccountName = "prefeitura"
	obs.Permalink = "https://www.instagram.com/p/ABC123/"
	obs.Comment.SourceCommentID = "17924500"
	return obs
}

// The recipient is on a phone, woken up. A link they can tap is the difference
// between acting now and acting after they get to a laptop.
func TestAlertMessageCarriesThePostLink(t *testing.T) {
	msg := NewAlert(validRule(), linkedObservation(), alertNow()).Message()
	if !strings.Contains(msg, "https://www.instagram.com/p/ABC123/") {
		t.Fatalf("the post link is missing:\n%s", msg)
	}
}

// A deep link to the COMMENT itself, so the reader lands on the thing that
// fired the alert rather than on a post with four hundred comments.
func TestAlertCommentLink(t *testing.T) {
	alert := NewAlert(validRule(), linkedObservation(), alertNow())
	got := alert.CommentLink()
	if got != "https://www.instagram.com/p/ABC123/c/17924500/" {
		t.Fatalf("comment link = %q", got)
	}
	if !strings.Contains(alert.Message(), got) {
		t.Fatalf("the comment link is missing from the message:\n%s", alert.Message())
	}
}

// A permalink without its trailing slash still produces a usable link rather
// than one with a missing or doubled separator.
func TestAlertCommentLinkNormalizesTheSeparator(t *testing.T) {
	obs := linkedObservation()
	obs.Permalink = "https://www.instagram.com/p/ABC123"
	if got := NewAlert(validRule(), obs, alertNow()).CommentLink(); got != "https://www.instagram.com/p/ABC123/c/17924500/" {
		t.Fatalf("comment link = %q", got)
	}
}

// No permalink means no invented link. A dead link in an alert is worse than
// no link: it teaches the reader not to trust the next one.
func TestAlertLinksAreAbsentWithoutAPermalink(t *testing.T) {
	obs := linkedObservation()
	obs.Permalink = ""
	alert := NewAlert(validRule(), obs, alertNow())

	if alert.CommentLink() != "" {
		t.Fatalf("invented a comment link: %q", alert.CommentLink())
	}
	if strings.Contains(alert.Message(), "http") {
		t.Fatalf("message contains a link it cannot have:\n%s", alert.Message())
	}
}

// A windowed alert has no comment, so it links the account's post that
// triggered the look, and never a comment.
func TestAlertWindowedHasNoCommentLink(t *testing.T) {
	r := validRule()
	r.Metric = AlertMetricHostileCount
	r.Threshold = 10
	r.Normalize()

	obs := AlertObservation{
		Metric: AlertMetricHostileCount, Value: 14,
		AccountName: "prefeitura", Permalink: "https://www.instagram.com/p/ABC123/",
	}
	alert := NewAlert(r, obs, alertNow())
	if alert.CommentLink() != "" {
		t.Fatal("a windowed alert has no single comment to link")
	}
	if !strings.Contains(alert.Message(), "https://www.instagram.com/p/ABC123/") {
		t.Fatalf("the post link should still be there:\n%s", alert.Message())
	}
}

// THE bug this suite was written for: with nothing resolved, the message used
// to print the workspace's internal account UUID and the raw media id at the
// recipient. Neither means anything to a human, and printing them is worse
// than printing nothing.
func TestAlertNeverPrintsInternalIDs(t *testing.T) {
	obs := alertObservation() // no AccountName, no Permalink
	obs.Comment.ContainerID = "17924500123456"
	rule := validRule()
	rule.AccountID = "22222222-2222-2222-2222-222222222222"

	msg := NewAlert(rule, obs, alertNow()).Message()
	if strings.Contains(msg, rule.AccountID) {
		t.Fatalf("the account UUID reached the recipient:\n%s", msg)
	}
	if strings.Contains(msg, "17924500123456") {
		t.Fatalf("the raw media id reached the recipient:\n%s", msg)
	}
	// It still has to say what happened and quote the comment.
	if !strings.Contains(msg, "vocês são todos uns ladrões") && !strings.Contains(msg, "ladrões") {
		t.Fatalf("the comment itself must survive:\n%s", msg)
	}
}

// The account is named when we know its handle, and by nothing at all when we
// do not.
func TestAlertNamesTheAccountWhenKnown(t *testing.T) {
	msg := NewAlert(validRule(), linkedObservation(), alertNow()).Message()
	if !strings.Contains(msg, "prefeitura") {
		t.Fatalf("the account handle is missing:\n%s", msg)
	}
}

// ---- the AI briefing ----

// The briefing is guidance, not a fact, so it is clearly marked as the model's
// reading and sits below what actually happened.
func TestAlertBriefingIsMarkedAndLast(t *testing.T) {
	obs := linkedObservation()
	obs.Briefing = AlertBriefing{
		Context:    "Reclamação sobre a obra da Rua A, mesma pauta de ontem.",
		Suggestion: "Responda publicamente reconhecendo o prazo e ofereça o direct.",
	}
	msg := NewAlert(validRule(), obs, alertNow()).Message()

	if !strings.Contains(msg, "Reclamação sobre a obra") {
		t.Fatalf("the context is missing:\n%s", msg)
	}
	if !strings.Contains(msg, "ofereça o direct") {
		t.Fatalf("the suggestion is missing:\n%s", msg)
	}
	// The reader must be able to tell our facts from the model's reading.
	if !strings.Contains(strings.ToLower(msg), "ia") {
		t.Fatalf("the briefing is not attributed to the AI:\n%s", msg)
	}
	quoteAt := strings.Index(msg, "ladrões")
	briefAt := strings.Index(msg, "Reclamação sobre a obra")
	if briefAt < quoteAt {
		t.Fatal("the model's reading must come after the comment it is about")
	}
}

// No briefing, no empty heading. An alert without one has to read as a finished
// message rather than a broken one.
func TestAlertWithoutABriefingHasNoEmptySection(t *testing.T) {
	msg := NewAlert(validRule(), linkedObservation(), alertNow()).Message()
	if strings.Contains(msg, "\n\n\n") {
		t.Fatalf("blank block left behind:\n%q", msg)
	}
	if strings.Contains(strings.ToLower(msg), "leitura da ia") {
		t.Fatalf("an empty briefing left its heading behind:\n%s", msg)
	}
}

// A briefing is model output about a real incident: it is bounded and stripped
// of the control characters a template parameter cannot carry.
func TestAlertBriefingIsBoundedAndFlattened(t *testing.T) {
	obs := linkedObservation()
	obs.Briefing = AlertBriefing{
		Context:    strings.Repeat("muito longo ", 200),
		Suggestion: "linha um\nlinha dois",
	}
	obs.Briefing.Normalize()

	if len(obs.Briefing.Context) > MaxBriefingRunes+10 {
		t.Fatalf("context is %d runes, unbounded", len(obs.Briefing.Context))
	}
	if strings.ContainsAny(obs.Briefing.Suggestion, "\n\t") {
		t.Fatalf("suggestion carries a control character: %q", obs.Briefing.Suggestion)
	}
}

// The briefing reaches a template as its own named variable, so an operator can
// put it wherever their approved template has room.
func TestBriefingIsATemplateFact(t *testing.T) {
	obs := linkedObservation()
	obs.Briefing = AlertBriefing{Context: "Pauta da obra.", Suggestion: "Responda no post."}

	params := NewAlert(validRule(), obs, alertNow()).TemplateParamsFor([]string{"briefing"})
	if len(params) != 1 || strings.TrimSpace(params[0]) == "" {
		t.Fatalf("params = %v", params)
	}
	if !strings.Contains(params[0], "Pauta da obra") {
		t.Fatalf("the briefing did not fill its named variable: %q", params[0])
	}
}

// Without a briefing the variable is still filled, because an empty template
// parameter is refused by the provider.
func TestBriefingFactIsNeverEmpty(t *testing.T) {
	params := NewAlert(validRule(), linkedObservation(), alertNow()).TemplateParamsFor([]string{"briefing"})
	if strings.TrimSpace(params[0]) == "" {
		t.Fatalf("params = %v", params)
	}
}
