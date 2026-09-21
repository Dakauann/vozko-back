package unofficial_whatsapp

import (
	"testing"
	"time"
)

func TestStatusLifecycle(t *testing.T) {
	cases := []struct {
		name  string
		from  Status
		to    Status
		allow bool
	}{
		{"provision to awaiting scan", StatusProvisioning, StatusAwaitingScan, true},
		{"provision straight to connected", StatusProvisioning, StatusConnected, false},
		{"scan expires back to disconnected", StatusAwaitingScan, StatusDisconnected, true},
		{"scan succeeds", StatusAwaitingScan, StatusConnected, true},
		{"connected drops", StatusConnected, StatusDisconnected, true},
		{"connected hibernates", StatusConnected, StatusHibernated, true},
		{"hibernated wakes without a new scan", StatusHibernated, StatusConnected, true},
		{"disconnected relinks", StatusDisconnected, StatusAwaitingScan, true},
		{"anything can be banned", StatusConnected, StatusBanned, true},
		{"a ban is terminal", StatusBanned, StatusConnected, false},
		{"a ban cannot even be relinked", StatusBanned, StatusAwaitingScan, false},
		{"unknown target refused", StatusConnected, Status("PENDING"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.from.CanTransitionTo(tc.to); got != tc.allow {
				t.Errorf("%s → %s = %v, want %v", tc.from, tc.to, got, tc.allow)
			}
		})
	}
}

func TestBanIsTerminal(t *testing.T) {
	if !StatusBanned.Terminal() {
		t.Error("a banned number must be terminal")
	}
	for _, s := range []Status{StatusConnected, StatusDisconnected, StatusHibernated, StatusAwaitingScan} {
		if s.Terminal() {
			t.Errorf("%s must not be terminal", s)
		}
	}
}

