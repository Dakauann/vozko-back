package webchat

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func intakeWidget(name, email, phone FieldRule, policy string) *Widget {
	w := validWidget()
	w.IntakeName, w.IntakeEmail, w.IntakePhone, w.PrivacyPolicyURL = name, email, phone, policy
	w.Normalize()
	return w
}

func TestResolveIntakeKeepsOnlyTheFieldsTheWidgetAsks(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	w := intakeWidget(FieldRequired, FieldHidden, FieldOptional, "")
	got, err := w.ResolveIntake(IntakeAnswers{Name: " Ana ", Email: "ana@example.com", Phone: "(11) 99999-0000"}, Intake{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Ana" || got.Email != "" || got.Phone != "5511999990000" || got.ConsentedAt != nil {
		t.Fatalf("intake = %+v", got)
	}
}

func TestResolveIntakeRefusesMissingOrMalformedAnswers(t *testing.T) {
	now := time.Now()
	cases := map[string]struct {
		w    *Widget
		in   IntakeAnswers
		want error
	}{
		"required name missing":   {intakeWidget(FieldRequired, FieldHidden, FieldHidden, ""), IntakeAnswers{}, ErrIntakeFieldRequired},
		"required email missing":  {intakeWidget(FieldHidden, FieldRequired, FieldHidden, ""), IntakeAnswers{}, ErrIntakeFieldRequired},
		"required phone missing":  {intakeWidget(FieldHidden, FieldHidden, FieldRequired, ""), IntakeAnswers{}, ErrIntakeFieldRequired},
		"bad email":               {intakeWidget(FieldHidden, FieldOptional, FieldHidden, ""), IntakeAnswers{Email: "ana at example"}, ErrIntakeEmailInvalid},
		"email with display name": {intakeWidget(FieldHidden, FieldOptional, FieldHidden, ""), IntakeAnswers{Email: "Ana <ana@example.com>"}, ErrIntakeEmailInvalid},
		"short phone":             {intakeWidget(FieldHidden, FieldHidden, FieldOptional, ""), IntakeAnswers{Phone: "1234"}, ErrIntakePhoneInvalid},
		"letters in phone":        {intakeWidget(FieldHidden, FieldHidden, FieldOptional, ""), IntakeAnswers{Phone: "11 9999 abcd"}, ErrIntakePhoneInvalid},
		"long name":               {intakeWidget(FieldOptional, FieldHidden, FieldHidden, ""), IntakeAnswers{Name: strings.Repeat("a", MaxVisitorNameRunes+1)}, ErrIntakeNameInvalid},
		"control chars in name":   {intakeWidget(FieldOptional, FieldHidden, FieldHidden, ""), IntakeAnswers{Name: "Ana\x00"}, ErrIntakeNameInvalid},
		"consent missing":         {intakeWidget(FieldHidden, FieldHidden, FieldHidden, "https://loja.example.com/privacidade"), IntakeAnswers{}, ErrIntakeConsentRequired},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := tc.w.ResolveIntake(tc.in, Intake{}, now); !errors.Is(err, tc.want) {
				t.Fatalf("ResolveIntake = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestVerifiedIdentityWinsOverTypedAnswers(t *testing.T) {
	w := intakeWidget(FieldRequired, FieldRequired, FieldHidden, "")
	got, err := w.ResolveIntake(IntakeAnswers{Name: "Mallory", Email: "mallory@example.com"}, Intake{Name: "Ana", Email: "ana@example.com"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Ana" || got.Email != "ana@example.com" {
		t.Fatalf("verified values were overwritten: %+v", got)
	}
}

func TestConsentIsStampedWhenAPolicyIsConfigured(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	w := intakeWidget(FieldHidden, FieldHidden, FieldHidden, "https://loja.example.com/privacidade")
	got, err := w.ResolveIntake(IntakeAnswers{Consent: true}, Intake{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.ConsentedAt == nil || !got.ConsentedAt.Equal(now) {
		t.Fatalf("consent = %v", got.ConsentedAt)
	}
}

func TestNormalizePhone(t *testing.T) {
	cases := map[string]string{
		"+55 (11) 99999-0000": "5511999990000",
		"11 99999-0000":       "5511999990000",
		"5511999990000":       "5511999990000",
		"+1 415 555 0100":     "14155550100",
		"+351 912 345 678":    "351912345678",
		"999":                 "",
		"11.9999.0000x":       "",
		"++5511999990000":     "",
		"":                    "",
	}
	for in, want := range cases {
		if got := NormalizePhone(in, "55"); got != want {
			t.Errorf("NormalizePhone(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIntakeBlocksMessagesOnlyWhenSomethingIsRequired(t *testing.T) {
	optional := intakeWidget(FieldOptional, FieldOptional, FieldHidden, "")
	required := intakeWidget(FieldRequired, FieldHidden, FieldHidden, "")
	consent := intakeWidget(FieldHidden, FieldHidden, FieldHidden, "https://loja.example.com/p")
	fresh := &Visitor{}
	done := &Visitor{IntakeCompletedAt: ptrTime(time.Now())}

	if fresh.MustCompleteIntake(optional) {
		t.Fatal("optional-only intake must not block messages")
	}
	if !fresh.MustCompleteIntake(required) || !fresh.MustCompleteIntake(consent) {
		t.Fatal("required fields or consent must block messages")
	}
	if done.MustCompleteIntake(required) {
		t.Fatal("a completed intake must not block")
	}
	if !fresh.IntakePending(optional) || done.IntakePending(optional) {
		t.Fatal("pending must follow completion")
	}
}

func TestVisitorDisplayNameNeverLeaksAnEmptyLabel(t *testing.T) {
	cases := map[string]struct {
		v    Visitor
		want string
	}{
		"name":  {Visitor{ID: "abcdef123456", Name: "Ana", Email: "ana@example.com"}, "Ana"},
		"email": {Visitor{ID: "abcdef123456", Email: "ana@example.com"}, "ana@example.com"},
		"phone": {Visitor{ID: "abcdef123456", Phone: "5511999990000"}, "+5511999990000"},
		"none":  {Visitor{ID: "abcdef123456"}, "Visitante #abcdef"},
	}
	for name, tc := range cases {
		if got := tc.v.DisplayName(); got != tc.want {
			t.Errorf("%s: DisplayName = %q, want %q", name, got, tc.want)
		}
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
