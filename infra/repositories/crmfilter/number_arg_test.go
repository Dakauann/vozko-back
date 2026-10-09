package crmfilter

import (
	"errors"
	"testing"

	"vozko/domain/crmfilter"
)

func TestNumberArgumentsRefuseTextPostgresNumericCannotRead(t *testing.T) {
	for _, raw := range []string{"NaN", "Infinity", "-inf", "0x1p4", "1,5", "dez"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := numberArg(raw); !errors.Is(err, crmfilter.ErrInvalidNumber) {
				t.Fatalf("numberArg(%q) = %v, want ErrInvalidNumber", raw, err)
			}
			if _, err := scalarArg(crmfilter.KindNumber, raw); !errors.Is(err, crmfilter.ErrInvalidNumber) {
				t.Fatalf("scalarArg(%q) = %v, want ErrInvalidNumber", raw, err)
			}
		})
	}
	got, err := numberArg(" 12.5 ")
	if err != nil || got != 12.5 {
		t.Fatalf("numberArg(12.5) = %v, %v", got, err)
	}
}
