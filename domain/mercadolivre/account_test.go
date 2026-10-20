package mercadolivre

import (
	"errors"
	"testing"
	"time"
)

func ptrString(s string) *string { return &s }

func ptrTime(t time.Time) *time.Time { return &t }

func TestStatusTransitions(t *testing.T) {
	cases := []struct {
		from Status
		to   Status
		want bool
	}{
		{StatusConnected, StatusConnected, true},
		{StatusConnected, StatusNeedsReconnect, true},
		{StatusConnected, StatusRevoked, true},
		{StatusConnected, StatusDisconnected, true},
		{StatusNeedsReconnect, StatusConnected, true},
		{StatusNeedsReconnect, StatusRevoked, true},
		{StatusNeedsReconnect, StatusDisconnected, true},
		{StatusRevoked, StatusConnected, true},
		{StatusRevoked, StatusDisconnected, true},
		{StatusRevoked, StatusNeedsReconnect, false},
		{StatusDisconnected, StatusConnected, true},
		{StatusDisconnected, StatusNeedsReconnect, false},
		{StatusDisconnected, StatusRevoked, false},
		{StatusConnected, Status("PAUSED"), false},
		{Status(""), StatusConnected, false},
		{Status("PAUSED"), StatusConnected, false},
	}
	for _, tc := range cases {
		if got := tc.from.CanTransitionTo(tc.to); got != tc.want {
			t.Errorf("%q -> %q = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestParseStatusRefusesUnknownValues(t *testing.T) {
	for _, s := range []Status{StatusConnected, StatusNeedsReconnect, StatusRevoked, StatusDisconnected} {
		got, err := ParseStatus(string(s))
		if err != nil || got != s {
			t.Errorf("ParseStatus(%q) = %q, %v", s, got, err)
		}
	}
	for _, raw := range []string{"", "connected", "PENDING", "ACTIVE"} {
		if _, err := ParseStatus(raw); !errors.Is(err, ErrInvalidStatus) {
			t.Errorf("ParseStatus(%q) err = %v, want ErrInvalidStatus", raw, err)
		}
	}
}

func TestTokenNeedsRefresh(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		status  Status
		refresh string
		expiry  *time.Time
		want    bool
	}{
		{"far from expiry", StatusConnected, "TG-1", ptrTime(now.Add(11 * time.Minute)), false},
		{"exactly ten minutes left", StatusConnected, "TG-1", ptrTime(now.Add(10 * time.Minute)), true},
		{"inside the lead", StatusConnected, "TG-1", ptrTime(now.Add(5 * time.Minute)), true},
		{"already expired", StatusConnected, "TG-1", ptrTime(now.Add(-time.Hour)), true},
		{"unknown expiry", StatusConnected, "TG-1", nil, true},
		{"needs reconnect never retries the dead token", StatusNeedsReconnect, "TG-1", ptrTime(now.Add(-time.Hour)), false},
		{"revoked", StatusRevoked, "TG-1", ptrTime(now.Add(-time.Hour)), false},
		{"disconnected", StatusDisconnected, "TG-1", ptrTime(now.Add(-time.Hour)), false},
		{"no refresh token", StatusConnected, "", ptrTime(now.Add(-time.Hour)), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &Account{Status: tc.status, RefreshToken: tc.refresh, AccessExpiresAt: tc.expiry}
			if got := a.TokenNeedsRefresh(now); got != tc.want {
				t.Errorf("TokenNeedsRefresh = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAutomationNeverRunsTheAnalysisEngine(t *testing.T) {
	a := &Account{
		AgentID:              ptrString("agent-1"),
		WorkflowID:           ptrString("wf-1"),
		EnableAgentResponses: true,
		EnableWorkflow:       true,
		AutomationDisclosure: "  Sou a assistente da loja.  ",
	}
	got := a.Automation()
	if got.AgentID == nil || *got.AgentID != "agent-1" || got.WorkflowID == nil || *got.WorkflowID != "wf-1" {
		t.Fatalf("agent or workflow lost: %+v", got)
	}
	if !got.EnableAgentResponses || !got.EnableWorkflow {
		t.Fatalf("toggles lost: %+v", got)
	}
	if got.EnableAnalysis || got.EnableAutoMemory || got.EnableAutoStaging || got.RunsAnalysis() {
		t.Fatalf("analysis, auto staging and auto memory must stay off for Mercado Livre: %+v", got)
	}
	if got.Disclosure != "Sou a assistente da loja." {
		t.Fatalf("disclosure = %q", got.Disclosure)
	}
	if empty := (&Account{}).Automation(); empty.Disclosure != "" || empty.RunsAnalysis() {
		t.Fatalf("an account without settings must automate nothing: %+v", empty)
	}
}

func TestPurgeDue(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		status Status
		after  *time.Time
		want   bool
	}{
		{"never scheduled", StatusDisconnected, nil, false},
		{"revoked and past", StatusRevoked, ptrTime(now.Add(-time.Minute)), true},
		{"disconnected at the instant", StatusDisconnected, ptrTime(now), true},
		{"still in grace", StatusDisconnected, ptrTime(now.Add(time.Minute)), false},
		{"connected account is never purged", StatusConnected, ptrTime(now.Add(-time.Hour)), false},
		{"needs reconnect is never purged", StatusNeedsReconnect, ptrTime(now.Add(-time.Hour)), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &Account{Status: tc.status, PurgeAfter: tc.after}
			if got := a.PurgeDue(now); got != tc.want {
				t.Errorf("PurgeDue = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPurgeScheduleUsesTheThirtyDayGrace(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	if got := PurgeScheduleFrom(now); !got.Equal(now.Add(30 * 24 * time.Hour)) {
		t.Fatalf("PurgeScheduleFrom = %v", got)
	}
}

func TestAccountValidate(t *testing.T) {
	valid := func() *Account {
		return &Account{WorkspaceID: "ws-1", MLUserID: "415458330", SiteID: SiteMLB, Status: StatusConnected}
	}
	cases := []struct {
		name   string
		mutate func(a *Account)
		want   error
	}{
		{"valid", func(a *Account) {}, nil},
		{"missing workspace", func(a *Account) { a.WorkspaceID = " " }, ErrWorkspaceIDRequired},
		{"missing user", func(a *Account) { a.MLUserID = "" }, ErrUserIDInvalid},
		{"non numeric user", func(a *Account) { a.MLUserID = "41545x" }, ErrUserIDInvalid},
		{"other site", func(a *Account) { a.SiteID = "MLA" }, ErrSiteNotSupported},
		{"empty site", func(a *Account) { a.SiteID = "" }, ErrSiteNotSupported},
		{"unknown status", func(a *Account) { a.Status = "PENDING" }, ErrInvalidStatus},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := valid()
			tc.mutate(a)
			err := a.Validate()
			if tc.want == nil && err != nil {
				t.Fatalf("Validate = %v", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("Validate = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestAccountAbilitiesFollowStatusTokenAndGrants(t *testing.T) {
	base := func() *Account {
		return &Account{
			Status:             StatusConnected,
			MLUserID:           "415458330",
			AccessToken:        "APP_USR-1",
			CanReadMessages:    true,
			CanSendMessages:    true,
			CanReadQuestions:   true,
			CanAnswerQuestions: true,
		}
	}
	cases := []struct {
		name                       string
		mutate                     func(a *Account)
		send, receive, answer, see bool
	}{
		{"fully granted", func(a *Account) {}, true, true, true, true},
		{"needs reconnect", func(a *Account) { a.Status = StatusNeedsReconnect }, false, false, false, false},
		{"token wiped", func(a *Account) { a.AccessToken = "" }, false, false, false, false},
		{"send not granted", func(a *Account) { a.CanSendMessages = false }, false, true, true, true},
		{"messages not readable", func(a *Account) { a.CanReadMessages = false }, true, false, true, true},
		{"answer not granted", func(a *Account) { a.CanAnswerQuestions = false }, true, true, false, true},
		{"questions not readable", func(a *Account) { a.CanReadQuestions = false }, true, true, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := base()
			tc.mutate(a)
			if a.CanSend() != tc.send || a.CanReceiveMessages() != tc.receive || a.CanAnswer() != tc.answer || a.CanReadQuestionsNow() != tc.see {
				t.Fatalf("send=%v receive=%v answer=%v questions=%v", a.CanSend(), a.CanReceiveMessages(), a.CanAnswer(), a.CanReadQuestionsNow())
			}
		})
	}
}

func TestCredentialsCarryTheSellerAndTheToken(t *testing.T) {
	a := &Account{MLUserID: "415458330", AccessToken: "APP_USR-1"}
	c := a.Credentials()
	if c.UserID != "415458330" || c.AccessToken != "APP_USR-1" {
		t.Fatalf("credentials = %+v", c)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate = %v", err)
	}
	for _, bad := range []Credentials{{UserID: "", AccessToken: "x"}, {UserID: "1a", AccessToken: "x"}, {UserID: "1", AccessToken: " "}} {
		if err := bad.Validate(); !errors.Is(err, ErrCredentialsInvalid) {
			t.Errorf("Validate(%+v) = %v", bad, err)
		}
	}
}

func TestParseScopes(t *testing.T) {
	got := ParseScopes("  offline_access read  write read ")
	want := []string{"offline_access", "read", "write"}
	if len(got) != len(want) {
		t.Fatalf("ParseScopes = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ParseScopes = %v", got)
		}
	}
	if !HasRequiredScopes(got) {
		t.Fatal("offline_access read write must satisfy the required scopes")
	}
	if HasRequiredScopes([]string{"read", "write"}) {
		t.Fatal("without offline_access the grant cannot refresh")
	}
}
