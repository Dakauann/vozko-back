package unofficial_whatsapp_group

import (
	"errors"
	"testing"
)

func TestNormalizeInviteLinkAcceptsEveryCommonShape(t *testing.T) {
	const want = "https://chat.whatsapp.com/Ky3pQm8Lx2AbCdEfGh1234"
	cases := map[string]string{
		"canonical":            "https://chat.whatsapp.com/Ky3pQm8Lx2AbCdEfGh1234",
		"plain http":           "http://chat.whatsapp.com/Ky3pQm8Lx2AbCdEfGh1234",
		"no scheme":            "chat.whatsapp.com/Ky3pQm8Lx2AbCdEfGh1234",
		"query and fragment":   "https://chat.whatsapp.com/Ky3pQm8Lx2AbCdEfGh1234?mode=r_c#x",
		"trailing slash":       "https://chat.whatsapp.com/Ky3pQm8Lx2AbCdEfGh1234/",
		"www chat path":        "https://www.whatsapp.com/chat/Ky3pQm8Lx2AbCdEfGh1234",
		"wa.me":                "https://wa.me/Ky3pQm8Lx2AbCdEfGh1234",
		"bare code":            "Ky3pQm8Lx2AbCdEfGh1234",
		"surrounding spaces":   "   https://chat.whatsapp.com/Ky3pQm8Lx2AbCdEfGh1234  ",
		"pasted with at":       "@chat.whatsapp.com/Ky3pQm8Lx2AbCdEfGh1234",
		"uppercase host":       "HTTPS://CHAT.WHATSAPP.COM/Ky3pQm8Lx2AbCdEfGh1234",
		"bare code with query": "Ky3pQm8Lx2AbCdEfGh1234?mode=r_c",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := NormalizeInviteLink(input)
			if err != nil {
				t.Fatalf("NormalizeInviteLink(%q) returned %v", input, err)
			}
			if got != want {
				t.Fatalf("NormalizeInviteLink(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func TestNormalizeInviteLinkRefusesWhatIsNotAnInvite(t *testing.T) {
	for _, input := range []string{
		"",
		"   ",
		"https://chat.whatsapp.com/",
		"https://example.com/Ky3pQm8Lx2AbCdEfGh1234",
		"short",
		"https://chat.whatsapp.com/has spaces in it",
		"https://www.whatsapp.com/download",
	} {
		if _, err := NormalizeInviteLink(input); !errors.Is(err, ErrInviteInvalid) {
			t.Fatalf("NormalizeInviteLink(%q) error = %v, want ErrInviteInvalid", input, err)
		}
	}
}
