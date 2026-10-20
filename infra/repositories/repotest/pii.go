package repotest

import (
	"bytes"
	"testing"

	"vozko/infra/crypto/pii"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/crypto/vault"
)

func WithPII(t testing.TB) {
	t.Helper()
	v, err := vault.New(bytes.Repeat([]byte{0x11}, 32), 1)
	if err != nil {
		t.Fatal(err)
	}
	s, err := pii.New(map[byte]*vault.Vault{1: v}, 1, bytes.Repeat([]byte{0x22}, 32))
	if err != nil {
		t.Fatal(err)
	}
	previous := piigorm.Service()
	piigorm.SetService(s)
	t.Cleanup(func() { piigorm.SetService(previous) })
}
