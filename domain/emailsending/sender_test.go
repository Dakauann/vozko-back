package emailsending

import (
	"errors"
	"testing"
)

func TestNewSender(t *testing.T) {
	verified := SendingDomain{ID: "d1", WorkspaceID: "w1", Domain: "lojadojoao.com.br", Kind: DomainOwn, Status: DomainVerified}
	cases := []struct {
		name      string
		domain    SendingDomain
		localPart string
		display   string
		forward   string
		wantErr   error
		wantFrom  string
	}{
		{name: "valid own domain sender", domain: verified, localPart: "Cobranca", display: "Loja do João", wantFrom: "cobranca@lojadojoao.com.br"},
		{name: "forward to a free mailbox is allowed", domain: verified, localPart: "contato", display: "Loja", forward: "joao@gmail.com", wantFrom: "contato@lojadojoao.com.br"},
		{name: "invalid forward is refused", domain: verified, localPart: "contato", display: "Loja", forward: "joao@", wantErr: ErrInvalidForward},
		{name: "empty local part is refused", domain: verified, localPart: " ", display: "Loja", wantErr: ErrInvalidLocalPart},
		{name: "local part with at is refused", domain: verified, localPart: "a@b", display: "Loja", wantErr: ErrInvalidLocalPart},
		{name: "reserved bounce local part is refused", domain: verified, localPart: "bounce", display: "Loja", wantErr: ErrInvalidLocalPart},
		{name: "reserved reply local part is refused", domain: verified, localPart: "reply", display: "Loja", wantErr: ErrInvalidLocalPart},
		{name: "display name with newline is refused", domain: verified, localPart: "contato", display: "Loja\r\nBcc: x", wantErr: ErrInvalidDisplayName},
		{name: "empty display name is refused", domain: verified, localPart: "contato", display: "  ", wantErr: ErrInvalidDisplayName},
		{name: "display name over 80 is refused", domain: verified, localPart: "contato", display: string(make([]rune, 81)), wantErr: ErrInvalidDisplayName},
		{name: "unverified domain is refused", domain: SendingDomain{ID: "d2", Domain: "x.com.br", Kind: DomainOwn, Status: DomainVerifying}, localPart: "contato", display: "Loja", wantErr: ErrDomainNotVerified},
		{name: "retired domain is refused", domain: SendingDomain{ID: "d3", Domain: "x.com.br", Kind: DomainOwn, Status: DomainVerified, Retired: true}, localPart: "contato", display: "Loja", wantErr: ErrDomainNotVerified},
		{name: "free mailbox domain is refused even if marked verified", domain: SendingDomain{ID: "d4", Domain: "gmail.com", Kind: DomainOwn, Status: DomainVerified}, localPart: "joao", display: "Joao", wantErr: ErrFreeMailboxFrom},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewSender(tc.domain, tc.localPart, tc.display, tc.forward)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("NewSender error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewSender unexpected error %v", err)
			}
			if got := s.FromAddress(tc.domain); string(got) != tc.wantFrom {
				t.Fatalf("FromAddress = %q, want %q", got, tc.wantFrom)
			}
		})
	}
}

func TestSenderCanSendFrom(t *testing.T) {
	domain := SendingDomain{ID: "d1", Domain: "loja.com.br", Kind: DomainOwn, Status: DomainVerified}
	s := Sender{ID: "s1", DomainID: "d1", LocalPart: "contato", DisplayName: "Loja"}
	if err := s.CanSendFrom(domain); err != nil {
		t.Fatalf("CanSendFrom = %v", err)
	}
	other := domain
	other.ID = "d9"
	if err := s.CanSendFrom(other); !errors.Is(err, ErrSenderDomainMismatch) {
		t.Fatalf("mismatched domain = %v", err)
	}
	failed := domain
	failed.Status = DomainFailed
	if err := s.CanSendFrom(failed); !errors.Is(err, ErrDomainNotVerified) {
		t.Fatalf("failed domain = %v", err)
	}
}
