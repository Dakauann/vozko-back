package webchat

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

func solve(t *testing.T, token string, bits int) string {
	t.Helper()
	for n := 0; n < 1<<24; n++ {
		nonce := strconv.Itoa(n)
		if leadingZeroBits(challengeDigest(token, nonce)) >= bits {
			return nonce
		}
	}
	t.Fatal("no solution found")
	return ""
}

func TestChallengeSolutionIsAccepted(t *testing.T) {
	keys := testKeys(t)
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	c, err := NewChallenge(keys, "w1", 8, now)
	if err != nil {
		t.Fatal(err)
	}
	id, err := VerifyChallenge(keys, c.Token, solve(t, c.Token, 8), "w1", 8, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if id == "" || id != c.ID {
		t.Fatalf("challenge id = %q, want %q", id, c.ID)
	}
}

func TestChallengeRefusesShortcuts(t *testing.T) {
	keys := testKeys(t)
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	c, _ := NewChallenge(keys, "w1", 8, now)
	easy, _ := NewChallenge(keys, "w1", 1, now)
	nonce := solve(t, c.Token, 8)
	weakened := strings.Replace(c.Token, ".8.", ".1.", 1)

	cases := map[string]struct {
		token, nonce, widget string
		at                   time.Time
		want                 error
	}{
		"expired":           {c.Token, nonce, "w1", now.Add(ChallengeTTL + time.Second), ErrChallengeExpired},
		"other widget":      {c.Token, nonce, "w2", now, ErrChallengeInvalid},
		"weakened bits":     {weakened, nonce, "w1", now, ErrChallengeInvalid},
		"below the minimum": {easy.Token, solve(t, easy.Token, 1), "w1", now, ErrChallengeInvalid},
		"wrong nonce":       {c.Token, wrongNonce(c.Token, 8), "w1", now, ErrChallengeUnsolved},
		"long nonce":        {c.Token, strings.Repeat("1", 40), "w1", now, ErrChallengeUnsolved},
		"garbage":           {"x.y", nonce, "w1", now, ErrChallengeInvalid},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := VerifyChallenge(keys, tc.token, tc.nonce, tc.widget, 8, tc.at)
			if !errors.Is(err, tc.want) {
				t.Fatalf("VerifyChallenge = %v, want %v", err, tc.want)
			}
		})
	}
}

func wrongNonce(token string, bits int) string {
	for n := 0; ; n++ {
		nonce := strconv.Itoa(n)
		if leadingZeroBits(challengeDigest(token, nonce)) < bits {
			return nonce
		}
	}
}

func TestLeadingZeroBits(t *testing.T) {
	cases := map[string]struct {
		in   []byte
		want int
	}{
		"none":        {[]byte{0x80}, 0},
		"one byte":    {[]byte{0x00, 0xFF}, 8},
		"partial":     {[]byte{0x00, 0x0F}, 12},
		"all zero":    {[]byte{0x00, 0x00}, 16},
		"single high": {[]byte{0x01}, 7},
	}
	for name, tc := range cases {
		if got := leadingZeroBits(tc.in); got != tc.want {
			t.Errorf("%s: leadingZeroBits = %d, want %d", name, got, tc.want)
		}
	}
}
