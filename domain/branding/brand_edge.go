package branding

import "strings"

const publicProviderPrefix = "ai_"

func ExternalModelID(id, brandPrefix string) string {
	if brandPrefix == "" || brandPrefix == publicProviderPrefix {
		return id
	}
	if rest, ok := strings.CutPrefix(id, publicProviderPrefix); ok {
		return brandPrefix + rest
	}
	return id
}

func InternalModelID(id, brandPrefix string) string {
	if brandPrefix == "" || brandPrefix == publicProviderPrefix {
		return id
	}
	if rest, ok := strings.CutPrefix(id, brandPrefix); ok {
		return publicProviderPrefix + rest
	}
	return id
}
