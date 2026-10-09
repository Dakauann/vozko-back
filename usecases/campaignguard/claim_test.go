package campaignguard

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestNewSpamGuardRefusesNoClaimStore(t *testing.T) {
	if _, err := NewSpamGuard(&fixedDays{}, &sendLog{}, nil, clock); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("no claim store: err = %v, want ErrUnavailable", err)
	}
}

func TestClaimLetsExactlyOneConcurrentSendThrough(t *testing.T) {
	claims := newClaimBook()
	guard, err := NewSpamGuard(&fixedDays{days: 3}, &sendLog{}, claims, clock)
	if err != nil {
		t.Fatal(err)
	}
	var won, refused atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := guard.Claim(context.Background(), "ws-1", "l-1", "bp-1")
			switch {
			case err == nil:
				won.Add(1)
			case errors.Is(err, ErrSendClaimed):
				refused.Add(1)
			}
		}()
	}
	wg.Wait()
	if won.Load() != 1 || refused.Load() != 39 {
		t.Fatalf("won = %d refused = %d, want exactly one send for the lead and number", won.Load(), refused.Load())
	}
	for key, ttl := range claims.held {
		if ttl != ClaimTTL {
			t.Fatalf("claim %s lives %v, want %v", key, ttl, ClaimTTL)
		}
	}
	if _, err := guard.Claim(context.Background(), "ws-1", "l-2", "bp-1"); err != nil {
		t.Fatalf("another lead on the same number must not wait: %v", err)
	}
	if _, err := guard.Claim(context.Background(), "ws-1", "l-1", "bp-2"); err != nil {
		t.Fatalf("the same lead on another number must not wait: %v", err)
	}
}

func TestReleasingAClaimLetsTheNextSendTry(t *testing.T) {
	claims := newClaimBook()
	guard, _ := NewSpamGuard(&fixedDays{days: 3}, &sendLog{}, claims, clock)
	release, err := guard.Claim(context.Background(), "ws-1", "l-1", "bp-1")
	if err != nil {
		t.Fatal(err)
	}
	release()
	if claims.count() != 0 {
		t.Fatalf("a released claim must be gone, %d left", claims.count())
	}
	if _, err := guard.Claim(context.Background(), "ws-1", "l-1", "bp-1"); err != nil {
		t.Fatalf("after a release the next send may claim: %v", err)
	}
}

func TestNoClaimIsNeededWithoutACooldown(t *testing.T) {
	cases := map[string]struct {
		days   int
		lead   string
		sender string
	}{
		"the workspace turned the window off": {days: 0, lead: "l-1", sender: "bp-1"},
		"no sender":                           {days: 3, lead: "l-1", sender: " "},
		"no lead":                             {days: 3, lead: "", sender: "bp-1"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			claims := newClaimBook()
			guard, _ := NewSpamGuard(&fixedDays{days: tc.days}, &sendLog{}, claims, clock)
			for i := 0; i < 2; i++ {
				release, err := guard.Claim(context.Background(), "ws-1", tc.lead, tc.sender)
				if err != nil || release == nil {
					t.Fatalf("Claim() = (%v, %v), want a free pass", release == nil, err)
				}
				release()
			}
			if claims.count() != 0 {
				t.Fatalf("%d claims taken, want none", claims.count())
			}
		})
	}
}

func TestClaimFailsClosed(t *testing.T) {
	broken := newClaimBook()
	broken.setErr = errBoom
	guard, _ := NewSpamGuard(&fixedDays{days: 3}, &sendLog{}, broken, clock)
	if release, err := guard.Claim(context.Background(), "ws-1", "l-1", "bp-1"); !errors.Is(err, errBoom) || release != nil {
		t.Fatalf("an unreachable claim store: (%v, %v), want the error and no pass", release != nil, err)
	}
	unreadable, _ := NewSpamGuard(&fixedDays{err: errBoom}, &sendLog{}, newClaimBook(), clock)
	if release, err := unreadable.Claim(context.Background(), "ws-1", "l-1", "bp-1"); err == nil || release != nil {
		t.Fatal("an unreadable policy must refuse the claim")
	}
}

func TestEligibilityClaimsThroughItsSpamGuard(t *testing.T) {
	f := newGuardFixture(t)
	if _, err := f.eligible.Claim(context.Background(), "ws-1", "l-1", "bp-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.eligible.Claim(context.Background(), "ws-1", "l-1", "bp-1"); !errors.Is(err, ErrSendClaimed) {
		t.Fatalf("a second claim: err = %v, want ErrSendClaimed", err)
	}
}
