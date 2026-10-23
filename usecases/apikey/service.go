package apikey_usecase

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/google/uuid"

	"vozko/domain/apikey"
)

var (
	ErrUnauthenticated   = errors.New("apikey: missing key")
	ErrInvalid           = errors.New("apikey: invalid key")
	ErrScopeMissing      = errors.New("apikey: the key lacks the scope of this route")
	ErrUnavailable       = errors.New("apikey: keys cannot be checked right now")
	ErrCacheUnavailable  = errors.New("apikey: the key was revoked but the cache could not be cleared; revoke again")
	ErrIncompleteService = errors.New("apikey: a required dependency is missing")
)

const (
	cachePrefix     = "apikey:"
	tombstonePrefix = "apikey:revoked:"
	touchPrefix     = "apikey:touch:"
	tombstoneTTL    = 2 * apikey.CacheTTL
)

type Cache interface {
	GetString(key string) (string, error)
	SetString(key, value string, ttl time.Duration) error
	SetNX(key, value string, ttl time.Duration) (bool, error)
	Del(keys ...string) error
}

type Deps struct {
	Keys   apikey.Repository
	Cache  Cache
	Now    func() time.Time
	Random io.Reader
	NewID  func() string
	Logf   func(format string, args ...any)
}

type Service struct {
	d Deps
}

func New(d Deps) (*Service, error) {
	if d.Keys == nil || d.Cache == nil {
		return nil, ErrIncompleteService
	}
	if d.Now == nil {
		d.Now = func() time.Time { return time.Now().UTC() }
	}
	if d.Random == nil {
		d.Random = rand.Reader
	}
	if d.NewID == nil {
		d.NewID = uuid.NewString
	}
	if d.Logf == nil {
		d.Logf = log.Printf
	}
	return &Service{d: d}, nil
}

type CreateInput struct {
	WorkspaceID string
	UserID      string
	Name        string
	Scopes      []string
}

type Created struct {
	Key   apikey.Key
	Plain string
}

func (s *Service) Create(ctx context.Context, in CreateInput) (Created, error) {
	name, err := apikey.ValidateName(in.Name)
	if err != nil {
		return Created{}, err
	}
	scopes, err := apikey.ValidateScopes(in.Scopes)
	if err != nil {
		return Created{}, err
	}
	secret, err := apikey.Generate(s.d.Random)
	if err != nil {
		return Created{}, err
	}
	k := apikey.Key{
		ID: s.d.NewID(), WorkspaceID: in.WorkspaceID, Name: name, LookupID: secret.LookupID, SecretHash: secret.Hash,
		Scopes: scopes, CreatedBy: in.UserID, CreatedAt: s.d.Now(),
	}
	if err := s.d.Keys.Create(ctx, &k); err != nil {
		return Created{}, err
	}
	return Created{Key: k, Plain: secret.Plain}, nil
}

func (s *Service) List(ctx context.Context, workspaceID string) ([]apikey.Key, error) {
	return s.d.Keys.ListByWorkspace(ctx, workspaceID)
}

func (s *Service) Revoke(ctx context.Context, workspaceID, id, userID string) (apikey.Key, error) {
	k, err := s.d.Keys.Revoke(ctx, workspaceID, id, userID, s.d.Now())
	if err != nil {
		return apikey.Key{}, err
	}
	if err := s.d.Cache.SetString(tombstonePrefix+k.LookupID, "1", tombstoneTTL); err != nil {
		return apikey.Key{}, fmt.Errorf("%w: %v", ErrCacheUnavailable, err)
	}
	if err := s.d.Cache.Del(cachePrefix + k.LookupID); err != nil {
		return apikey.Key{}, fmt.Errorf("%w: %v", ErrCacheUnavailable, err)
	}
	return *k, nil
}

type cached struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	Hash        string         `json:"hash"`
	Scopes      []apikey.Scope `json:"scopes"`
}

func (s *Service) Authenticate(ctx context.Context, token string, scope apikey.Scope) (apikey.Principal, error) {
	if token == "" {
		return apikey.Principal{}, ErrUnauthenticated
	}
	lookup, ok := apikey.Parse(token)
	if !ok {
		return apikey.Principal{}, ErrInvalid
	}
	record, err := s.record(ctx, lookup)
	if err != nil {
		return apikey.Principal{}, err
	}
	if !apikey.Matches(record.Hash, token) {
		return apikey.Principal{}, ErrInvalid
	}
	principal := apikey.Principal{WorkspaceID: record.WorkspaceID, KeyID: record.ID, Scopes: record.Scopes}
	if !principal.Allows(scope) {
		return apikey.Principal{}, ErrScopeMissing
	}
	s.touch(ctx, record.ID)
	return principal, nil
}

func (s *Service) record(ctx context.Context, lookup string) (cached, error) {
	raw, err := s.d.Cache.GetString(cachePrefix + lookup)
	if err != nil {
		return cached{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if raw != "" {
		var c cached
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			return cached{}, fmt.Errorf("%w: unreadable cache entry: %v", ErrUnavailable, err)
		}
		return c, nil
	}
	k, err := s.d.Keys.GetByLookupID(ctx, lookup)
	if errors.Is(err, apikey.ErrNotFound) {
		return cached{}, ErrInvalid
	}
	if err != nil {
		return cached{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if err := k.Usable(); err != nil {
		return cached{}, err
	}
	c := cached{ID: k.ID, WorkspaceID: k.WorkspaceID, Hash: k.SecretHash, Scopes: k.Scopes}
	encoded, err := json.Marshal(c)
	if err != nil {
		return cached{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if err := s.d.Cache.SetString(cachePrefix+lookup, string(encoded), apikey.CacheTTL); err != nil {
		return cached{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	revoked, err := s.d.Cache.GetString(tombstonePrefix + lookup)
	if err != nil {
		return cached{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if revoked != "" {
		if err := s.d.Cache.Del(cachePrefix + lookup); err != nil {
			return cached{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		return cached{}, apikey.ErrRevoked
	}
	return c, nil
}

func (s *Service) touch(ctx context.Context, id string) {
	due, err := s.d.Cache.SetNX(touchPrefix+id, "1", apikey.TouchInterval)
	if err != nil || !due {
		return
	}
	if err := s.d.Keys.Touch(context.WithoutCancel(ctx), id, s.d.Now()); err != nil {
		s.d.Logf("[apikey] last use of key %s was not recorded: %v", id, err)
	}
}
