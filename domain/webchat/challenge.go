package webchat

import (
	"crypto/sha256"
	"math/bits"
	"strconv"
	"strings"
	"time"
)

const (
	ChallengeTTL         = 2 * time.Minute
	ChallengeBits        = 16
	maxChallengeNonceLen = 20
)

type Challenge struct {
	ID        string    `json:"id"`
	Token     string    `json:"token"`
	Bits      int       `json:"bits"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func NewChallenge(keys Keys, widgetID string, difficulty int, now time.Time) (Challenge, error) {
	if len(keys.Challenge) == 0 {
		return Challenge{}, ErrSigningKeyMissing
	}
	if widgetID == "" || strings.Contains(widgetID, ".") || difficulty < 1 || difficulty > 32 {
		return Challenge{}, ErrChallengeInvalid
	}
	id, err := randomToken(16)
	if err != nil {
		return Challenge{}, err
	}
	expires := now.UTC().Add(ChallengeTTL).Truncate(time.Second)
	head := strings.Join([]string{id, widgetID, strconv.Itoa(difficulty), strconv.FormatInt(expires.Unix(), 10)}, ".")
	return Challenge{
		ID:        id,
		Token:     head + "." + sign(keys.Challenge, head),
		Bits:      difficulty,
		ExpiresAt: expires,
	}, nil
}

func VerifyChallenge(keys Keys, token, nonce, widgetID string, minimumBits int, now time.Time) (string, error) {
	if len(keys.Challenge) == 0 {
		return "", ErrSigningKeyMissing
	}
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 5 {
		return "", ErrChallengeInvalid
	}
	head := strings.Join(parts[:4], ".")
	if !hmacEqualString(parts[4], sign(keys.Challenge, head)) {
		return "", ErrChallengeInvalid
	}
	if parts[1] != widgetID {
		return "", ErrChallengeInvalid
	}
	difficulty, err := strconv.Atoi(parts[2])
	if err != nil || difficulty < minimumBits {
		return "", ErrChallengeInvalid
	}
	expires, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return "", ErrChallengeInvalid
	}
	if now.After(time.Unix(expires, 0)) {
		return "", ErrChallengeExpired
	}
	if nonce == "" || len(nonce) > maxChallengeNonceLen {
		return "", ErrChallengeUnsolved
	}
	if leadingZeroBits(challengeDigest(token, nonce)) < difficulty {
		return "", ErrChallengeUnsolved
	}
	return parts[0], nil
}

func challengeDigest(token, nonce string) []byte {
	sum := sha256.Sum256([]byte(token + ":" + nonce))
	return sum[:]
}

func leadingZeroBits(digest []byte) int {
	total := 0
	for _, b := range digest {
		if b == 0 {
			total += 8
			continue
		}
		return total + bits.LeadingZeros8(b)
	}
	return total
}
