package notification

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"vozko/domain/workspace"
)

func validDraft() Draft {
	return Draft{
		WorkspaceID: "ws-1",
		Category:    CategoryCampaigns,
		Severity:    SeverityWarning,
		Title:       "Campanha pausada",
		Body:        "O modelo foi pausado pela Meta.",
		Audience:    Everyone(),
	}
}

func TestEveryCatalogCategoryIsValidAndUnknownOnesAreNot(t *testing.T) {
	if len(Categories()) != 11 {
		t.Fatalf("the catalog must list eleven categories, got %d", len(Categories()))
	}
	if !CategoryAutomations.Valid() || CategoryAutomations != "automations" {
		t.Fatal("workflow runtime alerts need the automations category")
	}
	for _, c := range Categories() {
		if !c.Valid() {
			t.Fatalf("%q is in the catalog but not valid", c)
		}
	}
	for _, c := range []Category{"", "marketing", "Campaigns"} {
		if c.Valid() {
			t.Fatalf("%q must not be a valid category", c)
		}
	}
}

func TestSeverityValidity(t *testing.T) {
	for _, s := range []Severity{SeverityInfo, SeveritySuccess, SeverityWarning, SeverityCritical} {
		if !s.Valid() {
			t.Fatalf("%q must be valid", s)
		}
	}
	if Severity("urgent").Valid() || Severity("").Valid() {
		t.Fatal("unknown severities must be refused")
	}
}

