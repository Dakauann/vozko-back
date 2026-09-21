package audience

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

type AlertMetric string

const (
	AlertMetricCommentSeverity   AlertMetric = "comment_severity"
	AlertMetricHighSeverityCount AlertMetric = "high_severity_count"
	AlertMetricHostileCount      AlertMetric = "hostile_count"
	AlertMetricCommentVolume     AlertMetric = "comment_volume"
	AlertMetricAcceptanceScore   AlertMetric = "acceptance_score"

	AlertMetricAttendanceQuality AlertMetric = "attendance_quality"
	AlertMetricEscalationCount   AlertMetric = "escalation_count"
)

func AllAlertMetrics() []AlertMetric {
	return append(AlertMetricsFor(SubjectKindComment), AlertMetricsFor(SubjectKindConversation)...)
}

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

func (m AlertMetric) IsWindowed() bool {
	switch m {
	case AlertMetricCommentSeverity, AlertMetricAttendanceQuality:
		return false
	}
	return true
}

func (m AlertMetric) TriggersWhenBelow() bool {
	return m == AlertMetricAcceptanceScore || m == AlertMetricAttendanceQuality
}

type AlertChannel string

const (
	AlertChannelOfficial   AlertChannel = "official"
	AlertChannelUnofficial AlertChannel = "unofficial"
)

func (c AlertChannel) Valid() bool {
	return c == AlertChannelOfficial || c == AlertChannelUnofficial
}

const (
	MinAlertCooldownMinutes     = 5
	DefaultAlertCooldownMinutes = 60
	MaxAlertCooldownMinutes     = 7 * 24 * 60

	MaxAlertsPerDay     = 24
	DefaultAlertsPerDay = 6

	MinAlertWindowMinutes     = 5
	DefaultAlertWindowMinutes = 60
	MaxAlertWindowMinutes     = 24 * 60

	MaxAlertNameRunes = 60

	AlertSubjectSuppression = 24 * time.Hour

	MaxAlertMinMessages = 500

	minRecipientDigits = 8

	alertDayLayout = "2006-01-02"
)

const AlertTemplateParamCount = 6

type AlertRule struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Source      Source `json:"source"`
	AccountID   string `json:"accountId"`

	Name            string `json:"name"`
	Enabled         bool   `json:"enabled"`
	CreatedByUserID string `json:"createdByUserId,omitempty"`

	Metric        AlertMetric `json:"metric"`
	Threshold     int         `json:"threshold"`
	WindowMinutes int         `json:"windowMinutes"`
	MinMessages   int         `json:"minMessages"`

	Channel         AlertChannel `json:"channel"`
	Recipient       string       `json:"recipient"`
	BusinessPhoneID string       `json:"businessPhoneId,omitempty"`
	TemplateID      string       `json:"templateId,omitempty"`
	InstanceID      string       `json:"instanceId,omitempty"`

	Brief bool `json:"brief"`

	CooldownMinutes int `json:"cooldownMinutes"`
	MaxPerDay       int `json:"maxPerDay"`

	LastFiredAt *time.Time `json:"lastFiredAt,omitempty"`
	FiredToday  int        `json:"firedToday"`
	FiredDay    string     `json:"firedDay,omitempty"`
	LastError   string     `json:"lastError,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

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

func (r AlertRule) WatchesEveryChannel() bool { return strings.TrimSpace(string(r.Source)) == "" }

func (r AlertRule) Accepts(row *Analysis) bool {
	if r.MinMessages <= 0 || r.Metric.IsWindowed() {
		return true
	}
	if row == nil {
		return false
	}
	return row.MessageCount >= r.MinMessages
}

func (r AlertRule) Window() time.Duration {
	return time.Duration(r.WindowMinutes) * time.Minute
}

func (r AlertRule) Crossed(value int) bool {
	if r.Metric.TriggersWhenBelow() {
		return value <= r.Threshold
	}
	return value >= r.Threshold
}

func (r AlertRule) ShouldFire(value int, now time.Time) bool {
	if !r.Enabled || !r.Crossed(value) {
		return false
	}
	if r.LastFiredAt != nil && now.Sub(*r.LastFiredAt) < r.Cooldown() {
		return false
	}
	return r.FiredOn(now) < r.MaxPerDay
}

func (r AlertRule) Cooldown() time.Duration {
	return time.Duration(r.CooldownMinutes) * time.Minute
}

func (r AlertRule) FiredOn(now time.Time) int {
	if r.FiredDay != now.UTC().Format(alertDayLayout) {
		return 0
	}
	return r.FiredToday
}

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

const MaxBriefingRunes = 240

type AlertBriefing struct {
	Context    string `json:"context,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`

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

