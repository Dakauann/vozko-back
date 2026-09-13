package audience

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Alerts: "quando passar de X, me manda um WhatsApp".
//
// This is the first thing in the engine that acts on its own, without a person
// looking at a screen, and outward, to a phone. Everything in this file exists
// because of that. The rules that matter, in the order they matter:
//
//  1. A rule CANNOT become a flood. A cooldown floor and a daily cap are
//     enforced by Normalize, so an operator cannot configure them away, and
//     both are checked again by ShouldFire.
//  2. The direction is a property of the METRIC, not a field. Counts alarm when
//     they rise, a score alarms when it falls. Letting someone pick the
//     comparison mostly produces alerts that can never fire.
//  3. Every firing has a stable idempotency key, so a retry after a timeout
//     cannot send twice.
//  4. ShouldFire is necessary but NOT sufficient. It is pure and knows nothing
//     about other replicas; the authority is the repository's conditional
//     claim, which is what actually decides who sends. See ClaimFire.

// AlertMetric is what a rule watches. A closed set, like every other vocabulary
// in the engine: an unbounded one cannot be listed in a picker, counted, or
// explained in a message.
type AlertMetric string

const (
	// AlertMetricCommentSeverity watches ONE comment as it is classified.
	// "Avise quando alguém disser algo muito grave."
	AlertMetricCommentSeverity AlertMetric = "comment_severity"
	// AlertMetricHighSeverityCount counts severe comments in a window.
	AlertMetricHighSeverityCount AlertMetric = "high_severity_count"
	// AlertMetricHostileCount counts hostile comments in a window: the shape of
	// a brigade rather than one angry person.
	AlertMetricHostileCount AlertMetric = "hostile_count"
	// AlertMetricCommentVolume counts everything in a window, which catches a
	// spike whose tone the classifier has not judged yet.
	AlertMetricCommentVolume AlertMetric = "comment_volume"
	// AlertMetricAcceptanceScore watches the 0-100 score, and is the one metric
	// that alarms on the way DOWN.
	AlertMetricAcceptanceScore AlertMetric = "acceptance_score"

	// The conversation half. Same machinery, a different subject: these read an
	// analysed CONVERSATION rather than a comment, which is what let a workspace
	// that runs no Instagram account finally watch something.

	// AlertMetricAttendanceQuality watches ONE conversation's attendance score
	// as it is classified, and alarms on the way DOWN. "Avise quando um
	// atendimento cair abaixo de 70."
	AlertMetricAttendanceQuality AlertMetric = "attendance_quality"
	// AlertMetricEscalationCount counts, over a window, the conversations the
	// model says need a person. One is a bad conversation; five in an hour is a
	// queue nobody is working.
	AlertMetricEscalationCount AlertMetric = "escalation_count"
)

func AllAlertMetrics() []AlertMetric {
	return append(AlertMetricsFor(SubjectKindComment), AlertMetricsFor(SubjectKindConversation)...)
}

// AlertMetricsFor is the vocabulary offered for one subject.
//
// The picker reads this and so does Validate, which is what stops a comment
// metric being armed on a channel that has no comments: such a rule saves,
// shows "Regra ativa", and then never fires, because the evaluator has nothing
// to measure it against. Two lists rather than one flag so a new metric has to
// declare which subject it reads before it can be reached at all.
func AlertMetricsFor(kind SubjectKind) []AlertMetric {
	switch kind {
	case SubjectKindComment:
		return []AlertMetric{
			AlertMetricCommentSeverity,
			AlertMetricHighSeverityCount,
			AlertMetricHostileCount,
			AlertMetricCommentVolume,
			AlertMetricAcceptanceScore,
		}
	case SubjectKindConversation:
		return []AlertMetric{
			AlertMetricAttendanceQuality,
			AlertMetricEscalationCount,
		}
	}
	return nil
}

// SubjectKind is what this metric reads.
func (m AlertMetric) SubjectKind() SubjectKind {
	switch m {
	case AlertMetricAttendanceQuality, AlertMetricEscalationCount:
		return SubjectKindConversation
	}
	return SubjectKindComment
}

