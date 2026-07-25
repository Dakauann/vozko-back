package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
)

var ErrCiphertextShort = errors.New("vault: ciphertext too short")

type Vault struct {
	aead       cipher.AEAD
	kekVersion int
	rand       io.Reader
}

func New(key []byte, kekVersion int) (*Vault, error) {
	return NewWithRand(key, kekVersion, rand.Reader)
}

func NewWithRand(key []byte, kekVersion int, r io.Reader) (*Vault, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, _ := cipher.NewGCM(block)
	if r == nil {
		r = rand.Reader
	}
	return &Vault{aead: gcm, kekVersion: kekVersion, rand: r}, nil
}

func (v *Vault) Version() int { return v.kekVersion }

func (v *Vault) NonceSize() int { return v.aead.NonceSize() }

func (v *Vault) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := io.ReadFull(v.rand, nonce); err != nil {
		return nil, err
	}
	ct := v.aead.Seal(nil, nonce, plaintext, nil)
	return append(nonce, ct...), nil
}

func (v *Vault) Open(envelope []byte) ([]byte, error) {
	ns := v.aead.NonceSize()
	if len(envelope) < ns+1 {
		return nil, ErrCiphertextShort
	}
	nonce, ct := envelope[:ns], envelope[ns:]
	return v.aead.Open(nil, nonce, ct, nil)
}
