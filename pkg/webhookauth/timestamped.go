package webhookauth

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

var (
	ErrSignatureMalformed = errors.New("webhook signature header is malformed")
	ErrSignatureMismatch  = errors.New("webhook signature does not match")
	ErrSignatureStale     = errors.New("webhook signature timestamp is outside the tolerance")
)

func VerifyTimestampedHMAC(secret, header string, body []byte, now time.Time, tolerance time.Duration) error {
	timestamp, signature, err := parseTimestampedHeader(header)
	if err != nil {
		return err
	}
	signed := make([]byte, 0, len(timestamp)+1+len(body))
	signed = append(signed, timestamp...)
	signed = append(signed, '.')
	signed = append(signed, body...)
	if !VerifyHexHMAC(secret, signed, signature) {
		return ErrSignatureMismatch
	}
	seconds, _ := strconv.ParseInt(timestamp, 10, 64)
	drift := now.Sub(time.Unix(seconds, 0))
	if drift < 0 {
		drift = -drift
	}
	if drift > tolerance {
		return ErrSignatureStale
	}
	return nil
}

func parseTimestampedHeader(header string) (timestamp, signature string, err error) {
	var seenT, seenS bool
	for _, element := range strings.Split(header, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(element), "=")
		if !ok || value == "" {
			return "", "", ErrSignatureMalformed
		}
		switch key {
		case "t":
			if seenT {
				return "", "", ErrSignatureMalformed
			}
			seenT = true
			timestamp = value
		case "s":
			if seenS {
				return "", "", ErrSignatureMalformed
			}
			seenS = true
			signature = value
		}
	}
	if !seenT || !seenS {
		return "", "", ErrSignatureMalformed
	}
	if seconds, err := strconv.ParseInt(timestamp, 10, 64); err != nil || seconds < 0 {
		return "", "", ErrSignatureMalformed
	}
	return timestamp, signature, nil
}
