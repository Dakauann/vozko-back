package branding

import "strings"

// publicProviderPrefix is the fixed INTERNAL prefix every platform-managed AI
// model id carries (ai_*). Brand-facing ids swap it for the deployment's own
// prefix at the API edge; storage always keeps this form.
const publicProviderPrefix = "ai_"

// The functions here are the white-label "door": they translate AI model ids
// between the fixed INTERNAL public form (the ai_* prefix used everywhere for
// storage, validation and comparison) and the BRAND-facing form returned to API
// clients. Only the brand prefix crosses the API; the internal form never changes,
// so a mistyped or changed brand prefix can never corrupt or orphan stored data.
//
// Both functions are the identity when brandPrefix is empty (brand not loaded, as
// in unit tests) or equal to the internal prefix (the primary brand). That makes the
// door a strict no-op for the primary brand, so production behavior is unchanged.

// ExternalModelID rewrites an internal model id (ai_*) into the brand-facing
// form for API responses. Ids that do not carry the internal prefix are returned
// unchanged rather than corrupted.
func ExternalModelID(id, brandPrefix string) string {
	if brandPrefix == "" || brandPrefix == publicProviderPrefix {
		return id
	}
	if rest, ok := strings.CutPrefix(id, publicProviderPrefix); ok {
		return brandPrefix + rest
	}
	return id
}

// InternalModelID is the inverse of ExternalModelID: it rewrites a brand-facing
// model id received from an API client back into the internal form (ai_*)
// before validation and storage. Ids without the brand prefix are returned
// unchanged, so a client may also send the internal form directly.
func InternalModelID(id, brandPrefix string) string {
	if brandPrefix == "" || brandPrefix == publicProviderPrefix {
		return id
	}
	if rest, ok := strings.CutPrefix(id, brandPrefix); ok {
		return publicProviderPrefix + rest
	}
	return id
}
