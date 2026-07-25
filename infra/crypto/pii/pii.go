package pii

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"

	"vozko/infra/crypto/vault"
)

const (
	EnvelopeFormatV1 byte = 0x01

	headerLen = 2

	MinBlindIndexKeyLen = 32
)

var (
	ErrUnknownKEKVersion = errors.New("pii: ciphertext references unknown KEK version")
	ErrUnknownFormat     = errors.New("pii: unsupported envelope format")
	ErrEnvelopeShort     = errors.New("pii: envelope too short")
	ErrNoActiveKEK       = errors.New("pii: no active KEK configured")
	ErrBlindIndexKey     = errors.New("pii: blind index key missing or too short")
	ErrNilVault          = errors.New("pii: nil vault in keyring")
)

type Service struct {
	vaults   map[byte]*vault.Vault
	active   byte
	blindKey []byte
}

func New(vaults map[byte]*vault.Vault, activeVersion byte, blindIndexKey []byte) (*Service, error) {
	if len(vaults) == 0 {
		return nil, ErrNoActiveKEK
	}
	for ver, v := range vaults {
		if v == nil {
			return nil, ErrNilVault
		}
		_ = ver
	}
	if _, ok := vaults[activeVersion]; !ok {
		return nil, ErrNoActiveKEK
	}
	if len(blindIndexKey) < MinBlindIndexKeyLen {
		return nil, ErrBlindIndexKey
	}
	keyCopy := make([]byte, len(blindIndexKey))
	copy(keyCopy, blindIndexKey)
	return &Service{vaults: vaults, active: activeVersion, blindKey: keyCopy}, nil
}

func (s *Service) ActiveKEKVersion() byte { return s.active }

func (s *Service) Encrypt(plaintext []byte) ([]byte, error) {
	v := s.vaults[s.active]
	inner, err := v.Seal(plaintext)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, headerLen+len(inner))
	out = append(out, EnvelopeFormatV1, s.active)
	out = append(out, inner...)
	return out, nil
}

func (s *Service) Decrypt(envelope []byte) ([]byte, error) {
	if len(envelope) < headerLen {
		return nil, ErrEnvelopeShort
	}
	if envelope[0] != EnvelopeFormatV1 {
		return nil, ErrUnknownFormat
	}
	v, ok := s.vaults[envelope[1]]
	if !ok {
		return nil, ErrUnknownKEKVersion
	}
	return v.Open(envelope[headerLen:])
}

func (s *Service) BlindIndex(scope, value string) []byte {
	mac := hmac.New(sha256.New, s.blindKey)
	mac.Write([]byte(scope))
	mac.Write([]byte{0x00})
	mac.Write([]byte(value))
	return mac.Sum(nil)
}