func TestDraftValidationRefusesEachBrokenField(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Draft)
		want   error
	}{
		{"workspace", func(d *Draft) { d.WorkspaceID = " " }, ErrWorkspaceRequired},
		{"category", func(d *Draft) { d.Category = "misc" }, ErrCategoryUnknown},
		{"severity", func(d *Draft) { d.Severity = "loud" }, ErrSeverityUnknown},
		{"title blank", func(d *Draft) { d.Title = "   " }, ErrTitleRequired},
		{"title long", func(d *Draft) { d.Title = strings.Repeat("a", TitleMaxRunes+1) }, ErrTitleTooLong},
		{"body long", func(d *Draft) { d.Body = strings.Repeat("a", BodyMaxRunes+1) }, ErrBodyTooLong},
		{"protocol relative link", func(d *Draft) { d.ActionURL = "//evil.example/x" }, ErrActionURLInvalid},
		{"plain http link", func(d *Draft) { d.ActionURL = "http://vozko.com/x" }, ErrActionURLInvalid},
		{"script link", func(d *Draft) { d.ActionURL = "javascript:alert(1)" }, ErrActionURLInvalid},
		{"label long", func(d *Draft) { d.ActionURL = "/dashboard"; d.ActionLabel = strings.Repeat("a", ActionLabelMaxRunes+1) }, ErrActionLabelTooLong},
		{"label without link", func(d *Draft) { d.ActionLabel = "Abrir" }, ErrActionURLInvalid},
		{"image over http", func(d *Draft) { d.ImageURL = "http://cdn.example/x.png" }, ErrImageForeign},
		{"audience", func(d *Draft) { d.Audience = Users() }, ErrAudienceInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := validDraft()
			tc.mutate(&d)
			if err := d.Normalize().Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestDraftAcceptsRelativeAndHTTPSLinksAndCountsRunesNotBytes(t *testing.T) {
	for _, link := range []string{"/dashboard/whatsapp-campaigns/c-1", "https://docs.vozko.com.br/guia"} {
		d := validDraft()
		d.ActionURL = link
		d.Title = strings.Repeat("ç", TitleMaxRunes)
		if err := d.Normalize().Validate(); err != nil {
			t.Fatalf("link %q with a full-length accented title must pass: %v", link, err)
		}
	}
}

func TestNormalizeTrimsTextAndDefaultsSeverity(t *testing.T) {
	d := validDraft()
	d.Title, d.Body, d.Severity = "  Olá  ", "\n corpo \n", ""
	got := d.Normalize()
	if got.Title != "Olá" || got.Body != "corpo" || got.Severity != SeverityInfo {
		t.Fatalf("normalize = %+v", got)
	}
}

func TestClipTitleKeepsShortTitlesAndEllipsizesLongOnesWithinTheLimit(t *testing.T) {
	if got := ClipTitle("  Campanha pausada  "); got != "Campanha pausada" {
		t.Fatalf("short = %q", got)
	}
	long := ClipTitle(strings.Repeat("ação ", 60))
	if utf8.RuneCountInString(long) > TitleMaxRunes || !strings.HasSuffix(long, "…") || strings.HasSuffix(long, " …") {
		t.Fatalf("long = %q (%d runes)", long, utf8.RuneCountInString(long))
	}
}

func TestAbsoluteLinkPrefixesAppPathsOnly(t *testing.T) {
	cases := map[string]string{
		"/dashboard/balance":            "https://app.vozko.test/dashboard/balance",
		"https://business.facebook.com": "https://business.facebook.com",
		"":                              "",
	}
	for link, want := range cases {
		if got := AbsoluteLink("https://app.vozko.test/", link); got != want {
			t.Fatalf("AbsoluteLink(%q) = %q, want %q", link, got, want)
		}
	}
}

func TestAudienceValidation(t *testing.T) {
	valid := []Audience{Everyone(), Managers(), Users("u-1", "u-2"), Holding(workspace.ResourceWhatsAppCampaigns, workspace.ActionUpdate)}
	for _, a := range valid {
		if err := a.Validate(); err != nil {
			t.Fatalf("%+v must be valid: %v", a, err)
		}
	}
	invalid := []Audience{{}, {Kind: "robots"}, Users(), Users(" "), {Kind: AudiencePermission}}
	for _, a := range invalid {
		if err := a.Validate(); !errors.Is(err, ErrAudienceInvalid) {
			t.Fatalf("%+v must be refused, got %v", a, err)
		}
	}
	tooMany := make([]string, MaxAudienceUsers+1)
	for i := range tooMany {
		tooMany[i] = "u"
	}
	if err := Users(tooMany...).Validate(); !errors.Is(err, ErrAudienceInvalid) {
		t.Fatalf("more than %d listed users must be refused", MaxAudienceUsers)
	}
}

func TestAudienceSelectsMembers(t *testing.T) {
	members := []Member{
		{UserID: "owner", Email: "o@x.com", Manager: true},
		{UserID: "agent", Email: "a@x.com"},
		{UserID: "seller", Email: "s@x.com"},
	}
	holds := func(userID string) bool { return userID == "owner" || userID == "seller" }

	cases := []struct {
		name     string
		audience Audience
		want     []string
	}{
		{"everyone", Everyone(), []string{"owner", "agent", "seller"}},
		{"managers", Managers(), []string{"owner"}},
		{"listed users drop strangers", Users("agent", "stranger", "agent"), []string{"agent"}},
		{"permission holders", Holding(workspace.ResourceWhatsAppCampaigns, workspace.ActionUpdate), []string{"owner", "seller"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.audience.Select(members, holds)
			if len(got) != len(tc.want) {
				t.Fatalf("selected %v, want %v", got, tc.want)
			}
			for i, m := range got {
				if m.UserID != tc.want[i] {
					t.Fatalf("selected %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestAudienceEqualityComparesKindUsersAndPermission(t *testing.T) {
	campaigns := Holding(workspace.ResourceWhatsAppCampaigns, workspace.ActionUpdate)
	if !campaigns.Equal(Holding(workspace.ResourceWhatsAppCampaigns, workspace.ActionUpdate)) {
		t.Fatal("the same permission audience must be equal")
	}
	if campaigns.Equal(Holding(workspace.ResourceBalance, workspace.ActionRead)) || campaigns.Equal(Everyone()) {
		t.Fatal("different audiences must differ")
	}
	if !Users("a", "b").Equal(Users("b", "a")) || Users("a").Equal(Users("a", "b")) {
		t.Fatal("listed users compare as sets")
	}
}

func TestPermissionAudienceSelectsNobodyWithoutAChecker(t *testing.T) {
	got := Holding(workspace.ResourceWhatsAppCampaigns, workspace.ActionUpdate).Select([]Member{{UserID: "u"}}, nil)
	if len(got) != 0 {
		t.Fatalf("an unknown permission answer must select nobody, got %v", got)
	}
}
