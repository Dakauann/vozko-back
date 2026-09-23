package attendance_target

import (
	"errors"
	"math"
	"testing"
	"time"

	"vozko/domain/attendance"
)

func saoPaulo(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	return loc
}

func validTarget() Target {
	return Target{
		WorkspaceID: "ws1",
		Scope:       ScopeWorkspace,
		MetricKey:   attendance.MetricFinished,
		PeriodStart: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		Value:       1786,
	}
}

func TestNormalizePeriodStartCollapsesToTheFirstOfTheMonth(t *testing.T) {
	loc := saoPaulo(t)
	got := NormalizePeriodStart(time.Date(2026, 9, 28, 15, 36, 0, 0, loc), loc)

	if got.Day() != 1 || got.Month() != time.September || got.Year() != 2026 {
		t.Fatalf("NormalizePeriodStart() = %v, want 2026-09-01", got)
	}
	if got.Hour() != 0 || got.Minute() != 0 {
		t.Fatalf("NormalizePeriodStart() = %v, want midnight", got)
	}
}

func TestNormalizePeriodStartIsStableAcrossClientTimezones(t *testing.T) {
	loc := saoPaulo(t)
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}

	instant := time.Date(2026, 9, 30, 23, 30, 0, 0, loc)
	fromSaoPaulo := NormalizePeriodStart(instant, loc)
	fromTokyo := NormalizePeriodStart(instant.In(tokyo), loc)

	if !fromSaoPaulo.Equal(fromTokyo) {
		t.Fatalf("NormalizePeriodStart() differs by client timezone: %v vs %v", fromSaoPaulo, fromTokyo)
	}
}

func TestValidateRejectsAnUnknownMetric(t *testing.T) {
	target := validTarget()
	target.MetricKey = "made_up"

	if err := target.Validate(); !errors.Is(err, ErrUnknownMetric) {
		t.Fatalf("Validate() = %v, want %v", err, ErrUnknownMetric)
	}
}

func TestValidateRejectsANegativeOrNonFiniteValue(t *testing.T) {
	negative := validTarget()
	negative.Value = -1
	if err := negative.Validate(); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("Validate() on a negative value = %v, want %v", err, ErrInvalidValue)
	}

	infinite := validTarget()
	infinite.Value = math.Inf(1)
	if err := infinite.Validate(); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("Validate() on an infinite value = %v, want %v", err, ErrInvalidValue)
	}

	notANumber := validTarget()
	notANumber.Value = math.NaN()
	if err := notANumber.Validate(); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("Validate() on NaN = %v, want %v", err, ErrInvalidValue)
	}
}

func TestValidateCurrencyRules(t *testing.T) {
	money := validTarget()
	money.MetricKey = attendance.MetricRevenueCents
	if err := money.Validate(); !errors.Is(err, ErrCurrencyRequired) {
		t.Fatalf("Validate() on a money target without a currency = %v, want %v", err, ErrCurrencyRequired)
	}
	money.Currency = "BRL"
	if err := money.Validate(); err != nil {
		t.Fatalf("Validate() on a money target with a currency = %v, want nil", err)
	}

	counted := validTarget()
	counted.Currency = "BRL"
	if err := counted.Validate(); !errors.Is(err, ErrCurrencyForbidden) {
		t.Fatalf("Validate() on a count target with a currency = %v, want %v", err, ErrCurrencyForbidden)
	}
}

func TestValidateScopeRules(t *testing.T) {
	department := validTarget()
	department.Scope = ScopeDepartment
	if err := department.Validate(); !errors.Is(err, ErrScopeIDRequired) {
		t.Fatalf("Validate() on a department target with no scope id = %v, want %v", err, ErrScopeIDRequired)
	}

	workspace := validTarget()
	workspace.ScopeID = "dept1"
	if err := workspace.Validate(); !errors.Is(err, ErrScopeIDForbidden) {
		t.Fatalf("Validate() on a workspace target with a scope id = %v, want %v", err, ErrScopeIDForbidden)
	}
}

func TestNormalizeClearsTheScopeIDForAWorkspaceTarget(t *testing.T) {
	target := validTarget()
	target.ScopeID = "  dept1  "
	target.MetricKey = "  FINISHED  "
	target.Normalize(time.UTC)

	if target.ScopeID != "" {
		t.Fatalf("Normalize() ScopeID = %q, want empty for a workspace target", target.ScopeID)
	}
	if target.MetricKey != attendance.MetricFinished {
		t.Fatalf("Normalize() MetricKey = %q, want %q", target.MetricKey, attendance.MetricFinished)
	}
}

func TestPeriodHasClosed(t *testing.T) {
	target := validTarget()

	inside := time.Date(2026, 9, 30, 23, 59, 0, 0, time.UTC)
	if target.PeriodHasClosed(inside) {
		t.Fatalf("PeriodHasClosed(%v) = true, want false", inside)
	}

	after := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if !target.PeriodHasClosed(after) {
		t.Fatalf("PeriodHasClosed(%v) = false, want true", after)
	}
}
