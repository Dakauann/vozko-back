package piigorm

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"

	"vozko/infra/crypto/pii"
)

var ErrServiceNotConfigured = errors.New("piigorm: pii.Service not configured (call SetService at startup)")

var (
	mu      sync.RWMutex
	service *pii.Service
)

func SetService(s *pii.Service) {
	mu.Lock()
	service = s
	mu.Unlock()
}

func Service() *pii.Service {
	mu.RLock()
	defer mu.RUnlock()
	return service
}

func mustService() (*pii.Service, error) {
	if s := Service(); s != nil {
		return s, nil
	}
	return nil, ErrServiceNotConfigured
}

type EncryptedString struct {
	Plain string
	Valid bool
}

func NewEncrypted(s string) EncryptedString {
	return EncryptedString{Plain: s, Valid: true}
}

func Null() EncryptedString { return EncryptedString{} }

func (e *EncryptedString) Scan(src any) error {
	if src == nil {
		*e = EncryptedString{}
		return nil
	}
	var raw []byte
	switch v := src.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return fmt.Errorf("piigorm: unsupported scan type %T", src)
	}
	if len(raw) == 0 {
		*e = EncryptedString{}
		return nil
	}
	svc, err := mustService()
	if err != nil {
		return err
	}
	plain, err := svc.Decrypt(raw)
	if err != nil {
		return fmt.Errorf("piigorm: decrypt: %w", err)
	}
	e.Plain = string(plain)
	e.Valid = true
	return nil
}

func (e EncryptedString) Value() (driver.Value, error) {
	if !e.Valid {
		return nil, nil
	}
	svc, err := mustService()
	if err != nil {
		return nil, err
	}
	return svc.Encrypt([]byte(e.Plain))
}

func (EncryptedString) GormDataType() string { return "bytea" }

func (e EncryptedString) String() string {
	if !e.Valid {
		return ""
	}
	return e.Plain
}

type BlindIndex []byte

func NewBlindIndex(scope, value string) (BlindIndex, error) {
	if value == "" {
		return nil, nil
	}
	svc, err := mustService()
	if err != nil {
		return nil, err
	}
	return BlindIndex(svc.BlindIndex(scope, value)), nil
}

func (b *BlindIndex) Scan(src any) error {
	if src == nil {
		*b = nil
		return nil
	}
	switch v := src.(type) {
	case []byte:
		buf := make([]byte, len(v))
		copy(buf, v)
		*b = buf
		return nil
	case string:
		*b = []byte(v)
		return nil
	default:
		return fmt.Errorf("piigorm: unsupported scan type %T for BlindIndex", src)
	}
}

func (b BlindIndex) Value() (driver.Value, error) {
	if len(b) == 0 {
		return nil, nil
	}
	return []byte(b), nil
}

func (BlindIndex) GormDataType() string { return "bytea" }
