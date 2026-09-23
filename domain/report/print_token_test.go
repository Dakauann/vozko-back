package report

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const printSecret = "a-secret-that-is-long-enough"

func grantFor(expiresIn time.Duration, now time.Time) PrintGrant {
	return PrintGrant{JobID: "job-1", WorkspaceID: "ws-1", ExpiresAt: now.Add(expiresIn)}
}

func TestPrintTokenRoundTrips(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()

	token, err := NewPrintToken(printSecret, grantFor(PrintTokenTTL, now))
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	grant, err := VerifyPrintToken(printSecret, token, now)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if grant.JobID != "job-1" || grant.WorkspaceID != "ws-1" {
		t.Fatalf("grant = %+v", grant)
	}
}

func TestPrintTokenExpires(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	token, err := NewPrintToken(printSecret, grantFor(time.Minute, now))
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	if _, err := VerifyPrintToken(printSecret, token, now.Add(2*time.Minute)); !errors.Is(err, ErrPrintTokenExpired) {
		t.Fatalf("err = %v, want ErrPrintTokenExpired", err)
	}
}

func TestPrintTokenRejectsAnotherSecret(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	token, err := NewPrintToken(printSecret, grantFor(PrintTokenTTL, now))
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	if _, err := VerifyPrintToken("a-different-secret-entirely", token, now); !errors.Is(err, ErrPrintTokenInvalid) {
		t.Fatalf("err = %v, want ErrPrintTokenInvalid", err)
	}
}

func TestPrintTokenRejectsATamperedPayload(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	token, err := NewPrintToken(printSecret, grantFor(PrintTokenTTL, now))
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	_, signature, _ := strings.Cut(token, ".")
	forged, err := NewPrintToken(printSecret, PrintGrant{
		JobID: "job-2", WorkspaceID: "ws-2", ExpiresAt: now.Add(PrintTokenTTL),
	})
	if err != nil {
		t.Fatalf("mint forged: %v", err)
	}
	forgedPayload, _, _ := strings.Cut(forged, ".")

	if _, err := VerifyPrintToken(printSecret, forgedPayload+"."+signature, now); !errors.Is(err, ErrPrintTokenInvalid) {
		t.Fatalf("err = %v: another job's payload must not ride an old signature", err)
	}
}

func TestPrintTokenRefusesAnEmptySecret(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()

	if _, err := NewPrintToken("", grantFor(PrintTokenTTL, now)); !errors.Is(err, ErrPrintSecretUnset) {
		t.Fatalf("mint err = %v, want ErrPrintSecretUnset", err)
	}
	if _, err := VerifyPrintToken("", "anything.at.all", now); !errors.Is(err, ErrPrintSecretUnset) {
		t.Fatalf("verify err = %v, want ErrPrintSecretUnset", err)
	}
}
