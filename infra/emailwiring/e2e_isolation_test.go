package emailwiring_test

import (
	"testing"
	"time"

	"vozko/infra/testenv"
)

func TestEmailE2E_ConcurrentRunsNeverShareOrWipeEachOthersRedis(t *testing.T) {
	testenv.Postgres(t)
	testenv.Redis(t)
	mine := e2eRedis(t)
	if err := mine.SetString("email:inbound:sender:198.51.100.9", "299", time.Minute); err != nil {
		t.Fatal(err)
	}

	other := e2eRedis(t)

	if got, err := mine.GetString("email:inbound:sender:198.51.100.9"); err != nil || got != "299" {
		t.Fatalf("this run's flood count = %q (%v) after another run started", got, err)
	}
	if exists, err := other.Exists("email:inbound:sender:198.51.100.9"); err != nil || exists {
		t.Fatalf("another run sees this run's flood count (exists = %v, %v)", exists, err)
	}
}
