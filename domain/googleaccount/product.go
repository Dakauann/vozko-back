package googleaccount

import (
	"fmt"
	"slices"
	"strings"
)

type Product string

const (
	ProductCalendar Product = "calendar"
	ProductSheets   Product = "sheets"
)

const (
	ScopeCalendar         = "https://www.googleapis.com/auth/calendar"
	ScopeCalendarEvents   = "https://www.googleapis.com/auth/calendar.events"
	ScopeCalendarReadonly = "https://www.googleapis.com/auth/calendar.readonly"
	ScopeDriveFile        = "https://www.googleapis.com/auth/drive.file"
)

var identityScopes = []string{"openid", "email", "profile"}

var productScopes = map[Product][]string{
	ProductCalendar: {ScopeCalendar, ScopeCalendarEvents, ScopeCalendarReadonly},
	ProductSheets:   {ScopeDriveFile},
}

func Products() []Product { return []Product{ProductCalendar, ProductSheets} }

func (p Product) Valid() bool {
	_, ok := productScopes[p]
	return ok
}

func ParseProduct(raw string) (Product, error) {
	p := Product(strings.ToLower(strings.TrimSpace(raw)))
	if !p.Valid() {
		return "", fmt.Errorf("%w: %q", ErrUnknownProduct, raw)
	}
	return p, nil
}

func (p Product) Scopes() []string {
	return slices.Clone(productScopes[p])
}

func (p Product) AuthorizationScopes() []string {
	return append(p.Scopes(), identityScopes...)
}

func GrantCovers(granted []string, p Product) bool {
	required := productScopes[p]
	if len(required) == 0 {
		return false
	}
	for _, scope := range required {
		if !slices.Contains(granted, scope) {
			return false
		}
	}
	return true
}

func MergeScopes(lists ...[]string) []string {
	var merged []string
	for _, list := range lists {
		for _, scope := range list {
			if scope = strings.TrimSpace(scope); scope != "" && !slices.Contains(merged, scope) {
				merged = append(merged, scope)
			}
		}
	}
	slices.Sort(merged)
	return merged
}
