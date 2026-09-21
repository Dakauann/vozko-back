package audience

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func alertNow() time.Time { return time.Date(2026, 9, 10, 15, 0, 0, 0, time.UTC) }

func validRule() AlertRule {
	r := AlertRule{
		ID:          "rule-1",
		WorkspaceID: "ws-1",
		Source:      SourceInstagram,
		AccountID:   "acc-1",
		Name:        "Comentário grave",
		Metric:      AlertMetricCommentSeverity,
		Threshold:   80,
		Channel:     AlertChannelUnofficial,
		Recipient:   "5511999999999",
		Enabled:     true,
	}
	r.Normalize()
	return r
}

func TestAlertMetricIsAClosedSet(t *testing.T) {
	for _, m := range AllAlertMetrics() {
		if !m.Valid() {
			t.Fatalf("%q is listed but not valid", m)
		}
	}
	for _, bad := range []AlertMetric{"", "sentiment_drop", "Comment_Severity"} {
		if bad.Valid() {
			t.Fatalf("%q must not be valid", bad)
		}
	}
}

func TestAlertMetricDirectionIsNotConfigurable(t *testing.T) {
	if AlertMetricAcceptanceScore.TriggersWhenBelow() != true {
		t.Fatal("a falling acceptance score is the alarming direction")
	}
	for _, m := range []AlertMetric{
		AlertMetricCommentSeverity, AlertMetricHighSeverityCount,
		AlertMetricHostileCount, AlertMetricCommentVolume,
	} {
		if m.TriggersWhenBelow() {
			t.Fatalf("%q alarms when it rises", m)
		}
	}
}

func TestAlertMetricWindowing(t *testing.T) {
	if AlertMetricCommentSeverity.IsWindowed() {
		t.Fatal("severity is a property of one comment")
	}
	for _, m := range []AlertMetric{
		AlertMetricHighSeverityCount, AlertMetricHostileCount,
		AlertMetricCommentVolume, AlertMetricAcceptanceScore,
	} {
		if !m.IsWindowed() {
			t.Fatalf("%q is counted over a span", m)
		}
	}
}

func TestAlertRuleValidate(t *testing.T) {
	cases := map[string]struct {
		mutate  func(*AlertRule)
		wantErr bool
	}{
		"complete":            {mutate: func(*AlertRule) {}},
		"no workspace":        {mutate: func(r *AlertRule) { r.WorkspaceID = "" }, wantErr: true},
		"no account":          {mutate: func(r *AlertRule) { r.AccountID = " " }, wantErr: true},
		"no name":             {mutate: func(r *AlertRule) { r.Name = "  " }, wantErr: true},
		"unknown metric":      {mutate: func(r *AlertRule) { r.Metric = "vibes" }, wantErr: true},
		"unknown channel":     {mutate: func(r *AlertRule) { r.Channel = "sms" }, wantErr: true},
		"no recipient":        {mutate: func(r *AlertRule) { r.Recipient = "" }, wantErr: true},
		"recipient too short": {mutate: func(r *AlertRule) { r.Recipient = "1234" }, wantErr: true},
		"severity over 100":   {mutate: func(r *AlertRule) { r.Threshold = 101 }, wantErr: true},
		"severity at zero":    {mutate: func(r *AlertRule) { r.Threshold = 0 }, wantErr: true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := validRule()
			c.mutate(&r)
			r.Normalize()
			err := r.Validate()
			if c.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if c.wantErr && !errors.Is(err, ErrInvalidFilter) {
				t.Fatalf("err = %v, want ErrInvalidFilter", err)
			}
		})
	}
}

