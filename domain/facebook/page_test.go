package facebook

import (
	"errors"
	"testing"
	"time"
)

func TestStatusTransitions(t *testing.T) {
	cases := []struct {
		from, to Status
		want     bool
	}{
		{StatusPending, StatusConnected, true},
		{StatusConnected, StatusTokenRevoked, true},
		{StatusConnected, StatusNeedsRole, true},
		{StatusConnected, StatusRestricted, true},
		{StatusConnected, StatusDisconnected, true},
		{StatusTokenRevoked, StatusConnected, true},
		{StatusNeedsRole, StatusConnected, true},
		{StatusRestricted, StatusConnected, true},
		{StatusDisconnected, StatusConnected, true},
		{StatusPending, StatusRestricted, false},
		{StatusDisconnected, StatusTokenRevoked, false},
		{StatusConnected, "BOGUS", false},
		{StatusConnected, StatusConnected, true},
	}
	for _, tc := range cases {
		if got := tc.from.CanTransitionTo(tc.to); got != tc.want {
			t.Errorf("%s -> %s = %t, want %t", tc.from, tc.to, got, tc.want)
		}
	}
}

func adminPage() *Page {
	return &Page{
		ID: "p", WorkspaceID: "ws", FBPageID: "123", Status: StatusConnected,
		Tasks: []Task{TaskManage},
		GrantedScopes: []string{
			ScopeShowList, ScopeMessaging, ScopeManageMetadata, ScopeReadEngagement,
			ScopeReadUserContent, ScopeManagePosts, ScopeManageEngagement, ScopeBusinessManagement,
		},
	}
}

func TestAdminPageCanDoEverything(t *testing.T) {
	p := adminPage()
	for _, c := range AllCapabilities() {
		if !p.Can(c) {
			t.Errorf("admin page cannot %s", c)
		}
	}
}

func TestCapabilitiesNeedBothPermissionAndTask(t *testing.T) {
	cases := []struct {
		name       string
		scopes     []string
		tasks      []Task
		capability Capability
		want       bool
	}{
		{"messaging with the messaging task", []string{ScopeMessaging, ScopeManageMetadata}, []Task{TaskMessaging}, CapMessaging, true},
		{"messaging without the permission", []string{ScopeManageMetadata}, []Task{TaskMessaging}, CapMessaging, false},
		{"messaging without the task", []string{ScopeMessaging, ScopeManageMetadata}, []Task{TaskAnalyze}, CapMessaging, false},
		{"publish needs create content", []string{ScopeManagePosts, ScopeReadEngagement}, []Task{TaskModerate}, CapPublish, false},
		{"publish with create content", []string{ScopeManagePosts, ScopeReadEngagement}, []Task{TaskCreateContent}, CapPublish, true},
		{"moderate with the moderate task", []string{ScopeManageEngagement, ScopeReadUserContent}, []Task{TaskModerate}, CapModerate, true},
		{"subscribe with moderate", []string{ScopeManageMetadata}, []Task{TaskModerate}, CapSubscribe, true},
		{"subscribe with analyze only", []string{ScopeManageMetadata}, []Task{TaskAnalyze}, CapSubscribe, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &Page{Status: StatusConnected, GrantedScopes: tc.scopes, Tasks: tc.tasks}
			if got := p.Can(tc.capability); got != tc.want {
				t.Fatalf("got %t want %t", got, tc.want)
			}
		})
	}
}

func TestNoCapabilityUnlessConnected(t *testing.T) {
	p := adminPage()
	p.Status = StatusTokenRevoked
	for _, c := range AllCapabilities() {
		if p.Can(c) {
			t.Errorf("%s allowed on a revoked page", c)
		}
	}
}

func TestEmptyGrantMeansNothing(t *testing.T) {
	p := &Page{Status: StatusConnected, Tasks: []Task{TaskManage}}
	for _, c := range AllCapabilities() {
		if p.Can(c) {
			t.Errorf("%s allowed without any granted permission", c)
		}
	}
}

func TestPageValidateAndNormalize(t *testing.T) {
	p := &Page{WorkspaceID: " ws ", FBPageID: " 123 ", GrantedScopes: []string{"a", " a", "", "b"}, Tasks: []Task{"manage", "MANAGE", " create_content "}}
	p.Normalize()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusPending || p.FBPageID != "123" || len(p.GrantedScopes) != 2 || len(p.Tasks) != 2 {
		t.Fatalf("normalized = %+v", p)
	}
	if err := (&Page{WorkspaceID: "ws"}).Validate(); !errors.Is(err, ErrPageIDRequired) {
		t.Fatalf("got %v", err)
	}
	if err := (&Page{FBPageID: "1"}).Validate(); !errors.Is(err, ErrWorkspaceIDRequired) {
		t.Fatalf("got %v", err)
	}
}

