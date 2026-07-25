package auth_repository

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/auth"
	"vozko/domain/cache"
)

const (
	emailVerifPrefix = "email_verify:token:"
	emailVerifIndex  = "email_verify:email:"
)

type emailVerificationRedisRepository struct {
	shared cache.SharedState
}

func NewEmailVerificationRedisRepository(shared cache.SharedState) auth.EmailVerificationRepository {
	return &emailVerificationRedisRepository{shared: shared}
}

func (r *emailVerificationRedisRepository) Create(token *auth.EmailVerificationToken) error {
	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("marshal email verification token: %w", err)
	}

	ttl := time.Until(token.ExpiresAt)
	if ttl <= 0 {
		ttl = 1 * time.Second
	}

	key := emailVerifPrefix + token.Token
	if err := r.shared.SetString(key, string(data), ttl); err != nil {
		return fmt.Errorf("store email verification token: %w", err)
	}

	indexKey := emailVerifIndex + token.Email
	indexVal := token.Token + "|" + token.CreatedAt.Format(time.RFC3339Nano)
	if err := r.shared.SAdd(indexKey, indexVal); err != nil {
		log.Printf("[email-verify] SAdd email index: %v", err)
	}

	r.shared.Expire(indexKey, 24*time.Hour)

	return nil
}

func (r *emailVerificationRedisRepository) FindByToken(tokenStr string) (*auth.EmailVerificationToken, error) {
	key := emailVerifPrefix + tokenStr
	val, err := r.shared.GetString(key)
	if err != nil {
		return nil, nil
	}
	if val == "" {
		return nil, nil
	}

	var token auth.EmailVerificationToken
	if err := json.Unmarshal([]byte(val), &token); err != nil {
		return nil, fmt.Errorf("unmarshal email verification token: %w", err)
	}
	return &token, nil
}

func (r *emailVerificationRedisRepository) MarkAsUsed(tokenStr string) error {
	token, err := r.FindByToken(tokenStr)
	if err != nil || token == nil {
		return err
	}

	token.Used = true
	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("marshal updated email verification token: %w", err)
	}

	key := emailVerifPrefix + tokenStr
	ttl := time.Until(token.ExpiresAt)
	if ttl <= 0 {
		ttl = 1 * time.Second
	}
	return r.shared.SetString(key, string(data), ttl)
}

func (r *emailVerificationRedisRepository) DeleteExpired() error {

	return nil
}

func (r *emailVerificationRedisRepository) CountByEmailInWindow(email string, windowDuration time.Duration) (int, error) {
	indexKey := emailVerifIndex + email
	members, err := r.shared.SMembers(indexKey)
	if err != nil {
		return 0, nil
	}

	cutoff := time.Now().Add(-windowDuration)
	count := 0
	var stale []string

	for _, m := range members {
		parts := strings.SplitN(m, "|", 2)
		if len(parts) != 2 {
			stale = append(stale, m)
			continue
		}
		created, err := time.Parse(time.RFC3339Nano, parts[1])
		if err != nil {
			stale = append(stale, m)
			continue
		}
		if created.Before(cutoff) {
			stale = append(stale, m)
			continue
		}
		count++
	}

	for _, s := range stale {
		_ = r.shared.SRem(indexKey, s)
	}

	return count, nil
}