func TestAlertRuleOfficialChannelNeedsItsTemplate(t *testing.T) {
	r := validRule()
	r.Channel = AlertChannelOfficial
	r.Normalize()
	if err := r.Validate(); !errors.Is(err, ErrInvalidFilter) {
		t.Fatal("an official rule with no business phone or template must be refused")
	}

	r.BusinessPhoneID = "phone-1"
	r.TemplateID = "tpl-1"
	r.Normalize()
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAlertRuleUnofficialChannelNeedsNothingExtra(t *testing.T) {
	r := validRule()
	r.InstanceID = ""
	r.Normalize()
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAlertRuleWindowedMetricNeedsAWindow(t *testing.T) {
	r := validRule()
	r.Metric = AlertMetricHostileCount
	r.Threshold = 5
	r.WindowMinutes = 0
	r.Normalize()
	if r.WindowMinutes != DefaultAlertWindowMinutes {
		t.Fatalf("window = %d, want the default", r.WindowMinutes)
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}

	r.WindowMinutes = MaxAlertWindowMinutes + 1
	r.Normalize()
	if r.WindowMinutes != MaxAlertWindowMinutes {
		t.Fatalf("window = %d, must be clamped", r.WindowMinutes)
	}
}

func TestAlertRuleCooldownAndCapAreFloored(t *testing.T) {
	r := validRule()
	r.CooldownMinutes = 0
	r.MaxPerDay = 0
	r.Normalize()
	if r.CooldownMinutes < MinAlertCooldownMinutes {
		t.Fatalf("cooldown = %d, below the floor %d", r.CooldownMinutes, MinAlertCooldownMinutes)
	}
	if r.MaxPerDay < 1 {
		t.Fatalf("daily cap = %d, a rule that can never fire is not a rule", r.MaxPerDay)
	}

	greedy := validRule()
	greedy.CooldownMinutes = 1
	greedy.MaxPerDay = 10_000
	greedy.Normalize()
	if greedy.CooldownMinutes != MinAlertCooldownMinutes {
		t.Fatalf("cooldown = %d, want the floor", greedy.CooldownMinutes)
	}
	if greedy.MaxPerDay != MaxAlertsPerDay {
		t.Fatalf("daily cap = %d, want the ceiling %d", greedy.MaxPerDay, MaxAlertsPerDay)
	}
}

func TestAlertRuleFiresOnlyWhenTheThresholdIsCrossed(t *testing.T) {
	r := validRule()
	now := alertNow()

	if r.ShouldFire(79, now) {
		t.Fatal("below the threshold is not an alert")
	}
	if !r.ShouldFire(80, now) {
		t.Fatal("at the threshold is an alert")
	}
	if !r.ShouldFire(95, now) {
		t.Fatal("above the threshold is an alert")
	}
}

func TestAlertRuleFiresBelowForAcceptanceScore(t *testing.T) {
	r := validRule()
	r.Metric = AlertMetricAcceptanceScore
	r.Threshold = 40
	r.Normalize()
	now := alertNow()

	if r.ShouldFire(41, now) {
		t.Fatal("a healthy score is not an alert")
	}
	if !r.ShouldFire(40, now) {
		t.Fatal("at the threshold is an alert")
	}
	if !r.ShouldFire(12, now) {
		t.Fatal("a collapsed score is very much an alert")
	}
}

func TestAlertRuleDisabledNeverFires(t *testing.T) {
	r := validRule()
	r.Enabled = false
	if r.ShouldFire(100, alertNow()) {
		t.Fatal("a disabled rule must not fire")
	}
}

func TestAlertRuleRespectsTheCooldown(t *testing.T) {
	r := validRule()
	now := alertNow()
	fired := now.Add(-time.Duration(r.CooldownMinutes) * time.Minute).Add(time.Minute)
	r.LastFiredAt = &fired

	if r.ShouldFire(100, now) {
		t.Fatal("inside the cooldown a rule stays quiet")
	}

	older := now.Add(-time.Duration(r.CooldownMinutes) * time.Minute).Add(-time.Second)
	r.LastFiredAt = &older
	if !r.ShouldFire(100, now) {
		t.Fatal("past the cooldown it fires again")
	}
}

func TestAlertRuleRespectsTheDailyCap(t *testing.T) {
	r := validRule()
	now := alertNow()
	r.MaxPerDay = 3
	r.FiredToday = 3
	r.FiredDay = now.Format(alertDayLayout)

	if r.ShouldFire(100, now) {
		t.Fatal("a rule that hit its daily cap stays quiet")
	}

	r.FiredDay = now.Add(-24 * time.Hour).Format(alertDayLayout)
	if !r.ShouldFire(100, now) {
		t.Fatal("the cap resets with the day")
	}
}

func TestAlertRuleRegisterFire(t *testing.T) {
	r := validRule()
	now := alertNow()

	r.RegisterFire(now)
	if r.FiredToday != 1 || r.LastFiredAt == nil || !r.LastFiredAt.Equal(now) {
		t.Fatalf("after one fire: %+v", r)
	}

	r.RegisterFire(now.Add(time.Hour))
	if r.FiredToday != 2 {
		t.Fatalf("same day tally = %d, want 2", r.FiredToday)
	}

	tomorrow := now.Add(24 * time.Hour)
	r.RegisterFire(tomorrow)
	if r.FiredToday != 1 {
		t.Fatalf("new day tally = %d, want 1", r.FiredToday)
	}
	if r.FiredDay != tomorrow.Format(alertDayLayout) {
		t.Fatalf("day = %q", r.FiredDay)
	}
}

func alertObservation() AlertObservation {
	return AlertObservation{
		Metric:      AlertMetricCommentSeverity,
		Value:       92,
		AccountName: "prefeitura",
		Comment: &Analysis{
			ID: "row-1", WorkspaceID: "ws-1", Source: SourceInstagram,
			ContainerID: "media-1", AuthorHandle: "fulano", AuthorExternalID: "ig-9",
			Status: StatusAnalyzed, Stance: StanceHostile, Severity: 92,
			Excerpt: "vocês são uns ladrões", OccurredAt: alertNow(),
		},
	}
}

func TestAlertMessageCarriesTheReason(t *testing.T) {
	r := validRule()
	msg := NewAlert(r, alertObservation(), alertNow()).Message()

	for _, want := range []string{r.Name, "92", "vocês são uns ladrões", "@fulano"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message is missing %q:\n%s", want, msg)
		}
	}
	if strings.HasSuffix(msg, "\n") {
		t.Fatal("a trailing newline renders as an empty bubble line")
	}
}

func TestAlertMessageForAWindowedMetric(t *testing.T) {
	r := validRule()
	r.Metric = AlertMetricHostileCount
	r.Threshold = 10
	r.WindowMinutes = 60
	r.Normalize()

	obs := AlertObservation{Metric: AlertMetricHostileCount, Value: 14, AccountName: "prefeitura"}
	msg := NewAlert(r, obs, alertNow()).Message()

	if !strings.Contains(msg, "14") {
		t.Fatalf("the measured value must be in the message:\n%s", msg)
	}
	if strings.Contains(msg, "\"\"") {
		t.Fatalf("no comment means no empty quote:\n%s", msg)
	}
}

func TestAlertTemplateParamsAreOrderedAndNonEmpty(t *testing.T) {
	alert := NewAlert(validRule(), alertObservation(), alertNow())
	params := alert.TemplateParams()

	if len(params) != AlertTemplateParamCount {
		t.Fatalf("params = %d, want %d", len(params), AlertTemplateParamCount)
	}
	for i, p := range params {
		if strings.TrimSpace(p) == "" {
			t.Fatalf("param %d is empty, which WhatsApp rejects: %v", i, params)
		}
		if strings.ContainsAny(p, "\n\t") {
			t.Fatalf("param %d contains a control character: %q", i, p)
		}
	}
	if params[0] != validRule().Name {
		t.Fatalf("the first parameter should name the rule, got %q", params[0])
	}
}

func TestAlertTemplateParamsSurviveAMissingComment(t *testing.T) {
	r := validRule()
	r.Metric = AlertMetricCommentVolume
	r.Threshold = 100
	r.Normalize()

	params := NewAlert(r, AlertObservation{Metric: AlertMetricCommentVolume, Value: 250}, alertNow()).TemplateParams()
	for i, p := range params {
		if strings.TrimSpace(p) == "" {
			t.Fatalf("param %d is empty: %v", i, params)
		}
	}
}

func TestAlertIdempotencyKey(t *testing.T) {
	r := validRule()
	now := alertNow()

	first := NewAlert(r, alertObservation(), now).IdempotencyKey()
	same := NewAlert(r, alertObservation(), now).IdempotencyKey()
	if first != same {
		t.Fatalf("the same firing must produce one key: %q vs %q", first, same)
	}
	if first == "" {
		t.Fatal("an empty key defeats idempotency")
	}

	later := NewAlert(r, alertObservation(), now.Add(2*time.Hour)).IdempotencyKey()
	if later == first {
		t.Fatal("a later firing must be a different send")
	}

	other := validRule()
	other.ID = "rule-2"
	if NewAlert(other, alertObservation(), now).IdempotencyKey() == first {
		t.Fatal("two rules firing at once are two sends")
	}
}
