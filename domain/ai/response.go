package ai

import "strings"

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
