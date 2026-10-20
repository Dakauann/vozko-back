package tiktok

import (
	"errors"
	"reflect"
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
	}
	for _, tc := range cases {
		if got := tc.from.CanTransitionTo(tc.to); got != tc.want {
			t.Errorf("%q -> %q = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestParseStatusRefusesUnknownValues(t *testing.T) {
	for _, raw := range []string{"", "connected", "TOKEN_EXPIRED", "ACTIVE"} {
		if _, err := ParseStatus(raw); !errors.Is(err, ErrInvalidStatus) {
			t.Errorf("ParseStatus(%q) err = %v, want ErrInvalidStatus", raw, err)
		}
	}
}

func TestParseScopesSplitsOnCommasAndSpacesWithoutDuplicates(t *testing.T) {
	got := ParseScopes("user.info.basic,video.list, comment.list  comment.list,,message.list.read")
	want := []string{ScopeUserInfoBasic, ScopeVideoList, ScopeCommentList, ScopeMessageRead}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseScopes = %v, want %v", got, want)
	}
}

func TestCapabilitiesRequireEveryScopeTheyUse(t *testing.T) {
	cases := []struct {
		name     string
		scopes   []string
		eligible bool
		want     AccountCapabilities
	}{
		{"nothing", nil, true, AccountCapabilities{}},
		{"videos only", []string{ScopeVideoList}, true, AccountCapabilities{CanReadVideos: true}},
		{"comment list without videos", []string{ScopeCommentList}, true, AccountCapabilities{}},
		{"read comments", []string{ScopeVideoList, ScopeCommentList}, true, AccountCapabilities{CanReadVideos: true, CanReadComments: true}},
		{"manage without read", []string{ScopeVideoList, ScopeCommentManage}, true, AccountCapabilities{CanReadVideos: true}},
		{"manage comments", []string{ScopeVideoList, ScopeCommentList, ScopeCommentManage}, true, AccountCapabilities{CanReadVideos: true, CanReadComments: true, CanManageComments: true}},
		{"send without read", []string{ScopeMessageSend}, true, AccountCapabilities{}},
		{"messaging", []string{ScopeMessageRead, ScopeMessageSend}, true, AccountCapabilities{CanReceiveMessages: true, CanSendMessages: true}},
		{"inbox profile", []string{ScopeMessageRead, ScopeMessageManage}, true, AccountCapabilities{CanReceiveMessages: true, CanManageInboxProfile: true}},
		{"messaging on an ineligible account", []string{ScopeMessageRead, ScopeMessageSend, ScopeMessageManage, ScopeVideoList, ScopeCommentList}, false, AccountCapabilities{CanReadVideos: true, CanReadComments: true}},
	}
	for _, tc := range cases {
		if got := CapabilitiesFrom(tc.scopes, tc.eligible); got != tc.want {
			t.Errorf("%s: CapabilitiesFrom = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestCapabilitiesUsefulNeedsCommentsOrMessages(t *testing.T) {
	if (AccountCapabilities{CanReadVideos: true}).Useful() {
		t.Error("videos alone must not count as a usable connection")
	}
	if !(AccountCapabilities{CanReadComments: true}).Useful() {
		t.Error("reading comments is a usable connection")
	}
	if !(AccountCapabilities{CanReceiveMessages: true}).Useful() {
		t.Error("receiving messages is a usable connection")
	}
}

func TestMessagingEligibility(t *testing.T) {
	cases := []struct {
		advanced, business, want bool
	}{
		{false, false, false},
		{true, false, true},
		{false, true, true},
		{true, true, true},
	}
	for _, tc := range cases {
		if got := MessagingEligible(tc.advanced, tc.business); got != tc.want {
			t.Errorf("MessagingEligible(%v, %v) = %v", tc.advanced, tc.business, got)
		}
	}
}

func TestAccountUsabilityFailsClosed(t *testing.T) {
	base := Account{Status: StatusConnected, AccessToken: "tok", Capabilities: AccountCapabilities{CanReceiveMessages: true, CanSendMessages: true, CanReadComments: true, CanManageComments: true}}
	if !base.CanSend() || !base.CanManageCommentsNow() || !base.CanReadCommentsNow() || !base.CanReceiveMessagesNow() {
		t.Fatal("a connected account with every capability must be usable")
	}
	noToken := base
	noToken.AccessToken = " "
	if noToken.CanSend() || noToken.CanManageCommentsNow() {
		t.Error("an account without an access token must not be usable")
	}
	reconnect := base
	reconnect.Status = StatusNeedsReconnect
	if reconnect.CanSend() || reconnect.CanReadCommentsNow() {
		t.Error("an account that needs reconnecting must not be usable")
	}
	noSend := base
	noSend.Capabilities.CanSendMessages = false
	if noSend.CanSend() {
		t.Error("an account without the send capability must not send")
	}
}

func TestTokenNeedsRefresh(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		account Account
		want    bool
	}{
		{"fresh", Account{Status: StatusConnected, RefreshToken: "r", AccessExpiresAt: ptrTime(now.Add(20 * time.Hour))}, false},
		{"inside the lead", Account{Status: StatusConnected, RefreshToken: "r", AccessExpiresAt: ptrTime(now.Add(TokenRefreshLead - time.Minute))}, true},
		{"expired", Account{Status: StatusConnected, RefreshToken: "r", AccessExpiresAt: ptrTime(now.Add(-time.Hour))}, true},
		{"unknown expiry", Account{Status: StatusConnected, RefreshToken: "r"}, true},
		{"no refresh token", Account{Status: StatusConnected, AccessExpiresAt: ptrTime(now.Add(-time.Hour))}, false},
		{"not connected", Account{Status: StatusRevoked, RefreshToken: "r", AccessExpiresAt: ptrTime(now.Add(-time.Hour))}, false},
	}
	for _, tc := range cases {
		if got := tc.account.TokenNeedsRefresh(now); got != tc.want {
			t.Errorf("%s: TokenNeedsRefresh = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestReconnectSoon(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		expires *time.Time
		want    bool
	}{
		{"unknown", nil, false},
		{"far", ptrTime(now.Add(200 * 24 * time.Hour)), false},
		{"inside the warning", ptrTime(now.Add(ReconnectWarningLead - time.Hour)), true},
		{"past", ptrTime(now.Add(-time.Hour)), true},
	}
	for _, tc := range cases {
		a := Account{Status: StatusConnected, RefreshExpiresAt: tc.expires}
		if got := a.ReconnectSoon(now); got != tc.want {
			t.Errorf("%s: ReconnectSoon = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestPurgeDueOnlyForDisconnectedOrRevokedAccountsPastTheirDate(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	past := ptrTime(now.Add(-time.Minute))
	future := ptrTime(now.Add(time.Hour))
	cases := []struct {
		name    string
		account Account
		want    bool
	}{
		{"no date", Account{Status: StatusDisconnected}, false},
		{"future", Account{Status: StatusDisconnected, PurgeAfter: future}, false},
		{"disconnected past", Account{Status: StatusDisconnected, PurgeAfter: past}, true},
		{"revoked past", Account{Status: StatusRevoked, PurgeAfter: past}, true},
		{"reconnected keeps data", Account{Status: StatusConnected, PurgeAfter: past}, false},
	}
	for _, tc := range cases {
		if got := tc.account.PurgeDue(now); got != tc.want {
			t.Errorf("%s: PurgeDue = %v, want %v", tc.name, got, tc.want)
		}
	}
	if got := PurgeScheduleFrom(now); !got.Equal(now.Add(30 * 24 * time.Hour)) {
		t.Errorf("PurgeScheduleFrom = %v", got)
	}
}

func TestAccountAutomationCarriesEverySwitch(t *testing.T) {
	a := Account{
		AgentID:              ptrString("agent"),
		WorkflowID:           ptrString("wf"),
		EnableAgentResponses: true,
		EnableWorkflow:       true,
		EnableAnalysis:       true,
		EnableAutoStaging:    true,
		EnableAutoMemory:     true,
		AutomationDisclosure: "  Resposta automática  ",
	}
	got := a.Automation()
	if *got.AgentID != "agent" || *got.WorkflowID != "wf" || !got.EnableAgentResponses || !got.EnableWorkflow ||
		!got.EnableAnalysis || !got.EnableAutoStaging || !got.EnableAutoMemory || got.Disclosure != "Resposta automática" {
		t.Fatalf("Automation() = %+v", got)
	}
}

func TestAccountValidate(t *testing.T) {
	valid := Account{WorkspaceID: "ws", OpenID: "_000abc", Status: StatusConnected}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid account: %v", err)
	}
	noWorkspace := valid
	noWorkspace.WorkspaceID = ""
	if err := noWorkspace.Validate(); !errors.Is(err, ErrWorkspaceIDRequired) {
		t.Errorf("missing workspace: %v", err)
	}
	badOpenID := valid
	badOpenID.OpenID = "a b"
	if err := badOpenID.Validate(); !errors.Is(err, ErrOpenIDInvalid) {
		t.Errorf("bad open id: %v", err)
	}
	badStatus := valid
	badStatus.Status = "x"
	if err := badStatus.Validate(); !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("bad status: %v", err)
	}
}

func TestDisplayNamePrefersTheProfileName(t *testing.T) {
	if got := (&Account{DisplayName: " Loja ", Username: "loja", OpenID: "x"}).Name(); got != "Loja" {
		t.Errorf("Name = %q", got)
	}
	if got := (&Account{Username: "loja", OpenID: "x"}).Name(); got != "@loja" {
		t.Errorf("Name = %q", got)
	}
	if got := (&Account{OpenID: "x"}).Name(); got != "x" {
		t.Errorf("Name = %q", got)
	}
}
