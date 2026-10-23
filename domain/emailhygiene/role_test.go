package emailhygiene

import (
	"testing"

	"vozko/domain/emailaddress"
)

func TestIsRoleWarnsOnSharedMailboxes(t *testing.T) {
	cases := []struct {
		address string
		want    bool
	}{
		{address: "contato@loja.com.br", want: true},
		{address: "vendas@loja.com.br", want: true},
		{address: "financeiro@loja.com.br", want: true},
		{address: "atendimento@loja.com.br", want: true},
		{address: "sac@loja.com.br", want: true},
		{address: "no-reply@loja.com.br", want: true},
		{address: "postmaster@loja.com.br", want: true},
		{address: "contato+site@loja.com.br", want: true},
		{address: "ana@loja.com.br", want: false},
		{address: "contatoana@loja.com.br", want: false},
		{address: "ana.vendas@loja.com.br", want: false},
	}
	for _, tc := range cases {
		if got := IsRole(emailaddress.Address(tc.address)); got != tc.want {
			t.Fatalf("IsRole(%q) = %v, want %v", tc.address, got, tc.want)
		}
	}
}
