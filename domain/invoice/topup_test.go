package invoice

import (
	"errors"
	"strings"
	"testing"
)

func TestTopUpBoundsAreOneRule(t *testing.T) {
	cases := []struct {
		name   string
		amount float64
		want   error
	}{
		{"below the minimum", 4.99, ErrTopUpBelowMinimum},
		{"zero", 0, ErrTopUpBelowMinimum},
		{"negative", -10, ErrTopUpBelowMinimum},
		{"the minimum", MinTopUpBRL, nil},
		{"in between", 250, nil},
		{"the maximum", MaxTopUpBRL, nil},
		{"above the maximum", 50000.01, ErrTopUpAboveMaximum},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateTopUpAmount(tc.amount); !errors.Is(err, tc.want) {
				t.Fatalf("ValidateTopUpAmount(%v) = %v, want %v", tc.amount, err, tc.want)
			}
		})
	}
}

func TestClientIdempotencyKeysAreScopedToTheirWorkspace(t *testing.T) {
	a, err := ClientIdempotencyKey("ws-a", "5f0c6a3e-1d2b-4c5d-9e8f-0a1b2c3d4e5f")
	if err != nil {
		t.Fatalf("a uuid key must be accepted: %v", err)
	}
	b, err := ClientIdempotencyKey("ws-b", "5f0c6a3e-1d2b-4c5d-9e8f-0a1b2c3d4e5f")
	if err != nil {
		t.Fatalf("a uuid key must be accepted: %v", err)
	}
	if a == b {
		t.Fatalf("the same client key in two workspaces must never point at the same invoice: %q", a)
	}
	if !strings.Contains(a, "ws-a") {
		t.Fatalf("the stored key must carry its workspace, got %q", a)
	}
}

func TestAnEmptyClientKeyMeansNoIdempotency(t *testing.T) {
	key, err := ClientIdempotencyKey("ws-a", "   ")
	if err != nil || key != "" {
		t.Fatalf("an empty client key must yield no key and no error, got %q, %v", key, err)
	}
}

func TestMalformedClientKeysAreRefused(t *testing.T) {
	for _, raw := range []string{
		strings.Repeat("a", 65),
		"monthly:ws-x:2026-10-18",
		"key with spaces",
		"key/../other",
	} {
		if _, err := ClientIdempotencyKey("ws-a", raw); !errors.Is(err, ErrInvalidIdempotencyKey) {
			t.Errorf("ClientIdempotencyKey(%q) = %v, want ErrInvalidIdempotencyKey", raw, err)
		}
	}
}

func TestAClientKeyNeedsAWorkspace(t *testing.T) {
	if _, err := ClientIdempotencyKey("  ", "abc"); !errors.Is(err, ErrInvalidIdempotencyKey) {
		t.Fatalf("a key without a workspace must be refused, got %v", err)
	}
}