func (m AlertMetric) Valid() bool {
	switch m {
	case AlertMetricCommentSeverity, AlertMetricHighSeverityCount,
		AlertMetricHostileCount, AlertMetricCommentVolume, AlertMetricAcceptanceScore,
		AlertMetricAttendanceQuality, AlertMetricEscalationCount:
		return true
	}
	return false
}

// IsWindowed says whether the metric is counted over a span. A per-comment
// metric is checked against the comment that just arrived; a windowed one needs
// a query over the period.
func (m AlertMetric) IsWindowed() bool {
	switch m {
	case AlertMetricCommentSeverity, AlertMetricAttendanceQuality:
		return false
	}
	return true
}

// TriggersWhenBelow is the metric's own direction.
func (m AlertMetric) TriggersWhenBelow() bool {
	return m == AlertMetricAcceptanceScore || m == AlertMetricAttendanceQuality
}

// AlertChannel is how the message leaves.
type AlertChannel string

const (
	// AlertChannelOfficial sends an approved template through the official API.
	// It costs money and reaches anyone; it is the reliable one.
	AlertChannelOfficial AlertChannel = "official"
	// AlertChannelUnofficial sends plain text from the workspace's own
	// connected number. Free, and subject to that channel's ban posture.
	AlertChannelUnofficial AlertChannel = "unofficial"
)

func (c AlertChannel) Valid() bool {
	return c == AlertChannelOfficial || c == AlertChannelUnofficial
}

const (
	// MinAlertCooldownMinutes is the floor between two firings of one rule. The
	// number that stops an incident becoming a thousand messages.
	MinAlertCooldownMinutes = 5
	// DefaultAlertCooldownMinutes is what a rule gets when nobody chooses.
	DefaultAlertCooldownMinutes = 60
	// MaxAlertCooldownMinutes is a week: beyond that, disable the rule instead.
	MaxAlertCooldownMinutes = 7 * 24 * 60

	// MaxAlertsPerDay is the backstop for a condition that persists all day.
	MaxAlertsPerDay     = 24
	DefaultAlertsPerDay = 6

	MinAlertWindowMinutes     = 5
	DefaultAlertWindowMinutes = 60
	MaxAlertWindowMinutes     = 24 * 60

	// MaxAlertNameRunes bounds the operator's label, which is echoed into the
	// message and into a template parameter.
	MaxAlertNameRunes = 60

	// AlertSubjectSuppression is how long one rule stays quiet about the SAME
	// subject after alerting on it.
	//
	// The cooldown and the daily cap bound how often a RULE speaks; this bounds
	// how often it speaks about one thing. Conversations are re-analysed as they
	// grow, so without it a single bad conversation would spend the rule's whole
	// daily allowance and the second conversation to go wrong that day would be
	// dropped in silence. A day, matching the cap's own framing: you hear about
	// a given conversation at most once a day.
	AlertSubjectSuppression = 24 * time.Hour

	// MaxAlertMinMessages bounds the conversation-length floor. Past this the
	// rule is not selective, it is off, and an operator who typed an extra zero
	// would never find out.
	MaxAlertMinMessages = 500

	// minRecipientDigits is a sanity floor, not a phone validator. The channels
	// own the real rules; this only refuses something nobody could have meant.
	minRecipientDigits = 8

	alertDayLayout = "2006-01-02"
)

// AlertTemplateParamCount is how many distinct facts an alert can supply to a
// template. It is NOT a requirement on the template: a template may declare
// fewer (the extra facts are dropped), more (the rest are padded), or none at
// all. See TemplateParamsFor.
//
// Kept in step with AlertFactKeys by a test, because the two drifting means a
// settings screen advertising a different number of variables than an alert
// actually fills.
const AlertTemplateParamCount = 6

