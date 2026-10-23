package emailsending

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	es "vozko/domain/emailsending"
)

const (
	sendQuotaKey       = "email:ses:send_quota"
	sendQuotaTTL       = 24 * time.Hour
	sentBucketPrefix   = "email:ses:sent:"
	sentBucketTTL      = es.SendQuotaStaleAfter + 5*time.Minute
	sentBucketInterval = time.Minute
)

type SendState interface {
	GetString(key string) (string, error)
	SetString(key, value string, ttl time.Duration) error
	SetNX(key, value string, ttl time.Duration) (bool, error)
	Del(keys ...string) error
	IncrWithTTL(key string, ttl time.Duration) (int64, error)
	TryIncrBy(key string, delta, max int64) (bool, error)
	DecrBy(key string, amount int64) (int64, error)
	SAdd(key string, members ...string) error
	Expire(key string, ttl time.Duration) (bool, error)
	SMembers(key string) ([]string, error)
}

func loadSendQuota(state SendState, now time.Time) (es.SendQuota, error) {
	raw, err := state.GetString(sendQuotaKey)
	if err != nil {
		return es.SendQuota{}, fmt.Errorf("emailsending: read the send quota: %w", err)
	}
	if strings.TrimSpace(raw) == "" {
		return es.SandboxSendQuota(now), nil
	}
	var quota es.SendQuota
	if err := json.Unmarshal([]byte(raw), &quota); err != nil {
		return es.SendQuota{}, fmt.Errorf("emailsending: decode the send quota: %w", err)
	}
	return quota, nil
}

func saveSendQuota(state SendState, quota es.SendQuota) error {
	raw, err := json.Marshal(quota)
	if err != nil {
		return err
	}
	if err := state.SetString(sendQuotaKey, string(raw), sendQuotaTTL); err != nil {
		return fmt.Errorf("emailsending: store the send quota: %w", err)
	}
	return nil
}

func sentBucketKey(at time.Time) string {
	return sentBucketPrefix + strconv.FormatInt(at.Truncate(sentBucketInterval).Unix(), 10)
}

func countSent(state SendState, at time.Time) error {
	_, err := state.IncrWithTTL(sentBucketKey(at), sentBucketTTL)
	return err
}

func sentSince(state SendState, readAt, now time.Time) (int64, error) {
	from := readAt
	if horizon := now.Add(-es.SendQuotaStaleAfter); from.Before(horizon) {
		from = horizon
	}
	var total int64
	for at := from.Truncate(sentBucketInterval); !at.After(now); at = at.Add(sentBucketInterval) {
		n, err := readCount(state, sentBucketKey(at))
		if err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

func readCount(state SendState, key string) (int64, error) {
	raw, err := state.GetString(key)
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("emailsending: counter %s holds %q: %w", key, raw, err)
	}
	return n, nil
}
