package workflowsecret

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestNamesAreLowercaseLettersDigitsAndUnderscoreUpTo64(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
	}{
		{"unifecaf_senha", true},
		{"a", true},
		{"key_2", true},
		{"_x", true},
		{strings.Repeat("a", 64), true},
		{strings.Repeat("a", 65), false},
		{"", false},
		{"Upper", false},
		{"with-dash", false},
		{"with space", false},
		{"ponto.final", false},
		{"ação", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateName(c.name)
			if c.ok && err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
			if !c.ok && !errors.Is(err, ErrInvalidName) {
				t.Fatalf("expected ErrInvalidName, got %v", err)
			}
		})
	}
}

func TestValuesMustBePresentAndCapped(t *testing.T) {
	if err := ValidateValue(""); !errors.Is(err, ErrValueRequired) {
		t.Fatalf("empty: %v", err)
	}
	if err := ValidateValue(strings.Repeat("x", MaxValueBytes+1)); !errors.Is(err, ErrValueTooLong) {
		t.Fatalf("too long: %v", err)
	}
	if err := ValidateValue(strings.Repeat("x", MaxValueBytes)); err != nil {
		t.Fatalf("at the cap: %v", err)
	}
}

func TestMissingNamesListsEveryReferenceWithoutAValueOnce(t *testing.T) {
	got := MissingNames([]string{"b", "a", "b", "c"}, map[string]string{"c": "v"})
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("got %v", got)
	}
	if got := MissingNames([]string{"c"}, map[string]string{"c": "v"}); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestMissingErrorNamesTheSecretsAndMatchesErrMissing(t *testing.T) {
	err := error(&MissingError{Names: []string{"unifecaf_senha", "api_key"}})
	if !errors.Is(err, ErrMissing) {
		t.Fatal("must match ErrMissing")
	}
	for _, want := range []string{"unifecaf_senha", "api_key"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("message %q must name %q", err.Error(), want)
		}
	}
}

func TestUniqueNamesDropsBlanksAndRepeatsKeepingOrder(t *testing.T) {
	got := UniqueNames([]string{" b ", "a", "", "b"})
	if !reflect.DeepEqual(got, []string{"b", "a"}) {
		t.Fatalf("got %v", got)
	}
}
