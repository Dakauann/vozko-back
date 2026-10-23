package calendar

import (
	"fmt"
	"net/mail"
	"strings"
)

func ParseAttendees(raw interface{}) (accepted []Attendee, rejected []string) {
	tokens := attendeeTokens(raw)
	accepted = make([]Attendee, 0, len(tokens))
	seen := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		candidate := strings.TrimSpace(token)
		if candidate == "" {
			continue
		}
		parsed, err := mail.ParseAddress(candidate)
		if err != nil {
			parsed, err = mail.ParseAddress("<" + candidate + ">")
			if err != nil {
				rejected = append(rejected, candidate)
				continue
			}
		}
		email := strings.ToLower(strings.TrimSpace(parsed.Address))
		if email == "" {
			continue
		}
		if _, exists := seen[email]; exists {
			continue
		}
		seen[email] = struct{}{}
		accepted = append(accepted, Attendee{Email: email, Name: strings.TrimSpace(parsed.Name)})
	}
	return accepted, rejected
}

func attendeeTokens(raw interface{}) []string {
	switch v := raw.(type) {
	case nil:
		return nil
	case string:
		return strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ';' || r == '\n' })
	case []string:
		return v
	case []interface{}:
		tokens := make([]string, 0, len(v))
		for _, item := range v {
			if item != nil {
				tokens = append(tokens, fmt.Sprintf("%v", item))
			}
		}
		return tokens
	default:
		return []string{fmt.Sprintf("%v", v)}
	}
}
