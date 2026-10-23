package emailsending

import (
	"errors"
	"slices"
	"testing"
)

func TestTenantNameFor(t *testing.T) {
	got, err := TenantNameFor("0b6f2c1e-1111-4a2b-9c3d-2e4f5a6b7c8d")
	if err != nil || got != "ws-0b6f2c1e-1111-4a2b-9c3d-2e4f5a6b7c8d" {
		t.Fatalf("TenantNameFor = %q, %v", got, err)
	}
	for _, bad := range []string{"", "  ", "ws id", "a/b", string(make([]byte, 70))} {
		if _, err := TenantNameFor(bad); err == nil {
			t.Fatalf("TenantNameFor(%q) should fail", bad)
		}
	}
}

func TestSenderAccountCanSend(t *testing.T) {
	cases := []struct {
		name    string
		account SenderAccount
		wantErr error
	}{
		{name: "active account sends", account: SenderAccount{Status: AccountActive, Mode: ModeProbation, TenantName: "ws-1"}},
		{name: "paused by provider refuses", account: SenderAccount{Status: AccountPausedByProvider, Mode: ModeTrusted, TenantName: "ws-1"}, wantErr: ErrAccountPaused},
		{name: "paused by platform refuses", account: SenderAccount{Status: AccountPausedByPlatform, Mode: ModeTrusted, TenantName: "ws-1"}, wantErr: ErrAccountPaused},
		{name: "dormant account needs reactivation, not a pause", account: SenderAccount{Status: AccountDormant, Mode: ModeTrusted, TenantName: "ws-1"}, wantErr: ErrAccountDormant},
		{name: "unknown status refuses", account: SenderAccount{Status: "weird", Mode: ModeTrusted, TenantName: "ws-1"}, wantErr: ErrAccountPaused},
		{name: "unknown mode refuses", account: SenderAccount{Status: AccountActive, Mode: "vip", TenantName: "ws-1"}, wantErr: ErrAccountNotProvisioned},
		{name: "missing tenant refuses", account: SenderAccount{Status: AccountActive, Mode: ModeProbation}, wantErr: ErrAccountNotProvisioned},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.account.CanSend()
			if !errors.Is(err, tc.wantErr) && !(err == nil && tc.wantErr == nil) {
				t.Fatalf("CanSend = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestSenderAccountCanSendOnDemand(t *testing.T) {
	cases := []struct {
		name    string
		account SenderAccount
		wantErr error
	}{
		{name: "active account sends", account: SenderAccount{Status: AccountActive, Mode: ModeProbation, TenantName: "ws-1"}},
		{name: "dormant account sends after reactivation", account: SenderAccount{Status: AccountDormant, Mode: ModeProbation, TenantName: "ws-1"}},
		{name: "paused account still refuses", account: SenderAccount{Status: AccountPausedByProvider, Mode: ModeTrusted, TenantName: "ws-1"}, wantErr: ErrAccountPaused},
		{name: "unprovisioned dormant account refuses", account: SenderAccount{Status: AccountDormant, Mode: ModeProbation}, wantErr: ErrAccountNotProvisioned},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.account.CanSendOnDemand()
			if !errors.Is(err, tc.wantErr) && !(err == nil && tc.wantErr == nil) {
				t.Fatalf("CanSendOnDemand = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestEverySendableStatusSendsOnDemand(t *testing.T) {
	for _, status := range SendableStatuses() {
		if !status.Valid() {
			t.Fatalf("%q is not a valid status", status)
		}
		if err := (SenderAccount{Status: status, Mode: ModeProbation, TenantName: "ws-1"}).CanSendOnDemand(); err != nil {
			t.Fatalf("%q refuses on demand: %v", status, err)
		}
	}
	for _, status := range []AccountStatus{AccountPausedByProvider, AccountPausedByPlatform} {
		if slices.Contains(SendableStatuses(), status) {
			t.Fatalf("%q must not be sendable", status)
		}
	}
}
