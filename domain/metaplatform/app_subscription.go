package metaplatform

import (
	"errors"
	"slices"
)

var (
	ErrAppSubscriptionMissing = errors.New("meta platform: the app has no active subscription for this object")
	ErrAppSubscriptionNotKept = errors.New("meta platform: meta did not keep the requested subscription fields")
)

type AppSubscription struct {
	Object      string
	CallbackURL string
	Fields      []string
	Active      bool
}

func (s AppSubscription) CoveredBy(current []AppSubscription) bool {
	for _, c := range current {
		if c.Object != s.Object || !c.Active || c.CallbackURL != s.CallbackURL {
			continue
		}
		for _, field := range s.Fields {
			if !slices.Contains(c.Fields, field) {
				return false
			}
		}
		return true
	}
	return false
}

func (s AppSubscription) WithFields(fields ...string) AppSubscription {
	merged := slices.Clone(s.Fields)
	for _, field := range fields {
		if !slices.Contains(merged, field) {
			merged = append(merged, field)
		}
	}
	s.Fields = merged
	return s
}

func ActiveSubscription(current []AppSubscription, object string) (AppSubscription, bool) {
	for _, c := range current {
		if c.Object == object && c.Active {
			return c, true
		}
	}
	return AppSubscription{}, false
}
