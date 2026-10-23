package mercadolivre

import (
	"strings"

	"vozko/domain/shared"
)

const MaxDisplayNameRunes = 255

func SellerDisplayName(u User) string {
	for _, candidate := range []string{u.BrandName, u.CorporateName, u.FirstName + " " + u.LastName, u.Nickname} {
		if name := collapseSpaces(candidate); name != "" {
			capped, _ := shared.TruncateRunes(name, MaxDisplayNameRunes)
			return strings.TrimSpace(capped)
		}
	}
	return ""
}

func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func (a *Account) Label() string {
	if n := strings.TrimSpace(a.DisplayName); n != "" {
		return n
	}
	if n := strings.TrimSpace(a.Nickname); n != "" {
		return n
	}
	return a.MLUserID
}

func (a *Account) RefreshNames(u User) bool {
	changed := false
	if nickname := strings.TrimSpace(u.Nickname); nickname != "" && nickname != a.Nickname {
		a.Nickname, changed = nickname, true
	}
	if display := SellerDisplayName(u); display != "" && display != a.DisplayName {
		a.DisplayName, changed = display, true
	}
	return changed
}