type AlertObservation struct {
	Metric      AlertMetric
	Value       int
	AccountName string
	Permalink   string
	Comment     *Analysis
	Briefing    AlertBriefing
}

type Alert struct {
	Rule        AlertRule
	Observation AlertObservation
	FiredAt     time.Time
}

func NewAlert(rule AlertRule, observation AlertObservation, firedAt time.Time) Alert {
	return Alert{Rule: rule, Observation: observation, FiredAt: firedAt.UTC()}
}

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

func (a Alert) Where() string {
	return strings.TrimSpace(a.Observation.AccountName)
}

func (a Alert) PostLink() string {
	return strings.TrimSpace(a.Observation.Permalink)
}

func (a Alert) CommentLink() string {
	post := a.PostLink()
	c := a.Observation.Comment
	if post == "" || c == nil || strings.TrimSpace(c.SubjectID) == "" {
		return ""
	}
	return strings.TrimSuffix(post, "/") + "/c/" + strings.TrimSpace(c.SubjectID) + "/"
}

func (a Alert) Message() string {
	var b strings.Builder
	if a.Observation.Metric.SubjectKind() == SubjectKindConversation {
		b.WriteString("Alerta de conversas\n\n")
	} else {
		b.WriteString("Alerta de comentários\n\n")
	}
	b.WriteString(a.Headline())

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
		b.WriteString(":\n\"")
		b.WriteString(strings.TrimSpace(c.Excerpt))
		b.WriteString("\"")
	}

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

type AlertFact struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func AlertFactKeys() []string {
	return []string{"rule", "measurement", "where", "excerpt", "link", "briefing"}
}

func (a Alert) Facts() []AlertFact {
	excerpt := "-"
	if c := a.Observation.Comment; c != nil {
		if text := strings.TrimSpace(c.Excerpt); text != "" {
			excerpt = text
		}
	}
	where := a.Where()
	if where == "" {
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

func (a Alert) TemplateParams() []string { return a.TemplateParamsFor(nil) }

func (a Alert) TemplateParamsFor(names []string) []string {
	return FillTemplateParams(a.Facts(), names)
}

func FillTemplateParams(facts []AlertFact, names []string) []string {
	if names == nil {
		out := make([]string, 0, len(facts))
		for _, f := range facts {
			out = append(out, f.Value)
		}
		return out
	}

	if len(facts) == 0 {
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
		out[i] = "-"
	}
	return out
}

func normalizeParamName(name string) string {
	trimmed := strings.ToLower(strings.TrimSpace(name))
	trimmed = strings.TrimPrefix(trimmed, "{{")
	trimmed = strings.TrimSuffix(trimmed, "}}")
	return strings.TrimSpace(trimmed)
}

func sanitizeTemplateParam(value string) string {
	flattened := strings.Join(strings.Fields(value), " ")
	flattened, _ = TruncateRunes(flattened, 300)
	if flattened == "" {
		return "-"
	}
	return flattened
}

func (a Alert) IdempotencyKey() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		"comment-alert",
		a.Rule.ID,
		a.FiredAt.UTC().Format(time.RFC3339),
	}, "|")))
	return "ca-alert-" + hex.EncodeToString(sum[:])[:24]
}
