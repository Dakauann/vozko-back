package unofficial_whatsapp_group

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

var ErrInviteInvalid = errors.New("that is not a WhatsApp group invite link")

const inviteBase = "https://chat.whatsapp.com/"

var inviteCodePattern = regexp.MustCompile(`^[A-Za-z0-9\-_]{8,32}$`)

func NormalizeInviteLink(input string) (string, error) {
	raw := strings.TrimPrefix(strings.TrimSpace(input), "@")
	if raw == "" {
		return "", ErrInviteInvalid
	}

	probe := raw
	if !strings.Contains(probe, "://") && (strings.Contains(probe, ".") || strings.Contains(probe, "/")) {
		probe = "https://" + probe
	}
	if u, err := url.Parse(probe); err == nil && u.Host != "" {
		if code, ok := codeFromURL(u); ok {
			return inviteBase + code, nil
		}
		return "", ErrInviteInvalid
	}

	bare := raw
	if i := strings.IndexAny(bare, "?#"); i >= 0 {
		bare = bare[:i]
	}
	bare = strings.Trim(bare, "/ \t")
	if inviteCodePattern.MatchString(bare) {
		return inviteBase + bare, nil
	}
	return "", ErrInviteInvalid
}

func codeFromURL(u *url.URL) (string, bool) {
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	last := parts[len(parts)-1]

	switch host {
	case "chat.whatsapp.com", "wa.me":
		return last, inviteCodePattern.MatchString(last)
	case "whatsapp.com":
		if len(parts) >= 2 && strings.EqualFold(parts[0], "chat") {
			return last, inviteCodePattern.MatchString(last)
		}
	}
	return "", false
}