func TestGrantedPagesIntersectsEveryScopeOfACapability(t *testing.T) {
	grant := &Grant{GranularScopes: map[string][]string{
		ScopeShowList:       {"1", "2", "3"},
		ScopeMessaging:      {"1", "2"},
		ScopeManageMetadata: {"1", "3"},
	}}
	if got := grant.ScopesForPage("1"); len(got) != 3 {
		t.Fatalf("page 1 scopes = %v", got)
	}
	if got := grant.ScopesForPage("3"); len(got) != 2 {
		t.Fatalf("page 3 scopes = %v", got)
	}
	if !grant.Lists("2") || grant.Lists("9") {
		t.Fatal("page listing wrong")
	}
}

func TestGrantScopesWithoutTargetsApplyToEveryListedPage(t *testing.T) {
	grant := &Grant{GranularScopes: map[string][]string{
		ScopeShowList:           {"1"},
		ScopeBusinessManagement: nil,
	}}
	got := grant.ScopesForPage("1")
	if len(got) != 2 {
		t.Fatalf("scopes = %v", got)
	}
}

func TestAutomationConfig(t *testing.T) {
	agent := "a"
	p := &Page{AgentID: &agent, EnableAgentResponses: true, EnableAnalysis: true}
	cfg := p.Automation()
	if !cfg.HasAgent() || !cfg.RunsAnalysis() || cfg.RunsWorkflows() {
		t.Fatalf("automation = %+v", cfg)
	}
}

func TestConversationLastInbound(t *testing.T) {
	now := time.Now()
	c := &Conversation{LastCustomerMessageAt: &now}
	if c.LastInboundAt() != &now {
		t.Fatal("last inbound must be the customer clock")
	}
}

func TestWatermarksOnlyMoveForward(t *testing.T) {
	early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	late := early.Add(time.Hour)
	c := &Conversation{}
	if !c.WatermarkAdvances(WatermarkRead, early) {
		t.Fatal("first watermark must advance")
	}
	c.ReadWatermark = &late
	if c.WatermarkAdvances(WatermarkRead, early) {
		t.Fatal("older watermark must not advance")
	}
	if !c.WatermarkAdvances(WatermarkDelivered, early) {
		t.Fatal("delivery watermark is independent")
	}
}

func TestContactDisplayName(t *testing.T) {
	cases := []struct {
		c    Contact
		want string
	}{
		{Contact{Name: "Maria Silva"}, "Maria Silva"},
		{Contact{FirstName: "Maria", LastName: "Silva"}, "Maria Silva"},
		{Contact{FirstName: "Maria"}, "Maria"},
		{Contact{PSID: "1234567890"}, "Facebook user 7890"},
		{Contact{PSID: "12"}, "Facebook user 12"},
	}
	for _, tc := range cases {
		if got := tc.c.DisplayName(); got != tc.want {
			t.Errorf("%+v display = %q, want %q", tc.c, got, tc.want)
		}
	}
}

func TestProfileStaleness(t *testing.T) {
	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	fetched := now.Add(-8 * 24 * time.Hour)
	recent := now.Add(-time.Hour)
	cases := []struct {
		name string
		c    Contact
		want bool
	}{
		{"never fetched", Contact{}, true},
		{"old available profile", Contact{ProfileStatus: ProfileAvailable, ProfileFetchedAt: &fetched}, true},
		{"fresh profile", Contact{ProfileStatus: ProfileAvailable, ProfileFetchedAt: &recent}, false},
		{"denied recently stays denied", Contact{ProfileStatus: ProfileDenied, ProfileFetchedAt: &fetched}, false},
		{"no profile never retried soon", Contact{ProfileStatus: ProfileNone, ProfileFetchedAt: &fetched}, false},
	}
	for _, tc := range cases {
		if got := tc.c.ProfileIsStale(now); got != tc.want {
			t.Errorf("%s: stale = %t, want %t", tc.name, got, tc.want)
		}
	}
}

type codedTestErr struct{ code, subcode int }

func (c codedTestErr) Error() string         { return "coded" }
func (c codedTestErr) ErrorCode() (int, int) { return c.code, c.subcode }

func TestHasCodeMatchesCodeOrSubcode(t *testing.T) {
	if !HasCode(codedTestErr{code: 100, subcode: CodeNoProfile}, CodeNoProfile) || !HasCode(codedTestErr{code: CodeNoProfile}, CodeNoProfile) {
		t.Fatal("code or subcode must match")
	}
	if HasCode(errors.New("plain"), CodeNoProfile) {
		t.Fatal("plain error matched")
	}
}

func TestPageAutomationAlwaysCarriesADisclosure(t *testing.T) {
	custom := (&Page{AutomationDisclosure: "  Sou a Lia, assistente da loja.  "}).Automation()
	if custom.Disclosure != "Sou a Lia, assistente da loja." {
		t.Errorf("custom = %q", custom.Disclosure)
	}
	if fallback := (&Page{}).Automation(); fallback.Disclosure != DefaultAutomationDisclosure {
		t.Errorf("fallback = %q", fallback.Disclosure)
	}
}
