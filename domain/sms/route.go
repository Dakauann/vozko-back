package sms

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/conversation"
)

const (
	MinBrandPrefixRunes = 2
	MaxBrandPrefixRunes = 20
	minNumberDigits     = 4
	maxNumberDigits     = 15
)

var (
	ErrRouteInvalid      = errors.New("sms: invalid sms route")
	ErrRouteNotFound     = errors.New("sms: sms route not found")
	ErrRouteCannotSend   = errors.New("sms: the sms route is paused")
	ErrNoSharedNumber    = errors.New("sms: the platform has no shared sms number")
	ErrInboundUnroutable = errors.New("sms: the reply cannot be matched to a workspace")
)

type RouteKind string

const (
	RouteShared    RouteKind = "shared"
	RouteDedicated RouteKind = "dedicated"
)

type RouteStatus string

const (
	RouteActive RouteStatus = "active"
	RoutePaused RouteStatus = "paused"
)

type Route struct {
	ID           string
	WorkspaceID  string
	BrandPrefix  string
	Kind         RouteKind
	Number       string
	Status       RouteStatus
	PauseReason  string
	Automation   conversation.ChannelAutomation
	DepartmentID *string
	PipelineID   *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (r Route) Validate() error {
	invalid := func(why string) error { return fmt.Errorf("%w: %s", ErrRouteInvalid, why) }
	prefix := strings.TrimSpace(r.BrandPrefix)
	if n := utf8.RuneCountInString(prefix); n < MinBrandPrefixRunes || n > MaxBrandPrefixRunes {
		return invalid("the brand prefix needs 2 to 20 characters")
	}
	for _, c := range prefix {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == ' ' || c == '.' || c == '-' || c == '&') {
			return invalid("the brand prefix takes letters without accents, digits, spaces, dots, hyphens and &")
		}
	}
	if !r.Kind.Valid() {
		return invalid("unknown route kind")
	}
	switch r.Kind {
	case RouteShared:
		if r.Number != "" {
			return invalid("a shared route uses the platform number")
		}
	case RouteDedicated:
		if !digitsBetween(r.Number, minNumberDigits, maxNumberDigits) {
			return invalid("a dedicated route needs its number in digits")
		}
	}
	if !r.Status.Valid() {
		return invalid("unknown route status")
	}
	return nil
}

func digitsBetween(s string, min, max int) bool {
	if len(s) < min || len(s) > max {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (r Route) CanSend() bool {
	return r.Status == RouteActive
}

func (r Route) Compose(text string) string {
	return strings.TrimSpace(r.BrandPrefix) + ": " + text
}

func (r Route) From(sharedNumber string) string {
	if r.Kind == RouteDedicated {
		return r.Number
	}
	return sharedNumber
}
