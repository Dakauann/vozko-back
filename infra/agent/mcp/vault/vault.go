package vault

import cvault "vozko/infra/crypto/vault"

type Vault = cvault.Vault

var ErrCiphertextShort = cvault.ErrCiphertextShort

func New(key []byte, kekVersion int) (*Vault, error) {
	return cvault.New(key, kekVersion)
}