func TestCanSendDistinguishesItsRefusals(t *testing.T) {
	now := time.Now().UTC()
	future := now.Add(time.Hour)
	blocked := false

	cases := []struct {
		name     string
		instance Instance
		wantOK   bool
		wantErr  error
	}{
		{
			name:     "live session sends",
			instance: Instance{Status: StatusConnected},
			wantOK:   true,
		},
		{
			name:     "dead session names the session",
			instance: Instance{Status: StatusDisconnected},
			wantErr:  ErrInstanceNotConnected,
		},
		{
			name: "restriction names WhatsApp, not us",
			instance: Instance{
				Status:      StatusConnected,
				Restriction: Restriction{Until: &future},
			},
			wantErr: ErrRestrictedByWA,
		},
		{
			name: "an explicit no from WhatsApp also blocks",
			instance: Instance{
				Status:      StatusConnected,
				Restriction: Restriction{CanSendNewChats: &blocked},
			},
			wantErr: ErrRestrictedByWA,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := tc.instance.CanSend(now)
			if ok != tc.wantOK {
				t.Errorf("CanSend ok = %v, want %v", ok, tc.wantOK)
			}
			if err != tc.wantErr {
				t.Errorf("CanSend err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestRestrictionUnknownIsNotRestricted(t *testing.T) {
	if (Restriction{}).Active(time.Now().UTC()) {
		t.Error("an unchecked restriction must not block sending")
	}
}

func TestRestrictionExpires(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	if (Restriction{Until: &past}).Active(now) {
		t.Error("a restriction whose window has passed must not still block")
	}
}

func TestNormalizePhoneKeepsOnlyDigits(t *testing.T) {
	cases := map[string]string{
		"+55 (11) 99999-9999": "5511999999999",
		"5511999999999":       "5511999999999",
		"+5511999999999":      "5511999999999",
		"":                    "",
		"não é número":        "",
	}
	for in, want := range cases {
		if got := NormalizePhone(in); got != want {
			t.Errorf("NormalizePhone(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPhoneFromJIDOnlyReadsUserJIDs(t *testing.T) {
	cases := map[string]string{
		"5511999999999@s.whatsapp.net":    "5511999999999",
		"5511999999999:12@s.whatsapp.net": "5511999999999",
		"189923456789012@lid":             "",
		"189923456789012@LID":             "",
		"120363012345678901@g.us":         "",
		"120363012345678901@newsletter":   "",
		"+55 11 99999-9999":               "5511999999999",
		"":                                "",
	}
	for in, want := range cases {
		if got := PhoneFromJID(in); got != want {
			t.Errorf("PhoneFromJID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGroupSubjectHasNoNumber(t *testing.T) {
	group := &Contact{JID: "120363012345678901@g.us", IsGroup: true}
	if got := group.Handle(); got != "" {
		t.Errorf("group Handle() = %q, want empty", got)
	}
	if got := group.DisplayName(); got != UnnamedGroupLabel {
		t.Errorf("unnamed group DisplayName() = %q, want %q", got, UnnamedGroupLabel)
	}

	group.Name = "Time Comercial"
	if got := group.DisplayName(); got != "Time Comercial" {
		t.Errorf("named group DisplayName() = %q, want the subject", got)
	}

	person := &Contact{JID: "5511999999999@s.whatsapp.net", PhoneNumber: "5511999999999"}
	if got := person.Handle(); got != "+5511999999999" {
		t.Errorf("person Handle() = %q, want the number", got)
	}
}

func TestGroupScopeAndAutomationAreSeparate(t *testing.T) {
	off := false
	group := &Conversation{IsGroup: true}
	private := &Conversation{}

	if group.InScope(false) {
		t.Error("a group is out of scope unless its instance opts in")
	}
	if !group.InScope(true) {
		t.Error("HandleGroups must bring a group into scope")
	}
	if !private.InScope(false) {
		t.Error("a private chat is always in scope")
	}

	if group.RunsAutomation(false) {
		t.Error("an out-of-scope group must not run automation")
	}
	if !group.RunsAutomation(true) {
		t.Error("an opted-in group with no override must run automation")
	}

	paused := &Conversation{IsGroup: true, AutomationEnabled: &off}
	if paused.RunsAutomation(true) {
		t.Error("the per-conversation override must still win")
	}
	if !paused.InScope(true) {
		t.Error("pausing the AI must not take the conversation out of scope")
	}
}

func TestJIDClassification(t *testing.T) {
	if !IsGroupJID("120363012345678901@g.us") {
		t.Error("group JID not recognised")
	}
	if IsGroupJID("5511999999999@s.whatsapp.net") {
		t.Error("a private chat must not be classified as a group")
	}
	if !IsNewsletterJID("120363012345678901@newsletter") {
		t.Error("newsletter JID not recognised")
	}
	if got := UserJID("+55 11 99999-9999"); got != "5511999999999@s.whatsapp.net" {
		t.Errorf("UserJID = %q", got)
	}
	if got := UserJID("abc"); got != "" {
		t.Errorf("UserJID of a non-number must be empty, got %q", got)
	}
}

func TestSendDelayRangeIsClamped(t *testing.T) {
	instance := Instance{SendDelayMinMS: 0, SendDelayMaxMS: 0}
	minMS, maxMS := instance.SendDelayRange()
	if minMS < MinSendDelayMS {
		t.Errorf("min delay %d is under the floor %d", minMS, MinSendDelayMS)
	}
	if maxMS < minMS {
		t.Errorf("max delay %d is below min %d", maxMS, minMS)
	}

	instance = Instance{SendDelayMinMS: 9000, SendDelayMaxMS: 1000}
	minMS, maxMS = instance.SendDelayRange()
	if maxMS < minMS {
		t.Errorf("inverted range not repaired: min=%d max=%d", minMS, maxMS)
	}
}

func TestNormalizeAppliesPacingDefaults(t *testing.T) {
	instance := Instance{WorkspaceID: "ws", ServerID: "srv"}
	instance.Normalize()

	if instance.Provider != ProviderUazapi {
		t.Errorf("provider = %q, want the default", instance.Provider)
	}
	if instance.Status != StatusProvisioning {
		t.Errorf("status = %q, want %q", instance.Status, StatusProvisioning)
	}
	if instance.SendDelayMinMS != DefaultSendDelayMinMS || instance.SendDelayMaxMS != DefaultSendDelayMaxMS {
		t.Errorf("pacing defaults not applied: %d..%d", instance.SendDelayMinMS, instance.SendDelayMaxMS)
	}
	if err := instance.Validate(); err != nil {
		t.Errorf("a normalized instance must validate: %v", err)
	}
}

func TestServerCapacityFailsClosed(t *testing.T) {
	cases := []struct {
		name   string
		server Server
		want   bool
	}{
		{"room available", Server{Enabled: true, Capacity: 10, InUse: 3}, true},
		{"unknown capacity is full", Server{Enabled: true, Capacity: 0, InUse: 0}, false},
		{"at the ceiling", Server{Enabled: true, Capacity: 10, InUse: 10}, false},
		{"draining takes no new work", Server{Enabled: true, Draining: true, Capacity: 10}, false},
		{"disabled", Server{Enabled: false, Capacity: 10}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.server.HasCapacity(); got != tc.want {
				t.Errorf("HasCapacity() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGroupsDoNotRunAutomationByDefault(t *testing.T) {
	group := Conversation{IsGroup: true}
	if group.RunsAutomation(false) {
		t.Error("a group must not run automation by default")
	}
	if !group.RunsAutomation(true) {
		t.Error("an instance that opted in must run automation on groups")
	}

	private := Conversation{}
	if !private.RunsAutomation(false) {
		t.Error("a private conversation with no override must run automation")
	}

	off := false
	paused := Conversation{AutomationEnabled: &off}
	if paused.RunsAutomation(true) {
		t.Error("an operator's takeover must override the instance switch")
	}
}

func TestContactDisplayNamePrefersHumanNames(t *testing.T) {
	cases := []struct {
		name    string
		contact Contact
		want    string
	}{
		{"saved contact name wins", Contact{ContactName: "Maria", VerifiedName: "Loja", Name: "m"}, "Maria"},
		{"then the verified business name", Contact{VerifiedName: "Loja ABC", Name: "m"}, "Loja ABC"},
		{"then the profile name", Contact{Name: "Maria S."}, "Maria S."},
		{"falls back to the number", Contact{PhoneNumber: "5511999999999"}, "+5511999999999"},
		{"and only then to the jid", Contact{JID: "189923456789012@lid"}, "189923456789012@lid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.contact.DisplayName(); got != tc.want {
				t.Errorf("DisplayName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestInstanceLabelNeverShowsAProviderID(t *testing.T) {
	instance := Instance{PhoneNumber: "5511999999999", ProviderInstanceID: "r183e2ef9597845"}
	if got := instance.Label(); got != "+5511999999999" {
		t.Errorf("Label() = %q, want the phone number", got)
	}
}

func TestMapStateRefusesToGuess(t *testing.T) {
	cases := []struct {
		state     string
		connected bool
		want      Status
		ok        bool
	}{
		{"connected", true, StatusConnected, true},
		{"connecting", false, StatusAwaitingScan, true},
		{"hibernated", false, StatusHibernated, true},
		{"disconnected", false, StatusDisconnected, true},
		{"CONNECTED", true, StatusConnected, true},
		{"", true, StatusConnected, true},
		{"", false, "", false},
		{"quantum_superposition", false, "", false},
	}
	for _, tc := range cases {
		got, ok := MapState(tc.state, tc.connected)
		if ok != tc.ok || got != tc.want {
			t.Errorf("MapState(%q, %v) = (%q, %v), want (%q, %v)",
				tc.state, tc.connected, got, ok, tc.want, tc.ok)
		}
	}
}
