package ai

import "strings"

// UnfenceJSON returns a model's JSON answer with the markdown fence stripped.
//
// Providers that support strict JSON schema still sometimes wrap the object in
// ```json ... ```, and every caller that asks for structured output has to cope
// with it. Tolerated rather than refused, because the alternative is throwing
// away an answer that has already been paid for.
//
// It lives here rather than in each use case because it was already written
// twice, identically, and a third copy was about to be: the behaviour belongs to
// the port that produces the response, not to whoever happens to be decoding it.
//
// Only the OUTERMOST fence is removed. Backticks inside the body are the
// model's content and survive.
func UnfenceJSON(content string) string {
	body := strings.TrimSpace(content)
	if !strings.HasPrefix(body, "```") {
		return body
	}
	body = strings.TrimPrefix(body, "```json")
	body = strings.TrimPrefix(body, "```")
	body = strings.TrimSuffix(strings.TrimSpace(body), "```")
	return strings.TrimSpace(body)
}
