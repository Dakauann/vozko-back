package instagram

import (
	"encoding/json"
	"fmt"
	"strings"
)

// permissionList decodes Instagram's granted-permission list from either shape it
// actually returns.
//
// Meta's documentation shows a comma-separated STRING:
//
//	"permissions": "instagram_business_basic,instagram_business_manage_messages"
//
// The live api.instagram.com/oauth/access_token endpoint returns a JSON ARRAY:
//
//	"permissions": ["instagram_business_basic", "instagram_business_manage_messages"]
//
// Decoding into a plain string therefore fails the whole exchange with
// "cannot unmarshal array into Go struct field ... of type string", after the
// token was already successfully issued. Accepting both shapes is the only safe
// option: the documented form may still appear on other hosts or older versions,
// and a decode error here throws away a valid credential.
type permissionList []string

func (p *permissionList) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*p = nil
		return nil
	}

	switch trimmed[0] {
	case '[':
		var list []string
		if err := json.Unmarshal(data, &list); err != nil {
			return fmt.Errorf("permissions: decode array: %w", err)
		}
		*p = normalizePermissions(list)
		return nil

	case '"':
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("permissions: decode string: %w", err)
		}
		*p = normalizePermissions(strings.Split(raw, ","))
		return nil

	default:
		// Anything else is unusable, but an unknown shape must not discard a token
		// that was already issued, treat it as "not reported" instead.
		*p = nil
		return nil
	}
}

func (p permissionList) Strings() []string { return p }

// normalizePermissions trims, drops empties and de-duplicates while preserving
// order.
func normalizePermissions(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// graphID decodes an Instagram id from either a JSON string or a JSON number,
// preserving the exact digits.
//
// This exists because ids MUST NOT round-trip through float64. Decoding into `any`
// yields a float64, and an Instagram professional account id (~1.78e16) exceeds
// float64's exact-integer range of 2^53 (~9.01e15), so 17841458366137975 silently
// becomes 17841458366137976.
//
// That single digit is catastrophic rather than cosmetic: the wrong id gets stored
// on the account, and every inbound webhook (whose entry.id carries the REAL id)
// then fails to resolve an account and is dropped as "unknown account". Messages
// would simply never arrive, with no error anywhere.
type graphID string

func (g *graphID) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*g = ""
		return nil
	}

	// A JSON string: take it verbatim.
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return fmt.Errorf("id: decode string: %w", err)
		}
		*g = graphID(strings.TrimSpace(s))
		return nil
	}

	// A JSON number: keep the literal digits rather than converting to a float.
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("id: decode number: %w", err)
	}
	*g = graphID(n.String())
	return nil
}

func (g graphID) String() string { return string(g) }
