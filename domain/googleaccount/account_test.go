package googleaccount

import (
	"errors"
	"testing"
	"time"
)

func TestParseProduct(t *testing.T) {
	cases := []struct {
		raw     string
		want    Product
		refused bool
	}{
		{raw: "calendar", want: ProductCalendar},
		{raw: " Sheets ", want: ProductSheets},
		{raw: "drive", refused: true},
		{raw: "", refused: true},
	}
	for _, c := range cases {
		got, err := ParseProduct(c.raw)
		if c.refused {
			if !errors.Is(err, ErrUnknownProduct) {
				t.Fatalf("ParseProduct(%q) err = %v, want ErrUnknownProduct", c.raw, err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Fatalf("ParseProduct(%q) = %q, %v; want %q", c.raw, got, err, c.want)
		}
	}
}

func TestEveryProductAsksForItsOwnScopesPlusIdentity(t *testing.T) {
	for _, p := range Products() {
		if len(p.Scopes()) == 0 {
			t.Fatalf("%s declares no API scope", p)
		}
		asked := p.AuthorizationScopes()
		for _, s := range append(p.Scopes(), identityScopes...) {
			if !contains(asked, s) {
				t.Fatalf("%s authorization does not ask for %q", p, s)
			}
		}
	}
}

func TestSheetsOnlyAsksForFilesTheAppCreates(t *testing.T) {
	scopes := ProductSheets.Scopes()
	if len(scopes) != 1 || scopes[0] != ScopeDriveFile {
		t.Fatalf("sheets scopes = %v, want only %s", scopes, ScopeDriveFile)
	}
}

func TestGrantCovers(t *testing.T) {
	cases := []struct {
		name    string
		granted []string
		product Product
		want    bool
	}{
		{name: "sheets granted", granted: []string{ScopeDriveFile, "openid"}, product: ProductSheets, want: true},
		{name: "sheets unchecked on the consent screen", granted: []string{"openid", "https://www.googleapis.com/auth/userinfo.email"}, product: ProductSheets},
		{name: "calendar granted with sheets alongside", granted: append(ProductCalendar.Scopes(), ScopeDriveFile), product: ProductCalendar, want: true},
		{name: "calendar missing one scope", granted: ProductCalendar.Scopes()[1:], product: ProductCalendar},
		{name: "nothing granted", product: ProductCalendar},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := GrantCovers(c.granted, c.product); got != c.want {
				t.Fatalf("GrantCovers = %v, want %v", got, c.want)
			}
		})
	}
}

func TestMergeScopesKeepsEveryGrantOnce(t *testing.T) {
	got := MergeScopes([]string{"b", "a"}, []string{"a", " c ", ""})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("MergeScopes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("MergeScopes = %v, want %v", got, want)
		}
	}
}

func TestNormalizeEmail(t *testing.T) {
	if got := NormalizeEmail("  Ana.Souza@Gmail.COM "); got != "ana.souza@gmail.com" {
		t.Fatalf("NormalizeEmail = %q", got)
	}
}

func TestAccountNeedsRefreshInsideTheLead(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		expires time.Time
		want    bool
	}{
		{name: "far from expiring", expires: now.Add(time.Hour)},
		{name: "inside the lead", expires: now.Add(TokenRefreshLead - time.Second), want: true},
		{name: "already expired", expires: now.Add(-time.Minute), want: true},
		{name: "never stored", want: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := Account{AccessExpiresAt: c.expires}
			if got := a.NeedsRefresh(now); got != c.want {
				t.Fatalf("NeedsRefresh = %v, want %v", got, c.want)
			}
		})
	}
}

func TestAccountUsable(t *testing.T) {
	future := time.Now().Add(time.Hour)
	cases := []struct {
		name    string
		account Account
		err     error
	}{
		{name: "connected", account: Account{Status: StatusConnected, AccessToken: "t", RefreshToken: "r", AccessExpiresAt: future}},
		{name: "needs reconnect", account: Account{Status: StatusNeedsReconnect, AccessToken: "t"}, err: ErrNeedsReconnect},
		{name: "disconnected", account: Account{Status: StatusDisconnected}, err: ErrNotConnected},
		{name: "connected without a refresh token", account: Account{Status: StatusConnected, AccessToken: "t"}, err: ErrNeedsReconnect},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.account.Usable()
			if c.err == nil && err != nil {
				t.Fatalf("Usable = %v, want nil", err)
			}
			if c.err != nil && !errors.Is(err, c.err) {
				t.Fatalf("Usable = %v, want %v", err, c.err)
			}
		})
	}
}

func TestNeedsReconnectIsAKindOfNotConnected(t *testing.T) {
	if !errors.Is(ErrNeedsReconnect, ErrNotConnected) {
		t.Fatal("callers that only know ErrNotConnected must also catch ErrNeedsReconnect")
	}
}

func TestStatusTransitions(t *testing.T) {
	cases := []struct {
		from, to Status
		want     bool
	}{
		{StatusConnected, StatusNeedsReconnect, true},
		{StatusConnected, StatusDisconnected, true},
		{StatusNeedsReconnect, StatusConnected, true},
		{StatusDisconnected, StatusConnected, true},
		{StatusDisconnected, StatusNeedsReconnect, false},
		{Status("odd"), StatusConnected, false},
	}
	for _, c := range cases {
		if got := c.from.CanTransitionTo(c.to); got != c.want {
			t.Fatalf("%s -> %s = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

func TestGrantValidate(t *testing.T) {
	cases := []struct {
		name  string
		grant Grant
		err   error
	}{
		{name: "complete", grant: Grant{AccessToken: "a", RefreshToken: "r", ExpiresIn: time.Hour}},
		{name: "no access token", grant: Grant{RefreshToken: "r", ExpiresIn: time.Hour}, err: ErrIncompleteGrant},
		{name: "no lifetime", grant: Grant{AccessToken: "a", RefreshToken: "r"}, err: ErrIncompleteGrant},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.grant.Validate()
			if c.err == nil && err != nil {
				t.Fatalf("Validate = %v", err)
			}
			if c.err != nil && !errors.Is(err, c.err) {
				t.Fatalf("Validate = %v, want %v", err, c.err)
			}
		})
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
