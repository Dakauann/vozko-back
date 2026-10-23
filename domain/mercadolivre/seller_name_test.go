package mercadolivre

import (
	"strings"
	"testing"
)

func TestSellerDisplayName(t *testing.T) {
	cases := []struct {
		name string
		user User
		want string
	}{
		{"brand name wins", User{Nickname: "DGCAFEDHB50478", FirstName: "Ana", LastName: "Souza", BrandName: " Café do Vale ", CorporateName: "Vale Cafés Ltda"}, "Café do Vale"},
		{"corporate name without brand", User{Nickname: "DGCAFEDHB50478", FirstName: "Ana", LastName: "Souza", BrandName: "   ", CorporateName: " Vale Cafés Ltda "}, "Vale Cafés Ltda"},
		{"person name without company", User{Nickname: "DGCAFEDHB50478", FirstName: " Ana ", LastName: " Souza  Lima "}, "Ana Souza Lima"},
		{"first name only", User{Nickname: "DGCAFEDHB50478", FirstName: "Ana", LastName: "  "}, "Ana"},
		{"last name only", User{Nickname: "DGCAFEDHB50478", LastName: "Souza"}, "Souza"},
		{"nickname when nothing else", User{Nickname: " DGCAFEDHB50478 ", FirstName: " ", LastName: "\t", BrandName: "", CorporateName: " "}, "DGCAFEDHB50478"},
		{"empty when everything is blank", User{Nickname: "  "}, ""},
	}
	long := strings.Repeat("é", MaxDisplayNameRunes+10)
	cases = append(cases, struct {
		name string
		user User
		want string
	}{"capped to the stored length", User{BrandName: long}, strings.Repeat("é", MaxDisplayNameRunes)})
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SellerDisplayName(c.user); got != c.want {
				t.Fatalf("SellerDisplayName = %q, want %q", got, c.want)
			}
		})
	}
}

func TestAccountLabel(t *testing.T) {
	cases := []struct {
		name    string
		account Account
		want    string
	}{
		{"stored display name", Account{MLUserID: "415458330", Nickname: "DGCAFEDHB50478", DisplayName: " Café do Vale "}, "Café do Vale"},
		{"nickname when display name is blank", Account{MLUserID: "415458330", Nickname: " DGCAFEDHB50478 ", DisplayName: "  "}, "DGCAFEDHB50478"},
		{"seller id when both are blank", Account{MLUserID: "415458330", Nickname: " "}, "415458330"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.account.Label(); got != c.want {
				t.Fatalf("Label = %q, want %q", got, c.want)
			}
		})
	}
}

func TestAccountRefreshNames(t *testing.T) {
	cases := []struct {
		name         string
		account      Account
		user         User
		wantChanged  bool
		wantNickname string
		wantDisplay  string
	}{
		{"fills both on a fresh account", Account{}, User{Nickname: " DGCAFEDHB50478 ", BrandName: "Café do Vale"}, true, "DGCAFEDHB50478", "Café do Vale"},
		{"unchanged names report no change", Account{Nickname: "DGCAFEDHB50478", DisplayName: "Café do Vale"}, User{Nickname: "DGCAFEDHB50478", BrandName: " Café do Vale "}, false, "DGCAFEDHB50478", "Café do Vale"},
		{"a new display name is a change", Account{Nickname: "DGCAFEDHB50478", DisplayName: "DGCAFEDHB50478"}, User{Nickname: "DGCAFEDHB50478", FirstName: "Ana", LastName: "Souza"}, true, "DGCAFEDHB50478", "Ana Souza"},
		{"a new nickname is a change", Account{Nickname: "LOJA", DisplayName: "Café do Vale"}, User{Nickname: "LOJA NOVA", BrandName: "Café do Vale"}, true, "LOJA NOVA", "Café do Vale"},
		{"no name at all keeps what is stored", Account{Nickname: "LOJA", DisplayName: "Café do Vale"}, User{Nickname: "  "}, false, "LOJA", "Café do Vale"},
		{"a blank nickname keeps the stored nickname", Account{Nickname: "LOJA", DisplayName: "LOJA"}, User{Nickname: " ", CorporateName: "Vale Cafés Ltda"}, true, "LOJA", "Vale Cafés Ltda"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := c.account
			if got := a.RefreshNames(c.user); got != c.wantChanged {
				t.Fatalf("RefreshNames changed = %v, want %v", got, c.wantChanged)
			}
			if a.Nickname != c.wantNickname || a.DisplayName != c.wantDisplay {
				t.Fatalf("names = %q / %q, want %q / %q", a.Nickname, a.DisplayName, c.wantNickname, c.wantDisplay)
			}
		})
	}
}