// AlertRule is one configured watch.
type AlertRule struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Source      Source `json:"source"`
	AccountID   string `json:"accountId"`

	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// CreatedByUserID is who configured the rule, and therefore who its
	// messages are attributed to. An automated paid send with no author is a
	// support ticket nobody can answer, so the person who armed the alert owns
	// what it says.
	CreatedByUserID string `json:"createdByUserId,omitempty"`

	Metric    AlertMetric `json:"metric"`
	Threshold int         `json:"threshold"`
	// WindowMinutes is the span a windowed metric is counted over. Ignored for
	// a per-comment metric.
	WindowMinutes int `json:"windowMinutes"`
	// MinMessages is how long a conversation must be before it is worth
	// judging. Zero means no floor, which is what every rule written before
	// this existed has.
	//
	// A two-message conversation scores badly because it barely happened, not
	// because it was handled badly, so without a floor the first alert an
	// operator ever receives is about a customer who said "oi" and left. Only
	// meaningful for a per-conversation metric; Validate refuses it elsewhere
	// rather than storing a setting that does nothing.
	MinMessages int `json:"minMessages"`

	Channel   AlertChannel `json:"channel"`
	Recipient string       `json:"recipient"`
	// BusinessPhoneID and TemplateID are required for the official channel:
	// which number it leaves from and which approved template it sends.
	BusinessPhoneID string `json:"businessPhoneId,omitempty"`
	TemplateID      string `json:"templateId,omitempty"`
	// InstanceID names the connected number for the unofficial channel. Empty
	// means "whichever one this workspace has", resolved at send time.
	InstanceID string `json:"instanceId,omitempty"`

	// Brief asks the model to add its reading of what happened and how to
	// respond. Off by default: it is one AI call per firing, bounded by the
	// cooldown and the daily cap but still billed.
	Brief bool `json:"brief"`

	CooldownMinutes int `json:"cooldownMinutes"`
	MaxPerDay       int `json:"maxPerDay"`

	// The firing history, kept on the rule rather than in a second table: it is
	// what the cooldown and the cap are checked against, and it is also the
	// answer to "por que não disparou".
	LastFiredAt *time.Time `json:"lastFiredAt,omitempty"`
	FiredToday  int        `json:"firedToday"`
	FiredDay    string     `json:"firedDay,omitempty"`
	LastError   string     `json:"lastError,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Normalize fills defaults and CLAMPS the safety limits. The clamping is the
// point: an operator who types a one-minute cooldown gets the floor, not an
// error they would work around by typing five.
func (r *AlertRule) Normalize() {
	r.WorkspaceID = strings.TrimSpace(r.WorkspaceID)
	r.AccountID = strings.TrimSpace(r.AccountID)
	r.Name, _ = TruncateRunes(strings.TrimSpace(r.Name), MaxAlertNameRunes)
	r.Recipient = normalizeAlertRecipient(r.Recipient)
	r.BusinessPhoneID = strings.TrimSpace(r.BusinessPhoneID)
	r.TemplateID = strings.TrimSpace(r.TemplateID)
	r.InstanceID = strings.TrimSpace(r.InstanceID)

	if r.Metric.IsWindowed() {
		if r.WindowMinutes <= 0 {
			r.WindowMinutes = DefaultAlertWindowMinutes
		}
		if r.WindowMinutes < MinAlertWindowMinutes {
			r.WindowMinutes = MinAlertWindowMinutes
		}
		if r.WindowMinutes > MaxAlertWindowMinutes {
			r.WindowMinutes = MaxAlertWindowMinutes
		}
	} else {
		// A per-comment metric has no window, and storing one would show up in
		// the UI as a setting that does nothing.
		r.WindowMinutes = 0
	}

	if r.CooldownMinutes <= 0 {
		r.CooldownMinutes = DefaultAlertCooldownMinutes
	}
	if r.CooldownMinutes < MinAlertCooldownMinutes {
		r.CooldownMinutes = MinAlertCooldownMinutes
	}
	if r.CooldownMinutes > MaxAlertCooldownMinutes {
		r.CooldownMinutes = MaxAlertCooldownMinutes
	}

	if r.MaxPerDay <= 0 {
		r.MaxPerDay = DefaultAlertsPerDay
	}
	if r.MaxPerDay > MaxAlertsPerDay {
		r.MaxPerDay = MaxAlertsPerDay
	}

	if r.FiredToday < 0 {
		r.FiredToday = 0
	}
}

// normalizeAlertRecipient keeps digits and a leading plus. The channels own
// real phone validation; this only makes the stored value comparable.
func normalizeAlertRecipient(raw string) string {
	var b strings.Builder
	for i, ch := range strings.TrimSpace(raw) {
		if ch >= '0' && ch <= '9' {
			b.WriteRune(ch)
			continue
		}
		if ch == '+' && i == 0 {
			b.WriteRune(ch)
		}
	}
	return b.String()
}

func (r AlertRule) Validate() error {
	if r.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id is required", ErrInvalidFilter)
	}
	if r.AccountID == "" {
		return fmt.Errorf("%w: account id is required", ErrInvalidFilter)
	}
	if r.Name == "" {
		return fmt.Errorf("%w: the rule needs a name", ErrInvalidFilter)
	}
	if r.Source != "" && !r.Source.Valid() {
		return fmt.Errorf("%w: source %q", ErrInvalidFilter, r.Source)
	}
	if !r.Metric.Valid() {
		return fmt.Errorf("%w: metric %q", ErrInvalidFilter, r.Metric)
	}
	// The metric and the channel have to be talking about the same thing. A
	// comment metric on WhatsApp saves happily and then never fires, because
	// the channel produces no comments for it to read.
	if r.Source != "" && !r.Metric.SubjectKind().SupportedOn(r.Source) {
		return fmt.Errorf("%w: %s has no %s to watch on %s",
			ErrInvalidFilter, r.Metric, r.Metric.SubjectKind(), r.Source)
	}
	if r.MinMessages != 0 && r.Metric.IsWindowed() {
		return fmt.Errorf("%w: a minimum message count only applies to a rule that watches one conversation", ErrInvalidFilter)
	}
	if r.MinMessages < 0 || r.MinMessages > MaxAlertMinMessages {
		return fmt.Errorf("%w: the minimum message count must be between 0 and %d", ErrInvalidFilter, MaxAlertMinMessages)
	}
	if !r.Channel.Valid() {
		return fmt.Errorf("%w: channel %q", ErrInvalidFilter, r.Channel)
	}
	if err := r.validateThreshold(); err != nil {
		return err
	}
	if len(strings.TrimLeft(r.Recipient, "+")) < minRecipientDigits {
		return fmt.Errorf("%w: the recipient does not look like a phone number", ErrInvalidFilter)
	}
	// The official channel spends money on an approved template. A rule missing
	// either half would fail at the one moment it mattered.
	if r.Channel == AlertChannelOfficial {
		if r.BusinessPhoneID == "" {
			return fmt.Errorf("%w: an official alert needs the number it sends from", ErrInvalidFilter)
		}
		if r.TemplateID == "" {
			return fmt.Errorf("%w: an official alert needs an approved template", ErrInvalidFilter)
		}
	}
	return nil
}

// validateThreshold bounds the threshold by what the metric can ever measure.
// A severity rule set to 500 is not a strict rule, it is a rule that never
// fires, and the operator will not find out until they need it.
func (r AlertRule) validateThreshold() error {
	switch r.Metric {
	case AlertMetricCommentSeverity:
		if r.Threshold < 1 || r.Threshold > 100 {
			return fmt.Errorf("%w: severity must be between 1 and 100", ErrInvalidFilter)
		}
	case AlertMetricAcceptanceScore, AlertMetricAttendanceQuality:
		if r.Threshold < 0 || r.Threshold > 100 {
			return fmt.Errorf("%w: the score must be between 0 and 100", ErrInvalidFilter)
		}
	default:
		if r.Threshold < 1 {
			return fmt.Errorf("%w: the threshold must be at least 1", ErrInvalidFilter)
		}
	}
	return nil
}

// WatchesEveryChannel reports whether this rule is the wildcard: no source, so
// every channel whose conversations the workspace analyses.
//
// A rule is keyed on (source, account), so watching four channels used to mean
// four rules, each with its own cooldown and its own daily cap. One incident
// spanning two channels then sent two messages, and raising a threshold meant
// editing four rules and missing one. The wildcard is one rule, one cooldown,
// one cap, which is what an operator means by "tell me when attendance drops".
func (r AlertRule) WatchesEveryChannel() bool { return strings.TrimSpace(string(r.Source)) == "" }

// Accepts reports whether this row is one the rule is willing to judge.
//
// Separate from Crossed and ShouldFire because it asks a different question:
// those decide whether the MEASUREMENT warrants an alert, this decides whether
// the subject is substantial enough for the measurement to mean anything. A
// windowed rule has no single row and accepts everything.
func (r AlertRule) Accepts(row *Analysis) bool {
	if r.MinMessages <= 0 || r.Metric.IsWindowed() {
		return true
	}
	if row == nil {
		return false
	}
	return row.MessageCount >= r.MinMessages
}

// Window is the span a windowed metric counts over.
func (r AlertRule) Window() time.Duration {
	return time.Duration(r.WindowMinutes) * time.Minute
}

// Crossed reports whether a measured value is on the alarming side of the
// threshold, in the metric's own direction.
func (r AlertRule) Crossed(value int) bool {
	if r.Metric.TriggersWhenBelow() {
		return value <= r.Threshold
	}
	return value >= r.Threshold
}

// ShouldFire is the whole decision, minus concurrency.
//
// It is pure and knows nothing about other replicas, so two of them can both
// answer true for the same rule at the same instant. That is expected: the
// repository's conditional claim is what actually decides, and this is what
// keeps a claim from being attempted for a rule that is quiet, disabled or
// already at its cap.
func (r AlertRule) ShouldFire(value int, now time.Time) bool {
	if !r.Enabled || !r.Crossed(value) {
		return false
	}
	if r.LastFiredAt != nil && now.Sub(*r.LastFiredAt) < r.Cooldown() {
		return false
	}
	return r.FiredOn(now) < r.MaxPerDay
}

// Cooldown is how long the rule stays quiet after firing.
//
// Deliberately not called a window: this rule has two spans, "how far back do I
// count" (Window) and "how long do I stay quiet" (this), and naming them alike
// is how a call site ends up using the wrong one.
func (r AlertRule) Cooldown() time.Duration {
	return time.Duration(r.CooldownMinutes) * time.Minute
}

// FiredOn is today's tally, which is zero once the day rolls over.
func (r AlertRule) FiredOn(now time.Time) int {
	if r.FiredDay != now.UTC().Format(alertDayLayout) {
		return 0
	}
	return r.FiredToday
}

// RegisterFire records a successful send. Applied by whoever won the claim.
func (r *AlertRule) RegisterFire(now time.Time) {
	day := now.UTC().Format(alertDayLayout)
	if r.FiredDay != day {
		r.FiredDay, r.FiredToday = day, 0
	}
	r.FiredToday++
	fired := now
	r.LastFiredAt = &fired
	r.LastError = ""
}

// ---- what happened ----

// MaxBriefingRunes bounds each half of the model's reading. Two sentences on a
// phone screen, not an essay: the recipient is being woken up.
const MaxBriefingRunes = 240

// AlertBriefing is the model's reading of what fired: what is going on, and
// what to do about it.
//
// Guidance, never fact. It is generated best effort and the alert is sent
// without it when the model cannot answer, because an alert that does not
// arrive is worse than one that arrives without advice. The message marks it as
// the AI's reading and puts it BELOW the comment, so nobody mistakes a
// suggestion for something that actually happened.
type AlertBriefing struct {
	// Context is why this matters: the pattern, the subject, the history.
	Context string `json:"context,omitempty"`
	// Suggestion is how to respond. An approach, not a canned reply.
	Suggestion string `json:"suggestion,omitempty"`

	// What the call cost, so the briefing appears on the spend page under its
	// own kind rather than being invisible.
	Model            string `json:"-"`
	PromptTokens     int    `json:"-"`
	CompletionTokens int    `json:"-"`
}

func (b *AlertBriefing) Normalize() {
	b.Context = sanitizeTemplateParam(b.Context)
	b.Suggestion = sanitizeTemplateParam(b.Suggestion)
	if b.Context == "-" {
		b.Context = ""
	}
	if b.Suggestion == "-" {
		b.Suggestion = ""
	}
	b.Context, _ = TruncateRunes(b.Context, MaxBriefingRunes)
	b.Suggestion, _ = TruncateRunes(b.Suggestion, MaxBriefingRunes)
}

func (b AlertBriefing) Empty() bool {
	return strings.TrimSpace(b.Context) == "" && strings.TrimSpace(b.Suggestion) == ""
}

// Text is both halves as one paragraph, for a template variable.
func (b AlertBriefing) Text() string {
	parts := make([]string, 0, 2)
	if c := strings.TrimSpace(b.Context); c != "" {
		parts = append(parts, c)
	}
	if s := strings.TrimSpace(b.Suggestion); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

// AlertObservation is the measurement that crossed the threshold. Comment is
// set only for a per-comment metric; a windowed one has no single row behind it
// and the message must not invent one.
type AlertObservation struct {
	Metric AlertMetric
	Value  int
	// AccountName is the account's public handle. Resolved by the caller,
	// because the engine holds only an internal id and printing THAT at a
	// recipient tells them nothing.
	AccountName string
	// Permalink is the post's public link, from the channel's own projection.
	Permalink string
	Comment   *Analysis
	Briefing  AlertBriefing
}

// Alert is one firing, formatted. Same posture as Escalation: the wording lives
// in the domain so the message a customer forwards to their own boss does not
// depend on which channel happened to carry it.
type Alert struct {
	Rule        AlertRule
	Observation AlertObservation
	FiredAt     time.Time
}

func NewAlert(rule AlertRule, observation AlertObservation, firedAt time.Time) Alert {
	return Alert{Rule: rule, Observation: observation, FiredAt: firedAt.UTC()}
}

// Headline is the one-line reason, reused by both channels.
func (a Alert) Headline() string {
	return fmt.Sprintf("%s: %s", a.Rule.Name, a.measurement())
}

func (a Alert) measurement() string {
	switch a.Observation.Metric {
	case AlertMetricCommentSeverity:
		return fmt.Sprintf("comentário com gravidade %d", a.Observation.Value)
	case AlertMetricHighSeverityCount:
		return fmt.Sprintf("%d comentários graves em %s", a.Observation.Value, a.windowLabel())
	case AlertMetricHostileCount:
		return fmt.Sprintf("%d comentários hostis em %s", a.Observation.Value, a.windowLabel())
	case AlertMetricCommentVolume:
		return fmt.Sprintf("%d comentários em %s", a.Observation.Value, a.windowLabel())
	case AlertMetricAcceptanceScore:
		return fmt.Sprintf("aceitação caiu para %d em %s", a.Observation.Value, a.windowLabel())
	case AlertMetricAttendanceQuality:
		return fmt.Sprintf("atendimento avaliado em %d", a.Observation.Value)
	case AlertMetricEscalationCount:
		return fmt.Sprintf("%d conversas aguardando uma pessoa em %s", a.Observation.Value, a.windowLabel())
	}
	return fmt.Sprintf("%d", a.Observation.Value)
}

func (a Alert) windowLabel() string {
	minutes := a.Rule.WindowMinutes
	if minutes <= 0 {
		minutes = DefaultAlertWindowMinutes
	}
	if minutes%60 == 0 {
		hours := minutes / 60
		if hours == 1 {
			return "1 hora"
		}
		return fmt.Sprintf("%d horas", hours)
	}
	return fmt.Sprintf("%d minutos", minutes)
}

// Where names the account in terms a human recognises.
//
// It never falls back to an internal id. The engine identifies an account by a
// workspace UUID and a post by the channel's own numeric id, and putting either
// in front of a recipient tells them nothing while making the alert look
// broken. Unknown means the line is simply left out.
func (a Alert) Where() string {
	return strings.TrimSpace(a.Observation.AccountName)
}

// PostLink is the public link to the post, when the channel gave us one.
func (a Alert) PostLink() string {
	return strings.TrimSpace(a.Observation.Permalink)
}

// CommentLink deep-links the comment that fired the alert.
//
// Built from the post's permalink plus the comment id, which is the shape
// Instagram's own web app uses. It is DERIVED rather than returned by the API:
// the comment edge gives an id and no link of its own. The post link is always
// sent alongside it, so a reader whose deep link does not resolve still lands
// somewhere useful.
//
// Empty when there is no permalink, because a link we cannot build correctly is
// worse than none: one dead link teaches the reader to ignore the next.
func (a Alert) CommentLink() string {
	post := a.PostLink()
	c := a.Observation.Comment
	if post == "" || c == nil || strings.TrimSpace(c.SubjectID) == "" {
		return ""
	}
	return strings.TrimSuffix(post, "/") + "/c/" + strings.TrimSpace(c.SubjectID) + "/"
}

// Message is the free-text form, for the unofficial channel.
//
// Ordered by what a woken-up reader needs first: what fired, where, when, the
// links they can tap, then the words somebody actually wrote, and only then the
// model's reading of it.
func (a Alert) Message() string {
	var b strings.Builder
	// The subject's own word. This said "comentários" for every alert, which on
	// a conversation rule is the first line the recipient reads and the first
	// thing about it that is wrong.
	if a.Observation.Metric.SubjectKind() == SubjectKindConversation {
		b.WriteString("Alerta de conversas\n\n")
	} else {
		b.WriteString("Alerta de comentários\n\n")
	}
	b.WriteString(a.Headline())

	// Each of these is omitted rather than printed empty or printed as an
	// internal id.
	if where := a.Where(); where != "" {
		b.WriteString("\nConta: @")
		b.WriteString(where)
	}
	b.WriteString("\nQuando: ")
	b.WriteString(a.FiredAt.Format("02/01/2006 15:04"))
	b.WriteString(" UTC")
	if post := a.PostLink(); post != "" {
		b.WriteString("\nPost: ")
		b.WriteString(post)
	}
	if comment := a.CommentLink(); comment != "" {
		b.WriteString("\nComentário: ")
		b.WriteString(comment)
	}

	if c := a.Observation.Comment; c != nil && strings.TrimSpace(c.Excerpt) != "" {
		b.WriteString("\n\n")
		if handle := strings.TrimSpace(c.AuthorHandle); handle != "" {
			b.WriteString("@")
			b.WriteString(handle)
		} else if c.AuthorExternalID != "" {
			b.WriteString(c.AuthorExternalID)
		}
		// Quoted, so a comment that itself reads like an instruction reads as
		// somebody's words rather than as ours.
		b.WriteString(":\n\"")
		b.WriteString(strings.TrimSpace(c.Excerpt))
		b.WriteString("\"")
	}

	// Last, and labelled: a suggestion is not a fact, and the reader has to be
	// able to tell which is which.
	if !a.Observation.Briefing.Empty() {
		b.WriteString("\n\nLeitura da IA:")
		if c := strings.TrimSpace(a.Observation.Briefing.Context); c != "" {
			b.WriteString("\n")
			b.WriteString(c)
		}
		if s := strings.TrimSpace(a.Observation.Briefing.Suggestion); s != "" {
			b.WriteString("\n")
			b.WriteString(s)
		}
	}
	return b.String()
}

// AlertFact is one thing an alert knows, under a stable key.
//
// The key exists so a NAMED template can be filled by meaning rather than by
// position: a customer whose template says {{excerpt}} should get the comment
// there, wherever they put it.
type AlertFact struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// AlertFactKeys is the canonical order, and the order a POSITIONAL template is
// filled in. Published so a settings screen can tell the operator what their
// template's variables will receive.
func AlertFactKeys() []string {
	return []string{"rule", "measurement", "where", "excerpt", "link", "briefing"}
}

// Facts is everything this alert can put into a template, in canonical order.
// No value is ever empty: WhatsApp refuses an empty parameter.
func (a Alert) Facts() []AlertFact {
	excerpt := "-"
	if c := a.Observation.Comment; c != nil {
		if text := strings.TrimSpace(c.Excerpt); text != "" {
			excerpt = text
		}
	}
	where := a.Where()
	if where == "" {
		// A template parameter cannot be empty, and "-" is better than an
		// internal id nobody can read.
		where = "-"
	}
	link := a.CommentLink()
	if link == "" {
		link = a.PostLink()
	}
	values := map[string]string{
		"rule":        a.Rule.Name,
		"measurement": a.measurement(),
		"where":       where,
		"excerpt":     excerpt,
		"link":        link,
		"briefing":    a.Observation.Briefing.Text(),
	}
	out := make([]AlertFact, 0, len(values))
	for _, key := range AlertFactKeys() {
		out = append(out, AlertFact{Key: key, Value: sanitizeTemplateParam(values[key])})
	}
	return out
}

// TemplateParams fills the canonical four, for a caller that has not resolved
// the template. Equivalent to TemplateParamsFor(nil).
func (a Alert) TemplateParams() []string { return a.TemplateParamsFor(nil) }

// TemplateParamsFor fills exactly the parameters a template declares.
//
// An approved template's shape is the CUSTOMER'S, not ours: it may have no
// variables, two, or eight, and it may name them or number them. Supplying a
// fixed four would be rejected by the provider for every template that is not
// shaped the way we guessed, and a rejection is an alert that never arrived.
//
// names is the template's body parameter names, in the order it declares them.
// Nil means "the canonical set", which is what a caller without a resolved
// template gets.
//
// Matching is by NAME first, so {{excerpt}} receives the comment wherever it
// sits; anything unmatched is filled from the canonical order, skipping facts
// a name already claimed so no two parameters carry the same thing. Whatever is
// left over is padded, because empty is refused.
func (a Alert) TemplateParamsFor(names []string) []string {
	return FillTemplateParams(a.Facts(), names)
}

// FillTemplateParams is TemplateParamsFor's rule, usable by a caller that holds
// the facts and the template's parameter names but not the Alert: the
// composition root resolves the template, so that is where the two meet.
func FillTemplateParams(facts []AlertFact, names []string) []string {
	if names == nil {
		out := make([]string, 0, len(facts))
		for _, f := range facts {
			out = append(out, f.Value)
		}
		return out
	}

	if len(facts) == 0 {
		// Nothing to say, but a declared parameter still cannot be empty.
		out := make([]string, len(names))
		for i := range out {
			out[i] = "-"
		}
		return out
	}

	byKey := make(map[string]string, len(facts))
	for _, f := range facts {
		byKey[f.Key] = f.Value
	}

	out := make([]string, len(names))
	used := make(map[string]bool, len(facts))
	unmatched := make([]int, 0, len(names))

	for i, name := range names {
		key := normalizeParamName(name)
		if value, ok := byKey[key]; ok && !used[key] {
			out[i], used[key] = value, true
			continue
		}
		unmatched = append(unmatched, i)
	}

	// Everything a name did not claim, in canonical order.
	remaining := make([]string, 0, len(facts))
	for _, f := range facts {
		if !used[f.Key] {
			remaining = append(remaining, f.Value)
		}
	}
	for n, i := range unmatched {
		if n < len(remaining) {
			out[i] = remaining[n]
			continue
		}
		// A template with more variables than we have facts. Padded rather
		// than left empty, so the send is accepted and the operator can see
		// which variables their template does not get anything useful in.
		out[i] = "-"
	}
	return out
}

// normalizeParamName folds the shapes people actually name variables in:
// case, spacing, the {{ }} an editor leaves behind, and underscores.
func normalizeParamName(name string) string {
	trimmed := strings.ToLower(strings.TrimSpace(name))
	trimmed = strings.TrimPrefix(trimmed, "{{")
	trimmed = strings.TrimSuffix(trimmed, "}}")
	return strings.TrimSpace(trimmed)
}

// sanitizeTemplateParam flattens whitespace and guarantees something non-empty.
func sanitizeTemplateParam(value string) string {
	flattened := strings.Join(strings.Fields(value), " ")
	flattened, _ = TruncateRunes(flattened, 300)
	if flattened == "" {
		return "-"
	}
	return flattened
}

// IdempotencyKey identifies THIS firing, so a retry after a timeout is the same
// send rather than a second one.
//
// It is built from the rule and the firing instant, which is the claim's own
// timestamp: the same firing retried produces the same key, and the next
// firing, minutes later at the earliest, produces a different one.
func (a Alert) IdempotencyKey() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		"comment-alert",
		a.Rule.ID,
		a.FiredAt.UTC().Format(time.RFC3339),
	}, "|")))
	return "ca-alert-" + hex.EncodeToString(sum[:])[:24]
}
